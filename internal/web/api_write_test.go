package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// apiAuthServer builds an auth-enabled handler with one seeded user and returns
// the handler, the db, and a valid raw API token for that user.
func apiAuthServer(t *testing.T) (http.Handler, *store.TokenRepo, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	users := store.NewUserRepo(db)
	if err := BootstrapAdmin(users, testLogger(), "alice", "password123", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	u, err := users.GetByUsername("alice")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	tokens := store.NewTokenRepo(db)
	raw, err := newAPIToken()
	if err != nil {
		t.Fatalf("newAPIToken: %v", err)
	}
	if _, err := tokens.Create(store.APIToken{
		TokenHash: hashToken(raw), UserID: u.ID, Label: "ci", Scope: string(domain.ScopeReadWrite), CreatedAt: nowRFC3339(),
	}); err != nil {
		t.Fatalf("Create token: %v", err)
	}
	return newAuthedTestHandler(t, db), tokens, raw
}

func TestAPIWriteRequiresToken(t *testing.T) {
	h, _, _ := apiAuthServer(t)
	// No credentials at all → 401.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/hosts", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth GET = %d, want 401", rec.Code)
	}
	// Write with a bad token → 401.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/hosts", bytes.NewReader([]byte(`{"name":"x"}`)))
	req.Header.Set("Authorization", "Bearer alm_bogus")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad-token POST = %d, want 401", rec.Code)
	}
}

// TestAPIWriteSessionCookieOnlyRejected is the CSRF-critical case: a write
// carrying only a valid session cookie (no bearer token) must be rejected.
// If this ever passed, a browser with a live session would let a third-party
// page silently trigger a state-changing API write via a plain form POST —
// exactly the vector session-only auth on /api would reopen.
func TestAPIWriteSessionCookieOnlyRejected(t *testing.T) {
	h, cookie := authTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/hosts", bytes.NewReader([]byte(`{"name":"nas","type":"physical"}`)))
	req.AddCookie(cookie)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("session-cookie-only write = %d, want 401 (body %s)", rec.Code, rec.Body)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body["error"] != "api writes require a token" {
		t.Fatalf("error = %q, want %q", body["error"], "api writes require a token")
	}
}

func TestAPICreateGetUpdateDelete(t *testing.T) {
	h, _, raw := apiAuthServer(t)
	auth := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+raw) }

	// Create
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/hosts", bytes.NewReader([]byte(`{"name":"nas","type":"physical"}`)))
	auth(req)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d, want 201 (body %s)", rec.Code, rec.Body)
	}
	var created struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if created.ID == 0 || created.Name != "nas" {
		t.Fatalf("created = %+v", created)
	}

	idPath := "/api/hosts/" + strconv.FormatInt(created.ID, 10)

	// Update (full replace)
	rec = httptest.NewRecorder()
	body := []byte(`{"name":"nas2","type":"physical"}`)
	req = httptest.NewRequest(http.MethodPut, idPath, bytes.NewReader(body))
	auth(req)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d, want 200 (body %s)", rec.Code, rec.Body)
	}

	// Delete
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, idPath, nil)
	auth(req)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204", rec.Code)
	}

	// Delete again → 404
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, idPath, nil)
	auth(req)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second DELETE = %d, want 404", rec.Code)
	}
}

// TestAPIReadSessionCookieOnlyAllowed is the positive counterpart to
// TestAPIWriteSessionCookieOnlyRejected: reads are explicitly permitted to fall
// back to the session cookie (see apiAuth), so a logged-in browser/dashboard
// keeps read access without minting a token. This locks down that allowed path,
// not just the write-rejection path.
func TestAPIReadSessionCookieOnlyAllowed(t *testing.T) {
	h, cookie := authTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/hosts", nil)
	req.AddCookie(cookie)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session-cookie-only read = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
}

func TestAPICreateValidationAndBadJSON(t *testing.T) {
	h, _, raw := apiAuthServer(t)
	auth := func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+raw) }

	// Missing required name → 400 (domain.Validate).
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/hosts", bytes.NewReader([]byte(`{"type":"physical"}`)))
	auth(req)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid create = %d, want 400", rec.Code)
	}

	// Malformed JSON → 400.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/hosts", bytes.NewReader([]byte(`{not json`)))
	auth(req)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON = %d, want 400", rec.Code)
	}
}

func TestAPIWriteAttributedToTokenUser(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	users := store.NewUserRepo(db)
	if err := BootstrapAdmin(users, testLogger(), "alice", "password123", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	u, _ := users.GetByUsername("alice")
	tokens := store.NewTokenRepo(db)
	raw, _ := newAPIToken()
	_, _ = tokens.Create(store.APIToken{TokenHash: hashToken(raw), UserID: u.ID, Label: "ci", Scope: string(domain.ScopeReadWrite), CreatedAt: nowRFC3339()})
	h := newAuthedTestHandler(t, db)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/hosts", bytes.NewReader([]byte(`{"name":"nas","type":"physical"}`)))
	req.Header.Set("Authorization", "Bearer "+raw)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d, want 201", rec.Code)
	}

	var actor string
	if err := db.QueryRow(
		`SELECT actor FROM changelog WHERE entity_type='host' ORDER BY id DESC LIMIT 1`,
	).Scan(&actor); err != nil {
		t.Fatalf("query changelog: %v", err)
	}
	if actor != "alice" {
		t.Fatalf("changelog actor = %q, want alice", actor)
	}
}
