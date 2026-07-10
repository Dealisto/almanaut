package store

import (
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestLivenessRepoUpsertGet(t *testing.T) {
	db := newTestDB(t)
	repo := NewLivenessRepo(db)

	if _, err := repo.Get("host", 1); err == nil {
		t.Fatal("want ErrNotFound before any upsert")
	}

	t0 := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	s := domain.LivenessStatus{Status: domain.LivenessUp, CheckedAt: t0, ChangedAt: t0}
	if err := repo.Upsert("host", 1, s); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := repo.Get("host", 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.LivenessUp || !got.CheckedAt.Equal(t0) {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	t1 := t0.Add(time.Minute)
	s2 := domain.LivenessStatus{Status: domain.LivenessDown, CheckedAt: t1, ChangedAt: t1, LastError: "connection refused"}
	if err := repo.Upsert("host", 1, s2); err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	got, _ = repo.Get("host", 1)
	if got.Status != domain.LivenessDown || got.LastError != "connection refused" {
		t.Fatalf("update mismatch: %+v", got)
	}
}
