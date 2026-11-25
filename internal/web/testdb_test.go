package web

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// Cache only an immutable file image, never a live database or a test-owned
// temporary directory. Every test still opens its own on-disk WAL database
// through store.Open; migration behavior is exercised by the store tests.
var webTestSchema = sync.OnceValues(func() ([]byte, error) {
	dir, err := os.MkdirTemp("", "almanaut-web-schema-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "schema.db")
	db, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := store.Migrate(db, ""); err != nil {
		return nil, err
	}
	// Closing the last connection checkpoints WAL into the main file. Do not
	// copy an open database: its committed schema may still live in the WAL.
	if err := db.Close(); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
})

func newWebTestDB(t testing.TB) *sql.DB {
	t.Helper()
	schema, err := webTestSchema()
	if err != nil {
		t.Fatalf("prepare database schema: %v", err)
	}
	path := filepath.Join(t.TempDir(), "test.db")
	if err := os.WriteFile(path, schema, 0o600); err != nil {
		t.Fatalf("copy database schema: %v", err)
	}
	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestWebTestDBCopiesAreIsolated(t *testing.T) {
	for i := range 8 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			db := newWebTestDB(t)
			if _, err := store.Export(db); err != nil {
				t.Fatalf("copied schema is incomplete: %v", err)
			}
			id, err := store.NewHostRepo(db).Create(domain.Host{Name: "independent", Type: "vm"})
			if err != nil || id != 1 {
				t.Fatalf("fixture is not a fresh isolated inventory: id=%d error=%v", id, err)
			}
			if db.Stats().MaxOpenConnections != 0 {
				t.Fatal("fixture capped the production connection pool")
			}
			// Reserve one connection while obtaining another, so settings and
			// reads are checked on the multi-connection path used in production.
			conn, err := db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			for pragma, want := range map[string]string{"journal_mode": "wal", "foreign_keys": "1", "busy_timeout": "5000"} {
				var got string
				if err := db.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil || got != want {
					t.Errorf("%s=%q error=%v; want %q", pragma, got, err, want)
				}
			}
			if got, err := store.NewHostRepo(db).Get(id); err != nil || got.Name != "independent" {
				t.Fatalf("second connection lost committed data: %+v, %v", got, err)
			}
		})
	}
}
