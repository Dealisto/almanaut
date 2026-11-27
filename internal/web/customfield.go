package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/go-chi/chi/v5"
)

// customFieldFormRow is one custom field rendered on an entity form: its slug
// name (input key cf_<Name>), human label, kind, and current value.
type customFieldFormRow struct {
	Name  string
	Label string
	Kind  string
	Value string
}

// parseCustomFields reads cf_<name> for each definition of entityType, validates
// and canonicalises each via the domain rules, and returns a map[defID]value
// suitable for CustomFieldRepo.SetForEntity. An empty text/number/date value is
// kept as "" (SetForEntity then deletes it).
func (d handlerDeps) parseCustomFields(entityType string, get func(string) string) (map[int64]string, error) {
	defs, err := d.customFields.ListDefs(entityType)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(defs))
	for _, def := range defs {
		canon, err := domain.ValidateCustomFieldValue(def.Kind, get("cf_"+def.Name))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", def.Label, err)
		}
		out[def.ID] = canon
	}
	return out, nil
}

// customFieldValidationError distinguishes invalid input from definition lookup
// failures, which must be reported as server errors rather than client errors.
type customFieldValidationError struct{ error }

// parseJSONCustomFields resolves a decoded custom_fields object (name → raw JSON
// value) against the definitions for entityType, validating each value, and
// returns a map[defID]canonical-value for SetForEntity. Names with no matching
// definition are ignored. A value that fails validation is an error.
func (d handlerDeps) parseJSONCustomFields(entityType string, raw map[string]json.RawMessage) (map[int64]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	defs, err := d.customFields.ListDefs(entityType)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]domain.CustomFieldDef, len(defs))
	for _, def := range defs {
		byName[def.Name] = def
	}
	out := map[int64]string{}
	for name, rawVal := range raw {
		def, ok := byName[name]
		if !ok {
			continue // unknown field name: ignore
		}
		var v any
		decoder := json.NewDecoder(bytes.NewReader(rawVal))
		decoder.UseNumber()
		if err := decoder.Decode(&v); err != nil {
			return nil, customFieldValidationError{fmt.Errorf("%s: invalid value", name)}
		}
		switch v.(type) {
		case []any, map[string]any:
			return nil, customFieldValidationError{fmt.Errorf("%s: value must be a string, number, boolean, or null", def.Label)}
		}
		// Preserve numeric boolean notation (1.0 and 1e0 mean true), but
		// require exact zero or one. float64 would round near-one values or
		// underflow tiny nonzero values into valid booleans.
		if number, ok := v.(json.Number); ok && def.Kind == domain.KindBool {
			value, valid := jsonNumberBoolean(number.String())
			if !valid {
				return nil, customFieldValidationError{fmt.Errorf("%s: numeric boolean must be exactly 0 or 1", def.Label)}
			}
			v = value
		}
		canon, err := domain.ValidateCustomFieldValue(def.Kind, jsonValueToString(v))
		if err != nil {
			return nil, customFieldValidationError{fmt.Errorf("%s: %w", def.Label, err)}
		}
		out[def.ID] = canon
	}
	return out, nil
}

// jsonNumberBoolean recognizes exact zero or one in an already validated JSON
// number. Counting decimal places keeps work linear in the request size; parsing
// an arbitrarily precise float would make long mantissas disproportionately costly.
func jsonNumberBoolean(raw string) (value, valid bool) {
	mantissa, exponent := raw, ""
	if i := strings.IndexAny(raw, "eE"); i >= 0 {
		mantissa, exponent = raw[:i], raw[i+1:]
	}
	negative := strings.HasPrefix(mantissa, "-")
	if negative {
		mantissa = mantissa[1:]
	}
	digits, fractionDigits, onePosition := 0, 0, -1
	fraction := false
	for i := 0; i < len(mantissa); i++ {
		if mantissa[i] == '.' {
			fraction = true
			continue
		}
		if fraction {
			fractionDigits++
		}
		if mantissa[i] != '0' {
			if mantissa[i] != '1' || onePosition >= 0 {
				return false, false
			}
			onePosition = digits
		}
		digits++
	}
	if onePosition < 0 {
		return false, true // Zero remains zero regardless of sign or exponent size.
	}
	if negative {
		return false, false
	}
	var scale int64
	if exponent != "" {
		var err error
		scale, err = strconv.ParseInt(exponent, 10, 64)
		if err != nil {
			return false, false // An overflowing exponent cannot balance a bounded mantissa.
		}
	}
	trailingZeros := digits - onePosition - 1
	valid = scale == int64(fractionDigits-trailingZeros)
	return valid, valid
}

