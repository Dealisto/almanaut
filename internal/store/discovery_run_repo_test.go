package store

import (
	"testing"
	"time"
)

func TestDiscoveryRunRepoRecordLatestList(t *testing.T) {
	db := newTestDB(t)
	repo := NewDiscoveryRunRepo(db)

	if _, err := repo.Latest("docker"); err == nil {
		t.Fatal("want ErrNotFound before any run")
	}

	t0 := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	if _, err := repo.Record(DiscoveryRun{
		Source: "docker", StartedAt: t0, FinishedAt: t0.Add(time.Second),
		FoundCount: 3, NewCount: 2, NewKeys: []string{"a", "b"},
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := repo.Record(DiscoveryRun{
		Source: "docker", StartedAt: t0.Add(time.Minute), FinishedAt: t0.Add(time.Minute),
		FoundCount: 3, NewCount: 0, Error: "boom",
	}); err != nil {
		t.Fatalf("record 2: %v", err)
	}

	latest, err := repo.Latest("docker")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.Error != "boom" || latest.NewCount != 0 {
		t.Fatalf("latest should be the 2nd run: %+v", latest)
	}
	// Latest is scoped per source.
	if _, err := repo.Latest("network"); err == nil {
		t.Fatal("want ErrNotFound for a source with no runs")
	}

	list, err := repo.List(10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].Error != "boom" {
		t.Fatalf("list should be most-recent-first, got %d rows", len(list))
	}
	// new_keys JSON round-trips.
	if len(list[1].NewKeys) != 2 {
		t.Fatalf("new_keys not round-tripped: %+v", list[1].NewKeys)
	}
}

func TestDiscoveryRunRepoLatestSuccessfulSkipsFailedRuns(t *testing.T) {
	db := newTestDB(t)
	repo := NewDiscoveryRunRepo(db)

	if _, err := repo.LatestSuccessful("docker"); err == nil {
		t.Fatal("want ErrNotFound before any run")
	}

	t0 := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	if _, err := repo.Record(DiscoveryRun{
		Source: "docker", StartedAt: t0, FinishedAt: t0.Add(time.Second),
		FoundCount: 1, NewCount: 1, NewKeys: []string{"a"},
	}); err != nil {
		t.Fatalf("record success: %v", err)
	}
	if _, err := repo.Record(DiscoveryRun{
		Source: "docker", StartedAt: t0.Add(time.Minute), FinishedAt: t0.Add(time.Minute),
		FoundCount: 0, NewCount: 0, Error: "docker daemon unreachable",
	}); err != nil {
		t.Fatalf("record failure: %v", err)
	}

	// The most recent run overall is the failure, but LatestSuccessful must
	// still return the earlier successful run (its NewKeys baseline).
	latest, err := repo.Latest("docker")
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if latest.Error == "" {
		t.Fatalf("Latest should be the failed run, got %+v", latest)
	}

	successful, err := repo.LatestSuccessful("docker")
	if err != nil {
		t.Fatalf("latest successful: %v", err)
	}
	if successful.Error != "" || len(successful.NewKeys) != 1 || successful.NewKeys[0] != "a" {
		t.Fatalf("LatestSuccessful returned the wrong run: %+v", successful)
	}

	// A source with only failed runs has no successful baseline.
	if _, err := repo.Record(DiscoveryRun{
		Source: "network", StartedAt: t0, FinishedAt: t0, Error: "scan timeout",
	}); err != nil {
		t.Fatalf("record network failure: %v", err)
	}
	if _, err := repo.LatestSuccessful("network"); err == nil {
		t.Fatal("want ErrNotFound for a source with only failed runs")
	}
}
