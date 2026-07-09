package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func ctxReq(method string, u *domain.User, scope *domain.Scope) *http.Request {
	r := httptest.NewRequest(method, "/hosts", nil)
	ctx := r.Context()
	if u != nil {
		ctx = withUser(ctx, *u)
	}
	if scope != nil {
		ctx = withTokenScope(ctx, *scope)
	}
	return r.WithContext(ctx)
}

func TestEffectiveCanWrite(t *testing.T) {
	admin := domain.User{Role: domain.RoleAdmin}
	viewer := domain.User{Role: domain.RoleViewer}
	ro := domain.ScopeReadOnly
	rw := domain.ScopeReadWrite
	cases := []struct {
		name  string
		user  *domain.User
		scope *domain.Scope
		want  bool
	}{
		{"admin session", &admin, nil, true},
		{"viewer session", &viewer, nil, false},
		{"admin + read-only token", &admin, &ro, false},
		{"admin + read-write token", &admin, &rw, true},
		{"viewer + read-write token", &viewer, &rw, false},
		{"no user", nil, nil, false},
	}
	for _, c := range cases {
		if got := effectiveCanWrite(ctxReq(http.MethodPost, c.user, c.scope).Context()); got != c.want {
			t.Errorf("%s: effectiveCanWrite = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRequireWriteBlocksAndAllows(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := requireWrite(ok)

	// Viewer POST → 403.
	rec := httptest.NewRecorder()
	viewer := domain.User{Role: domain.RoleViewer}
	h.ServeHTTP(rec, ctxReq(http.MethodPost, &viewer, nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer POST = %d, want 403", rec.Code)
	}
	// Viewer GET → passes (safe method).
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, ctxReq(http.MethodGet, &viewer, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("viewer GET = %d, want 200", rec.Code)
	}
	// Editor POST → passes.
	rec = httptest.NewRecorder()
	editor := domain.User{Role: domain.RoleEditor}
	h.ServeHTTP(rec, ctxReq(http.MethodPost, &editor, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("editor POST = %d, want 200", rec.Code)
	}
}

func TestRequireAdmin(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := requireAdmin(ok)
	editor := domain.User{Role: domain.RoleEditor}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, ctxReq(http.MethodGet, &editor, nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor admin route = %d, want 403", rec.Code)
	}
	admin := domain.User{Role: domain.RoleAdmin}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, ctxReq(http.MethodGet, &admin, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin admin route = %d, want 200", rec.Code)
	}
}

func TestForbiddenFormatByPath(t *testing.T) {
	rec := httptest.NewRecorder()
	forbidden(rec, httptest.NewRequest(http.MethodPost, "/api/hosts", nil))
	if rec.Code != http.StatusForbidden || rec.Header().Get("Content-Type") == "" {
		t.Fatalf("api forbidden = %d, ct %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}
