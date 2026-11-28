package web

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

// csvFields returns the set of importable CSV column names for an entity: the
// snake_case names from its yaml struct tags. This is the same naming the YAML
// export uses, so an export column always round-trips as a CSV column.
func csvFields(sample any) map[string]bool {
	t := reflect.TypeOf(sample)
	fields := make(map[string]bool, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name := strings.SplitN(t.Field(i).Tag.Get("yaml"), ",", 2)[0]
		if name != "" && name != "-" {
			fields[name] = true
		}
	}
	return fields
}

// importCSV adds or updates entities from a CSV whose header row uses the
// entity's snake_case field names. A row with a non-empty id updates that row;
// an empty/absent id creates. The import is all-or-nothing: if any row fails to
// decode, validate, or resolve its id, nothing is written and the returned
// rowErrs describe invalid CSV syntax or bad rows. A non-nil err is an
// infrastructure failure (reader or database error).
func (rs resource[T]) importCSV(d handlerDeps, r io.Reader, actor string) (int, int, []string, error) {
	return rs.importCSVContext(context.Background(), d, r, actor)
}

func (rs resource[T]) importCSVContext(ctx context.Context, d handlerDeps, r io.Reader, actor string) (int, int, []string, error) {
	cr := csv.NewReader(contextReader{ctx: ctx, Reader: r})
	cr.FieldsPerRecord = -1 // we length-check each row ourselves for row-level errors
	cr.ReuseRecord = true   // only the current record's field slice is needed
	header, err := cr.Read()
	if errors.Is(err, io.EOF) {
		return 0, 0, []string{"the file is empty"}, nil
	}
	if err != nil {
		rowErrs, readErr := csvReadError(err)
		return 0, 0, rowErrs, readErr
	}
	// Spreadsheets that save as "CSV UTF-8" prepend a BOM; strip it so the first
	// header cell is not read as "\uFEFFname" and rejected as an unknown column.
	header[0] = strings.TrimPrefix(header[0], "\uFEFF")
	columnCount := len(header)
	allowed := csvFields(rs.newItem)
	columns := make(map[string]int, columnCount)
	var headerErr string
	for i, raw := range header {
		col := strings.TrimSpace(raw)
		if !allowed[col] {
			headerErr = fmt.Sprintf("unknown column %q for this type", col)
			break
		}
		if _, exists := columns[col]; exists {
			headerErr = fmt.Sprintf("duplicate column %q", col)
			break
		}
		columns[col] = i
	}

	type pending struct {
		item   T
		create bool
		line   int
	}
	var (
		plan          []pending
		rowErrs       []string
		validationErr error
	)
	reject := func(message string) {
		// Any invalid row makes the entire plan unusable. Keep validating the
		// remaining rows for diagnostics, but release the entities already built.
		plan = nil
		rowErrs = append(rowErrs, message)
	}
	for line := 2; ; line++ { // record numbers, preserving multiline-cell diagnostics
		rec, readErr := cr.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			rowErrs, readErr := csvReadError(readErr)
			return 0, 0, rowErrs, readErr
		}
		if err := ctx.Err(); err != nil {
			return 0, 0, nil, err
		}
		// ReadAll historically reported syntax/reader failures before header
		// or validation errors. Drain without retaining rows to keep that order.
		if headerErr != "" || validationErr != nil {
			continue
		}
		if len(rec) != columnCount {
			reject(fmt.Sprintf("row %d: has %d columns, expected %d", line, len(rec), columnCount))
			continue
		}
		get := func(field string) string {
			if field == "backup_policy_present" && rs.sing == "backup" {
				if _, present := columns["expected_interval_hours"]; present {
					return "1"
				}
			}
			// Host protection is a safety policy: omitted columns preserve it,
			// while a present empty cell explicitly clears the policy.
			if field == "agent_protection_present" && rs.sing == "host" {
				if _, exists := columns["agent_protected_fields"]; exists {
					return "1"
				}
			}
			if index, exists := columns[field]; exists {
				return rec[index]
			}
			return ""
		}

		id, create := int64(0), true
		if raw := strings.TrimSpace(get("id")); raw != "" {
			parsed, perr := strconv.ParseInt(raw, 10, 64)
			if perr != nil {
				reject(fmt.Sprintf("row %d: invalid id %q", line, raw))
				continue
			}
			if _, gerr := rs.repo.Get(parsed); gerr != nil {
				if errors.Is(gerr, store.ErrNotFound) {
					reject(fmt.Sprintf("row %d: id %d does not exist", line, parsed))
					continue
				}
				validationErr = gerr
				plan, rowErrs = nil, nil
				continue
			}
			id, create = parsed, false
		}

		item, parseErr := rs.parseInput(get, id)
		if parseErr != nil {
			reject(fmt.Sprintf("row %d: %v", line, parseErr))
			continue
		}
		if verr := item.Validate(); verr != nil {
			reject(fmt.Sprintf("row %d: %v", line, verr))
			continue
		}
		if len(rowErrs) == 0 {
			plan = append(plan, pending{item: item, create: create, line: line})
		}
	}
	if headerErr != "" {
		return 0, 0, []string{headerErr}, nil
	}
	if validationErr != nil {
		return 0, 0, nil, validationErr
	}
	if len(rowErrs) > 0 {
		return 0, 0, rowErrs, nil // all-or-nothing: write nothing
	}

	var created, updated int
	var events []webhook.Event
	failedLine := 0
	err = store.WithTxContext(ctx, d.db, func(tx *sql.Tx) error {
		// This import owns all staging for the transaction, including indirect
		// mutation events. Keep the scope local; post-commit dispatch uses d.
		batchDeps := d
		batchDeps.webhooks = webhook.ForTransaction(tx, d.webhooks)
		for _, p := range plan {
			if err := ctx.Err(); err != nil {
				return err
			}
			failedLine = p.line
			if p.create {
				if _, e := rs.createEntityTx(tx, batchDeps, p.item, nil, actor, &events); e != nil {
					return e
				}
				created++
				continue
			}
			if e := rs.updateEntityTx(tx, batchDeps, p.item, nil, actor, &events); e != nil {
				return e
			}
			updated++
		}
		return nil
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return 0, 0, nil, err
	}
	if c := constraintError(err); c != nil {
		// A row that contradicts the rows before it is a row error too; the
		// rollback has already undone the earlier rows.
		return 0, 0, []string{fmt.Sprintf("row %d: %v", failedLine, c)}, nil
	}
	if err != nil {
		return 0, 0, nil, err
	}
	d.webhooks.Dispatch(events...)
	return created, updated, nil, nil
}

// csvReadError keeps malformed input separate from transport/storage failures.
func csvReadError(err error) ([]string, error) {
	var syntax *csv.ParseError
	if errors.As(err, &syntax) {
		return []string{"invalid CSV: " + syntax.Error()}, nil
	}
	return nil, err
}
