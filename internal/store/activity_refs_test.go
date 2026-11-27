package store

import (
	"database/sql"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestActivityRefsBatchAndNamespace(t *testing.T) {
	db := newTestDB(t)
	refs := []domain.EntityRef{{Type: "hardware", ID: 1}, {Type: "host", ID: -1}, {Type: "host", ID: 9999}, {Type: "hosts; DROP TABLE hosts", ID: 1}}
	if err := WithTx(db, func(tx *sql.Tx) error {
		for i := 1; i <= 601; i++ {
			if _, err := tx.Exec(`INSERT INTO hosts(id,name,type) VALUES(?,?,'physical')`, i, "node"); err != nil {
				return err
			}
			refs = append(refs, domain.EntityRef{Type: "host", ID: int64(i)}, domain.EntityRef{Type: "host", ID: int64(i)})
		}
		if _, err := tx.Exec(`INSERT INTO hardware(id,name,kind) VALUES(1,'switch','switch')`); err != nil {
			return err
		}
		live, err := NewChangelogRepo(db).WithTx(tx).ExistingRefs(refs)
		if err != nil || len(live) != 602 || !live[domain.EntityRef{Type: "hardware", ID: 1}] || !live[domain.EntityRef{Type: "host", ID: 601}] {
			t.Fatalf("transaction references count=%d error=%v", len(live), err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM hosts WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	live, err := NewChangelogRepo(db).ExistingRefs(refs)
	if err != nil || len(live) != 601 || live[domain.EntityRef{Type: "host", ID: 1}] || !live[domain.EntityRef{Type: "hardware", ID: 1}] {
		t.Fatalf("deleted reference/namespace mismatch: count=%d error=%v", len(live), err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewChangelogRepo(db).ExistingRefs(refs); err == nil {
		t.Fatal("database failure was reported as deleted entities")
	}
}
