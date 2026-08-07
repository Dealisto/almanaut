package web

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Dealisto/almanaut/internal/agentapi"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

// errAgentConflict signals a report that must be rejected with 409: either the
// fingerprint says a bound agent_id now belongs to a different machine, or the
// agent_id lost a race to bind to a host (store.ErrAgentIDConflict). Both are
// routed through the same sentinel so the transaction always rolls back (no
// host write survives a losing report) and the handler answers identically.
var errAgentConflict = errors.New("agent report conflict")

// agentRepo is the subset of *store.AgentRepo the report handler needs. It
// exists so agentDeps.agents can hold a fake in tests: two racing goroutines
// almost never actually land in the Upsert -> store.ErrAgentIDConflict
// branch (store.WithTx plus SQLite's own write serialization mean the loser
// typically observes the winner's committed binding first and takes the
// ordinary DecideConflict path instead), so a fake that forces
// ErrAgentIDConflict on demand is the only way to deterministically test that
// the resulting rollback actually happens.
type agentRepo interface {
	WithTx(tx *sql.Tx) agentRepo
	ByAgentID(agentID string) (store.AgentBinding, error)
	Upsert(b store.AgentBinding) error
	RecordReport(hostID int64, agentID, receivedAt, agentVersion string, schemaVersion int, payload []byte) error
}

// storeAgentRepo adapts *store.AgentRepo to agentRepo. The adapter is needed
// because store.AgentRepo.WithTx returns *store.AgentRepo — the concrete
// type — not agentRepo; Go interface satisfaction does not accept a
// covariant return type, so the concrete type alone cannot satisfy an
// interface whose own WithTx returns the interface. This wrapper re-wraps
// WithTx's result so it does, without touching store.AgentRepo itself.
type storeAgentRepo struct{ *store.AgentRepo }

func (r storeAgentRepo) WithTx(tx *sql.Tx) agentRepo {
	return storeAgentRepo{r.AgentRepo.WithTx(tx)}
}

// agentDeps is what the report handler needs. createHost/updateHost are
// closures over the hosts resource so that the agent path goes through the very
// same create/update code as the UI and the entity API — which is what gives it
// change history, no-op suppression and webhook dispatch without reimplementing
// any of the three.
type agentDeps struct {
	db         *sql.DB
	agents     agentRepo
	hosts      *store.HostRepo
	webhooks   webhook.Dispatcher
	createHost func(tx *sql.Tx, h domain.Host, actor string, events *[]webhook.Event) (int64, error)
	updateHost func(tx *sql.Tx, h domain.Host, actor string, events *[]webhook.Event) error
}

// agentReport ingests one host-fact report from the on-host agent.
func agentReport(d agentDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if s, ok := tokenScopeFrom(req.Context()); !ok || s != domain.ScopeAgent {
			writeJSONError(w, http.StatusForbidden, "this endpoint requires an agent-scoped token")
			return
		}
		u, ok := userFrom(req.Context())
		if !ok || !u.Role.CanWrite() {
			writeJSONError(w, http.StatusForbidden, "forbidden")
			return
		}

		raw, err := io.ReadAll(req.Body)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "could not read body")
			return
		}
		var rep agentapi.Report
		if err := json.Unmarshal(raw, &rep); err != nil {
			writeJSONError(w, http.StatusBadRequest, "malformed JSON")
			return
		}
		if err := rep.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}

		var (
			hostID  int64
			changed = []string{}
			events  []webhook.Event
		)
		err = store.WithTx(d.db, func(tx *sql.Tx) error {
			agents := d.agents.WithTx(tx)

			var existing *agentapi.Binding
			b, err := agents.ByAgentID(rep.AgentID)
			switch {
			case err == nil:
				existing = &agentapi.Binding{HostID: b.HostID, AgentID: b.AgentID, Fingerprint: b.Fingerprint}
			case errors.Is(err, store.ErrNotFound):
				// unbound agent: fall through to adoption
			default:
				return err
			}

			// Adoption needs every host, and this read drives a write, so it runs
			// on the tx-bound repo inside the transaction.
			var refs []agentapi.HostRef
			if existing == nil {
				list, err := d.hosts.WithTx(tx).List()
				if err != nil {
					return err
				}
				for _, h := range list {
					refs = append(refs, agentapi.HostRef{ID: h.ID, Name: h.Name, IPs: h.IPs})
				}
			}

			decision := agentapi.Resolve(rep, existing, refs)
			if decision.Kind == agentapi.DecideConflict {
				// Nothing has been written yet, so this is a plain rollback (of a
				// read-only transaction); errAgentConflict just routes the response.
				return errAgentConflict
			}

			actor := "agent:" + rep.Hostname
			switch decision.Kind {
			case agentapi.DecideCreate:
				h := mergeAgentReport(domain.Host{}, rep, true)
				if err := h.Validate(); err != nil {
					return err
				}
				id, err := d.createHost(tx, h, actor, &events)
				if err != nil {
					return err
				}
				hostID = id
				changed = changedFields(domain.Host{}, h)
			default: // DecideAdopt, DecideUpdate
				h, err := d.hosts.WithTx(tx).Get(decision.HostID)
				if err != nil {
					return err
				}
				merged := mergeAgentReport(h, rep, false)
				if err := merged.Validate(); err != nil {
					return err
				}
				if err := d.updateHost(tx, merged, actor, &events); err != nil {
					return err
				}
				hostID = decision.HostID
				changed = changedFields(h, merged)
			}

			now := nowRFC3339()
			if err := agents.Upsert(store.AgentBinding{
				HostID: hostID, AgentID: rep.AgentID, Fingerprint: rep.Fingerprint(), LastSeen: now,
			}); err != nil {
				// The host write above (create or update) must not survive a losing
				// race on agent_id: returning the error here rolls the whole
				// transaction back, so a loser never leaves an orphaned host behind.
				if errors.Is(err, store.ErrAgentIDConflict) {
					return errAgentConflict
				}
				return err
			}
			return agents.RecordReport(hostID, rep.AgentID, now, rep.AgentVersion, rep.SchemaVersion, raw)
		})
		if errors.Is(err, errAgentConflict) {
			writeJSONError(w, http.StatusConflict,
				"this agent_id is already bound to a different machine; run 'almanaut-agent reset-id' on the clone")
			return
		}
		if err != nil {
			apiServerError(w, req, err)
			return
		}
		d.webhooks.Dispatch(events...)
		writeJSON(w, http.StatusOK, map[string]any{"host_id": hostID, "changed": changed})
	}
}

// changedFields names the fields that differ between two host revisions, so the
// agent can log what its report actually altered. It reuses domain.Diff, the
// same comparison the changelog is built from, so the reported list and the
// recorded history can never disagree.
func changedFields(old, updated domain.Host) []string {
	out := []string{}
	changes, err := domain.Diff(old, updated)
	if err != nil {
		return out // the response field is a debugging aid, never a failure cause
	}
	for _, c := range changes {
		out = append(out, c.Field)
	}
	return out
}