// jsonValueToString renders a decoded JSON scalar as the string form
// ValidateCustomFieldValue expects (number→formatted, bool→true/false, else as-is).
func jsonValueToString(v any) string {
	switch x := v.(type) {
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case string:
		return x
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", x)
	}
}

// customFieldFormRows builds the form rows for entityType: one per definition.
// When get is non-nil, values come from the submitted form (used on a
// validation-error re-render); otherwise they come from the stored values for
// entityID (0 on the create form → all blank).
func (d handlerDeps) customFieldFormRows(entityType string, entityID int64, get func(string) string) ([]customFieldFormRow, error) {
	defs, err := d.customFields.ListDefs(entityType)
	if err != nil {
		return nil, err
	}
	stored := map[string]string{}
	if get == nil && entityID != 0 {
		vals, err := d.customFields.ListForEntity(entityType, entityID)
		if err != nil {
			return nil, err
		}
		for _, v := range vals {
			stored[v.Name] = v.Value
		}
	}
	rows := make([]customFieldFormRow, 0, len(defs))
	for _, def := range defs {
		value := stored[def.Name]
		if get != nil {
			value = get("cf_" + def.Name)
			if def.Kind == domain.KindBool {
				value, _ = domain.ValidateCustomFieldValue(def.Kind, value)
			}
		}
		rows = append(rows, customFieldFormRow{
			Name: def.Name, Label: def.Label, Kind: string(def.Kind), Value: value,
		})
	}
	return rows, nil
}

// withCustomFields returns a copy of base (the resource's static form extras)
// with the custom field rows added under "customFields". base may be nil.
func withCustomFields(base map[string]any, rows []customFieldFormRow) map[string]any {
	m := map[string]any{}
	for k, v := range base {
		m[k] = v
	}
	m["customFields"] = rows
	return m
}

// customFieldsPageData is the view model for the /custom-fields admin page.
type customFieldsPageData struct {
	Title       string
	Defs        []domain.CustomFieldDef
	EntityTypes []string
	Kinds       []domain.CustomFieldKind
	Error       string
}

func customFieldsPage(repo *store.CustomFieldRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		defs, err := repo.ListAllDefs()
		if err != nil {
			serverError(w, req, err)
			return
		}
		render(w, req, "custom_fields.html", customFieldsPageData{
			Title: "Custom fields", Defs: defs,
			EntityTypes: domain.EntityTypes, Kinds: domain.CustomFieldKinds,
		})
	}
}

func createCustomField(repo *store.CustomFieldRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		label := req.FormValue("label")
		name := req.FormValue("name")
		if name == "" {
			name = domain.SlugifyCustomField(label)
		} else {
			name = domain.SlugifyCustomField(name)
		}
		def := domain.CustomFieldDef{
			EntityType: req.FormValue("entity_type"),
			Name:       name,
			Label:      label,
			Kind:       domain.CustomFieldKind(req.FormValue("kind")),
			CreatedAt:  nowRFC3339(),
		}
		if err := def.Validate(); err != nil {
			renderCustomFieldsError(w, req, repo, err)
			return
		}
		if _, err := repo.CreateDef(def); err != nil {
			if c := constraintError(err); c != nil {
				renderCustomFieldsError(w, req, repo, c)
			} else {
				serverError(w, req, err)
			}
			return
		}
		http.Redirect(w, req, "/custom-fields", http.StatusSeeOther)
	}
}

func renderCustomFieldsError(w http.ResponseWriter, req *http.Request, repo *store.CustomFieldRepo, cause error) {
	defs, err := repo.ListAllDefs()
	if err != nil {
		serverError(w, req, err)
		return
	}
	render(w, req, "custom_fields.html", customFieldsPageData{
		Title: "Custom fields", Defs: defs,
		EntityTypes: domain.EntityTypes, Kinds: domain.CustomFieldKinds,
		Error: cause.Error(),
	})
}

func deleteCustomField(repo *store.CustomFieldRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := repo.DeleteDef(id); err != nil {
			serverError(w, req, err)
			return
		}
		http.Redirect(w, req, "/custom-fields", http.StatusSeeOther)
	}
}

func updateCustomFieldLabel(repo *store.CustomFieldRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		label := req.FormValue("label")
		if strings.TrimSpace(label) == "" {
			renderCustomFieldsError(w, req, repo, fmt.Errorf("label is required"))
			return
		}
		if err := repo.UpdateDefLabel(id, label); err != nil {
			notFoundOrServerError(w, req, "custom field", err)
			return
		}
		http.Redirect(w, req, "/custom-fields", http.StatusSeeOther)
	}
}
