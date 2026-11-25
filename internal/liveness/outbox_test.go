package liveness

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/notify"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestPersistentTransitionRetriesAfterRestartAndKeepsEntityOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, path); err != nil {
		t.Fatal(err)
	}
	hosts, services := store.NewHostRepo(db), store.NewServiceRepo(db)
	id, err := hosts.Create(domain.Host{Name: "web", Type: "vm", CheckAddress: "web:443"})
	if err != nil {
		t.Fatal(err)
	}
	state := store.NewLivenessRepo(db)
	now := time.Now().Add(-time.Hour)
	record := func(status string) {
		t.Helper()
		now = now.Add(time.Second)
		if _, accepted, err := state.RecordCheck("host", id, "web:443", domain.LivenessStatus{Status: status, CheckedAt: now}); err != nil || !accepted {
			t.Fatalf("record: accepted=%v err=%v", accepted, err)
		}
	}
	record(domain.LivenessUp)
	record(domain.LivenessDown)
	record(domain.LivenessUp)
	attempts := 0
	failure := errors.New("delivery unavailable")
	first := New(hosts, services, state, senderFunc(func(context.Context, notify.Notification) error { attempts++; return failure }), func(context.Context, string) error { return nil }, time.Second, nil, nil)
	if err := first.Run(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("Run=%v", err)
	}
	if attempts != 1 {
		t.Fatalf("failed transition attempted %d times in one pass", attempts)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM liveness_notifications`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("pending=%d err=%v", count, err)
	}
	// Close and reopen the database as well as replacing the checker. No
	// in-memory state participates in the retry.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	hosts, services = store.NewHostRepo(db), store.NewServiceRepo(db)
	sender := &countingSender{}
	restarted := New(hosts, services, store.NewLivenessRepo(db), sender, func(context.Context, string) error { return nil }, time.Second, nil, nil)
	if err := restarted.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sender.count() != 2 || sender.notes[0].Tags != "warning" || sender.notes[1].Tags != "" {
		t.Fatalf("wrong transition order: %+v", sender.notes)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM liveness_notifications`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("pending=%d err=%v", count, err)
	}
}

func TestQueuedTransitionsRemovedWhenTargetChanges(t *testing.T) {
	for _, typ := range []string{"host", "service"} {
		for _, change := range []string{"changed", "disabled", "deleted"} {
			t.Run(typ+"/"+change, func(t *testing.T) {
				db := livenessTestDB(t)
				hosts, services := store.NewHostRepo(db), store.NewServiceRepo(db)
				var id int64
				var err error
				table := "hosts"
				if typ == "host" {
					id, err = hosts.Create(domain.Host{Name: "web", Type: "vm", CheckAddress: "web:443"})
				} else {
					table = "services"
					id, err = services.Create(domain.Service{Name: "web", CheckAddress: "web:443"})
				}
				if err != nil {
					t.Fatal(err)
				}
				state := store.NewLivenessRepo(db)
				now := time.Now()
				for i, status := range []string{domain.LivenessUp, domain.LivenessDown} {
					if _, _, err := state.RecordCheck(typ, id, "web:443", domain.LivenessStatus{Status: status, CheckedAt: now.Add(time.Duration(i) * time.Second)}); err != nil {
						t.Fatal(err)
					}
				}
				if change == "deleted" {
					_, err = db.Exec(`DELETE FROM `+table+` WHERE id=?`, id)
				} else {
					address := "other:443"
					if change == "disabled" {
						address = ""
					}
					_, err = db.Exec(`UPDATE `+table+` SET check_address=? WHERE id=?`, address, id)
				}
				if err != nil {
					t.Fatal(err)
				}
				pending, err := state.PendingNotifications("", 100)
				if err != nil || len(pending) != 0 {
					t.Fatalf("obsolete notifications=%v err=%v", pending, err)
				}
			})
		}
	}
}

