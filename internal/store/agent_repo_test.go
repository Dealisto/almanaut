package store

import (
	"errors"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func agentTestDB(t *testing.T) *AgentRepo {
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
	if _, err := NewHostRepo(db).Create(domain.Host{Name: "nas", Type: "physical"}); err != nil {
		t.Fatalf("create host: %v", err)
	}
	return NewAgentRepo(db)
}

func TestAgentBindingRoundTripAndUpsert(t *testing.T) {
	r := agentTestDB(t)
	if _, err := r.ByAgentID("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ByAgentID(missing) err = %v, want ErrNotFound", err)
	}
	b := AgentBinding{HostID: 1, AgentID: "uuid-1", Fingerprint: "h|aa", LastSeen: "2026-08-07T10:00:00Z"}
	if err := r.Upsert(b); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := r.ByAgentID("uuid-1")
	if err != nil {
		t.Fatalf("ByAgentID: %v", err)
	}
	if got != b {
		t.Fatalf("ByAgentID = %+v, want %+v", got, b)
	}
	// A second Upsert must update in place, not fail on the primary key.
	b.Fingerprint, b.LastSeen = "h|bb", "2026-08-07T11:00:00Z"
	if err := r.Upsert(b); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	got, _ = r.ByAgentID("uuid-1")
	if got.Fingerprint != "h|bb" || got.LastSeen != "2026-08-07T11:00:00Z" {
		t.Fatalf("Upsert did not update: %+v", got)
	}
}

func TestRecordReportPrunesToRetention(t *testing.T) {
	r := agentTestDB(t)
	for i := 0; i < agentReportRetention+5; i++ {
		payload := []byte(`{"n":` + strconv.Itoa(i) + `}`)
		if err := r.RecordReport(1, "uuid-1", "2026-08-07T10:00:00Z", "0.1.0", 1, payload); err != nil {
			t.Fatalf("RecordReport %d: %v", i, err)
		}
	}
	var n int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM agent_reports WHERE host_id = 1`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != agentReportRetention {
		t.Fatalf("kept %d reports, want %d", n, agentReportRetention)
	}
	latest, err := r.LatestReport(1)
	if err != nil {
		t.Fatalf("LatestReport: %v", err)
	}
	if latest != `{"n":`+strconv.Itoa(agentReportRetention+4)+`}` {
		t.Fatalf("LatestReport = %s, want the newest payload", latest)
	}
}

func TestLatestReportNotFound(t *testing.T) {
	r := agentTestDB(t)
	if _, err := r.LatestReport(1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LatestReport err = %v, want ErrNotFound", err)
	}
}
