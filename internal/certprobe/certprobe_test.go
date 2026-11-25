package certprobe

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(db, path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func leaf(notAfter time.Time, issuer string, serial int64, sans ...string) *x509.Certificate {
	return &x509.Certificate{
		NotAfter:     notAfter,
		Issuer:       pkix.Name{CommonName: issuer},
		SerialNumber: big.NewInt(serial),
		DNSNames:     sans,
	}
}

func TestProbeOneSuccessUpdatesCert(t *testing.T) {
	db := testDB(t)
	certs := store.NewCertificateRepo(db)
	id, err := certs.Create(domain.Certificate{Subject: "example.com", ExpiresOn: "2020-01-01", ProbeTarget: "example.com:443"})
	if err != nil {
		t.Fatal(err)
	}

	na := time.Date(2027, 3, 4, 0, 0, 0, 0, time.UTC)
	dial := func(context.Context, string) (*x509.Certificate, error) {
		return leaf(na, "Test CA", 42, "example.com"), nil
	}
	now := func() time.Time { return time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC) }
	p := New(certs, store.NewCertProbeRepo(db), db, dial, time.Second, nil, now)

	cert, err := certs.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ProbeOne(context.Background(), cert); err != nil {
		t.Fatal(err)
	}
	got, err := certs.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExpiresOn != "2027-03-04" {
		t.Fatalf("expiry not updated: %q", got.ExpiresOn)
	}
	if got.Issuer == "" {
		t.Fatalf("issuer not updated: %q", got.Issuer)
	}
	st, err := store.NewCertProbeRepo(db).Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Success || st.Issuer == "" {
		t.Fatalf("probe state wrong: %+v", st)
	}
	if st.Serial == "" {
		t.Fatalf("serial not recorded: %+v", st)
	}
	if len(st.SANs) != 1 || st.SANs[0] != "example.com" {
		t.Fatalf("sans not recorded: %+v", st.SANs)
	}
	if len(st.Mismatches) != 0 {
		t.Fatalf("expected no mismatches, got %v", st.Mismatches)
	}
}

func TestProbeOneFailureRecordsAndPreservesExpiry(t *testing.T) {
	db := testDB(t)
	certs := store.NewCertificateRepo(db)
	id, err := certs.Create(domain.Certificate{Subject: "x", ExpiresOn: "2027-01-01", ProbeTarget: "x:443"})
	if err != nil {
		t.Fatal(err)
	}
	dial := func(context.Context, string) (*x509.Certificate, error) { return nil, errors.New("connection refused") }
	p := New(certs, store.NewCertProbeRepo(db), db, dial, time.Second, nil, nil)
	cert, err := certs.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ProbeOne(context.Background(), cert); err != nil {
		t.Fatal(err)
	}
	got, err := certs.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExpiresOn != "2027-01-01" {
		t.Fatalf("expiry must not change on failure: %q", got.ExpiresOn)
	}
	if got.Issuer != "" {
		t.Fatalf("issuer must not change on failure: %q", got.Issuer)
	}
	st, err := store.NewCertProbeRepo(db).Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Success || st.LastError == "" {
		t.Fatalf("failure not recorded: %+v", st)
	}
}

func TestProbeOneStoresMismatchWhenSubjectNotInSANs(t *testing.T) {
	db := testDB(t)
	certs := store.NewCertificateRepo(db)
	id, err := certs.Create(domain.Certificate{Subject: "other.example.com", ExpiresOn: "2020-01-01", ProbeTarget: "example.com:443"})
	if err != nil {
		t.Fatal(err)
	}
	na := time.Date(2027, 3, 4, 0, 0, 0, 0, time.UTC)
	// Served cert's SANs don't cover the tracked subject "other.example.com".
	dial := func(context.Context, string) (*x509.Certificate, error) {
		return leaf(na, "Test CA", 7, "example.com"), nil
	}
	p := New(certs, store.NewCertProbeRepo(db), db, dial, time.Second, nil, nil)

	cert, err := certs.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ProbeOne(context.Background(), cert); err != nil {
		t.Fatal(err)
	}
	st, err := store.NewCertProbeRepo(db).Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Mismatches) == 0 {
		t.Fatalf("expected a mismatch reason, got none: %+v", st)
	}
}

