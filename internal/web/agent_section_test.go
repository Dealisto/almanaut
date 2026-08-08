package web

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/agentapi"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// agentSectionDB seeds a database with hosts and returns the repos the builder
// needs, so each test states only what it is about.
func agentSectionDB(t *testing.T, hosts ...domain.Host) (*store.AgentRepo, *store.HostRepo) {
	t.Helper()
	db := rbacDB(t)
	hr := store.NewHostRepo(db)
	for _, h := range hosts {
		if _, err := hr.Create(h); err != nil {
			t.Fatalf("create host %q: %v", h.Name, err)
		}
	}
	return store.NewAgentRepo(db), hr
}

func TestAgentSectionUnboundHostStillRenders(t *testing.T) {
	agents, hosts := agentSectionDB(t, domain.Host{Name: "nas01", Type: "physical"})
	sec := agentSectionFor(agents, hosts, entityCatalog{})(domain.Host{ID: 1, Name: "nas01"})
	if sec == nil {
		t.Fatal("section is nil for an unbound host; the panel must explain how to install the agent")
	}
	if sec.Bound {
		t.Fatalf("Bound = true for a host with no agent: %+v", sec)
	}
}

func TestAgentSectionShowsBindingAndReport(t *testing.T) {
	agents, hosts := agentSectionDB(t, domain.Host{Name: "nas01", Type: "physical"})
	if err := agents.Upsert(store.AgentBinding{
		HostID: 1, AgentID: "uuid-1", Fingerprint: "nas01|aa", LastSeen: "2026-08-07T10:00:00Z",
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	rep := agentapi.Report{
		SchemaVersion: agentapi.SchemaVersion, AgentID: "uuid-1", AgentVersion: "1.2.3",
		Hostname: "nas01", Kernel: "6.1.0-18-amd64", Uptime: 90061,
		Disks:      []agentapi.Disk{{Device: "nvme0n1", Model: "Samsung 990", SizeBytes: 1000204886016}},
		Interfaces: []agentapi.Interface{{Name: "eth0", MAC: "aa:bb:cc:00:00:01", Addrs: []string{"192.168.1.10"}}},
	}
	payload, _ := json.Marshal(rep)
	if err := agents.RecordReport(1, "uuid-1", "2026-08-07T10:00:00Z", "1.2.3", 1, payload); err != nil {
		t.Fatalf("RecordReport: %v", err)
	}

	sec := agentSectionFor(agents, hosts, entityCatalog{})(domain.Host{ID: 1, Name: "nas01"})
	if !sec.Bound || sec.AgentID != "uuid-1" || sec.AgentVersion != "1.2.3" {
		t.Fatalf("binding facts wrong: %+v", sec)
	}
	if sec.Kernel != "6.1.0-18-amd64" {
		t.Fatalf("Kernel = %q", sec.Kernel)
	}
	// humanizeBytes is base-1024 with an "iB" suffix, so a ~1TB drive's
	// marketed capacity renders as GiB, not TB; pin the exact string rather
	// than a substring so a future unit change is a deliberate, visible edit.
	if len(sec.Disks) != 1 || sec.Disks[0].Device != "nvme0n1" || sec.Disks[0].Size != "931.5 GiB" {
		t.Fatalf("disks = %+v", sec.Disks)
	}
	if len(sec.Interfaces) != 1 || sec.Interfaces[0].Addrs != "192.168.1.10" {
		t.Fatalf("interfaces = %+v", sec.Interfaces)
	}
	// 90061s is 1 day, 1 hour, 1 minute — the panel must not print raw seconds.
	if sec.Uptime == "" || strings.Contains(sec.Uptime, "90061") {
		t.Fatalf("Uptime = %q, want a human duration", sec.Uptime)
	}
}

// A bound host whose report cannot be parsed must still render its binding.
// Losing the whole host page because one JSON blob is malformed would be a
// self-inflicted outage.
func TestAgentSectionSurvivesAnUnparseableReport(t *testing.T) {
	agents, hosts := agentSectionDB(t, domain.Host{Name: "nas01", Type: "physical"})
	_ = agents.Upsert(store.AgentBinding{HostID: 1, AgentID: "uuid-1", Fingerprint: "f", LastSeen: "t"})
	if err := agents.RecordReport(1, "uuid-1", "t", "1.0.0", 1, []byte("{not json")); err != nil {
		t.Fatalf("RecordReport: %v", err)
	}
	sec := agentSectionFor(agents, hosts, entityCatalog{})(domain.Host{ID: 1, Name: "nas01"})
	if sec == nil || !sec.Bound || sec.AgentID != "uuid-1" {
		t.Fatalf("binding lost when the report was unparseable: %+v", sec)
	}
	if len(sec.Disks) != 0 || len(sec.Interfaces) != 0 {
		t.Fatalf("invented report detail from bad JSON: %+v", sec)
	}
}

func TestAgentSectionShowsTheConflict(t *testing.T) {
	agents, hosts := agentSectionDB(t, domain.Host{Name: "nas01", Type: "physical"})
	_ = agents.Upsert(store.AgentBinding{HostID: 1, AgentID: "uuid-1", Fingerprint: "f", LastSeen: "t"})
	if err := agents.RecordConflict("uuid-1", "web02", "2026-08-07T12:00:00Z"); err != nil {
		t.Fatalf("RecordConflict: %v", err)
	}
	sec := agentSectionFor(agents, hosts, entityCatalog{})(domain.Host{ID: 1, Name: "nas01"})
	if sec.ConflictHostname != "web02" || sec.ConflictAt == "" {
		t.Fatalf("conflict not surfaced: %+v", sec)
	}
}

// The duplicate a lost /var/lib/almanaut-agent produces: two records, same
// name, same addresses, each with its own binding.
func TestAgentSectionFlagsADuplicateByName(t *testing.T) {
	agents, hosts := agentSectionDB(t,
		domain.Host{Name: "nas01", Type: "physical"},
		domain.Host{Name: "NAS01", Type: "physical"},
	)
	_ = agents.Upsert(store.AgentBinding{HostID: 1, AgentID: "old", Fingerprint: "f", LastSeen: "t"})
	_ = agents.Upsert(store.AgentBinding{HostID: 2, AgentID: "new", Fingerprint: "f", LastSeen: "t"})

	sec := agentSectionFor(agents, hosts, entityCatalog{})(domain.Host{ID: 1, Name: "nas01"})
	if len(sec.Duplicates) != 1 || sec.Duplicates[0].ID != 2 {
		t.Fatalf("duplicates = %+v, want host 2", sec.Duplicates)
	}
	if !strings.Contains(strings.ToLower(sec.Duplicates[0].Reason), "name") {
		t.Fatalf("Reason = %q, want it to explain the match", sec.Duplicates[0].Reason)
	}
}

func TestAgentSectionFlagsADuplicateBySharedIP(t *testing.T) {
	agents, hosts := agentSectionDB(t,
		domain.Host{Name: "nas01", Type: "physical", IPs: []string{"192.168.1.10"}},
		domain.Host{Name: "totally-different", Type: "physical", IPs: []string{"192.168.1.10"}},
	)
	_ = agents.Upsert(store.AgentBinding{HostID: 2, AgentID: "new", Fingerprint: "f", LastSeen: "t"})

	sec := agentSectionFor(agents, hosts, entityCatalog{})(
		domain.Host{ID: 1, Name: "nas01", IPs: []string{"192.168.1.10"}})
	if len(sec.Duplicates) != 1 || sec.Duplicates[0].ID != 2 {
		t.Fatalf("duplicates = %+v, want host 2", sec.Duplicates)
	}
	if !strings.Contains(sec.Duplicates[0].Reason, "192.168.1.10") {
		t.Fatalf("Reason = %q, want it to name the shared address", sec.Duplicates[0].Reason)
	}
}

// Two hand-entered hosts sharing a name are the operator's business, not the
// agent's — the panel must not nag about them.
func TestAgentSectionIgnoresDuplicatesWhenNeitherIsBound(t *testing.T) {
	agents, hosts := agentSectionDB(t,
		domain.Host{Name: "nas01", Type: "physical"},
		domain.Host{Name: "nas01", Type: "physical"},
	)
	sec := agentSectionFor(agents, hosts, entityCatalog{})(domain.Host{ID: 1, Name: "nas01"})
	if len(sec.Duplicates) != 0 {
		t.Fatalf("duplicates = %+v, want none when no agent is involved", sec.Duplicates)
	}
}

func TestAgentSectionNeverListsItself(t *testing.T) {
	agents, hosts := agentSectionDB(t, domain.Host{Name: "nas01", Type: "physical", IPs: []string{"10.0.0.1"}})
	_ = agents.Upsert(store.AgentBinding{HostID: 1, AgentID: "uuid-1", Fingerprint: "f", LastSeen: "t"})
	sec := agentSectionFor(agents, hosts, entityCatalog{})(
		domain.Host{ID: 1, Name: "nas01", IPs: []string{"10.0.0.1"}})
	for _, d := range sec.Duplicates {
		if d.ID == 1 {
			t.Fatalf("the host listed itself as a duplicate: %+v", sec.Duplicates)
		}
	}
}
