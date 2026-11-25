package liveness

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func livenessTestDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "liveness.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.Migrate(db, path); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCheckerDiscardsResultAfterTargetChange(t *testing.T) {
	for _, entityType := range []string{"host", "service"} {
		for _, change := range []string{"changed", "disabled", "deleted"} {
			t.Run(entityType+"/"+change, func(t *testing.T) {
				db := livenessTestDB(t)
				hosts, services := store.NewHostRepo(db), store.NewServiceRepo(db)
				var id int64
				var err error
				if entityType == "host" {
					id, err = hosts.Create(domain.Host{Name: "web", Type: "vm", CheckAddress: "old:443"})
				} else {
					id, err = services.Create(domain.Service{Name: "web", CheckAddress: "old:443"})
				}
				if err != nil {
					t.Fatal(err)
				}
				state := store.NewLivenessRepo(db)
				before := time.Date(2026, 11, 25, 12, 0, 0, 123, time.UTC)
				initial := domain.LivenessStatus{Status: domain.LivenessUp, CheckedAt: before, ChangedAt: before}
				if err := state.Upsert(entityType, id, initial); err != nil {
					t.Fatal(err)
				}
				sender := &countingSender{}
				dial := func(context.Context, string) error {
					table := "hosts"
					if entityType == "service" {
						table = "services"
					}
					var err error
					if change == "deleted" {
						_, err = db.Exec(`DELETE FROM `+table+` WHERE id = ?`, id)
					} else {
						address := "new:443"
						if change == "disabled" {
							address = ""
						}
						_, err = db.Exec(`UPDATE `+table+` SET check_address = ? WHERE id = ?`, address, id)
					}
					if err != nil {
						t.Errorf("change target: %v", err)
					}
					return errors.New("old address refused")
				}
				checker := New(hosts, services, state, sender, dial, time.Second, nil, func() time.Time { return before.Add(time.Nanosecond) })
				if err := checker.Run(context.Background()); err != nil {
					t.Fatal(err)
				}
				got, err := state.Get(entityType, id)
				if !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("obsolete result recreated invalidated state: %+v, %v", got, err)
				}
				if sender.count() != 0 {
					t.Fatal("obsolete result sent a transition notification")
				}
			})
		}
	}
}

func TestCheckerOverlappingChecksKeepLatestStartedObservation(t *testing.T) {
	db := livenessTestDB(t)
	hosts, services := store.NewHostRepo(db), store.NewServiceRepo(db)
	id, err := hosts.Create(domain.Host{Name: "web", Type: "vm", CheckAddress: "web:443"})
	if err != nil {
		t.Fatal(err)
	}
	state, sender := store.NewLivenessRepo(db), &countingSender{}
	started, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	first := time.Date(2026, 11, 25, 12, 0, 0, 123, time.UTC)
	second := first.Add(time.Nanosecond)
	old := New(hosts, services, state, sender, func(context.Context, string) error {
		close(started)
		<-release
		return errors.New("old timeout")
	}, time.Second, nil, func() time.Time { return first })
	newer := New(hosts, services, state, sender, func(context.Context, string) error { return nil }, time.Second, nil, func() time.Time { return second })
	go func() { done <- old.Run(context.Background()) }()
	<-started
	err = newer.Run(context.Background())
	close(release)
	oldErr := <-done
	if err != nil || oldErr != nil {
		t.Fatalf("check errors: newer=%v, older=%v", err, oldErr)
	}
	got, err := state.Get("host", id)
	if err != nil || got.Status != domain.LivenessUp || !got.CheckedAt.Equal(second) {
		t.Fatalf("older result replaced newer observation: %+v, %v", got, err)
	}
	if sender.count() != 0 {
		t.Fatal("obsolete failure sent a DOWN notification")
	}
}
