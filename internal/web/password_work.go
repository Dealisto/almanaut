package web

import (
	"context"
	"net/http"
)

type passwordWorkKey struct{}

// passwordWorkLimit shares a bounded CPU-work budget across authentication,
// password changes, and account provisioning. There is no waiting queue: an
// overloaded server rejects work before looking up or verifying credentials.
func passwordWorkLimit(limit int) func(http.Handler) http.Handler {
	slots := make(chan struct{}, max(limit, 1))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), passwordWorkKey{}, slots)))
		})
	}
}

func acquirePasswordWork(w http.ResponseWriter, r *http.Request) (func(), bool) {
	if r.Context().Err() != nil {
		http.Error(w, "request canceled", http.StatusRequestTimeout)
		return nil, false
	}
	slots, ok := r.Context().Value(passwordWorkKey{}).(chan struct{})
	if !ok {
		// Standalone handlers in unit tests have no application middleware.
		return func() {}, true
	}
	select {
	case slots <- struct{}{}:
		return func() { <-slots }, true
	default:
		w.Header().Set("Retry-After", "1")
		http.Error(w, "authentication is busy; try again shortly", http.StatusServiceUnavailable)
		return nil, false
	}
}
