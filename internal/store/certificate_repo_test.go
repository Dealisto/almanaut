package store

import (
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newCertificateRepo(t *testing.T) *CertificateRepo {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return NewCertificateRepo(db)
}

func TestCertificateRepoCRUD(t *testing.T) {
	repo := newCertificateRepo(t)

	id, err := repo.Create(domain.Certificate{
		Subject: "*.example.com", Issuer: "Let's Encrypt", ExpiresOn: "2027-01-15", AutoRenew: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Subject != "*.example.com" || got.ExpiresOn != "2027-01-15" || !got.AutoRenew {
		t.Errorf("Get returned %+v", got)
	}

	if err := repo.Update(domain.Certificate{ID: id, Subject: "*.example.com", ExpiresOn: "2028-01-15", AutoRenew: false}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = repo.Get(id)
	if got.ExpiresOn != "2028-01-15" || got.AutoRenew {
		t.Errorf("Update not applied: %+v", got)
	}

	list, err := repo.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}

	if err := repo.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, _ = repo.List()
	if len(list) != 0 {
		t.Fatalf("List len after delete = %d, want 0", len(list))
	}
}

func TestCertificateRepoProbeTargetRoundTrip(t *testing.T) {
	db := newTestDB(t)
	repo := NewCertificateRepo(db)
	id, err := repo.Create(domain.Certificate{Subject: "example.com", ExpiresOn: "2027-01-01", ProbeTarget: "example.com:443"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := repo.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ProbeTarget != "example.com:443" {
		t.Fatalf("probe_target not persisted: %q", got.ProbeTarget)
	}
}
