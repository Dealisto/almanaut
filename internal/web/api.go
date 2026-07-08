package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/go-chi/chi/v5/middleware"
)

// writeJSON marshals v to a buffer first (so an encode error never yields a
// half-written body), then writes it with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	buf, err := json.Marshal(v)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf)
}

// writeJSONError writes {"error": msg} with the given status.
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// apiServerError logs like serverError but responds with a JSON body.
func apiServerError(w http.ResponseWriter, r *http.Request, err error) {
	id := middleware.GetReqID(r.Context())
	loggerFrom(r.Context()).Printf("api error: %s %s reqid=%q: %v", r.Method, r.URL.Path, id, err)
	writeJSONError(w, http.StatusInternalServerError, "internal server error")
}

// apiSearch returns a flat JSON array of entities whose searchable fields match
// q (case-insensitive). An empty q returns an empty array.
func apiSearch(cat entityCatalog, cf *store.CustomFieldRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		q := strings.TrimSpace(req.URL.Query().Get("q"))
		results := []searchEntry{}
		if q == "" {
			writeJSON(w, http.StatusOK, results)
			return
		}
		for _, rs := range cat.resources {
			entries, err := rs.searchEntries(cf)
			if err != nil {
				apiServerError(w, req, err)
				return
			}
			for _, e := range entries {
				if matchesQuery(e.Fields, q) {
					results = append(results, e)
				}
			}
		}
		writeJSON(w, http.StatusOK, results)
	}
}

// customFieldsObject builds the JSON "custom_fields" object {name: typed value}
// from an entity's set values: number→float, bool→bool, text/date→string.
func customFieldsObject(vals []domain.CustomFieldValue) map[string]any {
	obj := make(map[string]any, len(vals))
	for _, v := range vals {
		switch v.Kind {
		case domain.KindNumber:
			if f, err := strconv.ParseFloat(v.Value, 64); err == nil {
				obj[v.Name] = f
				continue
			}
			obj[v.Name] = v.Value // fall back to string if unparseable
		case domain.KindBool:
			obj[v.Name] = v.Value == "true"
		default:
			obj[v.Name] = v.Value
		}
	}
	return obj
}

// mergeCustomFields marshals item, then injects a "custom_fields" key built from
// vals. There is no marshal hook on the typed entity, so it round-trips through
// a map. When vals is empty the key is still emitted as {} for a stable shape.
func mergeCustomFields(item any, vals []domain.CustomFieldValue) ([]byte, error) {
	raw, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	m["custom_fields"] = customFieldsObject(vals)
	return json.Marshal(m)
}

// writeEntityJSON writes item with its custom_fields merged in.
func writeEntityJSON(w http.ResponseWriter, status int, item any, vals []domain.CustomFieldValue) {
	buf, err := mergeCustomFields(item, vals)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf)
}

// apiRelationships returns all relationships as JSON.
func apiRelationships(rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		list, err := rels.List()
		if err != nil {
			apiServerError(w, req, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
	}
}
