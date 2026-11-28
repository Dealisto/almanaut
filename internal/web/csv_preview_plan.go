package web

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

type csvImportMode string

const (
	csvPatch           csvImportMode = "patch"
	csvReplace         csvImportMode = "replace"
	maxCSVPreviewBytes               = 8 << 20
	maxCSVPreviewRows                = 500
)

var (
	errCSVPreviewRollback = errors.New("CSV preview rollback")
	errCSVPreviewStale    = errors.New("the inventory changed after this preview; upload the file again to review current changes")
)

type csvInputRecord struct {
	Line   int
	ID     int64
	Fields map[string]string
}

type csvPreviewChange struct {
	Row        int
	EntityType string
	EntityID   int64
	Label      string
	Action     string
	Changes    []domain.FieldChange
}

type csvPreviewSummary struct {
	EntityType string
	Mode       csvImportMode
	Created    int
	Updated    int
	Unchanged  int
	Changes    []csvPreviewChange
}

type csvApplyResult struct{ Created, Updated, Unchanged int }

// csvPreparedPreview contains immutable reviewed data and a transaction-bound
// apply operation. No writes survive preparation. The HTTP store owns expiry,
// user binding, admission limits, and single-use confirmation.
type csvPreparedPreview struct {
	Summary csvPreviewSummary
	Bytes   int
	apply   func(context.Context, string) (csvApplyResult, error)
}

type csvRowError struct {
	line int
	err  error
}

func (e *csvRowError) Error() string { return fmt.Sprintf("row %d: %v", e.line, e.err) }
func (e *csvRowError) Unwrap() error { return e.err }

func readCSVPreview(ctx context.Context, r io.Reader, allowed map[string]bool) ([]csvInputRecord, []string, error) {
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, Reader: r}, maxCSVPreviewBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(data) > maxCSVPreviewBytes {
		return nil, []string{"CSV previews accept files up to 8 MiB; split this file into smaller batches"}, nil
	}
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err == io.EOF {
		return nil, []string{"the file is empty"}, nil
	}
	if err != nil {
		rowErrors, fatal := csvReadError(err)
		return nil, rowErrors, fatal
	}
	header[0] = strings.TrimPrefix(header[0], "\uFEFF")
	seen := make(map[string]bool)
	for i, raw := range header {
		name := strings.TrimSpace(raw)
		if !allowed[name] {
			return nil, []string{fmt.Sprintf("unknown column %q for this type", name)}, nil
		}
		if seen[name] {
			return nil, []string{fmt.Sprintf("duplicate column %q", name)}, nil
		}
		seen[name], header[i] = true, name
	}
	var records []csvInputRecord
	var rowErrors []string
	ids := make(map[int64]bool)
	for line := 2; ; line++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		cells, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			rowErrors, fatal := csvReadError(err)
			return nil, rowErrors, fatal
		}
		if line > maxCSVPreviewRows+1 {
			return nil, []string{"CSV previews accept at most 500 rows; split this file into smaller batches"}, nil
		}
		if len(cells) != len(header) {
			rowErrors = append(rowErrors, fmt.Sprintf("row %d: has %d columns, expected %d", line, len(cells), len(header)))
			continue
		}
		fields := make(map[string]string, len(header))
		for i, name := range header {
			fields[name] = cells[i]
		}
		var id int64
		if raw := strings.TrimSpace(fields["id"]); raw != "" {
			id, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || id <= 0 {
				rowErrors = append(rowErrors, fmt.Sprintf("row %d: id must be a positive integer", line))
				continue
			}
			if ids[id] {
				rowErrors = append(rowErrors, fmt.Sprintf("row %d: repeated id %d; include each existing record once", line, id))
				continue
			}
			ids[id] = true
		}
		records = append(records, csvInputRecord{Line: line, ID: id, Fields: fields})
	}
	if len(rowErrors) > 0 {
		return nil, rowErrors, nil
	}
	if len(records) == 0 {
		return nil, []string{"the file has no data rows"}, nil
	}
	return records, nil, nil
}

