package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"modernc.org/sqlite"
)

// InventoryQuery is trusted SQL metadata supplied by the resource catalog.
// All entity expressions use the base-table alias e. Request values never
// become SQL identifiers or expressions.
type InventoryQuery struct {
	Type, Table, Label, Order string
	Fields                    []InventoryField
	Search                    []string
}
type InventoryField struct{ Label, Expression string }
type InventoryFilter struct {
	Query, Tag, Field, Value, Sort string
	Desc                           bool
}
type InventoryHit struct {
	Type  string
	ID    int64
	Label string
}

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("almanaut_contains", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		value, _ := args[0].(string)
		query, _ := args[1].(string)
		if strings.Contains(strings.ToLower(value), strings.ToLower(query)) {
			return int64(1), nil
		}
		return int64(0), nil
	})
	sqlite.MustRegisterDeterministicScalarFunction("almanaut_minute", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		value, _ := args[0].(string)
		parsed, _ := time.Parse(time.RFC3339, value)
		return parsed.Format("2006-01-02 15:04"), nil
	})
	sqlite.MustRegisterCollationUtf8("almanaut_value", CompareInventoryValues)
}

// CompareInventoryValues orders numbers before Unicode case-insensitive text.
// NaN is text; infinity remains a number, matching list sorting semantics.
func CompareInventoryValues(a, b string) int {
	fa, ea := strconv.ParseFloat(strings.TrimSpace(a), 64)
	fb, eb := strconv.ParseFloat(strings.TrimSpace(b), 64)
	an, bn := ea == nil && !math.IsNaN(fa), eb == nil && !math.IsNaN(fb)
	if an && bn {
		if fa < fb {
			return -1
		}
		if fa > fb {
			return 1
		}
		// A float64 tie can hide distinct integer IDs or precise custom field
		// values. Resolve it with enough precision for both input mantissas.
		// Float's separate exponent avoids expanding tiny values such as 1e-1000
		// into enormous denominators, as a rational representation would.
		if a != b && !math.IsInf(fa, 0) {
			precision := uint(64 + 4*(len(a)+len(b)))
			aa, _, aerr := big.ParseFloat(strings.TrimSpace(a), 0, precision, big.ToNearestEven)
			bb, _, berr := big.ParseFloat(strings.TrimSpace(b), 0, precision, big.ToNearestEven)
			if aerr == nil && berr == nil {
				return aa.Cmp(bb)
			}
			// Extremely small exponents can fit float64 only as zero while
			// exceeding Float's exponent range. Keep those values in a separate,
			// deterministic subgroup of the float64 tie: treating every failed
			// parse as equal to a successful one would break transitivity.
			if aerr == nil {
				return -1
			}
			if berr == nil {
				return 1
			}
			return strings.Compare(strings.TrimSpace(a), strings.TrimSpace(b))
		}
		return 0
	}
	if an {
		return -1
	}
	if bn {
		return 1
	}
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}
func sqlText(s string) string           { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func textExpression(expr string) string { return "CAST(COALESCE((" + expr + "), '') AS TEXT)" }
func containsExpression(expr string) string {
	return "almanaut_contains(" + textExpression(expr) + ", ?)"
}

// WithCustomFields projects custom values by their visible labels. A set value
// overrides a standard column with the same label, as it does on list pages.
func (s InventoryQuery) WithCustomFields(defs []domain.CustomFieldDef) InventoryQuery {
	s.Fields = append([]InventoryField(nil), s.Fields...)
	for _, d := range defs {
		expr := fmt.Sprintf("(SELECT value FROM custom_field_values WHERE entity_type=%s AND entity_id=e.id AND def_id=%d)", sqlText(s.Type), d.ID)
		found := false
		for i, f := range s.Fields {
			if f.Label == d.Label {
				s.Fields[i].Expression = "COALESCE(" + expr + ", " + f.Expression + ")"
				found = true
				break
			}
		}
		if !found {
			s.Fields = append(s.Fields, InventoryField{d.Label, "COALESCE(" + expr + ", '')"})
		}
	}
	return s
}
func (s InventoryQuery) field(label string) string {
	for _, f := range s.Fields {
		if f.Label == label {
			return f.Expression
		}
	}
	return "''"
}
func (s InventoryQuery) matching(query string, listFields, tags bool) (string, []any) {
	var tests []string
	var args []any
	expressions := s.Search
	if listFields {
		expressions = append([]string(nil), expressions...)
		for _, f := range s.Fields {
			expressions = append(expressions, f.Expression)
		}
	}
	for _, expr := range expressions {
		tests = append(tests, containsExpression(expr))
		args = append(args, query)
	}
	tests = append(tests, "EXISTS (SELECT 1 FROM custom_field_values v JOIN custom_field_definitions d ON d.id=v.def_id WHERE v.entity_type="+sqlText(s.Type)+" AND v.entity_id=e.id AND almanaut_contains(v.value, ?))")
	args = append(args, query)
	if tags {
		tests = append(tests, "EXISTS (SELECT 1 FROM tags t WHERE t.entity_type="+sqlText(s.Type)+" AND t.entity_id=e.id AND almanaut_contains(t.name, ?))")
		args = append(args, query)
	}
	return "(" + strings.Join(tests, " OR ") + ")", args
}
func (s InventoryQuery) where(f InventoryFilter) (string, []any) {
	tests := []string{"1=1"}
	var args []any
	if f.Query != "" {
		expr, a := s.matching(f.Query, true, false)
		tests = append(tests, expr)
		args = append(args, a...)
	}
	if f.Tag != "" {
		tests = append(tests, "EXISTS (SELECT 1 FROM tags t WHERE t.entity_type="+sqlText(s.Type)+" AND t.entity_id=e.id AND t.name=?)")
		args = append(args, f.Tag)
	}
	if f.Field != "" && f.Value != "" {
		tests = append(tests, containsExpression(s.field(f.Field)))
		args = append(args, f.Value)
	}
	return strings.Join(tests, " AND "), args
}

// InventoryIDs selects only the requested page; count and selection share the
// caller's read-only transaction, so concurrent writes cannot split the result.
func InventoryIDs(ctx context.Context, tx *sql.Tx, spec InventoryQuery, filter InventoryFilter, limit, offset int) ([]int64, int, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, 0, fmt.Errorf("invalid inventory page")
	}
	where, args := spec.where(filter)
	from := " FROM " + spec.Table + " e WHERE " + where
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*)"+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := spec.Order
	if filter.Sort != "" {
		direction := " ASC"
		if filter.Desc {
			direction = " DESC"
		}
		order = textExpression(spec.field(filter.Sort)) + " COLLATE almanaut_value" + direction + ", " + order
	}
	rows, err := tx.QueryContext(ctx, "SELECT e.id"+from+" ORDER BY "+order+", e.id LIMIT ? OFFSET ?", append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, 0, err
		}
		ids = append(ids, id)
	}
	return ids, total, rows.Err()
}

