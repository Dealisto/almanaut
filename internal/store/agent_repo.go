package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// agentReportRetention is how many reports are kept per host. Reports exist to
// show the current machine state and to give recent history a look-back; older
// ones are dead weight, so RecordReport prunes on write.
const agentReportRetention = 30

// AgentBinding links one agent's self-generated UUID to a host, with the
// fingerprint used to notice a cloned VM reusing that UUID.
type AgentBinding struct {
	HostID      int64
	AgentID     string
	Fingerprint string
	LastSeen    string // RFC3339 UTC

	// LastConflictAt and LastConflictHostname record the most recent report
	// rejected as a clone of this binding: a different machine using this
	// agent id. Both are empty when there is no outstanding conflict, and are
	// cleared by the next successful report.
	LastConflictAt       string
	LastConflictHostname string
}

// AgentRepo persists agent bindings and report history.
type AgentRepo struct {
	db DBTX
}

// NewAgentRepo returns an AgentRepo backed by db.
func NewAgentRepo(db *sql.DB) *AgentRepo { return &AgentRepo{db: db} }

// WithTx returns a copy of the repo whose operations run inside tx.
func (r *AgentRepo) WithTx(tx *sql.Tx) *AgentRepo { return &AgentRepo{db: tx} }

// ByAgentID returns the binding for agentID, or ErrNotFound.
func (r *AgentRepo) ByAgentID(agentID string) (AgentBinding, error) {
	var b AgentBinding
	err := r.db.QueryRow(
		`SELECT host_id, agent_id, fingerprint, last_seen, last_conflict_at, last_conflict_hostname FROM host_agents WHERE agent_id = ?`,
		agentID,
	).Scan(&b.HostID, &b.AgentID, &b.Fingerprint, &b.LastSeen, &b.LastConflictAt, &b.LastConflictHostname)
	if err != nil {
		return AgentBinding{}, notFound(fmt.Errorf("scan agent binding: %w", err))
	}
	return b, nil
}

// Upsert creates or refreshes the binding for b.HostID.
func (r *AgentRepo) Upsert(b AgentBinding) error {
	if _, err := r.db.Exec(
		`INSERT INTO host_agents (host_id, agent_id, fingerprint, last_seen)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(host_id) DO UPDATE SET
		     agent_id = excluded.agent_id,
		     fingerprint = excluded.fingerprint,
		     last_seen = excluded.last_seen,
		     last_conflict_at = '',
		     last_conflict_hostname = ''`,
		b.HostID, b.AgentID, b.Fingerprint, b.LastSeen,
	); err != nil {
		// Detect agent_id UNIQUE constraint violation and wrap with sentinel.
		if strings.Contains(err.Error(), "UNIQUE constraint failed: host_agents.agent_id") {
			return fmt.Errorf("upsert agent binding: %w", ErrAgentIDConflict)
		}
		return fmt.Errorf("upsert agent binding: %w", err)
	}
	return nil
}

// BoundHostIDs returns the set of host ids that already have a binding, so
// callers can exclude them from adoption matching: a host already bound to one
// agent must never be handed to a different, unbound agent (that would steal
// the binding and, once the first agent reports again, cause the two to
// oscillate the record back and forth forever).
func (r *AgentRepo) BoundHostIDs() (map[int64]bool, error) {
	rows, err := r.db.Query(`SELECT host_id FROM host_agents`)
	if err != nil {
		return nil, fmt.Errorf("query bound host ids: %w", err)
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan bound host id: %w", err)
		}
		out[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bound host ids: %w", err)
	}
	return out, nil
}

// ByHostID returns the agent binding for a host, or ErrNotFound when the host
// has no agent reporting for it.
func (r *AgentRepo) ByHostID(hostID int64) (AgentBinding, error) {
	var b AgentBinding
	err := r.db.QueryRow(
		`SELECT host_id, agent_id, fingerprint, last_seen, last_conflict_at, last_conflict_hostname
		 FROM host_agents WHERE host_id = ?`, hostID,
	).Scan(&b.HostID, &b.AgentID, &b.Fingerprint, &b.LastSeen, &b.LastConflictAt, &b.LastConflictHostname)
	if err != nil {
		return AgentBinding{}, notFound(fmt.Errorf("scan agent binding: %w", err))
	}
	return b, nil
}

// Unbind removes a host's agent binding, returning ErrNotFound when there was
// none. Freeing the agent id is what makes the host adoptable again: the next
// report from an unbound agent matches it by hostname or IP and re-binds,
// which is the recovery path for a host record whose agent lost its state file.
func (r *AgentRepo) Unbind(hostID int64) error {
	res, err := r.db.Exec(`DELETE FROM host_agents WHERE host_id = ?`, hostID)
	if err != nil {
		return fmt.Errorf("unbind agent: %w", err)
	}
	return rowsAffectedOrNotFound(res)
}

// RecordConflict stamps a binding with the machine that was rejected as a clone
// of it.
//
// An unknown agent id is not an error. This runs after agentReport's
// transaction has already rolled back, so the binding may legitimately have
// been removed in between — and failing here would turn a clean 409 into a 500,
// telling the operator the server broke when it in fact did its job.
func (r *AgentRepo) RecordConflict(agentID, hostname, at string) error {
	if _, err := r.db.Exec(
		`UPDATE host_agents SET last_conflict_at = ?, last_conflict_hostname = ? WHERE agent_id = ?`,
		at, hostname, agentID,
	); err != nil {
		return fmt.Errorf("record agent conflict: %w", err)
	}
	return nil
}

// RecordReport appends one report and prunes the host's history to
// agentReportRetention rows.
func (r *AgentRepo) RecordReport(hostID int64, agentID, receivedAt, agentVersion string, schemaVersion int, payload []byte) error {
	if _, err := r.db.Exec(
		`INSERT INTO agent_reports (host_id, agent_id, received_at, agent_version, schema_version, payload)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		hostID, agentID, receivedAt, agentVersion, schemaVersion, string(payload),
	); err != nil {
		return fmt.Errorf("insert agent report: %w", err)
	}
	if _, err := r.db.Exec(
		`DELETE FROM agent_reports
		 WHERE host_id = ? AND id NOT IN (
		     SELECT id FROM agent_reports WHERE host_id = ? ORDER BY id DESC LIMIT ?
		 )`,
		hostID, hostID, agentReportRetention,
	); err != nil {
		return fmt.Errorf("prune agent reports: %w", err)
	}
	return nil
}

// LatestReport returns the newest raw payload for hostID, or ErrNotFound.
func (r *AgentRepo) LatestReport(hostID int64) (string, error) {
	var payload string
	err := r.db.QueryRow(
		`SELECT payload FROM agent_reports WHERE host_id = ? ORDER BY id DESC LIMIT 1`, hostID,
	).Scan(&payload)
	if err != nil {
		return "", notFound(fmt.Errorf("scan agent report: %w", err))
	}
	return payload, nil
}
