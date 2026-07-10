package store

import (
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestCertProbeRepoUpsertGet(t *testing.T) {
	db := newTestDB(t)
	repo := NewCertProbeRepo(db)

	if _, err := repo.Get(1); err == nil {
		t.Fatal("want ErrNotFound before any upsert")
	}

	t0 := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	s := domain.CertProbeStatus{
		ProbedAt: t0, Success: true, Serial: "ab12", Issuer: "Test CA",
		SANs: []string{"example.com", "*.example.com"}, NotAfter: "2027-01-01",
		Mismatches: []string{"serial changed (was cd, now ab12)"},
	}
	if err := repo.Upsert(1, s); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := repo.Get(1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Success || got.Serial != "ab12" || len(got.SANs) != 2 || len(got.Mismatches) != 1 || !got.ProbedAt.Equal(t0) {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	// upsert overwrites (failure clears success)
	if err := repo.Upsert(1, domain.CertProbeStatus{ProbedAt: t0.Add(time.Hour), Success: false, LastError: "dial tcp: timeout"}); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	got, _ = repo.Get(1)
	if got.Success || got.LastError != "dial tcp: timeout" {
		t.Fatalf("update mismatch: %+v", got)
	}
}
