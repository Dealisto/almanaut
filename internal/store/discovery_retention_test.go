package store

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestDiscoveryHistoryBoundedWithSuccessfulBaselines(t *testing.T) {
	db := newTestDB(t)
	repo := NewDiscoveryRunRepo(db)
	record := func(source, message string) int64 {
		id, err := repo.Record(DiscoveryRun{Source: source, Error: message, StartedAt: time.Now(), FinishedAt: time.Now(), NewKeys: []string{"known"}})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	dockerBaseline := record("docker", "")
	proxmoxBaseline := record("proxmox", "")
	proxmoxLatest := record("proxmox", "offline")
	for i := 0; i < 70; i++ {
		record("docker", "offline")
	}
	list, err := repo.List(1000)
	if err != nil || len(list) != 53 {
		t.Fatalf("history=%d want 50 + 3 protected records, err=%v", len(list), err)
	}
	for source, want := range map[string]int64{"docker": dockerBaseline, "proxmox": proxmoxBaseline} {
		run, err := repo.LatestSuccessful(source)
		if err != nil || run.ID != want || len(run.NewKeys) != 1 || run.NewKeys[0] != "known" {
			t.Fatalf("lost %s comparison baseline: %+v err=%v", source, run, err)
		}
	}
	if run, err := repo.Latest("proxmox"); err != nil || run.ID != proxmoxLatest {
		t.Fatalf("lost latest failure: %+v err=%v", run, err)
	}
	record("docker", "")
	var oldCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM discovery_runs WHERE id=?", dockerBaseline).Scan(&oldCount); err != nil || oldCount != 0 {
		t.Fatalf("superseded baseline retained: count=%d err=%v", oldCount, err)
	}
}

func TestDiscoveryHistoryKeepsLastFifty(t *testing.T) {
	db := newTestDB(t)
	repo := NewDiscoveryRunRepo(db)
	for i := 0; i < 55; i++ {
		if _, err := repo.Record(DiscoveryRun{Source: "docker", StartedAt: time.Now(), FinishedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := repo.List(1000)
	if err != nil || len(list) != 50 || list[0].ID != 55 || list[49].ID != 6 {
		t.Fatalf("incorrect retained history: rows=%d err=%v", len(list), err)
	}
}

func TestDiscoveryRecordRollsBackWhenPruningFails(t *testing.T) {
	db := newTestDB(t)
	repo := NewDiscoveryRunRepo(db)
	for i := 0; i < 50; i++ {
		if _, err := repo.Record(DiscoveryRun{Source: "docker"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`CREATE TRIGGER reject_discovery_prune BEFORE DELETE ON discovery_runs
		BEGIN SELECT RAISE(ABORT, 'prune unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Record(DiscoveryRun{Source: "docker"}); err == nil {
		t.Fatal("expected prune failure")
	}
	if latest, err := repo.Latest("docker"); err != nil || latest.ID != 50 {
		t.Fatalf("insert survived failed cleanup: %+v err=%v", latest, err)
	}
}

func TestDiscoveryRecordUsesCallerTransaction(t *testing.T) {
	db := newTestDB(t)
	repo := NewDiscoveryRunRepo(db)
	rollback := errors.New("caller rollback")
	err := WithTx(db, func(tx *sql.Tx) error {
		if _, err := repo.WithTx(tx).Record(DiscoveryRun{Source: "docker"}); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if _, err := repo.Latest("docker"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("record escaped caller transaction: %v", err)
	}
}