// csvStoredFields uses the same persistent-field allowlist as CSV import. It
// excludes derived liveness and display values from optimistic comparisons.
func csvStoredFields(item any) map[string]any {
	v := reflect.ValueOf(item)
	t := v.Type()
	fields := make(map[string]any)
	for i := 0; i < t.NumField(); i++ {
		name := strings.SplitN(t.Field(i).Tag.Get("yaml"), ",", 2)[0]
		if name != "" && name != "-" {
			fields[name] = v.Field(i).Interface()
		}
	}
	return fields
}

func csvFieldText(value any) string {
	if value != nil {
		v := reflect.ValueOf(value)
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return ""
			}
			return csvFieldText(v.Elem().Interface())
		}
	}
	switch value := value.(type) {
	case nil:
		return ""
	case string:
		return value
	case []string:
		return strings.Join(value, ",")
	case bool:
		return strconv.FormatBool(value)
	default:
		return fmt.Sprint(value)
	}
}

func (rs resource[T]) csvPreviewItem(record csvInputRecord, current T, mode csvImportMode) (T, error) {
	old := csvStoredFields(current)
	get := func(field string) string {
		if field == "backup_policy_present" && rs.sing == "backup" {
			if _, present := record.Fields["expected_interval_hours"]; present {
				return "1"
			}
		}
		if field == "agent_protection_present" && rs.sing == "host" {
			if _, present := record.Fields["agent_protected_fields"]; present {
				return "1"
			}
		}
		if value, present := record.Fields[field]; present {
			return value
		}
		if mode == csvPatch && record.ID != 0 {
			return csvFieldText(old[field])
		}
		return ""
	}
	item, err := rs.parseInput(get, record.ID)
	if err != nil {
		return item, err
	}
	return item, item.Validate()
}

func (rs resource[T]) prepareCSVPreview(ctx context.Context, d handlerDeps, r io.Reader, mode csvImportMode) (*csvPreparedPreview, []string, error) {
	if mode != csvPatch && mode != csvReplace {
		return nil, []string{"choose patch or replace mode"}, nil
	}
	records, rowErrors, err := readCSVPreview(ctx, r, csvFields(rs.newItem))
	if err != nil || len(rowErrors) > 0 {
		return nil, rowErrors, err
	}
	baseline := make(map[int64][]byte)
	var summary csvPreviewSummary
	err = store.WithTxContext(ctx, d.db, func(tx *sql.Tx) error {
		for _, record := range records {
			if record.ID == 0 {
				continue
			}
			item, err := rs.repo.GetTx(tx, record.ID)
			if errors.Is(err, store.ErrNotFound) {
				return &csvRowError{record.Line, fmt.Errorf("id %d does not exist", record.ID)}
			}
			if err != nil {
				return err
			}
			raw, err := json.Marshal(csvStoredFields(item))
			if err != nil {
				return err
			}
			baseline[record.ID] = raw
		}
		var events []webhook.Event
		dry := d
		dry.webhooks = webhook.Noop{}
		var err error
		summary, _, err = rs.runCSVPreviewPlan(ctx, tx, dry, records, mode, "preview", &events)
		if err != nil {
			return err
		}
		return errCSVPreviewRollback
	})
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	if err != errCSVPreviewRollback {
		var invalid *csvRowError
		if errors.As(err, &invalid) {
			return nil, []string{invalid.Error()}, nil
		}
		return nil, nil, err
	}
	expected, err := json.Marshal(summary)
	if err != nil {
		return nil, nil, err
	}
	inputBytes, err := json.Marshal(records)
	if err != nil {
		return nil, nil, err
	}
	size := len(expected) + len(inputBytes)
	for _, raw := range baseline {
		size += len(raw)
	}
	prepared := &csvPreparedPreview{Summary: summary, Bytes: size}
	prepared.apply = func(ctx context.Context, actor string) (csvApplyResult, error) {
		var result csvApplyResult
		var events []webhook.Event
		err := store.WithTxContext(ctx, d.db, func(tx *sql.Tx) error {
			for _, record := range records {
				if record.ID == 0 {
					continue
				}
				item, err := rs.repo.GetTx(tx, record.ID)
				if errors.Is(err, store.ErrNotFound) {
					return errCSVPreviewStale
				}
				if err != nil {
					return err
				}
				raw, err := json.Marshal(csvStoredFields(item))
				if err != nil {
					return err
				}
				if !bytes.Equal(raw, baseline[record.ID]) {
					return errCSVPreviewStale
				}
			}
			actual := d
			actual.webhooks = webhook.ForTransaction(tx, d.webhooks)
			summary, counts, err := rs.runCSVPreviewPlan(ctx, tx, actual, records, mode, actor, &events)
			if err != nil {
				return err
			}
			encoded, err := json.Marshal(summary)
			if err != nil {
				return err
			}
			if !bytes.Equal(encoded, expected) {
				return errCSVPreviewStale
			}
			result = counts
			return nil
		})
		if err != nil {
			return csvApplyResult{}, err
		}
		d.webhooks.Dispatch(events...)
		return result, nil
	}
	return prepared, nil, nil
}

