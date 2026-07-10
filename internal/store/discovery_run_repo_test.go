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
