package web

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestCanceledUploadsDoNotChangeInventory(t *testing.T) {
	for _, format := range []string{"yaml", "csv", "attachment"} {
		t.Run(format, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			hosts := store.NewHostRepo(db)
			id, err := hosts.Create(domain.Host{Name: "keep", Type: "vm"})
			if err != nil {
				t.Fatal(err)
			}
			canceled := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx, cancel := context.WithCancel(r.Context())
				cancel()
				srv.ServeHTTP(w, r.WithContext(ctx))
			})
			var status int
			switch format {
			case "yaml":
				status = uploadImport(t, canceled, "version: 1\nhosts: []\n", true).Code
			case "csv":
				status = uploadCSV(t, canceled, "host", "name,type\nnew,vm\n").Code
			case "attachment":
				status = uploadAttachment(t, canceled, "/hosts/"+itoa(id)+"/attachments", "new.txt", []byte("new attachment")).Code
			}
			if status < 400 {
				t.Errorf("canceled upload returned success: %d", status)
			}
			remaining, err := hosts.List()
			if err != nil || len(remaining) != 1 || remaining[0].ID != id || remaining[0].Name != "keep" {
				t.Fatalf("canceled upload changed inventory: %+v %v", remaining, err)
			}
			for _, table := range []string{"attachments", "changelog", "webhook_outbox"} {
				var count int
				if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
					t.Errorf("canceled upload changed %s: count=%d error=%v", table, count, err)
				}
			}
		})
	}
}

type cancelAfterHostWrite struct {
	crud[domain.Host]
	cancel  context.CancelFunc
	failure error
}

func (r cancelAfterHostWrite) CreateTx(tx *sql.Tx, host domain.Host) (int64, error) {
	id, err := r.crud.CreateTx(tx, host)
	r.cancel()
	if err == nil {
		err = r.failure
	}
	return id, err
}

func TestCSVImportCancellationAfterWriteRollsBackEntireBatch(t *testing.T) {
	for _, failure := range []error{nil, &store.ConstraintError{Reason: "simultaneous constraint failure"}} {
		name := "successful write"
		if failure != nil {
			name = "constraint failure"
		}
		t.Run(name, func(t *testing.T) {
			rs, deps := hostResourceForTest(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			rs.repo = cancelAfterHostWrite{crud: rs.repo, cancel: cancel, failure: failure}
			dispatcher := &recordingDispatcher{}
			deps.webhooks = dispatcher
			created, updated, rowErrors, err := rs.importCSVContext(ctx, deps, strings.NewReader("name,type\nfirst,vm\nsecond,vm\n"), "tester")
			if !errors.Is(err, context.Canceled) || created != 0 || updated != 0 || len(rowErrors) != 0 {
				t.Fatalf("canceled batch result: created=%d updated=%d rowErrors=%v error=%v", created, updated, rowErrors, err)
			}
			if events := dispatcher.all(); len(events) != 0 {
				t.Fatalf("canceled import dispatched webhook events: %+v", events)
			}
			for _, table := range []string{"hosts", "changelog", "webhook_outbox"} {
				var count int
				if err := deps.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
					t.Errorf("canceled batch committed %s: count=%d error=%v", table, count, err)
				}
			}
		})
	}
}
