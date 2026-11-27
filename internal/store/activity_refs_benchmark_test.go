package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

// BenchmarkActivityReferenceLookup isolates live-link resolution for a short
// activity feed. The reference loads all host entities as the old catalog path
// did, excluding its additional option-label formatting and page rendering.
func BenchmarkActivityReferenceLookup(b *testing.B) {
	db, err := Open(filepath.Join(b.TempDir(), "activity.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(db, ""); err != nil {
		b.Fatal(err)
	}
	if err := WithTx(db, func(tx *sql.Tx) error {
		stmt, err := tx.Prepare(`INSERT INTO hosts(id,name,type,notes) VALUES(?,?,'physical',?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for i := 1; i <= 20000; i++ {
			if _, err := stmt.Exec(i, fmt.Sprintf("host-%d", i), strings.Repeat("x", 128)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		b.Fatal(err)
	}
	refs := []domain.EntityRef{{Type: "host", ID: 1}, {Type: "host", ID: 5}, {Type: "host", ID: 42}, {Type: "host", ID: 500}, {Type: "host", ID: 20000}}
	for _, targeted := range []bool{false, true} {
		b.Run(map[bool]string{false: "full-inventory-reference", true: "referenced-ids"}[targeted], func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var live map[domain.EntityRef]bool
				if targeted {
					live, err = NewChangelogRepo(db).ExistingRefs(refs)
				} else {
					var hosts []domain.Host
					hosts, err = NewHostRepo(db).List()
					live = make(map[domain.EntityRef]bool, len(hosts))
					for _, host := range hosts {
						live[domain.EntityRef{Type: "host", ID: host.ID}] = true
					}
				}
				if err != nil {
					b.Fatal(err)
				}
				for _, ref := range refs {
					if !live[ref] {
						b.Fatalf("missing live reference: %+v", ref)
					}
				}
			}
		})
	}
}