// SearchInventory performs one bounded UNION query across catalog resources.
// Tags are included for both browser and API search; each entity appears once.
func SearchInventory(ctx context.Context, tx *sql.Tx, specs []InventoryQuery, query string, limit, offset int) ([]InventoryHit, int, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, 0, fmt.Errorf("invalid search page")
	}
	if query == "" {
		return []InventoryHit{}, 0, nil
	}
	var parts []string
	var args []any
	for i, s := range specs {
		where, a := s.matching(query, false, true)
		parts = append(parts, fmt.Sprintf("SELECT %d AS position, %s AS type, e.id AS id, %s AS label FROM %s e WHERE %s", i, sqlText(s.Type), textExpression(s.Label), s.Table, where))
		args = append(args, a...)
	}
	if len(parts) == 0 {
		return []InventoryHit{}, 0, nil
	}
	union := strings.Join(parts, " UNION ALL ")
	var total int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM ("+union+")", args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT type,id,label FROM ("+union+") ORDER BY position,label,id LIMIT ? OFFSET ?", append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	hits := []InventoryHit{}
	for rows.Next() {
		var hit InventoryHit
		if err := rows.Scan(&hit.Type, &hit.ID, &hit.Label); err != nil {
			return nil, 0, err
		}
		hits = append(hits, hit)
	}
	return hits, total, rows.Err()
}
