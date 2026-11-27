package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestRelationshipPageUsesReadSnapshot(t *testing.T) {
	repo := newRelationshipRepo(t)
	db := repo.db.(*sql.DB)
	mustCreateRel(t, repo, "host", 1, "network", 1, "connected to")
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// Pin the snapshot before a concurrent writer adds another relationship.
	if _, _, err := RelationshipPage(context.Background(), tx, 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(domain.Relationship{FromType: "host", FromID: 2, ToType: "network", ToID: 1, Kind: "connected to"}); err != nil {
		t.Fatalf("read transaction blocked the writer: %v", err)
	}
	rows, total, err := RelationshipPage(context.Background(), tx, 1, 1)
	if err != nil || len(rows) != 0 || total != 1 {
		t.Fatalf("snapshot changed: rows=%v total=%d err=%v", rows, total, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := RelationshipPage(ctx, tx, 1, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}
