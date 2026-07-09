package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func getWith(t *testing.T, h http.Handler, cookie *http.Cookie, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, path, nil), cookie))
	return rec
}

func csrfPostRec(t *testing.T, h http.Handler, cookie *http.Cookie, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec0 := httptest.NewRecorder()
	h.ServeHTTP(rec0, withCookie(httptest.NewRequest(http.MethodGet, "/", nil), cookie))
	csrf := csrfCookie(rec0.Result().Cookies())
	form := strings.NewReader(body + "&" + csrfFieldName + "=" + csrf.Value)
	req := httptest.NewRequest(http.MethodPost, path, form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	req.AddCookie(csrf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestWebhooksPageRequiresAdmin(t *testing.T) {
	db := rbacDB(t)
	h := newAuthedTestHandler(t, db)
	viewer := seedUserAndLogin(t, h, db, "viewer", domain.RoleViewer)
	if rec := getWith(t, h, viewer, "/webhooks"); rec.Code != http.StatusForbidden {
		t.Fatalf("GET /webhooks as viewer = %d, want 403", rec.Code)
	}
}

func TestCreateWebhookGeneratesSecretShownOnce(t *testing.T) {
	db := rbacDB(t)
	h := newAuthedTestHandler(t, db)
	admin := seedUserAndLogin(t, h, db, "admin", domain.RoleAdmin)

	// Create re-renders the page (200) with the generated secret revealed once.
	rec := csrfPostRec(t, h, admin, "/webhooks", "url=https://ci.example/hook&events=created&entity_types=host")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /webhooks = %d, want 200 (reveal-once render)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "whsec_") {
		t.Fatalf("created page should reveal the generated secret once; body: %s", body)
	}

	// It is persisted and listed, but the secret is NOT shown on a plain list load.
	hooks, err := store.NewWebhookRepo(db).List()
	if err != nil || len(hooks) != 1 {
		t.Fatalf("List = %v, %v; want 1 webhook", hooks, err)
	}
	if hooks[0].URL != "https://ci.example/hook" || !hooks[0].Enabled {
		t.Errorf("stored webhook = %+v", hooks[0])
	}
	if len(hooks[0].Events) != 1 || hooks[0].Events[0] != "created" {
		t.Errorf("events = %v", hooks[0].Events)
	}
	if len(hooks[0].EntityTypes) != 1 || hooks[0].EntityTypes[0] != "host" {
		t.Errorf("entity_types = %v", hooks[0].EntityTypes)
	}
	list := getWith(t, h, admin, "/webhooks")
	if strings.Contains(list.Body.String(), hooks[0].Secret) {
		t.Errorf("plain list page must not display the stored secret")
	}
}

func TestCreateWebhookRejectsBadURL(t *testing.T) {
	db := rbacDB(t)
	h := newAuthedTestHandler(t, db)
	admin := seedUserAndLogin(t, h, db, "admin", domain.RoleAdmin)
	rec := csrfPostRec(t, h, admin, "/webhooks", "url=not-a-url&events=updated&entity_types=host")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "http") {
		t.Fatalf("bad URL should re-render with an error; got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `value="not-a-url"`) {
		t.Fatalf("create form should preserve the submitted URL; body: %s", body)
	}
	if !strings.Contains(body, `value="updated" checked`) {
		t.Fatalf("create form should pre-check the submitted event filter; body: %s", body)
	}
	hooks, _ := store.NewWebhookRepo(db).List()
	if len(hooks) != 0 {
		t.Errorf("no webhook should be created on validation error, got %d", len(hooks))
	}
}

func TestEditWebhookUpdatesURLAndFilters(t *testing.T) {
	db := rbacDB(t)
	h := newAuthedTestHandler(t, db)
	admin := seedUserAndLogin(t, h, db, "admin", domain.RoleAdmin)
	repo := store.NewWebhookRepo(db)
	id, err := repo.Create(domain.Webhook{
		URL: "https://old.example/h", Secret: "whsec_keep", Enabled: true,
		EntityTypes: []string{"host"}, Events: []string{"created"}, CreatedAt: nowRFC3339(),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	// The edit form pre-checks current filters.
	rec := getWith(t, h, admin, "/webhooks/1/edit")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "https://old.example/h") {
		t.Fatalf("edit form should render prefilled; got %d", rec.Code)
	}

	// Update URL + filters; secret and created_at must be preserved.
	if code := csrfPost(t, h, admin, "/webhooks/1", "url=https://new.example/h&events=updated&events=deleted&entity_types=service"); code != http.StatusSeeOther {
		t.Fatalf("update = %d, want 303", code)
	}
	got, _ := repo.Get(id)
	if got.URL != "https://new.example/h" {
		t.Errorf("URL = %q, want updated", got.URL)
	}
	if got.Secret != "whsec_keep" {
		t.Errorf("secret changed on edit: %q", got.Secret)
	}
	if got.CreatedAt == "" {
		t.Errorf("created_at should be preserved, got empty")
	}
	if len(got.Events) != 2 || got.Events[0] != "updated" || got.Events[1] != "deleted" {
		t.Errorf("events = %v", got.Events)
	}
	if len(got.EntityTypes) != 1 || got.EntityTypes[0] != "service" {
		t.Errorf("entity_types = %v", got.EntityTypes)
	}
	if !got.Enabled {
		t.Errorf("Enabled flipped unexpectedly on edit")
	}
}

func TestEditWebhookNotFound(t *testing.T) {
	db := rbacDB(t)
	h := newAuthedTestHandler(t, db)
	admin := seedUserAndLogin(t, h, db, "admin", domain.RoleAdmin)
	if rec := getWith(t, h, admin, "/webhooks/999/edit"); rec.Code != http.StatusNotFound {
		t.Fatalf("edit missing = %d, want 404", rec.Code)
	}
}

func TestUpdateWebhookRejectsBadURLAndPreservesForm(t *testing.T) {
	db := rbacDB(t)
	h := newAuthedTestHandler(t, db)
	admin := seedUserAndLogin(t, h, db, "admin", domain.RoleAdmin)
	repo := store.NewWebhookRepo(db)
	id, err := repo.Create(domain.Webhook{
		URL: "https://old.example/h", Secret: "whsec_keep", Enabled: true,
		EntityTypes: []string{"host"}, Events: []string{"created"}, CreatedAt: nowRFC3339(),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	rec := csrfPostRec(t, h, admin, "/webhooks/1", "url=not-a-url&events=updated&entity_types=service")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "http") {
		t.Fatalf("bad URL should re-render edit page with error; got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `value="not-a-url"`) {
		t.Fatalf("edit form should preserve the submitted URL; body: %s", body)
	}
	if !strings.Contains(body, `value="updated" checked`) {
		t.Fatalf("edit form should pre-check the submitted event filter; body: %s", body)
	}

	got, _ := repo.Get(id)
	if got.URL != "https://old.example/h" {
		t.Errorf("URL changed on validation error: %q", got.URL)
	}
	if got.Secret != "whsec_keep" {
		t.Errorf("secret changed on validation error: %q", got.Secret)
	}
}

func TestToggleAndDeleteWebhook(t *testing.T) {
	db := rbacDB(t)
	h := newAuthedTestHandler(t, db)
	admin := seedUserAndLogin(t, h, db, "admin", domain.RoleAdmin)
	repo := store.NewWebhookRepo(db)
	id, err := repo.Create(domain.Webhook{URL: "https://x.example/h", Secret: "whsec_x", Enabled: true, CreatedAt: nowRFC3339()})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	if code := csrfPost(t, h, admin, "/webhooks/1/toggle", ""); code != http.StatusSeeOther {
		t.Fatalf("toggle = %d, want 303", code)
	}
	got, _ := repo.Get(id)
	if got.Enabled {
		t.Errorf("after toggle Enabled = true, want false")
	}

	if code := csrfPost(t, h, admin, "/webhooks/1/delete", ""); code != http.StatusSeeOther {
		t.Fatalf("delete = %d, want 303", code)
	}
	if _, err := repo.Get(id); err != store.ErrNotFound {
		t.Errorf("after delete Get = %v, want ErrNotFound", err)
	}
}
