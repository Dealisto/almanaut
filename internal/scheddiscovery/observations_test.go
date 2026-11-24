package scheddiscovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

type observationDocker struct {
	containers []discovery.Container
	err        error
}

func (s *observationDocker) Containers(context.Context) ([]discovery.Container, error) {
	return s.containers, s.err
}

func TestScheduledObservationsPreserveLastSeenWhenMissingOrUnavailable(t *testing.T) {
	db := sourceTestDB(t)
	services, hosts := store.NewServiceRepo(db), store.NewHostRepo(db)
	id, err := services.Create(domain.Service{Name: "observed", Kind: "container", Notes: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	bindings := store.NewDiscoveryBindingRepo(db)
	if err := bindings.Create(domain.DiscoveryBinding{Source: "docker:default", ExternalID: "one", EntityType: "service", EntityID: id}); err != nil {
		t.Fatal(err)
	}
	scan := &observationDocker{containers: []discovery.Container{{ID: "one", Name: "observed"}}}
	observations := store.NewDiscoveryObservationRepo(db)
	runs := store.NewDiscoveryRunRepo(db)
	d := NewSource(Source{Key: "docker:default", Kind: "docker", Docker: scan}, services, hosts, bindings, runs, &countSender{}, observations)
	at := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	d.detector.now = func() time.Time { return at }
	if err := d.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertSeen := func() {
		t.Helper()
		seen, err := observations.ListSource("docker:default")
		if err != nil || seen["one"] != "2026-11-24T12:00:00Z" {
			t.Fatalf("last seen = %v, %v", seen, err)
		}
	}
	assertSeen()
	at = at.Add(time.Hour)
	scan.containers = nil
	if err := d.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertSeen()
	at = at.Add(time.Hour)
	scan.containers = []discovery.Container{{ID: "one", Name: "observed"}}
	scan.err = errors.New("unavailable")
	if err := d.Run(context.Background()); err == nil {
		t.Fatal("failed scan succeeded")
	}
	assertSeen()
	latest, err := runs.Latest("docker:default")
	if err != nil || latest.Error == "" {
		t.Fatalf("failed source health not recorded: %+v, %v", latest, err)
	}
	s, err := services.Get(id)
	if err != nil || s.Notes != "manual" {
		t.Fatalf("inventory changed: %+v, %v", s, err)
	}
}
