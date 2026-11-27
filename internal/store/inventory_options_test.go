package store

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestInventoryOptionsProjectionAndOrder(t *testing.T) {
	db := newTestDB(t)
	if err := WithTx(db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO hosts(id,name,type,notes) VALUES(1,'Zulu','physical','unused'),(3,'Alpha','physical','unused'),(2,'Alpha','physical','unused')`); err != nil {
			return err
		}
		spec := InventoryQuery{Table: "hosts", Label: "CASE WHEN e.id=2 THEN NULL ELSE e.name END", Order: "e.name,e.id"}
		options, err := InventoryOptions(context.Background(), tx, spec)
		want := []InventoryOption{{ID: 2, Label: ""}, {ID: 3, Label: "Alpha"}, {ID: 1, Label: "Zulu"}}
		if err != nil || !reflect.DeepEqual(options, want) {
			t.Fatalf("options=%v, err=%v; want=%v", options, err, want)
		}
		if _, err := tx.Exec(`DELETE FROM hosts`); err != nil {
			return err
		}
		if options, err := InventoryOptions(context.Background(), tx, spec); err != nil || len(options) != 0 {
			t.Fatalf("empty inventory options=%v err=%v", options, err)
		}
		badSpec := InventoryQuery{Table: "missing_inventory", Label: "e.name", Order: "e.id"}
		if options, err := InventoryOptions(context.Background(), tx, badSpec); err == nil || options != nil {
			t.Fatalf("storage error masked as empty options=%v err=%v", options, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestInventoryOptionsCallerSnapshotAndCancellation(t *testing.T) {
	db := newTestDB(t)
	repo := NewHostRepo(db)
	id, err := repo.Create(domain.Host{Name: "original", Type: "physical"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	spec := InventoryQuery{Table: "hosts", Label: "e.name", Order: "e.name"}
	if _, err := InventoryOptions(context.Background(), tx, spec); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(domain.Host{ID: id, Name: "renamed", Type: "physical"}); err != nil {
		t.Fatalf("read-only snapshot blocked writer: %v", err)
	}
	if _, err := repo.Create(domain.Host{Name: "new", Type: "physical"}); err != nil {
		t.Fatal(err)
	}
	options, err := InventoryOptions(context.Background(), tx, spec)
	if err != nil || len(options) != 1 || options[0].ID != id || options[0].Label != "original" {
		t.Fatalf("options escaped caller snapshot: %v, %v", options, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if options, err := InventoryOptions(ctx, tx, spec); !errors.Is(err, context.Canceled) || options != nil {
		t.Fatalf("cancellation lost: options=%v err=%v", options, err)
	}
}
