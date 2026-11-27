package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

func TestCSVStagingIncludesIndirectPortEventsInAtomicBudget(t *testing.T) {
	for _, capacity := range []int{2, 3} {
		t.Run(itoa(int64(capacity)), func(t *testing.T) {
			srv, db, recorder := newOutboxWebServer(t, capacity)
			if _, err := store.NewHostRepo(db).Create(domain.Host{Name: "owner", Type: "physical"}); err != nil {
				t.Fatal(err)
			}
			ports := store.NewPortRepo(db)
			for _, name := range []string{"A", "B", "C"} {
				if _, err := ports.Create(domain.Port{Name: name, OwnerType: "host", OwnerID: 1}); err != nil {
					t.Fatal(err)
				}
			}
			response := uploadCSV(t, srv, "port", "id,name,owner_type,owner_id,peer_port_id\n1,A,host,1,2\n3,Renamed,host,1,0\n")
			want := 3
			if capacity == 2 {
				want = 0
				if response.Code != http.StatusServiceUnavailable {
					t.Fatalf("quota failure status=%d body=%s", response.Code, response.Body)
				}
			} else if response.Code != http.StatusSeeOther {
				t.Fatalf("import status=%d body=%s", response.Code, response.Body)
			}
			if events := recorder.all(); len(events) != want {
				t.Fatalf("post-commit events=%+v, want %d", events, want)
			}
			history, err := store.NewChangelogRepo(db).ListRecent(100)
			if err != nil || len(history) != want {
				t.Fatalf("history=%+v err=%v", history, err)
			}
			if count, err := store.NewWebhookOutboxRepo(db).PendingCount(); err != nil || count != want {
				t.Fatalf("outbox count=%d err=%v", count, err)
			}
			all, err := ports.List()
			if err != nil {
				t.Fatal(err)
			}
			for _, port := range all {
				wantPeer := int64(0)
				if capacity == 3 && port.ID <= 2 {
					wantPeer = 3 - port.ID
				}
				if port.PeerPortID != wantPeer || (capacity == 2 && port.ID == 3 && port.Name != "C") {
					t.Fatalf("unexpected stored port %+v", port)
				}
			}
			if capacity == 3 {
				rows, err := db.Query(`SELECT body FROM webhook_outbox`)
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				seen := make(map[int64]bool)
				for rows.Next() {
					var raw []byte
					var payload struct {
						ID int64 `json:"id"`
					}
					if err := rows.Scan(&raw); err != nil {
						t.Fatal(err)
					}
					if err := json.Unmarshal(raw, &payload); err != nil {
						t.Fatal(err)
					}
					seen[payload.ID] = true
				}
				if err := rows.Err(); err != nil || !seen[1] || !seen[2] || !seen[3] {
					t.Fatalf("missing direct/indirect delivery: %v err=%v", seen, err)
				}
			}
		})
	}
}

func TestCSVStagingCancellationRollsBackWithoutDispatch(t *testing.T) {
	rs, deps := hostResourceForTest(t)
	queue := webhook.NewDurableQueue(deps.db, webhook.Options{
		Workers: 1, QueueSize: 10, MaxAttempts: 100, BaseDelay: time.Hour,
		Client: &http.Client{Transport: outboxTestTransport{}}, Logger: log.New(io.Discard, "", 0),
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := queue.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	if _, err := store.NewWebhookRepo(deps.db).Create(domain.Webhook{URL: "https://receiver.test", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	recorder := &recordingDispatcher{}
	deps.webhooks = webhook.Multi{queue, recorder}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rs.captureEffects = func(tx *sql.Tx, _, _ *domain.Host) (mutationEffects, error) {
		return func(_ handlerDeps, _ string, _ *[]webhook.Event) error {
			count, err := store.NewWebhookOutboxRepo(deps.db).WithTx(tx).PendingCount()
			if err != nil || count != 1 {
				t.Fatalf("cancellation must follow first staged row: count=%d err=%v", count, err)
			}
			cancel()
			return nil
		}, nil
	}
	created, updated, rowErrs, err := rs.importCSVContext(ctx, deps, strings.NewReader("name,type\nfirst,physical\nsecond,physical\n"), "tester")
	if !errors.Is(err, context.Canceled) || created != 0 || updated != 0 || len(rowErrs) != 0 {
		t.Fatalf("created=%d updated=%d rowErrs=%v err=%v", created, updated, rowErrs, err)
	}
	hosts, err := rs.repo.List()
	if err != nil || len(hosts) != 0 || len(recorder.all()) != 0 {
		t.Fatalf("canceled import wrote/dispatched: hosts=%v events=%v err=%v", hosts, recorder.all(), err)
	}
	for _, table := range []string{"webhook_outbox", "changelog"} {
		var count int
		if err := deps.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("canceled %s count=%d err=%v", table, count, err)
		}
	}
}
