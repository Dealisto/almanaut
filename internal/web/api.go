package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/go-chi/chi/v5/middleware"
)

// readAPIBody keeps body-limit failures distinct from malformed or interrupted
// requests, including uploads that omit Content-Length (such as chunked bodies).
func readAPIBody(w http.ResponseWriter, r *http.Request, invalidMessage string) ([]byte, bool) {
	body, err := io.ReadAll(r.Body)
	if err == nil {
		return body, true
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
	} else {
		writeJSONError(w, http.StatusBadRequest, invalidMessage)
	}
	return nil, false
}

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
func apiSearch(cat entityCatalog, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		p, err := parsePagination(req)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		entries, p, err := searchPage(req, db, cat, p)
		if err != nil {
			apiServerError(w, req, err)
			return
		}
		p.headers(w)
		writeJSON(w, http.StatusOK, entries)
	}
}

// customFieldsObject builds the JSON "custom_fields" object {name: typed value}
// from an entity's set values: number→JSON number, bool→bool, text/date→string.
func customFieldsObject(vals []domain.CustomFieldValue) map[string]any {
	obj := make(map[string]any, len(vals))
	for _, v := range vals {
		switch v.Kind {
		case domain.KindNumber:
			if f, err := strconv.ParseFloat(v.Value, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
				if raw := strings.TrimSpace(v.Value); json.Valid([]byte(raw)) {
					obj[v.Name] = json.Number(raw)
				} else {
					// Legacy/form values can use non-JSON spellings such as
					// +1, .5 or 0x1p2, which still need conversion for encoding.
					obj[v.Name] = f
				}
				continue
			}
			obj[v.Name] = v.Value // Preserve invalid legacy values without breaking JSON responses.
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
// raw JSON fields so int64 identifiers never round through float64. When vals
// is empty the key is still emitted as {} for a stable shape.
func mergeCustomFields(item any, vals []domain.CustomFieldValue) ([]byte, error) {
	raw, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	custom, err := json.Marshal(customFieldsObject(vals))
	if err != nil {
		return nil, err
	}
	m["custom_fields"] = custom
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
