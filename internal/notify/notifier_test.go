package notify

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// fakeSender records what was sent and can be told to fail.
type fakeSender struct {
	sent []Notification
	err  error
}

func (f *fakeSender) Send(_ context.Context, n Notification) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, n)
	return nil
}

func TestNotifierRunNotifiesOnceThenReArms(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := store.Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	certs := store.NewCertificateRepo(db)
	hw := store.NewHardwareRepo(db)
	subs := store.NewSubscriptionRepo(db)
	state := store.NewNotificationRepo(db)

	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	// Expires in 5 days -> inside the 30-day window.
	id, err := certs.Create(domain.Certificate{Subject: "a.com", ExpiresOn: "2026-07-06"})
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}

	sender := &fakeSender{}
	n := New(certs, hw, subs, state, sender, 30)

	// First run: one notification.
	if err := n.Run(context.Background(), now); err != nil {
		t.Fatalf("Run 1: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("run 1: want 1 notification, got %d", len(sender.sent))
	}

	// Second run, nothing changed: no new notification.
	if err := n.Run(context.Background(), now); err != nil {
		t.Fatalf("Run 2: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("run 2: want still 1 notification, got %d", len(sender.sent))
	}

	// Renew the cert far out; it leaves the window and state must re-arm.
	if err := certs.Update(domain.Certificate{ID: id, Subject: "a.com", ExpiresOn: "2027-07-06"}); err != nil {
		t.Fatalf("update cert: %v", err)
	}
	if err := n.Run(context.Background(), now); err != nil {
		t.Fatalf("Run 3: %v", err)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("run 3: want still 1 notification, got %d", len(sender.sent))
	}
	if s, _ := state.Sent(); len(s) != 0 {
		t.Fatalf("run 3: state should be cleared after renewal, got %v", s)
	}

	// Bring it back inside the window: it notifies again.
	if err := certs.Update(domain.Certificate{ID: id, Subject: "a.com", ExpiresOn: "2026-07-06"}); err != nil {
		t.Fatalf("re-update cert: %v", err)
	}
	if err := n.Run(context.Background(), now); err != nil {
		t.Fatalf("Run 4: %v", err)
	}
	if len(sender.sent) != 2 {
		t.Fatalf("run 4: want 2 notifications total, got %d", len(sender.sent))
	}
}

func TestNotifierRunGathersHardwareAndSubscriptions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := store.Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	certs := store.NewCertificateRepo(db)
	hw := store.NewHardwareRepo(db)
	subs := store.NewSubscriptionRepo(db)
	state := store.NewNotificationRepo(db)

	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		setup     func() error
		wantTitle string
	}{
		{
			name: "hardware warranty expiring",
			setup: func() error {
				_, err := hw.Create(domain.Hardware{Name: "nas-1", WarrantyEnd: "2026-07-10"})
				return err
			},
			wantTitle: "Warranty expiring",
		},
		{
			name: "subscription renewal due",
			setup: func() error {
				_, err := subs.Create(domain.Subscription{Name: "vps-1", RenewalDate: "2026-07-15"})
				return err
			},
			wantTitle: "Subscription renewal due",
		},
	}

	for _, tt := range tests {
		if err := tt.setup(); err != nil {
			t.Fatalf("%s: setup: %v", tt.name, err)
		}
	}

	sender := &fakeSender{}
	n := New(certs, hw, subs, state, sender, 30)
	if err := n.Run(context.Background(), now); err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := false
			for _, s := range sender.sent {
				if s.Title == tt.wantTitle {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("want a notification titled %q, got %v", tt.wantTitle, sender.sent)
			}
		})
	}
}

func TestNotifierRunSendFailureDoesNotMarkState(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := store.Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	certs := store.NewCertificateRepo(db)
	state := store.NewNotificationRepo(db)
	if _, err := certs.Create(domain.Certificate{Subject: "a.com", ExpiresOn: "2026-07-06"}); err != nil {
		t.Fatalf("create cert: %v", err)
	}

	sender := &fakeSender{err: context.DeadlineExceeded}
	n := New(certs, store.NewHardwareRepo(db), store.NewSubscriptionRepo(db), state, sender, 30)

	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if err := n.Run(context.Background(), now); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if s, _ := state.Sent(); len(s) != 0 {
		t.Fatalf("failed send must not mark state, got %v", s)
	}
}
