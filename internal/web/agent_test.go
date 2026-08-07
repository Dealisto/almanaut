package web

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Dealisto/almanaut/internal/agentapi"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// agentServer builds an auth-enabled handler plus a raw token with the given
// scope, and returns the db so tests can assert on rows. Modelled on
// apiAuthServer (api_write_test.go), which differs only in hard-coding
// read-write.
func agentServer(t *testing.T, scope domain.Scope) (http.Handler, *sql.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	users := store.NewUserRepo(db)
	if err := BootstrapAdmin(users, testLogger(), "alice", "password123", false); err != nil {
		t.Fatalf("BootstrapAdmin: %v", err)
	}
	u, err := users.GetByUsername("alice")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	raw, err := newAPIToken()
	if err != nil {
		t.Fatalf("newAPIToken: %v", err)
	}
	if _, err := store.NewTokenRepo(db).Create(store.APIToken{
		TokenHash: hashToken(raw), UserID: u.ID, Label: "agent",
		Scope: string(scope), CreatedAt: nowRFC3339(),
	}); err != nil {
		t.Fatalf("Create token: %v", err)
	}
	return newAuthedTestHandler(t, db), db, raw
}

func postReport(t *testing.T, h http.Handler, token string, r agentapi.Report) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/agent/report", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func baseReport() agentapi.Report {
	return agentapi.Report{
		SchemaVersion: agentapi.SchemaVersion,
		AgentID:       "uuid-1", AgentVersion: "0.1.0",
		Hostname: "nas01", VirtKind: "physical",
		OS: "Debian 12", CPU: "Ryzen 5950X", RAM: "64 GB", Disk: "1 TB NVMe",
		Interfaces: []agentapi.Interface{{Name: "eth0", MAC: "aa:bb:cc:00:00:01", Addrs: []string{"192.168.1.10"}}},
	}
}

// A read-write token must not reach the agent endpoint, and an agent token must
// not reach the entity API. The two ceilings are disjoint by design.
func TestAgentReportRejectsNonAgentScope(t *testing.T) {
	h, _, raw := agentServer(t, domain.ScopeReadWrite)
	if rec := postReport(t, h, raw, baseReport()); rec.Code != http.StatusForbidden {
		t.Fatalf("read-write token on /api/agent/report = %d, want 403", rec.Code)
	}
}

// A read-only token is rejected the same way a read-write one is: the agent
// endpoint requires the agent scope specifically, not merely "some valid
// token" or "a token that can't write the entity API".
func TestAgentReportRejectsReadOnlyScope(t *testing.T) {
	h, _, raw := agentServer(t, domain.ScopeReadOnly)
	if rec := postReport(t, h, raw, baseReport()); rec.Code != http.StatusForbidden {
		t.Fatalf("read-only token on /api/agent/report = %d, want 403", rec.Code)
	}
}

// The route sits behind apiAuth like every other /api route: no credentials
// at all must be rejected before the handler's own scope check ever runs.
func TestAgentReportRequiresToken(t *testing.T) {
	h, _, _ := agentServer(t, domain.ScopeAgent)
	body, err := json.Marshal(baseReport())
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/agent/report", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated POST /api/agent/report = %d, want 401", rec.Code)
	}
}

func TestAgentTokenCannotWriteEntityAPI(t *testing.T) {
	h, _, raw := agentServer(t, domain.ScopeAgent)
	req := httptest.NewRequest(http.MethodPost, "/api/hosts", bytes.NewReader([]byte(`{"name":"x","type":"vm"}`)))
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("agent token POST /api/hosts = %d, want 403", rec.Code)
	}
}

