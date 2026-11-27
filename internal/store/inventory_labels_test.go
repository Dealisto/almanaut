package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestInventoryLabelsBatchesAndMissingReferences(t *testing.T) {
	db := newTestDB(t)
	ids := []int64{-1, 0, 9999}
	err := WithTx(db, func(tx *sql.Tx) error {
		for id := int64(1); id <= 601; id++ {
			if _, err := tx.Exec(`INSERT INTO hosts(id,name,type) VALUES(?,?,'physical')`, id, fmt.Sprintf("host-%d", id)); err != nil {
				return err
			}
			ids = append(ids, id, id)
		}
		if _, err := tx.Exec(`DELETE FROM hosts WHERE id=301`); err != nil {
			return err
		}
		// Project labels only, with SQL NULL following the same empty-string
		// convention as inventory search. Uncommitted rows must be visible.
		spec := InventoryQuery{Table: "hosts", Label: "CASE WHEN e.id=2 THEN NULL ELSE e.name END"}
		labels, err := InventoryLabels(context.Background(), tx, spec, ids)
		if err != nil || len(labels) != 600 {
			t.Fatalf("labels=%d, err=%v", len(labels), err)
		}
		for id := int64(1); id <= 601; id++ {
			label, ok := labels[id]
			if id == 301 {
				if ok {
					t.Fatal("deleted reference received a label")
				}
				continue
			}
			want := fmt.Sprintf("host-%d", id)
			if id == 2 {
				want = ""
			}
			if !ok || label != want {
				t.Fatalf("label[%d]=%q present=%t, want %q", id, label, ok, want)
			}
		}
		badSpec := InventoryQuery{Table: "missing_inventory", Label: "e.name"}
		if empty, err := InventoryLabels(context.Background(), tx, badSpec, []int64{-1, 0}); err != nil || len(empty) != 0 {
			t.Fatalf("empty references queried inventory: labels=%v err=%v", empty, err)
		}
		if labels, err := InventoryLabels(context.Background(), tx, badSpec, []int64{1}); err == nil || labels != nil {
			t.Fatalf("storage error masked as missing records: labels=%v err=%v", labels, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInventoryLabelsUsesCallerSnapshotAndContext(t *testing.T) {
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
	spec := InventoryQuery{Table: "hosts", Label: "e.name"}
	if _, err := InventoryLabels(context.Background(), tx, spec, []int64{id}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(domain.Host{ID: id, Name: "renamed", Type: "physical"}); err != nil {
		t.Fatalf("read-only snapshot blocked writer: %v", err)
	}
	other, err := repo.Create(domain.Host{Name: "new", Type: "physical"})
	if err != nil {
		t.Fatal(err)
	}
	labels, err := InventoryLabels(context.Background(), tx, spec, []int64{id, other})
	if err != nil || len(labels) != 1 || labels[id] != "original" {
		t.Fatalf("labels escaped caller snapshot: %v, %v", labels, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if labels, err := InventoryLabels(ctx, tx, spec, []int64{id}); !errors.Is(err, context.Canceled) || labels != nil {
		t.Fatalf("cancellation lost: labels=%v err=%v", labels, err)
	}
}
