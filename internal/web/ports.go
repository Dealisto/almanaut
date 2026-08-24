package web

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

// generatePorts bulk-creates "Prefix 1..N" ports on one owner. Names that
// already exist on that owner are skipped, so re-submitting the form never
// duplicates. Each created port goes through createEntityTx, keeping the
// changelog and webhook events consistent with single-port creation.
func generatePorts(rs resource[domain.Port], ports *store.PortRepo, d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ownerType, ownerID, err := parseRef(req.FormValue("owner"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		count, _ := strconv.Atoi(req.FormValue("count"))
		count = min(max(count, 1), 256)
		prefix := req.FormValue("prefix")
		if strings.TrimSpace(prefix) == "" {
			prefix = "Port "
		}
		// Validate the template port once up front so an invalid owner fails
		// before the transaction (name is per-port but constant-shaped).
		if err := (domain.Port{OwnerType: ownerType, OwnerID: ownerID, Name: prefix + "1"}).Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var events []webhook.Event
		err = store.WithTx(d.db, func(tx *sql.Tx) error {
			// The existing-name check must see the same snapshot the inserts
			// write into, so it reads through the tx-bound repo.
			existing, err := ports.WithTx(tx).List()
			if err != nil {
				return err
			}
			taken := map[string]bool{}
			for _, p := range existing {
				if p.OwnerType == ownerType && p.OwnerID == ownerID {
					taken[p.Name] = true
				}
			}
			for i := 1; i <= count; i++ {
				name := prefix + strconv.Itoa(i)
				if taken[name] {
					continue
				}
				item := domain.Port{OwnerType: ownerType, OwnerID: ownerID, Name: name}
				if _, err := rs.createEntityTx(tx, d, item, nil, actor(req), &events); err != nil {
					return fmt.Errorf("create %s: %w", name, err)
				}
			}
			return nil
		})
		if err != nil {
			serverError(w, req, err)
			return
		}
		d.webhooks.Dispatch(events...)
		http.Redirect(w, req, "/ports", http.StatusSeeOther)
	}
}
