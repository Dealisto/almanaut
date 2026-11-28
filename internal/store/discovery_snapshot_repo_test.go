package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
)

func TestDiscoverySnapshotsPreserveSuccessfulEvidence(t *testing.T) {
	db := newTestDB(t)
	repo := NewDiscoverySnapshotRepo(db)
	start := time.Now().UTC().Add(-time.Hour)
	original := []discovery.Candidate{{ID: "vm/1", EntityType: "host", Host: domain.Host{Name: "vm", IPs: []string{"192.0.2.1"}}}}
	if err := repo.Record("proxmox:lab", start, original, nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.Record("proxmox:lab", start.Add(time.Minute), nil, errors.New("secret-token")); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get("proxmox:lab")
	if err != nil || len(got.Candidates) != 1 || got.Candidates[0].Host.IPs[0] != "192.0.2.1" || !got.SucceededAt.Equal(start) || got.LastError == "" || strings.Contains(got.LastError, "secret") {
		t.Fatalf("failed scan lost evidence: %+v, %v", got, err)
	}
	// A slower previous scan must neither replace a newer success nor hide failure.
	if err := repo.Record("proxmox:lab", start.Add(-time.Second), nil, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Get("proxmox:lab")
	if len(got.Candidates) != 1 || got.LastError == "" {
		t.Fatalf("older scan won: %+v", got)
	}
	if err := repo.Record("proxmox:other", start, nil, nil); err != nil {
		t.Fatal(err)
	}
	other, _ := repo.Get("proxmox:other")
	if len(other.Candidates) != 0 || other.SucceededAt.IsZero() {
		t.Fatalf("empty success/source isolation: %+v", other)
	}
	// An empty *successful* response is valid evidence that nothing was observed.
	if err := repo.Record("proxmox:lab", start.Add(2*time.Minute), nil, nil); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Get("proxmox:lab")
	if len(got.Candidates) != 0 || got.LastError != "" || !got.SucceededAt.Equal(start.Add(2*time.Minute)) {
		t.Fatalf("empty recovery: %+v", got)
	}
}

func TestDiscoverySnapshotsKeepSuccessWhileNewerAttemptFailed(t *testing.T) {
	repo := NewDiscoverySnapshotRepo(newTestDB(t))
	now := time.Now().UTC()
	if err := repo.Record("docker:lab", now, nil, errors.New("failed")); err != nil {
		t.Fatal(err)
	}
	if err := repo.Record("docker:lab", now.Add(-time.Minute), []discovery.Candidate{{ID: "a"}}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get("docker:lab")
	if err != nil || got.LastError == "" || !got.AttemptedAt.Equal(now) || len(got.Candidates) != 1 {
		t.Fatalf("late successful evidence: %+v %v", got, err)
	}
}

func TestDiscoverySnapshotsLimitsPreserveCompleteBaseline(t *testing.T) {
	for _, kind := range []string{"count", "bytes"} {
		t.Run(kind, func(t *testing.T) {
			repo := NewDiscoverySnapshotRepo(newTestDB(t))
			now := time.Now().UTC()
			if err := repo.Record("docker:lab", now, []discovery.Candidate{{ID: "before"}}, nil); err != nil {
				t.Fatal(err)
			}
			huge := make([]discovery.Candidate, MaxDiscoverySnapshotCandidates+1)
			if kind == "bytes" {
				huge = []discovery.Candidate{{ID: strings.Repeat("x", maxDiscoverySnapshotBytes)}}
			}
			if err := repo.Record("docker:lab", now.Add(time.Minute), huge, nil); err == nil {
				t.Fatal("unbounded snapshot accepted")
			}
			got, err := repo.Get("docker:lab")
			if err != nil || len(got.Candidates) != 1 || got.Candidates[0].ID != "before" || got.LastError == "" {
				t.Fatalf("partial evidence: %+v %v", got, err)
			}
		})
	}
}

func TestDiscoverySnapshotsClearedOnInventoryReplacement(t *testing.T) {
	db := newTestDB(t)
	repo := NewDiscoverySnapshotRepo(db)
	if err := repo.Record("docker:lab", time.Now(), []discovery.Candidate{{ID: "before"}}, nil); err != nil {
		t.Fatal(err)
	}
	snap, err := Export(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := Import(db, snap); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get("docker:lab"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale cache after replacement: %v", err)
	}
}
