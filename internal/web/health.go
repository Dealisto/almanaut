package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

// healthz is an unauthenticated liveness probe. It pings the database and
// returns 200 with a JSON body reporting status and the running version when
// reachable, or 503 otherwise, so a container HEALTHCHECK or reverse-proxy
// probe can tell whether the server is serving — and which version answered.
func healthz(db *sql.DB, version string) http.HandlerFunc {
	if version == "" {
		version = "dev"
	}
	// The healthy body is constant; marshal it once at construction.
	body, _ := json.Marshal(struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}{"ok", version})
	body = append(body, '\n')
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(body)
	}
}

// versionInfo serves the build version as JSON. The version is baked in at
// build time via -ldflags "-X main.version=..."; it falls back to "dev".
func versionInfo(version string) http.HandlerFunc {
	if version == "" {
		version = "dev"
	}
	// Marshal once at construction so the version is JSON-encoded properly
	// rather than assembled by hand.
	body, _ := json.Marshal(struct {
		Version string `json:"version"`
	}{version})
	body = append(body, '\n')
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(body)
	}
}
