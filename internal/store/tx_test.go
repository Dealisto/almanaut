package store

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestWithTxCommits(t *testing.T) {
	db := newTestDB(t)
	err := WithTx(db, func(tx *sql.Tx) error {
		_, err := NewServiceRepo(db).WithTx(tx).Create(domain.Service{Name: "jellyfin", Kind: "container"})
		return err
	})
	if err != nil {
		t.Fatalf("WithTx returned %v, want nil", err)
	}
	got, err := NewServiceRepo(db).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Name != "jellyfin" {
		t.Fatalf("after commit got %+v, want one service jellyfin", got)
	}
}

func TestWithTxRollsBackOnError(t *testing.T) {
	db := newTestDB(t)
	sentinel := errors.New("boom")
	err := WithTx(db, func(tx *sql.Tx) error {
		if _, err := NewServiceRepo(db).WithTx(tx).Create(domain.Service{Name: "jellyfin", Kind: "container"}); err != nil {
			return err
		}
		return sentinel // abort after a successful write
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx returned %v, want sentinel", err)
	}
	got, err := NewServiceRepo(db).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("after rollback got %+v, want no services", got)
	}
}

func TestWithTxBoundRepoIsolatedUntilCommit(t *testing.T) {
	db := newTestDB(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := NewServiceRepo(db).WithTx(tx).Create(domain.Service{Name: "sonarr", Kind: "container"}); err != nil {
		t.Fatalf("create in tx: %v", err)
	}
	// A read on the plain DB connection must not see the uncommitted row.
	got, err := NewServiceRepo(db).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("uncommitted row visible outside tx: %+v", got)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
}

func TestWithTxPanicRollsBack(t *testing.T) {
	db := newTestDB(t)
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		_ = WithTx(db, func(tx *sql.Tx) error {
			if _, err := NewServiceRepo(db).WithTx(tx).Create(domain.Service{Name: "plex", Kind: "container"}); err != nil {
				t.Fatalf("create in tx: %v", err)
			}
			panic("simulated crash")
		})
	}()
	if !panicked {
		t.Fatal("expected the panic to propagate out of WithTx")
	}
	got, err := NewServiceRepo(db).List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("after panic+rollback got %+v, want no services", got)
	}
}
