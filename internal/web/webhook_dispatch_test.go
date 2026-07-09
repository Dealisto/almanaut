package web

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

type recordingDispatcher struct {
	mu     sync.Mutex
	events []webhook.Event
}

func (r *recordingDispatcher) Dispatch(events ...webhook.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, events...)
}

func (r *recordingDispatcher) all() []webhook.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]webhook.Event(nil), r.events...)
}

func newTestServerWH(t *testing.T) (http.Handler, *recordingDispatcher) {
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
	rec := &recordingDispatcher{}
	srv := New(Config{
		Hosts: store.NewHostRepo(db), Services: store.NewServiceRepo(db), Networks: store.NewNetworkRepo(db),
		Domains: store.NewDomainRepo(db), Certificates: store.NewCertificateRepo(db), Backups: store.NewBackupRepo(db),
		Hardware: store.NewHardwareRepo(db), Subscriptions: store.NewSubscriptionRepo(db), Accounts: store.NewAccountRepo(db),
		Sites: store.NewSiteRepo(db), Locations: store.NewLocationRepo(db), Racks: store.NewRackRepo(db),
		Contacts:      store.NewContactRepo(db),
		Relationships: store.NewRelationshipRepo(db), Tags: store.NewTagRepo(db), VLANs: store.NewVLANRepo(db), Reservations: store.NewReservationRepo(db), DB: db,
		Logger: log.New(io.Discard, "", 0),
		Docker: fakeScanner{}, NetScan: fakeNetworkScanner{}, NetOpts: NetDiscoveryOptions{}, Proxmox: fakeProxmoxScanner{}, PVEOpts: ProxmoxOptions{},
		Webhooks: rec,
	})
	return srv, rec
}

func TestWebhookEventOnCreate(t *testing.T) {
	srv, rec := newTestServerWH(t)
	postForm(t, srv, "/hosts", url.Values{"name": {"box"}, "type": {"vm"}})

	evs := rec.all()
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	if evs[0].Type != "host" || evs[0].Action != webhook.ActionCreated {
		t.Errorf("event = %+v", evs[0])
	}
	var data map[string]any
	if err := json.Unmarshal(evs[0].Data, &data); err != nil {
		t.Fatalf("data: %v", err)
	}
	if data["name"] != "box" {
		t.Errorf("data.name = %v, want box", data["name"])
	}
}

func TestWebhookNoEventOnNoopUpdate(t *testing.T) {
	srv, rec := newTestServerWH(t)
	postForm(t, srv, "/hosts", url.Values{"name": {"box"}, "type": {"vm"}})
	// Re-submit identical values: typed diff is empty -> no changelog, no event.
	postForm(t, srv, "/hosts/1", url.Values{"name": {"box"}, "type": {"vm"}})

	evs := rec.all()
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1 (create only, no-op update fires nothing)", len(evs))
	}
}

func TestWebhookDeleteEventHasNoData(t *testing.T) {
	srv, rec := newTestServerWH(t)
	postForm(t, srv, "/hosts", url.Values{"name": {"box"}, "type": {"vm"}})
	postForm(t, srv, "/hosts/1/delete", nil)

	evs := rec.all()
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2 (create+delete)", len(evs))
	}
	del := evs[1]
	if del.Action != webhook.ActionDeleted || del.Type != "host" {
		t.Errorf("delete event = %+v", del)
	}
	if del.Data != nil {
		t.Errorf("delete event Data = %q, want nil", del.Data)
	}
}
