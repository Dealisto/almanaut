package web

import (
	"fmt"
	"net/http"
	"strconv"

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
			// UNIQUE(entity_type, name) violation → treat as a user error.
			renderCustomFieldsError(w, req, repo, fmt.Errorf("a field named %q already exists for %s", def.Name, def.EntityType))
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
		if err := repo.UpdateDefLabel(id, req.FormValue("label")); err != nil {
			notFoundOrServerError(w, req, "custom field", err)
			return
		}
		http.Redirect(w, req, "/custom-fields", http.StatusSeeOther)
	}
}
