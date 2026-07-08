package web

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/store"
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
// rowErrs describe every bad row. A non-nil err is an infrastructure failure
// (bad CSV framing, database error), distinct from per-row validation errors.
func (rs resource[T]) importCSV(d handlerDeps, r io.Reader, actor string) (int, int, []string, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // we length-check each row ourselves for row-level errors
	records, err := cr.ReadAll()
	if err != nil {
		return 0, 0, nil, err
	}
	if len(records) == 0 {
		return 0, 0, []string{"the file is empty"}, nil
	}
	// Spreadsheets that save as "CSV UTF-8" prepend a BOM; strip it so the first
	// header cell is not read as "\uFEFFname" and rejected as an unknown column.
	records[0][0] = strings.TrimPrefix(records[0][0], "\uFEFF")

	header := records[0]
	allowed := csvFields(rs.newItem)
	for _, col := range header {
		if !allowed[strings.TrimSpace(col)] {
			return 0, 0, []string{fmt.Sprintf("unknown column %q for this type", col)}, nil
		}
	}

	type pending struct {
		item   T
		create bool
	}
	var (
		plan    []pending
		rowErrs []string
	)
	for i, rec := range records[1:] {
		line := i + 2 // 1-based, and +1 for the header row
		if len(rec) != len(header) {
			rowErrs = append(rowErrs, fmt.Sprintf("row %d: has %d columns, expected %d", line, len(rec), len(header)))
			continue
		}
		row := make(map[string]string, len(header))
		for j, col := range header {
			row[strings.TrimSpace(col)] = rec[j]
		}
		get := func(field string) string { return row[field] }

		id, create := int64(0), true
		if raw := strings.TrimSpace(row["id"]); raw != "" {
			parsed, perr := strconv.ParseInt(raw, 10, 64)
			if perr != nil {
				rowErrs = append(rowErrs, fmt.Sprintf("row %d: invalid id %q", line, raw))
				continue
			}
			if _, gerr := rs.repo.Get(parsed); gerr != nil {
				if errors.Is(gerr, store.ErrNotFound) {
					rowErrs = append(rowErrs, fmt.Sprintf("row %d: id %d does not exist", line, parsed))
					continue
				}
				return 0, 0, nil, gerr
			}
			id, create = parsed, false
		}

		item := rs.parse(get, id)
		if verr := item.Validate(); verr != nil {
			rowErrs = append(rowErrs, fmt.Sprintf("row %d: %v", line, verr))
			continue
		}
		plan = append(plan, pending{item: item, create: create})
	}
	if len(rowErrs) > 0 {
		return 0, 0, rowErrs, nil // all-or-nothing: write nothing
	}

	var created, updated int
	err = store.WithTx(d.db, func(tx *sql.Tx) error {
		for _, p := range plan {
			if p.create {
				if _, e := rs.createEntityTx(tx, d, p.item, nil, actor); e != nil {
					return e
				}
				created++
				continue
			}
			if e := rs.updateEntityTx(tx, d, p.item, nil, actor); e != nil {
				return e
			}
			updated++
		}
		return nil
	})
	if err != nil {
		return 0, 0, nil, err
	}
	return created, updated, nil, nil
}
