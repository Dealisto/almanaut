package store

import (
	"database/sql"
	"errors"
	"testing"
)

func TestRecordReportRollsBackWhenRetentionFails(t *testing.T) {
	r := agentTestDB(t)
	for range agentReportRetention {
		if err := r.RecordReport(1, "agent", "2026-11-27T00:00:00Z", "1.0", 1, []byte(`{"state":"accepted"}`)); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate a storage failure after insertion, during the retention delete.
	if _, err := r.db.Exec(`CREATE TRIGGER reject_report_pruning BEFORE DELETE ON agent_reports BEGIN SELECT RAISE(ABORT, 'retention failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordReport(1, "agent", "2026-11-27T00:01:00Z", "1.0", 1, []byte(`{"state":"rejected"}`)); err == nil {
		t.Fatal("retention failure was ignored")
	}
	var count int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM agent_reports WHERE host_id = 1`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != agentReportRetention {
		t.Errorf("failed append left %d reports, want %d", count, agentReportRetention)
	}
	latest, err := r.LatestReport(1)
	if err != nil || latest != `{"state":"accepted"}` {
		t.Fatalf("failed append replaced latest report: %s, %v", latest, err)
	}
}

func TestRecordReportParticipatesInCallerTransaction(t *testing.T) {
	r := agentTestDB(t)
	abort := errors.New("abort inventory update")
	err := WithTx(r.db.(*sql.DB), func(tx *sql.Tx) error {
		if err := r.WithTx(tx).RecordReport(1, "agent", "2026-11-27T00:00:00Z", "1.0", 1, []byte(`{"state":"uncommitted"}`)); err != nil {
			return err
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
	if _, err := r.LatestReport(1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("report escaped caller rollback: %v", err)
	}
}
