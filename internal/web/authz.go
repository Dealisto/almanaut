package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
)

// Authorization enforcement. This is the central role/scope gate the RBAC
// design calls for; it lives here rather than in middleware.go, which is
// strictly transport middleware (logging, CSP, recovery, body limits).

type tokenScopeCtxKey struct{}

// withTokenScope records the scope of the API token that authenticated the
// request. Session (browser) requests carry no scope and are bounded by role
// alone.
func withTokenScope(ctx context.Context, s domain.Scope) context.Context {
	return context.WithValue(ctx, tokenScopeCtxKey{}, s)
}

// tokenScopeFrom returns the API-token scope stored by apiAuth, if any.
func tokenScopeFrom(ctx context.Context) (domain.Scope, bool) {
	s, ok := ctx.Value(tokenScopeCtxKey{}).(domain.Scope)
	return s, ok
}

// effectiveCanWrite reports whether the request may mutate state: the user's
// role must allow writes and — when authenticated via an API token — the
// token's scope must also allow writes (the intersection of the two).
func effectiveCanWrite(ctx context.Context) bool {
	u, ok := userFrom(ctx)
	if !ok || !u.Role.CanWrite() {
		return false
	}
	if s, ok := tokenScopeFrom(ctx); ok && !s.CanWrite() {
		return false
	}
	return true
}

// effectiveIsAdmin reports whether the request may perform admin actions.
// Admin actions have no API surface, so token scope does not narrow them.
func effectiveIsAdmin(ctx context.Context) bool {
	u, ok := userFrom(ctx)
	return ok && u.Role.IsAdmin()
}

// isSafeMethod reports whether m is read-only (mirrors apiAuth's GET/HEAD check).
func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead
}

// requireWrite rejects unsafe methods (non-GET/HEAD) with 403 unless the request
// is write-capable. Safe methods pass untouched, so a viewer reads everything.
// Because every catalog mutation is a POST, this one gate covers all present and
// future catalog entities without a per-type path list.
func requireWrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isSafeMethod(r.Method) && !effectiveCanWrite(r.Context()) {
			forbidden(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdmin rejects any request whose user is not an admin with 403.
func requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !effectiveIsAdmin(r.Context()) {
			forbidden(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// forbidden writes a 403 matching the route group: JSON under /api, else plain.
func forbidden(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}
	http.Error(w, "forbidden", http.StatusForbidden)
}
