package agentapi

import (
	"net"
	"strings"
)

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
	// DecideAmbiguous: several unbound hosts match the reported name or IPs.
	// Refuse to modify any candidate until the inventory is disambiguated.
	DecideAmbiguous
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

// Decision is Resolve's verdict. HostID is 0 for creation or ambiguity.
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
	if id, ambiguous := matchHost(r, hosts); ambiguous {
		return Decision{Kind: DecideAmbiguous}
	} else if id != 0 {
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
		// Guard: strings.Split("", ",") returns []string{""}, not nil. Without this
		// check, an empty MAC list would become a one-element slice with a phantom
		// MAC that could spuriously match and suppress a real clone detection.
		return name, nil
	}
	return name, strings.Split(macs, ",")
}

// matchHost accepts exactly one candidate across normalized names and shared
// IPs. Conflicting signals must not let input order choose which host to edit.
// The boolean reports ambiguity, not whether a match was found.
func matchHost(r Report, hosts []HostRef) (int64, bool) {
	want := normalizeName(r.Hostname)
	reported := map[string]bool{}
	for _, ip := range r.ReportedIPs() {
		reported[ip] = true
	}
	var matched int64
	for _, h := range hosts {
		matches := want != "" && normalizeName(h.Name) == want
		for _, raw := range h.IPs {
			ip := net.ParseIP(strings.TrimSpace(raw))
			if ip != nil && reported[ip.String()] {
				matches = true
				break
			}
		}
		if matches {
			if matched != 0 && matched != h.ID {
				return 0, true
			}
			matched = h.ID
		}
	}
	return matched, false
}

// normalizeName mirrors discovery.NormalizeName. It is duplicated rather than
// imported so that agentapi stays free of server-only packages and can be
// linked into the agent binary.
func normalizeName(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
