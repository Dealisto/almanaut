package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/go-chi/chi/v5"
)

// unbindAgent handles the host detail page's "unbind agent" POST. Freeing the
// host's agent binding is the recovery path for a host whose agent lost its
// state file (or had reset-id run on the wrong clone): with no binding in the
// way, the next report from an unbound agent can match this host by hostname
// or IP and re-adopt it, instead of creating a second record.
//
// It is not a resource[T] method (the agent binding lives outside the generic
// CRUD surface), so it parses the id param directly and looks the host up
// itself, matching probeCertificate's shape.
func unbindAgent(d handlerDeps, agents *store.AgentRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		host, err := store.NewHostRepo(d.db).Get(id)
		if err != nil {
			notFoundOrServerError(w, r, "host", err)
			return
		}

		act := actor(r)
		txErr := store.WithTx(d.db, func(tx *sql.Tx) error {
			txAgents := agents.WithTx(tx)
			// Read the binding before removing it so the changelog entry can
			// name the agent id that was freed; ErrNotFound here just means
			// there is nothing to report, so oldAgentID stays "".
			binding, bindErr := txAgents.ByHostID(id)
			if err := txAgents.Unbind(id); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					// Nothing was bound: the operator's desired end state
					// already holds, so this is not recorded as a change.
					return nil
				}
				return err
			}
			oldAgentID := ""
			if bindErr == nil {
				oldAgentID = binding.AgentID
			}
			return d.changelog.WithTx(tx).Create(store.ChangeEvent{
				EntityType: "host", EntityID: id, Label: host.Name,
				Action: domain.ActionUpdate, Actor: act,
				Changes:   []domain.FieldChange{{Field: "agent", Old: oldAgentID, New: ""}},
				CreatedAt: nowRFC3339(),
			})
		})
		if txErr != nil {
			serverError(w, r, txErr)
			return
		}
		http.Redirect(w, r, d.cat.path("host", id), http.StatusSeeOther)
	}
}
