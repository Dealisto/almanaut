package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkPortGenerationNameLookup(b *testing.B) {
	db, err := Open(filepath.Join(b.TempDir(), "ports.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(db, ""); err != nil {
		b.Fatal(err)
	}
	if err := WithTx(db, func(tx *sql.Tx) error {
		stmt, err := tx.Prepare(`INSERT INTO ports(owner_type, owner_id, name, notes) VALUES('host', ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for owner := 1; owner <= 1000; owner++ {
			if _, err := tx.Exec(`INSERT INTO hosts(id, name, type) VALUES(?, ?, 'physical')`, owner, fmt.Sprintf("switch-%d", owner)); err != nil {
				return err
			}
			for port := 1; port <= 24; port++ {
				if _, err := stmt.Exec(owner, fmt.Sprintf("Port %d", port), strings.Repeat("x", 128)); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		b.Fatal(err)
	}
	for _, all := range []bool{true, false} {
		name := "owner-names"
		if all {
			name = "all-ports-reference"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				err := WithTx(db, func(tx *sql.Tx) error {
					repo := NewPortRepo(db).WithTx(tx)
					taken := map[string]bool{}
					if all {
						ports, err := repo.List()
						if err != nil {
							return err
						}
						for _, port := range ports {
							if port.OwnerType == "host" && port.OwnerID == 1 {
								taken[port.Name] = true
							}
						}
					} else {
						names, err := repo.NamesForOwner("host", 1)
						if err != nil {
							return err
						}
						for _, name := range names {
							taken[name] = true
						}
					}
					if len(taken) != 24 {
						return fmt.Errorf("name count = %d, want 24", len(taken))
					}
					return nil
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
