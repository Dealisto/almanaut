package store

import (
	"database/sql"
	"errors"
	"slices"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestPortNamesForOwnerUsesExactOwnerAndTransaction(t *testing.T) {
	db := newTestDB(t)
	repo := NewPortRepo(db)
	for _, port := range []domain.Port{
		{OwnerType: "host", OwnerID: 1, Name: "Port 1"},
		{OwnerType: "hardware", OwnerID: 1, Name: "Hardware only"},
		{OwnerType: "host", OwnerID: 2, Name: "Other host"},
	} {
		if _, err := repo.Create(port); err != nil {
			t.Fatal(err)
		}
	}
	rollback := errors.New("rollback fixture")
	err := WithTx(db, func(tx *sql.Tx) error {
		if _, err := repo.CreateTx(tx, domain.Port{OwnerType: "host", OwnerID: 1, Name: "Port 2"}); err != nil {
			return err
		}
		names, err := repo.WithTx(tx).NamesForOwner("host", 1)
		slices.Sort(names)
		if err != nil || !slices.Equal(names, []string{"Port 1", "Port 2"}) {
			t.Fatalf("transaction owner names=%v error=%v", names, err)
		}
		names, err = repo.NamesForOwner("host", 1)
		if err != nil || !slices.Equal(names, []string{"Port 1"}) {
			t.Fatalf("uncommitted names escaped transaction: %v error=%v", names, err)
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if names, err := repo.NamesForOwner("host", 1); err != nil || !slices.Equal(names, []string{"Port 1"}) {
		t.Fatalf("rollback names=%v error=%v", names, err)
	}
	if names, err := repo.NamesForOwner("host", 99); err != nil || len(names) != 0 {
		t.Fatalf("missing owner names=%v error=%v", names, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.NamesForOwner("host", 1); err == nil {
		t.Fatal("storage failure reported as no existing names")
	}
}
