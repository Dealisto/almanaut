package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

type outboxTestTransport struct{}

func (outboxTestTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func newOutboxWebServer(t *testing.T, capacity int) (http.Handler, *sql.DB, *recordingDispatcher) {
	t.Helper()
	db := newWebTestDB(t)
	queue := webhook.NewDurableQueue(db, webhook.Options{
		Workers: 1, QueueSize: capacity, MaxAttempts: 100, BaseDelay: time.Hour,
		Client: &http.Client{Transport: outboxTestTransport{}}, Logger: log.New(io.Discard, "", 0),
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := queue.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	if _, err := store.NewWebhookRepo(db).Create(domain.Webhook{URL: "https://receiver.example/events", Secret: "test-secret", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	recorder := &recordingDispatcher{}
	handler := New(Config{
		DB: db, Hosts: store.NewHostRepo(db), Services: store.NewServiceRepo(db),
		Networks: store.NewNetworkRepo(db), Domains: store.NewDomainRepo(db),
		Certificates: store.NewCertificateRepo(db), Backups: store.NewBackupRepo(db),
		Hardware: store.NewHardwareRepo(db), Subscriptions: store.NewSubscriptionRepo(db),
		Accounts: store.NewAccountRepo(db), Sites: store.NewSiteRepo(db),
		Locations: store.NewLocationRepo(db), Racks: store.NewRackRepo(db),
		Contacts: store.NewContactRepo(db), Relationships: store.NewRelationshipRepo(db),
		Tags: store.NewTagRepo(db), VLANs: store.NewVLANRepo(db), Reservations: store.NewReservationRepo(db),
		Logger: log.New(io.Discard, "", 0), Webhooks: webhook.Multi{queue, recorder},
	})
	return handler, db, recorder
}

func TestInventoryWritesPersistWebhookEvents(t *testing.T) {
	h, db, recorder := newOutboxWebServer(t, 10)
	for _, op := range []struct {
		path, name string
	}{
		{"/hosts", "original"}, {"/hosts/1", "updated"}, {"/hosts/1", "updated"}, {"/hosts/1/delete", ""},
	} {
		if rec := postForm(t, h, op.path, url.Values{"name": {op.name}, "type": {"vm"}}); rec.Code != http.StatusSeeOther {
			t.Fatalf("%s: %d %s", op.path, rec.Code, rec.Body)
		}
	}
	if events := recorder.all(); len(events) != 3 {
		t.Fatalf("post-commit events = %d, want create, update, delete", len(events))
	}
	rows, err := db.Query(`SELECT body FROM webhook_outbox`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	events := make(map[string]string)
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Event string       `json:"event"`
			ID    int64        `json:"id"`
			Data  *domain.Host `json:"data"`
		}
		if err := json.Unmarshal(body, &payload); err != nil || payload.ID != 1 {
			t.Fatalf("payload = %s: %v", body, err)
		}
		name := ""
		if payload.Data != nil {
			name = payload.Data.Name
			if payload.Data.ID != 1 {
				t.Fatalf("payload lost stored entity identity: %s", body)
			}
		}
		events[payload.Event] = name
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events["created"] != "original" || events["updated"] != "updated" || events["deleted"] != "" {
		t.Fatalf("queued events changed or disappeared: %v", events)
	}
	adminHandler := newAuthedTestHandler(t, db)
	admin := seedUserAndLogin(t, adminHandler, db, "administrator", domain.RoleAdmin)
	page := getWith(t, adminHandler, admin, "/webhooks")
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "<strong>3</strong> pending deliveries") {
		t.Fatalf("backlog not visible to administrator: %d %s", page.Code, page.Body)
	}
}

func TestOutboxFailureRollsBackInventoryAndHistory(t *testing.T) {
	for _, action := range []string{"create", "update", "delete"} {
		t.Run(action, func(t *testing.T) {
			h, db, recorder := newOutboxWebServer(t, 10)
			if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "original", Type: "vm"}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`CREATE TRIGGER reject_outbox BEFORE INSERT ON webhook_outbox BEGIN SELECT RAISE(ABORT, 'outbox unavailable'); END`); err != nil {
				t.Fatal(err)
			}
			path := map[string]string{"create": "/hosts", "update": "/hosts/1", "delete": "/hosts/1/delete"}[action]
			response := postForm(t, h, path, url.Values{"name": {"changed"}, "type": {"vm"}})
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("write succeeded without durable event: %d %s", response.Code, response.Body)
			}
			hosts, err := store.NewHostRepo(db).List()
			if err != nil || len(hosts) != 1 || hosts[0].Name != "original" {
				t.Fatalf("failed write changed inventory: %v %v", hosts, err)
			}
			var history int
			if err := db.QueryRow(`SELECT COUNT(*) FROM changelog`).Scan(&history); err != nil || history != 0 {
				t.Fatalf("failed write left history: %d %v", history, err)
			}
			if len(recorder.all()) != 0 {
				t.Fatal("rolled-back event reached a post-commit integration")
			}
		})
	}
}

func TestWebhookBacklogReturnsRetryableErrorWithoutMutation(t *testing.T) {
	h, db, recorder := newOutboxWebServer(t, 1)
	if rec := postForm(t, h, "/hosts", url.Values{"name": {"original"}, "type": {"vm"}}); rec.Code != http.StatusSeeOther {
		t.Fatal(rec.Code, rec.Body)
	}
	response := postForm(t, h, "/hosts", url.Values{"name": {"blocked"}, "type": {"vm"}})
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") == "" || !strings.Contains(response.Body.String(), "no changes were saved") {
		t.Fatalf("backlog error not actionable: %d %s", response.Code, response.Body)
	}
	api := httptest.NewRecorder()
	h.ServeHTTP(api, httptest.NewRequest(http.MethodPut, "/api/hosts/1", strings.NewReader(`{"name":"blocked","type":"vm"}`)))
	if api.Code != http.StatusServiceUnavailable || api.Header().Get("Retry-After") == "" || !json.Valid(api.Body.Bytes()) {
		t.Fatalf("API backlog error not retryable: %d %s", api.Code, api.Body)
	}
	hosts, err := store.NewHostRepo(db).List()
	if err != nil || len(hosts) != 1 || hosts[0].Name != "original" || len(recorder.all()) != 1 {
		t.Fatalf("full outbox lost atomicity: %v %v events=%v", hosts, err, recorder.all())
	}
}
