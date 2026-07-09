package web

import (
	"context"
	"net/http"

	"github.com/Dealisto/almanaut/internal/kuma"
)

// kumaSyncer is the slice of *kuma.Syncer the admin page needs.
type kumaSyncer interface {
	TriggerSync()
	LastSync() kuma.LastSync
}

// KumaOptions wires the Uptime Kuma admin page. Zero value = disabled: no
// routes, no nav entry — the integration is invisible.
type KumaOptions struct {
	Enabled bool
	URL     string     // shown on the page so the admin can see what's targeted
	Syncer  kumaSyncer // nil iff !Enabled
}

type kumaPageData struct {
	Title   string
	URL     string
	Last    kuma.LastSync
	Message string
}

func kumaPage(opts KumaOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		render(w, r, "kuma.html", kumaPageData{
			Title: "Uptime Kuma", URL: opts.URL,
			Last:    opts.Syncer.LastSync(),
			Message: r.URL.Query().Get("msg"),
		})
	}
}

func kumaSyncNow(opts KumaOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		opts.Syncer.TriggerSync()
		http.Redirect(w, r, "/kuma?msg=sync-requested", http.StatusSeeOther)
	}
}

// kumaEnabledKey marks a request as having the Kuma admin page mounted, so the
// "kumaEnabled" template func (used by the nav link) can read it without New
// threading cfg through render.
type kumaEnabledKey struct{}

func markKumaEnabled(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), kumaEnabledKey{}, true)))
	})
}

func kumaEnabledFrom(ctx context.Context) bool {
	v, _ := ctx.Value(kumaEnabledKey{}).(bool)
	return v
}
