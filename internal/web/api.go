package web

import (
	"encoding/json"
	"net/http"
	"strings"

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