func TestObservationAndNotificationCommitAtomically(t *testing.T) {
	db := livenessTestDB(t)
	hosts := store.NewHostRepo(db)
	id, err := hosts.Create(domain.Host{Name: "web", Type: "vm", CheckAddress: "web:443"})
	if err != nil {
		t.Fatal(err)
	}
	state := store.NewLivenessRepo(db)
	now := time.Now()
	if _, _, err := state.RecordCheck("host", id, "web:443", domain.LivenessStatus{Status: domain.LivenessUp, CheckedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_outbox BEFORE INSERT ON liveness_notifications BEGIN SELECT RAISE(ABORT,'queue unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, accepted, err := state.RecordCheck("host", id, "web:443", domain.LivenessStatus{Status: domain.LivenessDown, CheckedAt: now.Add(time.Second)}); err == nil || accepted {
		t.Fatalf("accepted=%v err=%v", accepted, err)
	}
	got, err := state.Get("host", id)
	if err != nil || got.Status != domain.LivenessUp {
		t.Fatalf("observation committed without notification: %+v %v", got, err)
	}
}

func TestOutboxDeliveryBudgetDoesNotDrainUnboundedBacklog(t *testing.T) {
	db := livenessTestDB(t)
	hosts := store.NewHostRepo(db)
	id, err := hosts.Create(domain.Host{Name: "web", Type: "vm", CheckAddress: "web:443"})
	if err != nil {
		t.Fatal(err)
	}
	state := store.NewLivenessRepo(db)
	now := time.Now()
	// A single entity's ordered backlog also exercises reloading after each ack.
	for i := range 105 {
		if _, err := db.Exec(`INSERT INTO liveness_notifications(entity_type,entity_id,label,address,status,observed_at) VALUES ('host',?,'web','web:443','down',?)`, id, fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	sender := &countingSender{}
	c := New(hosts, store.NewServiceRepo(db), state, sender, func(context.Context, string) error { return nil }, time.Second, nil, func() time.Time { return now })
	if err := c.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sender.count() != 100 {
		t.Fatalf("sent %d, want budget 100", sender.count())
	}
}

func TestRetryFairnessSurvivesRestartWithMoreThanOnePassOfFailures(t *testing.T) {
	db := livenessTestDB(t)
	hosts := store.NewHostRepo(db)
	services := store.NewServiceRepo(db)
	for i := range 105 {
		label := fmt.Sprintf("host-%03d", i)
		id, err := hosts.Create(domain.Host{Name: label, Type: "vm", CheckAddress: label + ":443"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO liveness_notifications(entity_type,entity_id,label,address,status,observed_at) VALUES ('host',?,?,?,'down','2026-01-01T00:00:00Z')`, id, label, label+":443"); err != nil {
			t.Fatal(err)
		}
	}
	attempted := map[string]int{}
	failure := errors.New("notification service unavailable")
	sender := senderFunc(func(_ context.Context, n notify.Notification) error { attempted[n.Title]++; return failure })
	makeChecker := func() *Checker {
		return New(hosts, services, store.NewLivenessRepo(db), sender, func(context.Context, string) error { return nil }, time.Second, nil, nil)
	}
	if err := makeChecker().Run(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("first Run=%v", err)
	}
	if len(attempted) != 100 {
		t.Fatalf("first pass tried %d entities, want 100", len(attempted))
	}
	// Replacing the checker proves the fairness cursor is stored in SQLite.
	if err := makeChecker().Run(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("second Run=%v", err)
	}
	if len(attempted) != 105 {
		t.Fatalf("tail starved across retry passes: attempted %d entities", len(attempted))
	}
}

type observingState struct {
	*store.LivenessRepo
	recorded chan struct{}
}

func (s observingState) RecordCheck(typ string, id int64, address string, status domain.LivenessStatus) (*domain.LivenessStatus, bool, error) {
	previous, accepted, err := s.LivenessRepo.RecordCheck(typ, id, address, status)
	close(s.recorded)
	return previous, accepted, err
}

func TestBlockedNotificationDeliveryDoesNotPreventObservations(t *testing.T) {
	db := livenessTestDB(t)
	hosts := store.NewHostRepo(db)
	services := store.NewServiceRepo(db)
	id, err := hosts.Create(domain.Host{Name: "web", Type: "vm", CheckAddress: "web:443"})
	if err != nil {
		t.Fatal(err)
	}
	state := store.NewLivenessRepo(db)
	past := time.Now().Add(-time.Hour)
	for i, status := range []string{domain.LivenessUp, domain.LivenessDown} {
		if _, _, err := state.RecordCheck("host", id, "web:443", domain.LivenessStatus{Status: status, CheckedAt: past.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	sending, recorded := make(chan struct{}, 1), make(chan struct{})
	sender := senderFunc(func(ctx context.Context, _ notify.Notification) error {
		sending <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	})
	c := New(hosts, services, observingState{state, recorded}, sender, func(context.Context, string) error { return nil }, time.Second, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	select {
	case <-sending:
	case <-time.After(time.Second):
		t.Fatal("delivery did not start")
	}
	select {
	case <-recorded:
	case <-time.After(time.Second):
		t.Fatal("blocked delivery prevented observation")
	}
	got, err := state.Get("host", id)
	if err != nil || got.Status != domain.LivenessUp {
		t.Fatalf("fresh observation missing while delivery blocks: %+v %v", got, err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run=%v", err)
	}
}

func TestConcurrentRunAdmissionHonorsCancellation(t *testing.T) {
	entered := make(chan struct{})
	c := New(hostList{{ID: 1, Name: "web", CheckAddress: "web:443"}}, noServices{}, newFakeState(), &countingSender{}, func(ctx context.Context, _ string) error { close(entered); <-ctx.Done(); return ctx.Err() }, time.Second, nil, nil)
	firstCtx, stopFirst := context.WithCancel(context.Background())
	defer stopFirst()
	firstDone := make(chan error, 1)
	go func() { firstDone <- c.Run(firstCtx) }()
	<-entered
	waitingCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	waitingDone := make(chan error, 1)
	go func() { waitingDone <- c.Run(waitingCtx) }()
	select {
	case err := <-waitingDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("waiting Run=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiting Run ignored cancellation")
	}
	stopFirst()
	<-firstDone
}
