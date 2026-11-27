package notify

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestNotifierRefreshesQueuedItemsBeforeDelivery(t *testing.T) {
	for _, kind := range []Kind{KindCertificate, KindHardware, KindSubscription} {
		for _, change := range []string{"deleted", "renewed outside window", "new deadline and label"} {
			t.Run(string(kind)+"/"+change, func(t *testing.T) {
				db, err := store.Open(filepath.Join(t.TempDir(), "notifications.db"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { db.Close() })
				if err := store.Migrate(db, ""); err != nil {
					t.Fatal(err)
				}
				certs, hw, subs := store.NewCertificateRepo(db), store.NewHardwareRepo(db), store.NewSubscriptionRepo(db)
				if _, err := certs.Create(domain.Certificate{Subject: "first.example.com", ExpiresOn: "2026-11-28"}); err != nil {
					t.Fatal(err)
				}
				var id int64
				switch kind {
				case KindCertificate:
					id, err = certs.Create(domain.Certificate{Subject: "obsolete label", ExpiresOn: "2026-11-29"})
				case KindHardware:
					id, err = hw.Create(domain.Hardware{Name: "obsolete label", WarrantyEnd: "2026-11-29"})
				case KindSubscription:
					id, err = subs.Create(domain.Subscription{Name: "obsolete label", RenewalDate: "2026-11-29"})
				}
				if err != nil {
					t.Fatal(err)
				}
				var sent []Notification
				sender := senderFunc(func(_ context.Context, n Notification) error {
					sent = append(sent, n)
					if len(sent) != 1 {
						return nil
					}
					// Simulate an edit while the first receiver is handling its send.
					date := "2026-12-01"
					if change == "renewed outside window" {
						date = "2028-12-01"
					}
					switch kind {
					case KindCertificate:
						if change == "deleted" {
							return certs.Delete(id)
						}
						return certs.Update(domain.Certificate{ID: id, Subject: "current label", ExpiresOn: date})
					case KindHardware:
						if change == "deleted" {
							return hw.Delete(id)
						}
						return hw.Update(domain.Hardware{ID: id, Name: "current label", WarrantyEnd: date})
					default:
						if change == "deleted" {
							return subs.Delete(id)
						}
						return subs.Update(domain.Subscription{ID: id, Name: "current label", RenewalDate: date})
					}
				})
				state := store.NewNotificationRepo(db)
				n := New(certs, hw, subs, state, sender, 30)
				now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
				if err := n.Run(context.Background(), now); err != nil {
					t.Fatal(err)
				}
				want := 1
				if change == "new deadline and label" {
					want = 2
				}
				if len(sent) != want {
					t.Fatalf("sent %d notifications, want %d: %+v", len(sent), want, sent)
				}
				marked, err := state.Sent()
				if err != nil {
					t.Fatal(err)
				}
				date, exists := marked[store.SentKey{Kind: string(kind), ID: id}]
				if want == 2 {
					if !strings.Contains(sent[1].Body, "current label") || !strings.Contains(sent[1].Body, "2026-12-01") || date != "2026-12-01" {
						t.Fatalf("sent or marked stale details: notification=%+v date=%q", sent[1], date)
					}
				} else if exists {
					t.Fatalf("obsolete candidate marked notified: %q", date)
				}
			})
		}
	}
}
