package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"testing"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

// The staging probe persists through the caller's transaction, so tests catch
// both dispatch-before-commit and event records surviving an inventory rollback.
type discoveryStagingProbe struct {
	recordingDispatcher
	t        *testing.T
	db       *sql.DB
	fail     bool
	attempts int
}

func (p *discoveryStagingProbe) Stage(tx *sql.Tx, events ...webhook.Event) error {
	p.attempts += len(events)
	for _, event := range events {
		if _, err := tx.Exec(`INSERT INTO discovery_event_probe (entity_type, entity_id, action) VALUES (?, ?, ?)`, event.Type, event.ID, event.Action); err != nil {
			return err
		}
	}
	if p.fail {
		return errors.New("injected staging failure")
	}
	return nil
}

func (p *discoveryStagingProbe) Dispatch(events ...webhook.Event) {
	for _, event := range events {
		var present bool
		if err := p.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM discovery_event_probe WHERE entity_type=? AND entity_id=? AND action=?)`, event.Type, event.ID, event.Action).Scan(&present); err != nil || !present {
			p.t.Errorf("dispatched before staged event committed: %+v, %v", event, err)
		}
		var item any
		var err error
		if event.Type == "host" {
			item, err = store.NewHostRepo(p.db).Get(event.ID)
		} else {
			item, err = store.NewServiceRepo(p.db).Get(event.ID)
		}
		if err != nil {
			p.t.Errorf("dispatched before entity committed: %v", err)
		}
		body, err := json.Marshal(item)
		if err != nil || string(body) != string(event.Data) {
			p.t.Errorf("event does not describe committed entity: %s / %s, %v", event.Data, body, err)
		}
	}
	p.recordingDispatcher.Dispatch(events...)
}

func discoveryWebhookServer(t *testing.T, sources []DiscoverySource) (http.Handler, *sql.DB, *discoveryStagingProbe) {
	t.Helper()
	db := newWebTestDB(t)
	if _, err := db.Exec(`CREATE TABLE discovery_event_probe (entity_type TEXT, entity_id INTEGER, action TEXT)`); err != nil {
		t.Fatal(err)
	}
	probe := &discoveryStagingProbe{t: t, db: db}
	srv := New(Config{
		Hosts: store.NewHostRepo(db), Services: store.NewServiceRepo(db), Networks: store.NewNetworkRepo(db),
		Domains: store.NewDomainRepo(db), Certificates: store.NewCertificateRepo(db), Backups: store.NewBackupRepo(db),
		Hardware: store.NewHardwareRepo(db), Subscriptions: store.NewSubscriptionRepo(db), Accounts: store.NewAccountRepo(db),
		Sites: store.NewSiteRepo(db), Locations: store.NewLocationRepo(db), Racks: store.NewRackRepo(db), Contacts: store.NewContactRepo(db),
		Relationships: store.NewRelationshipRepo(db), Tags: store.NewTagRepo(db), VLANs: store.NewVLANRepo(db), Reservations: store.NewReservationRepo(db),
		DB: db, Logger: testLogger(), DiscoverySources: sources, NetScan: fakeNetworkScanner{}, NetOpts: NetDiscoveryOptions{Enabled: true}, Webhooks: probe,
	})
	return srv, db, probe
}

func assertDiscoveryStagedCount(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM discovery_event_probe`).Scan(&count); err != nil || count != want {
		t.Fatalf("staged events = %d, error=%v, want %d", count, err, want)
	}
}

func assertDiscoveryEvent(t *testing.T, db *sql.DB, event webhook.Event, typ, action, historyAction string) {
	t.Helper()
	if event.Type != typ || event.Action != action || event.ID <= 0 {
		t.Fatalf("unexpected event: %+v", event)
	}
	history, err := store.NewChangelogRepo(db).ListForEntity(typ, event.ID)
	if err != nil || len(history) != 1 || history[0].Action != historyAction || history[0].CreatedAt != event.Timestamp || history[0].Actor != event.Actor {
		t.Fatalf("event/history disagree: %+v / %+v, %v", event, history, err)
	}
}

