package web

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Dealisto/almanaut/internal/agentapi"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
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

// An agent-scoped token must not be able to read the entity API either: it is
// deployed on every machine in the fleet, so a compromised container reading
// it must not leak the whole inventory. requireWrite alone lets every GET
// through regardless of scope, so this is a distinct gate (rejectAgentScope).
func TestAgentScopeCannotReadHosts(t *testing.T) {
	h, _, raw := agentServer(t, domain.ScopeAgent)
	req := httptest.NewRequest(http.MethodGet, "/api/hosts", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("agent token GET /api/hosts = %d, want 403 (body %s)", rec.Code, rec.Body)
	}
}

// Same as above for /api/search: the agent scope must not reach any read
// route in the main API group, not just the entity CRUD ones.
func TestAgentScopeCannotSearch(t *testing.T) {
	h, _, raw := agentServer(t, domain.ScopeAgent)
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=x", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("agent token GET /api/search = %d, want 403 (body %s)", rec.Code, rec.Body)
	}
}

// A read-only token must still be able to read the entity API: the new
// agent-scope gate must not collaterally block any other scope's reads.
func TestReadOnlyScopeCanReadHosts(t *testing.T) {
	h, _, raw := agentServer(t, domain.ScopeReadOnly)
	req := httptest.NewRequest(http.MethodGet, "/api/hosts", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("read-only token GET /api/hosts = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
}

// A read-write token must still be able to both read and write the entity
// API: the new gate is scoped to ScopeAgent only.
func TestReadWriteScopeCanReadAndWriteHosts(t *testing.T) {
	h, _, raw := agentServer(t, domain.ScopeReadWrite)

	getReq := httptest.NewRequest(http.MethodGet, "/api/hosts", nil)
	getReq.Header.Set("Authorization", "Bearer "+raw)
	getRec := httptest.NewRecorder()
	h.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("read-write token GET /api/hosts = %d, want 200 (body %s)", getRec.Code, getRec.Body)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/hosts", bytes.NewReader([]byte(`{"name":"nas","type":"physical"}`)))
	postReq.Header.Set("Authorization", "Bearer "+raw)
	postRec := httptest.NewRecorder()
	h.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusCreated {
		t.Fatalf("read-write token POST /api/hosts = %d, want 201 (body %s)", postRec.Code, postRec.Body)
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

// Adoption must skip a host that is already bound to a different agent.
// Without this, two machines sharing a normalized hostname would fight over
// the same record forever: whichever agent is currently unbound adopts it,
// overwriting the other's facts, and the next time the rightful agent reports
// it re-adopts and flips the binding back — one changelog row and one webhook
// per cycle, forever. This pins that the second agent instead gets its own
// host, host 7's fields are untouched, and the original binding survives.
func TestAgentAdoptionSkipsHostBoundToDifferentAgent(t *testing.T) {
	h, db, raw := agentServer(t, domain.ScopeAgent)

	seedID, err := store.NewHostRepo(db).Create(domain.Host{Name: "docker", Type: "lxc", Notes: "hand-entered"})
	if err != nil {
		t.Fatalf("seed host: %v", err)
	}

	// Agent A is unbound and adopts the seed host by normalized hostname match.
	a := baseReport()
	a.AgentID = "agent-a"
	a.Hostname = "docker"
	a.RAM = "8 GB"
	recA := postReport(t, h, raw, a)
	if recA.Code != http.StatusOK {
		t.Fatalf("agent A report = %d, want 200 (body %s)", recA.Code, recA.Body)
	}
	var respA struct {
		HostID int64 `json:"host_id"`
	}
	if err := json.Unmarshal(recA.Body.Bytes(), &respA); err != nil {
		t.Fatalf("decode agent A response: %v", err)
	}
	if respA.HostID != seedID {
		t.Fatalf("agent A host_id = %d, want the seeded host %d", respA.HostID, seedID)
	}
	afterA, err := store.NewHostRepo(db).Get(seedID)
	if err != nil {
		t.Fatalf("Get host after A: %v", err)
	}

	// Agent B is also unbound and reports the very same hostname. Host 7 is
	// already bound to agent A, so B must NOT adopt it — it must create a new
	// host instead.
	b := baseReport()
	b.AgentID = "agent-b"
	b.Hostname = "docker"
	b.RAM = "16 GB"
	b.Interfaces = []agentapi.Interface{{Name: "eth0", MAC: "bb:bb:bb:00:00:02", Addrs: []string{"10.0.0.20"}}}
	recB := postReport(t, h, raw, b)
	if recB.Code != http.StatusOK {
		t.Fatalf("agent B report = %d, want 200 (body %s)", recB.Code, recB.Body)
	}
	var respB struct {
		HostID int64 `json:"host_id"`
	}
	if err := json.Unmarshal(recB.Body.Bytes(), &respB); err != nil {
		t.Fatalf("decode agent B response: %v", err)
	}
	if respB.HostID == seedID {
		t.Fatalf("agent B was allowed to steal host %d, which is already bound to agent A", seedID)
	}

	// Host 7's fields must be exactly what agent A left them as — untouched by
	// B's report.
	afterB, err := store.NewHostRepo(db).Get(seedID)
	if err != nil {
		t.Fatalf("Get host after B: %v", err)
	}
	if !reflect.DeepEqual(afterA, afterB) {
		t.Fatalf("host %d changed after agent B's report: before=%+v after=%+v", seedID, afterA, afterB)
	}

	// host_agents must still map host 7 to agent A, not agent B.
	var boundAgent string
	if err := db.QueryRow(`SELECT agent_id FROM host_agents WHERE host_id = ?`, seedID).Scan(&boundAgent); err != nil {
		t.Fatalf("query host_agents: %v", err)
	}
	if boundAgent != "agent-a" {
		t.Fatalf("host_agents.agent_id for host %d = %q, want agent-a", seedID, boundAgent)
	}

	// Agent A reporting again must still resolve to host 7 (no oscillation).
	a2 := baseReport()
	a2.AgentID = "agent-a"
	a2.Hostname = "docker"
	a2.RAM = "8 GB"
	recA2 := postReport(t, h, raw, a2)
	if recA2.Code != http.StatusOK {
		t.Fatalf("agent A second report = %d, want 200 (body %s)", recA2.Code, recA2.Body)
	}
	var respA2 struct {
		HostID int64 `json:"host_id"`
	}
	if err := json.Unmarshal(recA2.Body.Bytes(), &respA2); err != nil {
		t.Fatalf("decode agent A second response: %v", err)
	}
	if respA2.HostID != seedID {
		t.Fatalf("agent A second report host_id = %d, want the original host %d (oscillation detected)", respA2.HostID, seedID)
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

// A report whose interface addresses are all junk (link-local, loopback, or
// unparseable — the sort net.Interfaces() can actually produce) must still
// succeed: ReportedIPs() sanitizes them down to an empty list, and an empty
// list means "unknown", not "clear the field" — mergeAgentReport only
// replaces IPs when ReportedIPs() is non-empty. Before agentapi sanitized
// these forms, they reached domain.Host.Validate() unparsed and turned every
// report from a real machine into a 500.
func TestAgentReportAllJunkIPsPreservesExisting(t *testing.T) {
	h, db, raw := agentServer(t, domain.ScopeAgent)

	first := baseReport()
	rec := postReport(t, h, raw, first)
	if rec.Code != http.StatusOK {
		t.Fatalf("first report = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	var resp struct {
		HostID int64 `json:"host_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	junk := baseReport()
	junk.Interfaces = []agentapi.Interface{
		{Name: "lo", MAC: "", Addrs: []string{"127.0.0.1", "::1"}},
		{Name: "eth0", MAC: "aa:bb:cc:00:00:01", Addrs: []string{"fe80::1%eth0", "not-an-ip"}},
	}
	rec2 := postReport(t, h, raw, junk)
	if rec2.Code != http.StatusOK {
		t.Fatalf("all-junk-IPs report = %d, want 200 (body %s)", rec2.Code, rec2.Body)
	}

	got, err := store.NewHostRepo(db).Get(resp.HostID)
	if err != nil {
		t.Fatalf("Get host: %v", err)
	}
	if len(got.IPs) != 1 || got.IPs[0] != "192.168.1.10" {
		t.Fatalf("IPs = %v, want the original [192.168.1.10] preserved", got.IPs)
	}
}

// Defence in depth: if a merged host somehow still fails domain.Host.Validate
// — here simulated by a pre-existing row whose Type was valid when written but
// is no longer in domain.HostTypes, inserted directly to bypass Create's own
// validation — the handler must answer 400 (bad input) rather than routing it
// through apiServerError as a 500. mergeAgentReport does not touch Type on an
// update, so the invalid value survives into the merged host and Validate()
// rejects it.
func TestAgentReportInvalidMergedHostReturns400(t *testing.T) {
	h, db, raw := agentServer(t, domain.ScopeAgent)

	res, err := db.Exec(`INSERT INTO hosts (name, type) VALUES (?, ?)`, "legacy-host", "container")
	if err != nil {
		t.Fatalf("insert legacy host: %v", err)
	}
	hostID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	now := nowRFC3339()
	if err := store.NewAgentRepo(db).Upsert(store.AgentBinding{
		HostID: hostID, AgentID: "legacy-agent", Fingerprint: "legacy-host|", LastSeen: now,
	}); err != nil {
		t.Fatalf("seed binding: %v", err)
	}

	r := baseReport()
	r.AgentID = "legacy-agent"
	r.Hostname = "legacy-host"
	rec := postReport(t, h, raw, r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update producing an invalid host = %d, want 400 (body %s)", rec.Code, rec.Body)
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

	// These two counts are a general invariant of a losing report (whichever
	// conflict branch produced it) and cost nothing to check here, but they do
	// NOT reliably exercise the Upsert -> store.ErrAgentIDConflict branch
	// specifically: empirically, on this platform, the loser's ByAgentID read
	// almost always observes the winner's already-committed binding first and
	// takes the ordinary DecideConflict path instead, which never writes a
	// host in the first place. TestAgentReportRollsBackHostOnAgentIDConflict
	// below is the test that actually pins that branch's rollback, by forcing
	// it deterministically instead of racing for it.
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

// fakeAgentRepo wraps a real *store.AgentRepo but can be told to fail Upsert
// on demand, so a test can force the store.ErrAgentIDConflict branch
// deterministically instead of hoping two racing goroutines land on it.
type fakeAgentRepo struct {
	real      *store.AgentRepo
	upsertErr error // non-nil: Upsert returns this instead of delegating
}

func (f fakeAgentRepo) WithTx(tx *sql.Tx) agentRepo {
	return fakeAgentRepo{real: f.real.WithTx(tx), upsertErr: f.upsertErr}
}

func (f fakeAgentRepo) ByAgentID(agentID string) (store.AgentBinding, error) {
	return f.real.ByAgentID(agentID)
}

func (f fakeAgentRepo) Upsert(b store.AgentBinding) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	return f.real.Upsert(b)
}

func (f fakeAgentRepo) RecordReport(hostID int64, agentID, receivedAt, agentVersion string, schemaVersion int, payload []byte) error {
	return f.real.RecordReport(hostID, agentID, receivedAt, agentVersion, schemaVersion, payload)
}

func (f fakeAgentRepo) BoundHostIDs() (map[int64]bool, error) {
	return f.real.BoundHostIDs()
}

// TestAgentReportRollsBackHostOnAgentIDConflict is the test that actually
// pins the errAgentConflict rollback fix. TestAgentReportAgentIDConflictReturns409
// races two real HTTP requests but — empirically, on this platform — the
// loser almost always takes the pre-existing DecideConflict path rather than
// the Upsert -> store.ErrAgentIDConflict one, so it never proves the specific
// branch this test targets: it substitutes a fakeAgentRepo whose Upsert
// always fails with a wrapped store.ErrAgentIDConflict, forcing an ordinary,
// otherwise-successful create straight into that branch. A regression back to
// "conflict = true; return nil" there would commit the host this test's
// createHost call writes and still answer 409 — the hosts/host_agents counts
// below are what would catch that; the status code and body alone would not.
func TestAgentReportRollsBackHostOnAgentIDConflict(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	hosts := store.NewHostRepo(db)
	hostRS := resource[domain.Host]{
		sing:  "host",
		repo:  hosts,
		label: func(h domain.Host) string { return h.Name },
		id:    func(h domain.Host) int64 { return h.ID },
	}
	deps := handlerDeps{
		changelog:    store.NewChangelogRepo(db),
		customFields: store.NewCustomFieldRepo(db),
		db:           db,
	}

	fake := fakeAgentRepo{
		real:      store.NewAgentRepo(db),
		upsertErr: fmt.Errorf("insert host_agents: %w", store.ErrAgentIDConflict),
	}
	dispatcher := &recordingDispatcher{}

	d := agentDeps{
		db:       db,
		agents:   fake,
		hosts:    hosts,
		webhooks: dispatcher,
		createHost: func(tx *sql.Tx, h domain.Host, actor string, ev *[]webhook.Event) (int64, error) {
			return hostRS.createEntityTx(tx, deps, h, nil, actor, ev)
		},
		updateHost: func(tx *sql.Tx, h domain.Host, actor string, ev *[]webhook.Event) error {
			return hostRS.updateEntityTx(tx, deps, h, nil, actor, ev)
		},
	}

	body, err := json.Marshal(baseReport())
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/agent/report", bytes.NewReader(body))
	ctx := withTokenScope(req.Context(), domain.ScopeAgent)
	ctx = withUser(ctx, domain.User{ID: 1, Username: "alice", Role: domain.RoleEditor})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	agentReport(d)(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("forced ErrAgentIDConflict = %d, want 409 (body %s)", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "UNIQUE constraint") || strings.Contains(rec.Body.String(), "SQL") {
		t.Fatalf("409 body leaks a raw SQL string: %s", rec.Body)
	}

	var hostCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM hosts`).Scan(&hostCount); err != nil {
		t.Fatalf("count hosts: %v", err)
	}
	if hostCount != 0 {
		t.Fatalf("hosts = %d, want 0 (the create inside the rolled-back transaction must not survive an ErrAgentIDConflict)", hostCount)
	}
	var bindingCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM host_agents`).Scan(&bindingCount); err != nil {
		t.Fatalf("count host_agents: %v", err)
	}
	if bindingCount != 0 {
		t.Fatalf("host_agents = %d, want 0", bindingCount)
	}
	if got := dispatcher.all(); len(got) != 0 {
		t.Fatalf("dispatched %d webhook event(s), want 0 (nothing should be dispatched for a rolled-back transaction)", len(got))
	}
}