func (rs resource[T]) runCSVPreviewPlan(ctx context.Context, tx *sql.Tx, d handlerDeps, records []csvInputRecord, mode csvImportMode, actor string, events *[]webhook.Event) (csvPreviewSummary, csvApplyResult, error) {
	summary := csvPreviewSummary{EntityType: rs.sing, Mode: mode}
	var result csvApplyResult
	createdRows := make(map[int64]int)
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return summary, result, err
		}
		var current T
		if record.ID != 0 {
			var err error
			current, err = rs.repo.GetTx(tx, record.ID)
			if err != nil {
				return summary, result, err
			}
		}
		item, err := rs.csvPreviewItem(record, current, mode)
		if err != nil {
			return summary, result, &csvRowError{record.Line, err}
		}
		before := len(*events)
		if record.ID == 0 {
			id, err := rs.createEntityTx(tx, d, item, nil, actor, events)
			if err != nil {
				return summary, result, csvPlanWriteError(record.Line, err)
			}
			createdRows[id] = record.Line
			result.Created++
		} else {
			if err := rs.updateEntityTx(tx, d, item, nil, actor, events); err != nil {
				return summary, result, csvPlanWriteError(record.Line, err)
			}
			if len(*events) == before {
				result.Unchanged++
			} else {
				result.Updated++
			}
		}
		if added := len(*events) - before; added > 0 {
			history, err := d.changelog.WithTx(tx).ListRecent(added)
			if err != nil {
				return summary, result, err
			}
			for i := len(history) - 1; i >= 0; i-- {
				change := history[i]
				summary.Changes = append(summary.Changes, csvPreviewChange{Row: record.Line, EntityType: change.EntityType, EntityID: change.EntityID, Label: change.Label, Action: change.Action, Changes: change.Changes})
			}
		} else {
			summary.Changes = append(summary.Changes, csvPreviewChange{Row: record.Line, EntityType: rs.sing, EntityID: record.ID, Label: rs.label(item), Action: "unchanged"})
		}
	}
	// Generated IDs are provisional until apply. Replace them with stable CSV
	// row references, including reciprocal links made when creating ports.
	for i := range summary.Changes {
		change := &summary.Changes[i]
		if change.EntityType == rs.sing && createdRows[change.EntityID] != 0 {
			change.EntityID = 0
		}
		if rs.sing == "port" && change.EntityType == "port" {
			for j := range change.Changes {
				field := &change.Changes[j]
				if field.Field != "peer_port_id" {
					continue
				}
				for _, value := range []*string{&field.Old, &field.New} {
					id, _ := strconv.ParseInt(*value, 10, 64)
					if line := createdRows[id]; line != 0 {
						*value = fmt.Sprintf("new record from CSV row %d", line)
					}
				}
			}
		}
	}
	summary.Created, summary.Updated, summary.Unchanged = result.Created, result.Updated, result.Unchanged
	return summary, result, nil
}

func csvPlanWriteError(line int, err error) error {
	if invalid := constraintError(err); invalid != nil {
		return &csvRowError{line, invalid}
	}
	if errors.Is(err, store.ErrNotFound) {
		return &csvRowError{line, fmt.Errorf("a referenced record no longer exists")}
	}
	return err
}