func TestDiscoveryImportWebhooksNewEntitiesOnly(t *testing.T) {
	for _, kind := range []string{"docker", "podman"} {
		t.Run(kind, func(t *testing.T) {
			source := DiscoverySource{ID: "default", Name: "Containers", Kind: kind, Docker: fakeScanner{containers: []discovery.Container{
				{ID: "adopted", Name: "manual"}, {ID: "new", Name: "new-service"}, {ID: "invalid", Name: ""},
			}}}
			srv, db, probe := discoveryWebhookServer(t, []DiscoverySource{source})
			if _, err := store.NewServiceRepo(db).Create(domain.Service{Name: "manual", Kind: "container", Notes: "Keep"}); err != nil {
				t.Fatal(err)
			}
			form := url.Values{"id": {"adopted", "new", "new", "invalid", "absent"}}
			assertImportCount(t, postForm(t, srv, source.Path()+"/import", form), "added", 1)
			events := probe.all()
			if len(events) != 1 {
				t.Fatalf("events = %+v, want new entity only", events)
			}
			assertDiscoveryEvent(t, db, events[0], "service", webhook.ActionCreated, domain.ActionImport)
			// The canonical source path must use the same dispatcher as the
			// legacy /discovery/docker alias, without re-emitting bound entities.
			assertImportCount(t, postForm(t, srv, "/discovery/sources/"+kind+"/default/import", form), "added", 0)
			if len(probe.all()) != 1 {
				t.Fatal("repeat import emitted an event")
			}
			assertDiscoveryStagedCount(t, db, 1)
		})
	}
}

func TestDiscoveryHostImportWebhooks(t *testing.T) {
	for _, source := range []DiscoverySource{
		{ID: "lab", Name: "PVE", Kind: "proxmox", Proxmox: fakeProxmoxScanner{res: []discovery.ProxmoxResource{{ID: "qemu/101", Type: "qemu", VMID: 101, Name: "guest", Node: "node", MaxCPU: 2}}}},
		{ID: "lab", Name: "XO", Kind: "xen-orchestra", XenOrchestra: &fakeXenOrchestraScanner{resources: []discovery.XenOrchestraResource{{ID: "vm", Type: "vm", Name: "guest", CPU: 2}}}},
	} {
		t.Run(source.Kind, func(t *testing.T) {
			srv, db, probe := discoveryWebhookServer(t, []DiscoverySource{source})
			assertImportCount(t, postForm(t, srv, source.Path()+"/import", url.Values{"id": {"qemu/101", "vm"}}), "added", 1)
			events := probe.all()
			if len(events) != 1 {
				t.Fatalf("events = %+v", events)
			}
			assertDiscoveryEvent(t, db, events[0], "host", webhook.ActionCreated, domain.ActionImport)
			assertDiscoveryStagedCount(t, db, 1)
		})
	}
}

func TestDiscoveryNetworkImportWebhooks(t *testing.T) {
	srv, db, probe := discoveryWebhookServer(t, nil)
	form := url.Values{"type": {"physical"}, "host": {"192.0.2.1|new-host|22", "192.0.2.1|duplicate|22", "invalid", "bad-ip|bad-host|22"}}
	assertImportCount(t, postForm(t, srv, "/discovery/network/import", form), "added", 1)
	events := probe.all()
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
	assertDiscoveryEvent(t, db, events[0], "host", webhook.ActionCreated, domain.ActionImport)
	assertImportCount(t, postForm(t, srv, "/discovery/network/import", form), "added", 0)
	if len(probe.all()) != 1 {
		t.Fatal("repeat network import emitted an event")
	}
	assertDiscoveryStagedCount(t, db, 1)
}

func TestDiscoveryImportWebhookRollback(t *testing.T) {
	for _, scenario := range []string{"network-stage", "source-stage", "later-binding"} {
		t.Run(scenario, func(t *testing.T) {
			source := DiscoverySource{ID: "lab", Name: "Containers", Kind: "docker", Docker: fakeScanner{containers: []discovery.Container{{ID: "new", Name: "new-service"}}}}
			srv, db, probe := discoveryWebhookServer(t, []DiscoverySource{source})
			path, form := source.Path()+"/import", url.Values{"id": {"new"}}
			probe.fail = scenario != "later-binding"
			if scenario == "network-stage" {
				path, form = "/discovery/network/import", url.Values{"type": {"vm"}, "host": {"192.0.2.1|new-host|22"}}
			}
			if scenario == "later-binding" {
				if _, err := db.Exec(`CREATE TRIGGER fail_discovery_binding BEFORE INSERT ON discovery_bindings BEGIN SELECT RAISE(ABORT, 'injected binding failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			if rec := postForm(t, srv, path, form); rec.Code != http.StatusInternalServerError {
				t.Fatalf("rollback status = %d: %s", rec.Code, rec.Body.String())
			}
			if probe.attempts != 1 || len(probe.all()) != 0 {
				t.Fatalf("staged %d, dispatched %v", probe.attempts, probe.all())
			}
			assertDiscoveryStagedCount(t, db, 0)
			for _, table := range []string{"hosts", "services", "changelog", "discovery_bindings"} {
				var count int
				if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
					t.Fatalf("rollback left %s count=%d, err=%v", table, count, err)
				}
			}
		})
	}
}

func TestDiscoveryContainerImportRejectsDeletedHost(t *testing.T) {
	source := DiscoverySource{ID: "lab", Name: "Containers", Kind: "docker", Docker: fakeScanner{containers: []discovery.Container{{ID: "new", Name: "new-service"}}}}
	srv, db, probe := discoveryWebhookServer(t, []DiscoverySource{source})
	hosts := store.NewHostRepo(db)
	id, err := hosts.Create(domain.Host{Name: "selected-host", Type: "physical"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a host removed after the user opened the discovery form.
	getBody(t, srv, source.Path())
	if err := hosts.Delete(id); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"id": {"new"}, "host": {strconv.FormatInt(id, 10)}}
	if rec := postForm(t, srv, source.Path()+"/import", form); rec.Code != http.StatusBadRequest {
		t.Fatalf("deleted host accepted: status=%d, body=%s", rec.Code, rec.Body.String())
	}
	for _, table := range []string{"services", "relationships", "changelog", "discovery_bindings"} {
		if n := tableCount(t, db, table); n != 0 {
			t.Fatalf("invalid host import left %d %s rows", n, table)
		}
	}
	if len(probe.all()) != 0 {
		t.Fatal("invalid host import dispatched events")
	}
	assertDiscoveryStagedCount(t, db, 0)

	id, err = hosts.Create(domain.Host{Name: "valid-host", Type: "physical"})
	if err != nil {
		t.Fatal(err)
	}
	form.Set("host", strconv.FormatInt(id, 10))
	assertImportCount(t, postForm(t, srv, source.Path()+"/import", form), "linked", 1)
	links, err := store.NewRelationshipRepo(db).List()
	if err != nil || len(links) != 1 || links[0].ToID != id {
		t.Fatalf("valid host not linked: %+v, %v", links, err)
	}
	if len(probe.all()) != 1 {
		t.Fatalf("valid host import events: %+v", probe.all())
	}
}

func TestDiscoveryAcceptedChangeWebhooks(t *testing.T) {
	for _, kind := range []string{"service", "host"} {
		for _, fail := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "/success", true: "/rollback"}[fail], func(t *testing.T) {
				source := DiscoverySource{ID: "default", Name: "Containers", Kind: "docker", Docker: fakeScanner{containers: []discovery.Container{{ID: "external", Name: "observed"}}}}
				if kind == "host" {
					source = DiscoverySource{ID: "lab", Name: "XO", Kind: "xen-orchestra", XenOrchestra: &fakeXenOrchestraScanner{resources: []discovery.XenOrchestraResource{{ID: "external", Type: "vm", Name: "observed"}}}}
				}
				srv, db, probe := discoveryWebhookServer(t, []DiscoverySource{source})
				var id int64
				var err error
				if kind == "service" {
					id, err = store.NewServiceRepo(db).Create(domain.Service{Name: "manual", Kind: "container"})
				} else {
					id, err = store.NewHostRepo(db).Create(domain.Host{Name: "manual", Type: "vm"})
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := store.NewDiscoveryBindingRepo(db).Create(domain.DiscoveryBinding{Source: source.Key(), ExternalID: "external", EntityType: kind, EntityID: id}); err != nil {
					t.Fatal(err)
				}
				form := discoveryChangesForm(t, getBody(t, srv, source.Path()+"/changes"), "external", "name")
				probe.fail = fail
				rec := postForm(t, srv, "/discovery/sources/"+source.Kind+"/"+source.ID+"/changes", form)
				if fail {
					if rec.Code != http.StatusInternalServerError || len(probe.all()) != 0 {
						t.Fatalf("rollback = %d, events=%v", rec.Code, probe.all())
					}
					assertDiscoveryStagedCount(t, db, 0)
					var name string
					table := "hosts"
					if kind == "service" {
						table = "services"
					}
					if err := db.QueryRow("SELECT name FROM "+table+" WHERE id=?", id).Scan(&name); err != nil || name != "manual" {
						t.Fatalf("failed update survived: %s, %v", name, err)
					}
					history, err := store.NewChangelogRepo(db).ListForEntity(kind, id)
					if err != nil || len(history) != 0 {
						t.Fatalf("rollback history: %+v, %v", history, err)
					}
					return
				}
				if rec.Code != http.StatusOK {
					t.Fatalf("apply = %d: %s", rec.Code, rec.Body.String())
				}
				events := probe.all()
				if len(events) != 1 {
					t.Fatalf("events = %+v", events)
				}
				assertDiscoveryEvent(t, db, events[0], kind, webhook.ActionUpdated, domain.ActionUpdate)
				// A repeated acceptance has no remaining change and cannot emit a
				// second notification even if the stale fingerprint is replayed.
				if rec := postForm(t, srv, source.Path()+"/changes", form); rec.Code != http.StatusConflict {
					t.Fatalf("repeat apply = %d", rec.Code)
				}
				if !reflect.DeepEqual(probe.all(), events) {
					t.Fatal("unchanged acceptance emitted an event")
				}
				assertDiscoveryStagedCount(t, db, 1)
			})
		}
	}
}
