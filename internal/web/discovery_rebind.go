package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

type discoveryReplacement struct {
	Old                     domain.DiscoveryBinding
	NewID, Name, EntityPath string
}

// discoveryRebindOptions offers only a single absent identity and a single
// observed replacement with the same inventory name and compatible type.
// Absence means not observed in this scan, not proof that a resource was deleted.
func discoveryRebindOptions(source string, candidates []discovery.Candidate, hosts []domain.Host, services []domain.Service, bindings []domain.DiscoveryBinding) []discoveryReplacement {
	type key struct {
		kind string
		id   int64
	}
	type record struct{ name, typ string }
	records := make(map[key]record)
	names := make(map[string]int)
	for _, h := range hosts {
		records[key{"host", h.ID}] = record{h.Name, h.Type}
	}
	for _, s := range services {
		records[key{"service", s.ID}] = record{s.Name, s.Kind}
	}
	ids, candidateNames := make(map[string]int), make(map[string]int)
	candidateName := func(c discovery.Candidate) string {
		if c.EntityType == "host" {
			return c.Host.Name
		}
		return c.Service.Name
	}
	for _, c := range candidates {
		ids[c.ID]++
		candidateNames[c.EntityType+":"+discovery.NormalizeName(candidateName(c))]++
	}
	boundIDs := make(map[string]bool)
	bindingsByEntity := make(map[key][]domain.DiscoveryBinding)
	for _, b := range bindings {
		if b.Source == source {
			boundIDs[b.ExternalID] = true
			k := key{b.EntityType, b.EntityID}
			bindingsByEntity[k] = append(bindingsByEntity[k], b)
		}
	}
	// Equal names in another source remain independent. Only this source's
	// bound inventory records can be replacement targets.
	for k := range bindingsByEntity {
		if r, ok := records[k]; ok {
			names[k.kind+":"+discovery.NormalizeName(r.name)]++
		}
	}
	var out []discoveryReplacement
	for _, c := range candidates {
		name := candidateName(c)
		normalized := discovery.NormalizeName(name)
		nk := c.EntityType + ":" + normalized
		if normalized == "" || ids[c.ID] != 1 || boundIDs[c.ID] || candidateNames[nk] != 1 || names[nk] != 1 {
			continue
		}
		for k, r := range records {
			if k.kind != c.EntityType || discovery.NormalizeName(r.name) != normalized {
				continue
			}
			if (k.kind == "host" && r.typ != c.Host.Type) || (k.kind == "service" && r.typ != c.Service.Kind) {
				continue
			}
			bs := bindingsByEntity[k]
			if len(bs) != 1 || ids[bs[0].ExternalID] != 0 {
				continue
			}
			next := bs[0]
			next.ExternalID = c.ID
			if next.Validate() != nil {
				continue
			}
			out = append(out, discoveryReplacement{Old: bs[0], NewID: c.ID, Name: r.name})
		}
	}
	return out
}

func loadDiscoveryReplacements(source DiscoverySource, candidates []discovery.Candidate, tx *sql.Tx, db *sql.DB) ([]discoveryReplacement, error) {
	hr, sr, br := store.NewHostRepo(db), store.NewServiceRepo(db), store.NewDiscoveryBindingRepo(db)
	if tx != nil {
		hr = hr.WithTx(tx)
		sr = sr.WithTx(tx)
		br = br.WithTx(tx)
	}
	hosts, err := hr.List()
	if err != nil {
		return nil, err
	}
	services, err := sr.List()
	if err != nil {
		return nil, err
	}
	bindings, err := br.List()
	if err != nil {
		return nil, err
	}
	return discoveryRebindOptions(source.Key(), candidates, hosts, services, bindings), nil
}

func reviewDiscoveryRebind(source DiscoverySource, db *sql.DB, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !source.available() {
			http.NotFound(w, req)
			return
		}
		candidates, err := source.candidates(req.Context())
		if err != nil {
			http.Error(w, source.errorPrefix()+err.Error(), http.StatusBadGateway)
			return
		}
		// A review is a consistent read snapshot; the POST repeats both the scan and
		// inventory checks within its own write transaction before replacing anything.
		tx, err := db.BeginTx(req.Context(), &sql.TxOptions{ReadOnly: true})
		if err != nil {
			serverError(w, req, err)
			return
		}
		defer tx.Rollback()
		options, err := loadDiscoveryReplacements(source, candidates, tx, db)
		if err != nil {
			serverError(w, req, err)
			return
		}
		if err = tx.Commit(); err != nil {
			serverError(w, req, err)
			return
		}
		selected := make([]discoveryReplacement, 0, len(options))
		for _, option := range options {
			if id := req.URL.Query().Get("id"); id != "" && id != option.NewID {
				continue
			}
			option.EntityPath = cat.path(option.Old.EntityType, option.Old.EntityID)
			selected = append(selected, option)
		}
		render(w, req, "discovery_rebind.html", struct {
			Title   string
			Source  DiscoverySource
			Options []discoveryReplacement
		}{"Review replacement identities", source, selected})
	}
}

var errStaleDiscoveryReplacement = errors.New("replacement is no longer unambiguous; refresh discovery and review it again")

func replaceDiscoveryBinding(source DiscoverySource, db *sql.DB, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !source.available() {
			http.NotFound(w, req)
			return
		}
		if err := req.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		if req.PostFormValue("confirm") != "replace" {
			http.Error(w, "confirm the replacement identity", http.StatusBadRequest)
			return
		}
		id, err := strconv.ParseInt(req.PostFormValue("entity_id"), 10, 64)
		old := domain.DiscoveryBinding{Source: source.Key(), ExternalID: req.PostFormValue("old_id"), EntityType: req.PostFormValue("entity_type"), EntityID: id}
		newID := req.PostFormValue("new_id")
		next := old
		next.ExternalID = newID
		if err != nil || old.Validate() != nil || next.Validate() != nil || old.ExternalID == newID {
			http.Error(w, "invalid replacement identity", http.StatusBadRequest)
			return
		}
		candidates, err := source.candidates(req.Context())
		if err != nil {
			http.Error(w, source.errorPrefix()+err.Error(), http.StatusBadGateway)
			return
		}
		err = store.WithTx(db, func(tx *sql.Tx) error {
			options, err := loadDiscoveryReplacements(source, candidates, tx, db)
			if err != nil {
				return err
			}
			for _, option := range options {
				if option.Old == old && option.NewID == newID {
					if err := store.NewDiscoveryBindingRepo(db).WithTx(tx).Replace(old, newID); err != nil {
						return err
					}
					return store.NewChangelogRepo(db).WithTx(tx).Create(store.ChangeEvent{
						EntityType: old.EntityType, EntityID: old.EntityID, Label: option.Name,
						Action: domain.ActionUpdate, Actor: actor(req), CreatedAt: nowRFC3339(),
						Changes: []domain.FieldChange{{Field: "discovery_identity:" + source.Key(), Old: old.ExternalID, New: newID}},
					})
				}
			}
			return errStaleDiscoveryReplacement
		})
		if errors.Is(err, errStaleDiscoveryReplacement) || errors.Is(err, store.ErrNotFound) {
			http.Error(w, errStaleDiscoveryReplacement.Error(), http.StatusConflict)
			return
		}
		if err != nil {
			serverError(w, req, err)
			return
		}
		http.Redirect(w, req, cat.path(old.EntityType, old.EntityID), http.StatusSeeOther)
	}
}
