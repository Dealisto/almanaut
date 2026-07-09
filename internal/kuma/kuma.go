// Package kuma keeps Uptime Kuma's monitor list in sync with almanaut's
// services (one-way, almanaut → Kuma). Monitor CRUD is only reachable through
// Kuma's internal socket.io API, so the client here speaks a deliberately tiny
// subset of it; the reconcile is idempotent and only ever touches monitors
// recorded in the kuma_monitors mapping table.
package kuma

import (
	"net/url"

	"github.com/Dealisto/almanaut/internal/domain"
)

// Monitor is the subset of an Uptime Kuma monitor the sync manages. raw holds
// the full monitor object as Kuma sent it so edits round-trip fields almanaut
// does not model (intervals, retries, notification lists, ...).
type Monitor struct {
	ID   int64
	Name string
	URL  string
	raw  map[string]any
}

// monitorURL returns the service's monitorable URL, or ok=false when the
// service has no valid http(s) URL and must be skipped by the sync.
func monitorURL(s domain.Service) (string, bool) {
	u, err := url.Parse(s.URL)
	if err != nil {
		return "", false
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	return s.URL, true
}
