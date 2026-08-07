package agentapi

import "strings"

// DecisionKind is what the server should do with an incoming report.
type DecisionKind int

const (
	// DecideCreate: no binding and no plausible existing host — insert one.
	DecideCreate DecisionKind = iota
	// DecideAdopt: no binding, but an existing host matches — bind to it rather
	// than duplicating a hand-entered record.
	DecideAdopt
	// DecideUpdate: this agent is already bound to a host.
	DecideUpdate
	// DecideConflict: the agent_id is bound, but the machine behind it is a
	// different one — a cloned VM that copied the agent's state file.
	DecideConflict
)

// Binding is an existing agent_id -> host_id link with its last fingerprint.
type Binding struct {
	HostID      int64
	AgentID     string
	Fingerprint string
}

// HostRef is the minimum an existing host contributes to adoption matching.
type HostRef struct {
	ID   int64
	Name string
	IPs  []string
}

// Decision is Resolve's verdict. HostID is 0 for DecideCreate.
type Decision struct {
	Kind   DecisionKind
	HostID int64
}

// Resolve decides which host a report belongs to.
//
// Adoption exists so the agent's first run binds to the record you already
// typed by hand instead of creating a duplicate of it. Conflict detection is
// deliberately conservative: only a report whose hostname AND every MAC differ
// from the stored fingerprint is treated as a clone, because a machine that was
// merely renamed, or that gained a NIC, is still the same machine. Guessing
// wrong in the other direction silently forks the inventory.
func Resolve(r Report, existing *Binding, hosts []HostRef) Decision {
	if existing != nil {
		if isClone(existing.Fingerprint, r.Fingerprint()) {
			return Decision{Kind: DecideConflict, HostID: existing.HostID}
		}
		return Decision{Kind: DecideUpdate, HostID: existing.HostID}
	}
	if id, ok := matchHost(r, hosts); ok {
		return Decision{Kind: DecideAdopt, HostID: id}
	}
	return Decision{Kind: DecideCreate}
}

// isClone reports whether old and now share neither their hostname nor a single
// MAC address.
func isClone(old, now string) bool {
	oldName, oldMACs := splitFingerprint(old)
	newName, newMACs := splitFingerprint(now)
	if oldName == newName {
		return false
	}
	for _, m := range newMACs {
		for _, o := range oldMACs {
			if m == o {
				return false
			}
		}
	}
	return true
}

func splitFingerprint(fp string) (string, []string) {
	name, macs, _ := strings.Cut(fp, "|")
	if macs == "" {
		return name, nil
	}
	return name, strings.Split(macs, ",")
}

// matchHost finds an existing host for an unbound agent: by normalized name
// first, then by any shared IP.
func matchHost(r Report, hosts []HostRef) (int64, bool) {
	want := normalizeName(r.Hostname)
	for _, h := range hosts {
		if normalizeName(h.Name) == want {
			return h.ID, true
		}
	}
	reported := map[string]bool{}
	for _, ip := range r.ReportedIPs() {
		reported[ip] = true
	}
	for _, h := range hosts {
		for _, ip := range h.IPs {
			if reported[ip] {
				return h.ID, true
			}
		}
	}
	return 0, false
}

// normalizeName mirrors discovery.NormalizeName. It is duplicated rather than
// imported so that agentapi stays free of server-only packages and can be
// linked into the agent binary.
func normalizeName(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