func TestRunSkipsEmptyProbeTargetAndContinuesAfterFailure(t *testing.T) {
	db := testDB(t)
	certs := store.NewCertificateRepo(db)

	noTargetID, err := certs.Create(domain.Certificate{Subject: "no-target", ExpiresOn: "2020-01-01", ProbeTarget: ""})
	if err != nil {
		t.Fatal(err)
	}
	failID, err := certs.Create(domain.Certificate{Subject: "fails", ExpiresOn: "2020-01-01", ProbeTarget: "fails:443"})
	if err != nil {
		t.Fatal(err)
	}
	na := time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)
	okID, err := certs.Create(domain.Certificate{Subject: "ok.example.com", ExpiresOn: "2020-01-01", ProbeTarget: "ok.example.com:443"})
	if err != nil {
		t.Fatal(err)
	}

	dial := func(_ context.Context, addr string) (*x509.Certificate, error) {
		if addr == "fails:443" {
			return nil, errors.New("dial failed")
		}
		return leaf(na, "Test CA", 1, "ok.example.com"), nil
	}
	p := New(certs, store.NewCertProbeRepo(db), db, dial, time.Second, nil, nil)

	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	// The no-target cert was never probed: no probe state exists for it.
	if _, err := store.NewCertProbeRepo(db).Get(noTargetID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected no probe state for cert without a target, got err=%v", err)
	}

	failSt, err := store.NewCertProbeRepo(db).Get(failID)
	if err != nil {
		t.Fatal(err)
	}
	if failSt.Success {
		t.Fatalf("expected failure recorded for %d, got %+v", failID, failSt)
	}

	okCert, err := certs.Get(okID)
	if err != nil {
		t.Fatal(err)
	}
	if okCert.ExpiresOn != "2027-06-01" {
		t.Fatalf("ok cert should have been probed and updated despite the other's failure: %q", okCert.ExpiresOn)
	}
}

func TestProbeOnePreservesConcurrentEdits(t *testing.T) {
	db := testDB(t)
	certs := store.NewCertificateRepo(db)
	id, err := certs.Create(domain.Certificate{Subject: "old.example.com", ProbeTarget: "example.com:443"})
	if err != nil {
		t.Fatal(err)
	}
	cert, err := certs.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	dial := func(context.Context, string) (*x509.Certificate, error) {
		edited := cert
		edited.Subject, edited.Notes, edited.AutoRenew = "new.example.com", "edited during probe", true
		if err := certs.Update(edited); err != nil {
			t.Fatal(err)
		}
		return leaf(time.Date(2027, 3, 4, 0, 0, 0, 0, time.UTC), "Test CA", 42, "new.example.com"), nil
	}
	state := store.NewCertProbeRepo(db)
	p := New(certs, state, db, dial, time.Second, nil, nil)
	if err := p.ProbeOne(context.Background(), cert); err != nil {
		t.Fatal(err)
	}
	got, err := certs.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "new.example.com" || got.Notes != "edited during probe" || !got.AutoRenew || got.ExpiresOn != "2027-03-04" {
		t.Fatalf("probe overwrote concurrent edits: %+v", got)
	}
	st, err := state.Get(id)
	if err != nil || len(st.Mismatches) != 0 {
		t.Fatalf("mismatches must use current subject: state=%+v err=%v", st, err)
	}
}

func TestProbeOneDiscardsObsoleteResults(t *testing.T) {
	for _, change := range []string{"target", "delete"} {
		for _, failed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/failure=%t", change, failed), func(t *testing.T) {
				db := testDB(t)
				certs := store.NewCertificateRepo(db)
				id, err := certs.Create(domain.Certificate{Subject: "example.com", ExpiresOn: "2028-01-01", ProbeTarget: "example.com:443"})
				if err != nil {
					t.Fatal(err)
				}
				cert, err := certs.Get(id)
				if err != nil {
					t.Fatal(err)
				}
				dial := func(context.Context, string) (*x509.Certificate, error) {
					if change == "delete" {
						err = certs.Delete(id)
					} else {
						edited := cert
						edited.ProbeTarget = "new.example.com:443"
						err = certs.Update(edited)
					}
					if err != nil {
						t.Fatal(err)
					}
					if failed {
						return nil, errors.New("connection refused")
					}
					return leaf(time.Date(2027, 3, 4, 0, 0, 0, 0, time.UTC), "Test CA", 42, "example.com"), nil
				}
				state := store.NewCertProbeRepo(db)
				p := New(certs, state, db, dial, time.Second, nil, nil)
				if err := p.ProbeOne(context.Background(), cert); err != nil {
					t.Fatalf("obsolete probe should be discarded: %v", err)
				}
				if _, err := state.Get(id); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("obsolete probe result persisted: %v", err)
				}
				if change == "target" {
					got, err := certs.Get(id)
					if err != nil || got.ProbeTarget != "new.example.com:443" || got.ExpiresOn != "2028-01-01" {
						t.Fatalf("obsolete probe changed certificate: cert=%+v err=%v", got, err)
					}
				}
			})
		}
	}
}
