package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/go-chi/chi/v5"
)

type pausedWebhookForm struct {
	reader  io.Reader
	started chan struct{}
	release chan struct{}
}

func (b *pausedWebhookForm) Read(p []byte) (int, error) {
	if b.started != nil {
		close(b.started)
		b.started = nil
		<-b.release
	}
	return b.reader.Read(p)
}

func TestWebhookEditCannotRestoreConcurrentDisabledStateOrSecret(t *testing.T) {
	db := newWebTestDB(t)
	repo := store.NewWebhookRepo(db)
	id, err := repo.Create(domain.Webhook{URL: "https://old.example/hook", Secret: "old-secret", Enabled: true, CreatedAt: "original-time"})
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	router.Post("/webhooks/{id}", updateWebhook(repo))
	started, release := make(chan struct{}), make(chan struct{})
	body := &pausedWebhookForm{reader: strings.NewReader("url=https://new.example/hook&entity_types=service&events=updated"), started: started, release: release}
	req := httptest.NewRequest(http.MethodPost, "/webhooks/1", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); router.ServeHTTP(rec, req) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("edit handler did not read the form")
	}
	// The old handler has already read the complete row at this point. Another
	// administrator disables the receiver and rotates its secret before the
	// form is submitted. An edit only owns URL and filters, never these fields.
	err = repo.Update(domain.Webhook{ID: id, URL: "https://old.example/hook", Secret: "rotated-secret", Enabled: false})
	close(release)
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("edit status=%d body=%s", rec.Code, rec.Body)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.Secret != "rotated-secret" || got.CreatedAt != "original-time" {
		t.Fatalf("edit restored stale protected settings: %+v", got)
	}
	if got.URL != "https://new.example/hook" || len(got.EntityTypes) != 1 || got.EntityTypes[0] != "service" || len(got.Events) != 1 || got.Events[0] != "updated" {
		t.Fatalf("edit did not save submitted settings: %+v", got)
	}
}

func TestWebhookConcurrentTogglesDoNotLoseUpdates(t *testing.T) {
	db := newWebTestDB(t)
	repo := store.NewWebhookRepo(db)
	id, err := repo.Create(domain.Webhook{URL: "https://receiver.example/hook", Secret: "keep-secret", Enabled: true, EntityTypes: []string{"host"}, Events: []string{"created"}})
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	router.Post("/webhooks/{id}/toggle", toggleWebhook(repo))
	const toggles = 21
	start, results := make(chan struct{}), make(chan int, toggles)
	for range toggles {
		go func() {
			<-start
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhooks/1/toggle", nil))
			results <- rec.Code
		}()
	}
	close(start)
	for range toggles {
		if status := <-results; status != http.StatusSeeOther {
			t.Errorf("toggle status=%d", status)
		}
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Fatal("an odd number of concurrent toggles lost updates")
	}
	if got.URL != "https://receiver.example/hook" || got.Secret != "keep-secret" || len(got.EntityTypes) != 1 || got.EntityTypes[0] != "host" || len(got.Events) != 1 || got.Events[0] != "created" {
		t.Fatalf("toggle modified endpoint settings: %+v", got)
	}
}