func TestAgentReportCreatesThenUpdatesHost(t *testing.T) {
	h, db, raw := agentServer(t, domain.ScopeAgent)

	rec := postReport(t, h, raw, baseReport())
	if rec.Code != http.StatusOK {
		t.Fatalf("first report = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	var resp struct {
		HostID  int64    `json:"host_id"`
		Changed []string `json:"changed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.HostID == 0 {
		t.Fatalf("host_id = 0, want a created host")
	}

	got, err := store.NewHostRepo(db).Get(resp.HostID)
	if err != nil {
		t.Fatalf("Get host: %v", err)
	}
	if got.Name != "nas01" || got.RAM != "64 GB" || got.Type != "physical" {
		t.Fatalf("created host = %+v", got)
	}

	// Second report with more RAM updates in place and records one changelog row.
	r := baseReport()
	r.RAM = "128 GB"
	if rec := postReport(t, h, raw, r); rec.Code != http.StatusOK {
		t.Fatalf("second report = %d, want 200", rec.Code)
	}
	got, _ = store.NewHostRepo(db).Get(resp.HostID)
	if got.RAM != "128 GB" {
		t.Fatalf("RAM = %q, want 128 GB", got.RAM)
	}
	var hosts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hosts`).Scan(&hosts); err != nil {
		t.Fatalf("count hosts: %v", err)
	}
	if hosts != 1 {
		t.Fatalf("hosts = %d, want 1 (the second report must not duplicate)", hosts)
	}
}

// The heartbeat must not pollute the change history: an identical report writes
// no changelog row at all.
func TestAgentReportUnchangedWritesNoChangelog(t *testing.T) {
	h, db, raw := agentServer(t, domain.ScopeAgent)
	if rec := postReport(t, h, raw, baseReport()); rec.Code != http.StatusOK {
		t.Fatalf("first report = %d", rec.Code)
	}
	var before int
	if err := db.QueryRow(`SELECT COUNT(*) FROM changelog WHERE entity_type='host'`).Scan(&before); err != nil {
		t.Fatalf("count changelog: %v", err)
	}
	for i := 0; i < 3; i++ {
		if rec := postReport(t, h, raw, baseReport()); rec.Code != http.StatusOK {
			t.Fatalf("repeat report %d = %d", i, rec.Code)
		}
	}
	var after int
	if err := db.QueryRow(`SELECT COUNT(*) FROM changelog WHERE entity_type='host'`).Scan(&after); err != nil {
		t.Fatalf("count changelog: %v", err)
	}
	if after != before {
		t.Fatalf("changelog grew from %d to %d on identical reports", before, after)
	}
	// But each report is still stored as history.
	var reports int
	if err := db.QueryRow(`SELECT COUNT(*) FROM agent_reports`).Scan(&reports); err != nil {
		t.Fatalf("count agent_reports: %v", err)
	}
	if reports != 4 {
		t.Fatalf("agent_reports = %d, want 4", reports)
	}
}

// Adoption: an unbound agent whose hostname matches a hand-entered record must
// bind to it rather than create a second host.
func TestAgentReportAdoptsExistingHost(t *testing.T) {
	h, db, raw := agentServer(t, domain.ScopeAgent)
	id, err := store.NewHostRepo(db).Create(domain.Host{Name: "NAS01", Type: "vps", Notes: "typed by hand"})
	if err != nil {
		t.Fatalf("seed host: %v", err)
	}
	rec := postReport(t, h, raw, baseReport())
	if rec.Code != http.StatusOK {
		t.Fatalf("report = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	var resp struct {
		HostID int64 `json:"host_id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.HostID != id {
		t.Fatalf("host_id = %d, want the existing host %d", resp.HostID, id)
	}
	got, _ := store.NewHostRepo(db).Get(id)
	if got.Notes != "typed by hand" {
		t.Fatalf("Notes = %q, want preserved", got.Notes)
	}
	if got.Type != "vps" {
		t.Fatalf("Type = %q, want vps preserved on adoption", got.Type)
	}
	if got.Name != "NAS01" {
		t.Fatalf("Name = %q, want the hand-chosen name preserved", got.Name)
	}
}

func TestAgentReportCloneConflictReturns409(t *testing.T) {
	h, _, raw := agentServer(t, domain.ScopeAgent)
	if rec := postReport(t, h, raw, baseReport()); rec.Code != http.StatusOK {
		t.Fatalf("first report = %d", rec.Code)
	}
	clone := baseReport() // same AgentID: the state file was copied
	clone.Hostname = "web02"
	clone.Interfaces = []agentapi.Interface{{Name: "eth0", MAC: "de:ad:be:ef:00:09", Addrs: []string{"192.168.1.99"}}}
	rec := postReport(t, h, raw, clone)
	if rec.Code != http.StatusConflict {
		t.Fatalf("clone report = %d, want 409 (body %s)", rec.Code, rec.Body)
	}
}

func TestAgentReportRejectsUnknownSchemaVersion(t *testing.T) {
	h, _, raw := agentServer(t, domain.ScopeAgent)
	r := baseReport()
	r.SchemaVersion = 99
	if rec := postReport(t, h, raw, r); rec.Code != http.StatusBadRequest {
		t.Fatalf("future schema = %d, want 400", rec.Code)
	}
}

// Two concurrent first-time reports carrying the same never-before-seen
// agent_id but different hostnames/fingerprints must never both succeed and
// must never surface as a 500: whichever loses the race — whether it loses by
// seeing the winner's committed binding first (ordinary clone-conflict
// detection) or by losing the Upsert itself (a genuine UNIQUE-constraint race
// on agent_id, the new store.ErrAgentIDConflict path) — gets mapped to the
// same 409 a clone conflict gets. Both outcomes are legitimate depending on
// scheduling, so the assertion only pins down what must hold under either:
// exactly one 200 and one 409, never a 500 with a raw SQL string in the body.
func TestAgentReportAgentIDConflictReturns409(t *testing.T) {
	h, db, raw := agentServer(t, domain.ScopeAgent)

	a := baseReport()
	a.AgentID = "race-id"
	a.Hostname = "racer-a"
	a.Interfaces = []agentapi.Interface{{Name: "eth0", MAC: "aa:aa:aa:aa:aa:01", Addrs: []string{"192.168.1.11"}}}

	b := baseReport()
	b.AgentID = "race-id"
	b.Hostname = "racer-b"
	b.Interfaces = []agentapi.Interface{{Name: "eth0", MAC: "bb:bb:bb:bb:bb:02", Addrs: []string{"192.168.1.12"}}}

	var wg sync.WaitGroup
	codes := make([]int, 2)
	wg.Add(2)
	go func() { defer wg.Done(); codes[0] = postReport(t, h, raw, a).Code }()
	go func() { defer wg.Done(); codes[1] = postReport(t, h, raw, b).Code }()
	wg.Wait()

	var ok, conflict int
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("racing reports with the same agent_id got status %d, want 200 or 409 (codes=%v)", c, codes)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("racing reports with the same agent_id = %v, want exactly one 200 and one 409", codes)
	}

	// This is the assertion that actually pins the rollback fix: a regression
	// back to "conflict = true; return nil" on the ErrAgentIDConflict branch
	// would still produce one 200 and one 409 above, but would also commit the
	// loser's host write, leaving an orphaned host with no agent binding.
	var hosts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hosts`).Scan(&hosts); err != nil {
		t.Fatalf("count hosts: %v", err)
	}
	if hosts != 1 {
		t.Fatalf("hosts = %d after a losing race, want exactly 1 (the loser's create must have rolled back)", hosts)
	}
	var bindings int
	if err := db.QueryRow(`SELECT COUNT(*) FROM host_agents`).Scan(&bindings); err != nil {
		t.Fatalf("count host_agents: %v", err)
	}
	if bindings != 1 {
		t.Fatalf("host_agents = %d after a losing race, want exactly 1 (the loser's binding must have rolled back)", bindings)
	}
}
