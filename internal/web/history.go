package web

import (
	"net/http"
	"time"
)

// actor returns the Basic-auth username making the request, or "" when the app
// is unauthenticated. The M2 API-token work will extend this.
func actor(req *http.Request) string {
	user, _, _ := req.BasicAuth()
	return user
}

// nowRFC3339 is the single timestamp format used across history rows.
func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }
