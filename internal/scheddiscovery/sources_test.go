package scheddiscovery

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func sourceTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "inventory.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, path); err != nil {
		t.Fatal(err)
	}
	return db
}

type sourceProxmox struct{ resources []discovery.ProxmoxResource }

func (s sourceProxmox) Resources(context.Context) ([]discovery.ProxmoxResource, error) {
	return s.resources, nil
}

type sourceBindings struct {
	bindings []domain.DiscoveryBinding
	err      error
}

func (s *sourceBindings) List() ([]domain.DiscoveryBinding, error) { return s.bindings, s.err }

type sourceServices struct {
	services []domain.Service
	err      error
}

func (s *sourceServices) List() ([]domain.Service, error) { return s.services, s.err }

func TestSourceDetectorIndependentBaselines(t *testing.T) {
	db := sourceTestDB(t)
	runs := store.NewDiscoveryRunRepo(db)
	bindings := store.NewDiscoveryBindingRepo(db)
	hosts, services := store.NewHostRepo(db), store.NewServiceRepo(db)
	sender := &countSender{}
	makeSource := func(key string) *SourceDetector {
		return NewSource(Source{Key: key, Kind: "proxmox", Proxmox: sourceProxmox{[]discovery.ProxmoxResource{{ID: "qemu/100", Type: "qemu", Name: "same-name"}}}}, services, hosts, bindings, runs, sender)
	}
	first, second := makeSource("proxmox:first"), makeSource("proxmox:second")
	for _, source := range []*SourceDetector{first, second, first, second} {
		if err := source.Run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if sender.count() != 2 {
		t.Fatalf("each source should notify once despite equal IDs and names, got %d", sender.count())
	}
	for _, key := range []string{"proxmox:first", "proxmox:second"} {
		run, err := runs.LatestSuccessful(key)
		if err != nil || run.NewCount != 1 || !reflect.DeepEqual(run.NewKeys, []string{"qemu/100"}) {
			t.Fatalf("source %s baseline = %+v, %v", key, run, err)
		}
	}
	gotHosts, err := hosts.List()
	if err != nil || len(gotHosts) != 0 {
		t.Fatalf("discovery imported hosts: %+v, %v", gotHosts, err)
	}
	gotBindings, err := bindings.List()
	if err != nil || len(gotBindings) != 0 {
		t.Fatalf("discovery created bindings: %+v, %v", gotBindings, err)
	}
}

func TestSourceDetectorIdentitySurvivesRenameAndIsSourceScoped(t *testing.T) {
	for _, kind := range []string{"docker", "podman", "proxmox", "xen-orchestra"} {
		t.Run(kind, func(t *testing.T) {
			db := sourceTestDB(t)
			hosts, services := store.NewHostRepo(db), store.NewServiceRepo(db)
			bindings, runs := store.NewDiscoveryBindingRepo(db), store.NewDiscoveryRunRepo(db)
			key := kind + ":first"
			var id int64
			var err error
			entityType := "host"
			source := Source{Key: key, Kind: kind}
			switch kind {
			case "docker", "podman":
				entityType = "service"
				id, err = services.Create(domain.Service{Name: "manual-name", Kind: "container", Notes: "Manual details."})
				source.Docker = fakeDocker{cs: []discovery.Container{{ID: "resource-id", Name: "renamed-at-source"}}}
			case "proxmox":
				id, err = hosts.Create(domain.Host{Name: "manual-name", Type: "vm", Notes: "Manual details."})
				source.Proxmox = sourceProxmox{[]discovery.ProxmoxResource{{ID: "resource-id", Type: "qemu", Name: "renamed-at-source"}}}
			case "xen-orchestra":
				id, err = hosts.Create(domain.Host{Name: "manual-name", Type: "vm", Notes: "Manual details."})
				source.XenOrchestra = &fakeXenOrchestra{resources: []discovery.XenOrchestraResource{{ID: "resource-id", Type: "vm", Name: "renamed-at-source"}}}
			}
			if err != nil {
				t.Fatal(err)
			}
			binding := domain.DiscoveryBinding{Source: key, ExternalID: "resource-id", EntityType: entityType, EntityID: id}
			if err := bindings.Create(binding); err != nil {
				t.Fatal(err)
			}
			sender := &countSender{}
			if err := NewSource(source, services, hosts, bindings, runs, sender).Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			run, err := runs.LatestSuccessful(key)
			if err != nil || run.FoundCount != 1 || run.NewCount != 0 || sender.count() != 0 {
				t.Fatalf("renamed binding was rediscovered: %+v, %v, notifications=%d", run, err, sender.count())
			}
			// The second source has the same external ID and the current local
			// name; another source's binding must still prevent name adoption.
			other := source
			other.Key = kind + ":second"
			switch kind {
			case "docker", "podman":
				other.Docker = fakeDocker{cs: []discovery.Container{{ID: "resource-id", Name: "manual-name"}}}
			case "proxmox":
				other.Proxmox = sourceProxmox{[]discovery.ProxmoxResource{{ID: "resource-id", Type: "qemu", Name: "manual-name"}}}
			case "xen-orchestra":
				other.XenOrchestra = &fakeXenOrchestra{resources: []discovery.XenOrchestraResource{{ID: "resource-id", Type: "vm", Name: "manual-name"}}}
			}
			if err := NewSource(other, services, hosts, bindings, runs, sender).Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			run, err = runs.LatestSuccessful(other.Key)
			if err != nil || run.NewCount != 1 || sender.count() != 1 {
				t.Fatalf("other source suppressed new resource: %+v, %v", run, err)
			}
			got, err := bindings.List()
			if err != nil || !reflect.DeepEqual(got, []domain.DiscoveryBinding{binding}) {
				t.Fatalf("scheduled run changed identity: %+v, %v", got, err)
			}
			if entityType == "host" {
				got, err := hosts.List()
				if err != nil || len(got) != 1 || got[0].Name != "manual-name" || got[0].Notes != "Manual details." {
					t.Fatalf("scheduled run changed hosts: %+v, %v", got, err)
				}
			} else {
				got, err := services.List()
				if err != nil || len(got) != 1 || got[0].Name != "manual-name" || got[0].Notes != "Manual details." {
					t.Fatalf("scheduled run changed services: %+v, %v", got, err)
				}
			}
		})
	}
}

func TestSourceDetectorFailuresPreserveSuccessfulBaseline(t *testing.T) {
	for _, failure := range []string{"scanner", "host list", "binding list", "service list"} {
		t.Run(failure, func(t *testing.T) {
			runs := testStore(t)
			sender := &countSender{}
			xo := &fakeXenOrchestra{resources: []discovery.XenOrchestraResource{{ID: "vm", Type: "vm", Name: "guest"}}}
			hosts := &xoHostList{}
			bindings := &sourceBindings{}
			services := &sourceServices{}
			source := Source{Key: "xen-orchestra:lab", Kind: "xen-orchestra", XenOrchestra: xo}
			if failure == "service list" {
				source = Source{Key: "podman:lab", Kind: "podman", Docker: fakeDocker{cs: []discovery.Container{{ID: "container", Name: "service"}}}}
			}
			detector := NewSource(source, services, hosts, bindings, runs, sender)
			if err := detector.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			baseline, err := runs.LatestSuccessful(source.Key)
			if err != nil {
				t.Fatal(err)
			}
			wantErr := errors.New(failure + " unavailable")
			switch failure {
			case "scanner":
				xo.err = wantErr
			case "host list":
				hosts.err = wantErr
			case "binding list":
				bindings.err = wantErr
			case "service list":
				services.err = wantErr
			}
			if err := detector.Run(context.Background()); !errors.Is(err, wantErr) {
				t.Fatalf("failure was not returned: %v", err)
			}
			failed, err := runs.Latest(source.Key)
			if err != nil || failed.Error == "" {
				t.Fatalf("failure was not recorded: %+v, %v", failed, err)
			}
			kept, err := runs.LatestSuccessful(source.Key)
			if err != nil || !reflect.DeepEqual(kept, baseline) {
				t.Fatalf("failure changed successful baseline: %+v, %v", kept, err)
			}
			xo.err, hosts.err, bindings.err, services.err = nil, nil, nil, nil
			if err := detector.Run(context.Background()); err != nil {
				t.Fatal(err)
			}
			if sender.count() != 1 {
				t.Fatalf("failure/recovery renotified existing findings: %d", sender.count())
			}
		})
	}
}

func TestSourceDetectorCachesCompleteObservationsAndFailures(t *testing.T) {
	db := sourceTestDB(t)
	scanner := &observationDocker{containers: []discovery.Container{{ID: "container", Name: "app"}}}
	cache := store.NewDiscoverySnapshotRepo(db)
	detector := NewSource(Source{Key: "docker:lab", Kind: "docker", Docker: scanner}, store.NewServiceRepo(db), store.NewHostRepo(db), store.NewDiscoveryBindingRepo(db), store.NewDiscoveryRunRepo(db), &countSender{}).WithSnapshotRecorder(cache)
	if err := detector.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := cache.Get("docker:lab")
	if err != nil || len(got.Candidates) != 1 || got.Candidates[0].ID != "container" {
		t.Fatalf("scheduled evidence: %+v, %v", got, err)
	}
	scanner.err = errors.New("unavailable")
	if err := detector.Run(context.Background()); err == nil {
		t.Fatal("failed scan succeeded")
	}
	failed, err := cache.Get("docker:lab")
	if err != nil || len(failed.Candidates) != 1 || failed.LastError == "" || !failed.SucceededAt.Equal(got.SucceededAt) {
		t.Fatalf("failed scan lost baseline: %+v %v", failed, err)
	}
}
