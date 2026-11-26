package notify

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestNotifierInterruptedPassesDoNotStarveLaterItems(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "notifications.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, ""); err != nil {
		t.Fatal(err)
	}
	certs, hardware, subscriptions := store.NewCertificateRepo(db), store.NewHardwareRepo(db), store.NewSubscriptionRepo(db)
	for _, name := range []string{"first.example", "second.example"} {
		if _, err := certs.Create(domain.Certificate{Subject: name, ExpiresOn: "2026-07-06"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := hardware.Create(domain.Hardware{Name: "server-warranty", WarrantyEnd: "2026-07-06"}); err != nil {
		t.Fatal(err)
	}
	if _, err := subscriptions.Create(domain.Subscription{Name: "service-renewal", RenewalDate: "2026-07-06"}); err != nil {
		t.Fatal(err)
	}
	var cancel context.CancelFunc
	var messages []string
	sender := senderFunc(func(ctx context.Context, n Notification) error {
		messages = append(messages, n.Body)
		cancel() // Each slow delivery exhausts this pass's context.
		return ctx.Err()
	})
	state := store.NewNotificationRepo(db)
	notifier := New(certs, hardware, subscriptions, state, sender, 30)
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	for range 4 {
		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		err := notifier.Run(ctx, now)
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("interrupted pass did not fail: %v", err)
		}
	}
	for _, name := range []string{"first.example", "second.example", "server-warranty", "service-renewal"} {
		if !strings.Contains(strings.Join(messages, "\n"), name) {
			t.Errorf("%s was starved by earlier failures: %v", name, messages)
		}
	}
	if sent, err := state.Sent(); err != nil || len(sent) != 0 {
		t.Fatalf("interrupted attempts marked successful: %v, %v", sent, err)
	}
	// A removed cursor target must not prevent the remaining items from running.
	if err := certs.Delete(1); err != nil {
		t.Fatal(err)
	}
	healthy := &fakeSender{}
	notifier.sender = healthy
	if err := notifier.Run(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if len(healthy.sent) != 3 {
		t.Fatalf("recovery skipped remaining items: %v", healthy.sent)
	}
	if err := notifier.Run(context.Background(), now); err != nil || len(healthy.sent) != 3 {
		t.Fatalf("successful alerts were duplicated: %v, %v", healthy.sent, err)
	}
}
