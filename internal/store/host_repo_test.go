package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newTestRepo(t *testing.T) *HostRepo {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return NewHostRepo(db)
}

func TestHostRepoCreateGetListDelete(t *testing.T) {
	repo := newTestRepo(t)

	id, err := repo.Create(domain.Host{
		Name: "web01", Type: "vm", OS: "Debian 12", IPs: []string{"10.0.0.5"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == 0 {
		t.Fatal("Create returned id 0")
	}

	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "web01" || got.Type != "vm" {
		t.Errorf("Get returned %+v", got)
	}
	if len(got.IPs) != 1 || got.IPs[0] != "10.0.0.5" {
		t.Errorf("Get IPs = %v, want [10.0.0.5]", got.IPs)
	}

	list, err := repo.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}

	if err := repo.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, _ = repo.List()
	if len(list) != 0 {
		t.Fatalf("List len after delete = %d, want 0", len(list))
	}
}

func TestHostRepoUpdate(t *testing.T) {
	repo := newTestRepo(t)
	id, err := repo.Create(domain.Host{Name: "old", Type: "vm", IPs: []string{"10.0.0.1"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	err = repo.Update(domain.Host{
		ID: id, Name: "new", Type: "lxc", OS: "Debian 12", IPs: []string{"10.0.0.2"},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "new" || got.Type != "lxc" || got.OS != "Debian 12" {
		t.Errorf("Update not applied: %+v", got)
	}
	if len(got.IPs) != 1 || got.IPs[0] != "10.0.0.2" {
		t.Errorf("IPs = %v, want [10.0.0.2]", got.IPs)
	}
}

// TestHostRepoNotFound guards the not-found contract shared by every repo: a
// read for a missing id returns ErrNotFound (so handlers answer 404 instead of
// masking a real failure), and an Update that matches no row reports ErrNotFound
// rather than a false success.
func TestHostRepoNotFound(t *testing.T) {
	repo := newTestRepo(t)

	if _, err := repo.Get(404); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(missing) error = %v, want ErrNotFound", err)
	}
	if err := repo.Update(domain.Host{ID: 404, Name: "ghost"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update(missing) error = %v, want ErrNotFound", err)
	}

	// A no-op update of an existing row must still succeed: SQLite counts matched
	// rows, so re-writing identical values reports one row affected, not zero.
	id, err := repo.Create(domain.Host{Name: "keep", Type: "vm", IPs: []string{"10.0.0.9"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Update(domain.Host{ID: id, Name: "keep", Type: "vm", IPs: []string{"10.0.0.9"}}); err != nil {
		t.Errorf("Update(no-op) error = %v, want nil", err)
	}
}

func TestHostRepoTxMethods(t *testing.T) {
	db := newTestDB(t)
	repo := NewHostRepo(db)
	var id int64
	err := WithTx(db, func(tx *sql.Tx) error {
		var e error
		id, e = repo.CreateTx(tx, domain.Host{Name: "nas", Type: "physical"})
		if e != nil {
			return e
		}
		got, e := repo.GetTx(tx, id)
		if e != nil {
			return e
		}
		got.Status = "down"
		return repo.UpdateTx(tx, got)
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(id)
	if err != nil || got.Status != "down" {
		t.Fatalf("tx methods did not persist: %+v err=%v", got, err)
	}
}
