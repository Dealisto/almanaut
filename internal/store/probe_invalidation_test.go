package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestLivenessStateFollowsCurrentTarget(t *testing.T) {
	for _, entityType := range []string{"host", "service"} {
		t.Run(entityType, func(t *testing.T) {
			db := newTestDB(t)
			var id int64
			var err error
			if entityType == "host" {
				id, err = NewHostRepo(db).Create(domain.Host{Name: "original", Type: "physical", CheckAddress: "old.example:443"})
			} else {
				id, err = NewServiceRepo(db).Create(domain.Service{Name: "original", Kind: "container", CheckAddress: "old.example:443"})
			}
			if err != nil {
				t.Fatal(err)
			}
			table := entityType + "s"
			state := NewLivenessRepo(db)
			now := time.Now().UTC()
			if _, accepted, err := state.RecordCheck(entityType, id, "old.example:443", domain.LivenessStatus{Status: domain.LivenessUp, CheckedAt: now}); err != nil || !accepted {
				t.Fatalf("record initial check: accepted=%v error=%v", accepted, err)
			}
			if _, err := db.Exec(`UPDATE `+table+` SET name = 'renamed', check_address = check_address WHERE id = ?`, id); err != nil {
				t.Fatal(err)
			}
			if got, err := state.Get(entityType, id); err != nil || got.Status != domain.LivenessUp {
				t.Fatalf("rename discarded valid liveness state: %+v, %v", got, err)
			}
			abort := errors.New("abort edit")
			if err := WithTx(db, func(tx *sql.Tx) error {
				if _, err := tx.Exec(`UPDATE `+table+` SET check_address = 'new.example:443' WHERE id = ?`, id); err != nil {
					return err
				}
				return abort
			}); !errors.Is(err, abort) {
				t.Fatal(err)
			}
			if _, err := state.Get(entityType, id); err != nil {
				t.Fatalf("rolled-back edit discarded state: %v", err)
			}
			if _, err := db.Exec(`UPDATE `+table+` SET check_address = 'new.example:443' WHERE id = ?`, id); err != nil {
				t.Fatal(err)
			}
			if _, err := state.Get(entityType, id); !errors.Is(err, ErrNotFound) {
				t.Fatalf("target change retained old liveness state: %v", err)
			}
			if _, accepted, err := state.RecordCheck(entityType, id, "old.example:443", domain.LivenessStatus{Status: domain.LivenessDown, CheckedAt: now.Add(time.Second)}); err != nil || accepted {
				t.Fatalf("late result for old target accepted=%v error=%v", accepted, err)
			}
			previous, accepted, err := state.RecordCheck(entityType, id, "new.example:443", domain.LivenessStatus{Status: domain.LivenessDown, CheckedAt: now.Add(2 * time.Second)})
			if err != nil || !accepted || previous != nil {
				t.Fatalf("new target inherited old state: previous=%+v accepted=%v error=%v", previous, accepted, err)
			}
			if countRows(t, db, `SELECT COUNT(*) FROM liveness_notifications`) != 0 {
				t.Fatal("first observation of new target generated a false transition notification")
			}
			if _, err := db.Exec(`UPDATE `+table+` SET check_address = '' WHERE id = ?`, id); err != nil {
				t.Fatal(err)
			}
			if _, err := state.Get(entityType, id); !errors.Is(err, ErrNotFound) {
				t.Fatalf("disabling checks retained liveness state: %v", err)
			}
			if err := state.Upsert(entityType, id, domain.LivenessStatus{Status: domain.LivenessUp, CheckedAt: now}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`DELETE FROM `+table+` WHERE id = ?`, id); err != nil {
				t.Fatal(err)
			}
			if _, err := state.Get(entityType, id); !errors.Is(err, ErrNotFound) {
				t.Fatalf("deleting entity retained liveness state: %v", err)
			}
		})
	}
}

func TestCertificateProbeStateFollowsCurrentTarget(t *testing.T) {
	db := newTestDB(t)
	certs := NewCertificateRepo(db)
	cert := domain.Certificate{Subject: "original.example", ExpiresOn: "2027-01-01", ProbeTarget: "old.example:443"}
	var err error
	cert.ID, err = certs.Create(cert)
	if err != nil {
		t.Fatal(err)
	}
	state := NewCertProbeRepo(db)
	probe := domain.CertProbeStatus{ProbedAt: time.Now(), Success: true, Serial: "old-serial", Issuer: "Old CA", SANs: []string{"original.example"}}
	if err := state.Upsert(cert.ID, probe); err != nil {
		t.Fatal(err)
	}
	cert.Subject = "renamed.example"
	if err := certs.Update(cert); err != nil {
		t.Fatal(err)
	}
	if got, err := state.Get(cert.ID); err != nil || got.Serial != probe.Serial {
		t.Fatalf("same-target edit discarded probe state: %+v, %v", got, err)
	}
	for _, target := range []string{"new.example:443", ""} {
		cert.ProbeTarget = target
		if err := certs.Update(cert); err != nil {
			t.Fatal(err)
		}
		if _, err := state.Get(cert.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("target change to %q retained old TLS diagnostics: %v", target, err)
		}
		if err := state.Upsert(cert.ID, probe); err != nil {
			t.Fatal(err)
		}
	}
	if err := certs.Delete(cert.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Get(cert.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleting certificate retained probe state: %v", err)
	}
}
