package web

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

// This legacy dispatcher reads through the independent DB handle, proving that
// discovery still notifies integrations only after the inventory/history commit.
type committedDiscoveryRecorder struct {
	recordingDispatcher
	t  *testing.T
	db *sql.DB
}

func (r *committedDiscoveryRecorder) Dispatch(events ...webhook.Event) {
	for _, event := range events {
		assertDiscoveryEvent(r.t, r.db, event, event.Type, webhook.ActionCreated, domain.ActionImport)
	}
	r.recordingDispatcher.Dispatch(events...)
}

func discoveryDurableServer(t *testing.T, capacity int, sources ...DiscoverySource) (http.Handler, *sql.DB, *committedDiscoveryRecorder) {
	t.Helper()
	db := newWebTestDB(t)
	queue := webhook.NewDurableQueue(db, webhook.Options{
		Workers: 1, QueueSize: capacity, MaxAttempts: 100, BaseDelay: time.Hour,
		Client: &http.Client{Transport: outboxTestTransport{}}, Logger: testLogger(),
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := queue.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	if _, err := store.NewWebhookRepo(db).Create(domain.Webhook{URL: "https://receiver.test", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	recorder := &committedDiscoveryRecorder{t: t, db: db}
	srv := New(Config{
		Hosts: store.NewHostRepo(db), Services: store.NewServiceRepo(db), Networks: store.NewNetworkRepo(db),
		Domains: store.NewDomainRepo(db), Certificates: store.NewCertificateRepo(db), Backups: store.NewBackupRepo(db),
		Hardware: store.NewHardwareRepo(db), Subscriptions: store.NewSubscriptionRepo(db), Accounts: store.NewAccountRepo(db),
		Sites: store.NewSiteRepo(db), Locations: store.NewLocationRepo(db), Racks: store.NewRackRepo(db), Contacts: store.NewContactRepo(db),
		Relationships: store.NewRelationshipRepo(db), Tags: store.NewTagRepo(db), VLANs: store.NewVLANRepo(db), Reservations: store.NewReservationRepo(db),
		DB: db, Logger: testLogger(), DiscoverySources: sources, NetScan: fakeNetworkScanner{}, NetOpts: NetDiscoveryOptions{Enabled: true},
		Webhooks: webhook.Multi{queue, recorder},
	})
	return srv, db, recorder
}

func TestDiscoveryNetworkScopedStaging(t *testing.T) {
	for _, capacity := range []int{1, 2} {
		t.Run(itoa(int64(capacity)), func(t *testing.T) {
			srv, db, recorder := discoveryDurableServer(t, capacity)
			form := url.Values{"type": {"physical"}, "host": {"192.0.2.1|first|22", "192.0.2.1|duplicate|22", "192.0.2.2|second|443", "invalid"}}
			response := postForm(t, srv, "/discovery/network/import", form)
			want := 2
			if capacity == 1 {
				want = 0
				if response.Code != http.StatusServiceUnavailable {
					t.Fatalf("quota response=%d body=%s", response.Code, response.Body)
				}
			} else {
				assertImportCount(t, response, "added", 2)
			}
			assertBatchEventCounts(t, db, &recorder.recordingDispatcher, want)
			hosts, err := store.NewHostRepo(db).List()
			if err != nil || len(hosts) != want {
				t.Fatalf("hosts=%+v err=%v", hosts, err)
			}
			if capacity == 1 {
				return
			}
			assertImportCount(t, postForm(t, srv, "/discovery/network/import", form), "added", 0)
			form["host"] = []string{"192.0.2.3|third|22"}
			if response := postForm(t, srv, "/discovery/network/import", form); response.Code != http.StatusServiceUnavailable {
				t.Fatalf("next request lost committed budget: %d %s", response.Code, response.Body)
			}
			assertBatchEventCounts(t, db, &recorder.recordingDispatcher, 2)
			hosts, err = store.NewHostRepo(db).List()
			if err != nil || len(hosts) != 2 {
				t.Fatalf("rejected import changed hosts=%+v err=%v", hosts, err)
			}
		})
	}
}

func TestDiscoverySourceScopedStagingRollback(t *testing.T) {
	for _, failure := range []string{"capacity", "relationship"} {
		t.Run(failure, func(t *testing.T) {
			source := DiscoverySource{ID: "lab", Name: "Containers", Kind: "docker", Docker: fakeScanner{containers: []discovery.Container{{ID: "a", Name: "first"}, {ID: "b", Name: "second"}}}}
			capacity, wantStatus := 1, http.StatusServiceUnavailable
			if failure == "relationship" {
				capacity, wantStatus = 2, http.StatusInternalServerError
			}
			srv, db, recorder := discoveryDurableServer(t, capacity, source)
			if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "owner", Type: "physical"}); err != nil {
				t.Fatal(err)
			}
			if failure == "relationship" {
				// Both deliveries and source bindings have been staged by the
				// time relationship creation runs; all must disappear on failure.
				if _, err := db.Exec(`CREATE TRIGGER fail_discovery_relation BEFORE INSERT ON relationships BEGIN SELECT RAISE(ABORT,'injected relationship failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			response := postForm(t, srv, source.Path()+"/import", url.Values{"id": {"a", "b", "b"}, "host": {"1"}})
			if response.Code != wantStatus {
				t.Fatalf("failure status=%d want=%d body=%s", response.Code, wantStatus, response.Body)
			}
			assertBatchEventCounts(t, db, &recorder.recordingDispatcher, 0)
			for _, table := range []string{"services", "discovery_bindings", "relationships"} {
				var count int
				if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
					t.Fatalf("rollback left %s count=%d err=%v", table, count, err)
				}
			}
		})
	}
}

func TestDiscoverySourceScopedStagingPreservesIdentityAndLinks(t *testing.T) {
	scanner := &fakeScanner{containers: []discovery.Container{{ID: "a", Name: "first"}, {ID: "b", Name: "second"}}}
	sources := []DiscoverySource{
		{ID: "first", Name: "First engine", Kind: "docker", Docker: scanner},
		{ID: "second", Name: "Second engine", Kind: "docker", Docker: scanner},
	}
	srv, db, recorder := discoveryDurableServer(t, 4, sources...)
	if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "owner", Type: "physical"}); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"id": {"a", "b", "b"}, "host": {"1"}}
	for _, source := range sources {
		response := postForm(t, srv, source.Path()+"/import", form)
		assertImportCount(t, response, "added", 2)
		assertImportCount(t, response, "linked", 2)
	}
	bindings, err := store.NewDiscoveryBindingRepo(db).List()
	if err != nil || len(bindings) != 4 {
		t.Fatalf("bindings=%+v err=%v", bindings, err)
	}
	seen := make(map[int64]bool)
	counts := make(map[string]int)
	for _, binding := range bindings {
		if seen[binding.EntityID] || binding.EntityType != "service" {
			t.Fatalf("different source identities merged: %+v", bindings)
		}
		seen[binding.EntityID] = true
		counts[binding.Source]++
	}
	if counts[sources[0].Key()] != 2 || counts[sources[1].Key()] != 2 {
		t.Fatalf("source bindings changed: %v", counts)
	}
	for _, source := range sources {
		response := postForm(t, srv, source.Path()+"/import", form)
		assertImportCount(t, response, "added", 0)
		assertImportCount(t, response, "linked", 0)
	}
	scanner.containers = append(scanner.containers, discovery.Container{ID: "c", Name: "third"})
	response := postForm(t, srv, sources[0].Path()+"/import", url.Values{"id": {"c"}, "host": {"1"}})
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("later import ignored full backlog: %d %s", response.Code, response.Body)
	}
	assertBatchEventCounts(t, db, &recorder.recordingDispatcher, 4)
	for _, table := range []string{"services", "discovery_bindings", "relationships"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 4 {
			t.Fatalf("repeat/rejected import changed %s count=%d err=%v", table, count, err)
		}
	}
}
