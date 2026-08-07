package store

import (
	"database/sql"
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

func TestUpsertAgentIDConflict(t *testing.T) {
	r := agentTestDB(t)
	b1 := AgentBinding{HostID: 1, AgentID: "uuid-1", Fingerprint: "h|aa", LastSeen: "2026-08-07T10:00:00Z"}
	if err := r.Upsert(b1); err != nil {
		t.Fatalf("Upsert host 1: %v", err)
	}
	// Try to bind the same agent_id to host 2 — must fail with ErrAgentIDConflict.
	b2 := AgentBinding{HostID: 2, AgentID: "uuid-1", Fingerprint: "h|bb", LastSeen: "2026-08-07T10:00:00Z"}
	err := r.Upsert(b2)
	if !errors.Is(err, ErrAgentIDConflict) {
		t.Fatalf("Upsert host 2 with same agent_id err = %v, want ErrAgentIDConflict", err)
	}
}

// BoundHostIDs must return exactly the host ids that currently have a
// binding — no more, no less — so the report handler can exclude them from
// adoption matching.
func TestAgentRepoBoundHostIDs(t *testing.T) {
	r := agentTestDB(t)
	db := r.db.(*sql.DB)
	if _, err := NewHostRepo(db).Create(domain.Host{Name: "srv2", Type: "physical"}); err != nil {
		t.Fatalf("create host 2: %v", err)
	}
	if _, err := NewHostRepo(db).Create(domain.Host{Name: "srv3", Type: "physical"}); err != nil {
		t.Fatalf("create host 3: %v", err)
	}

	got, err := r.BoundHostIDs()
	if err != nil {
		t.Fatalf("BoundHostIDs (none bound): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("BoundHostIDs = %v, want empty before any binding exists", got)
	}

	if err := r.Upsert(AgentBinding{HostID: 1, AgentID: "uuid-1", Fingerprint: "h|aa", LastSeen: "2026-08-07T10:00:00Z"}); err != nil {
		t.Fatalf("Upsert host 1: %v", err)
	}
	if err := r.Upsert(AgentBinding{HostID: 3, AgentID: "uuid-3", Fingerprint: "h|cc", LastSeen: "2026-08-07T10:00:00Z"}); err != nil {
		t.Fatalf("Upsert host 3: %v", err)
	}

	got, err = r.BoundHostIDs()
	if err != nil {
		t.Fatalf("BoundHostIDs: %v", err)
	}
	want := map[int64]bool{1: true, 3: true}
	if len(got) != len(want) {
		t.Fatalf("BoundHostIDs = %v, want %v", got, want)
	}
	for id := range want {
		if !got[id] {
			t.Fatalf("BoundHostIDs = %v, missing host %d", got, id)
		}
	}
	if got[2] {
		t.Fatalf("BoundHostIDs = %v, host 2 was never bound", got)
	}
}

func TestRecordReportCrossHostIsolation(t *testing.T) {
	r := agentTestDB(t)
	// Create a second host
	if _, err := NewHostRepo(r.db.(*sql.DB)).Create(domain.Host{Name: "srv", Type: "physical"}); err != nil {
		t.Fatalf("create host 2: %v", err)
	}

	// Add a few reports to host 2
	for i := 0; i < 5; i++ {
		payload := []byte(`{"h":2,"n":` + strconv.Itoa(i) + `}`)
		if err := r.RecordReport(2, "uuid-2", "2026-08-07T10:00:00Z", "0.1.0", 1, payload); err != nil {
			t.Fatalf("RecordReport host 2: %v", err)
		}
	}
	host2Latest, err := r.LatestReport(2)
	if err != nil {
		t.Fatalf("LatestReport host 2 before: %v", err)
	}
	if host2Latest != `{"h":2,"n":4}` {
		t.Fatalf("host 2 latest before = %s, want h:2,n:4", host2Latest)
	}

	// Push 35 reports for host 1 (beyond retention of 30)
	for i := 0; i < 35; i++ {
		payload := []byte(`{"h":1,"n":` + strconv.Itoa(i) + `}`)
		if err := r.RecordReport(1, "uuid-1", "2026-08-07T10:00:00Z", "0.1.0", 1, payload); err != nil {
			t.Fatalf("RecordReport host 1 %d: %v", i, err)
		}
	}

	// Verify host 1 has exactly 30 reports
	var host1Count int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM agent_reports WHERE host_id = 1`).Scan(&host1Count); err != nil {
		t.Fatalf("count host 1: %v", err)
	}
	if host1Count != agentReportRetention {
		t.Fatalf("host 1 kept %d reports, want %d", host1Count, agentReportRetention)
	}

	// Verify host 2 still has all 5 reports (untouched by host 1's pruning)
	var host2Count int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM agent_reports WHERE host_id = 2`).Scan(&host2Count); err != nil {
		t.Fatalf("count host 2: %v", err)
	}
	if host2Count != 5 {
		t.Fatalf("host 2 kept %d reports, want 5", host2Count)
	}

	// Verify host 2's latest is still the same (unchanged)
	host2LatestAfter, err := r.LatestReport(2)
	if err != nil {
		t.Fatalf("LatestReport host 2 after: %v", err)
	}
	if host2LatestAfter != host2Latest {
		t.Fatalf("host 2 latest changed to %s, want %s", host2LatestAfter, host2Latest)
	}
}
