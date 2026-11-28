package web

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

const discoverySnapshotFreshness = 24 * time.Hour
const discoveryDeviationPageSize = 100

// observedCandidates records source evidence separately from inventory writes.
// The source must return a complete successful scan to replace cached evidence.
func (source DiscoverySource) observedCandidates(ctx context.Context, db *sql.DB) ([]discovery.Candidate, error) {
	started := time.Now().UTC()
	candidates, scanErr := source.candidates(ctx)
	if err := store.NewDiscoverySnapshotRepo(db).Record(source.Key(), started, candidates, scanErr); err != nil {
		return nil, err
	}
	return candidates, scanErr
}

type discoveryDeviationSource struct {
	DiscoverySource
	Attempted, Succeeded, Error string
	Health                      string
	Stale, HasSnapshot          bool
}

type discoveryDeviationRow struct {
	discoveryChangeRow
	Source DiscoverySource
	State  string
	Stale  bool
}

type discoveryDeviationsData struct {
	Title, SourceFilter, StateFilter, Query string
	Sources                                 []discoveryDeviationSource
	Rows                                    []discoveryDeviationRow
	Total                                   int
	PreviousURL, NextURL                    string
}

func discoveryDeviations(sources []DiscoverySource, db *sql.DB, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		data := discoveryDeviationsData{Title: "Discovery deviations", SourceFilter: q.Get("source"), StateFilter: q.Get("state"), Query: strings.TrimSpace(q.Get("q"))}
		switch data.StateFilter {
		case "", "changed", "missing", "conflict", "new", "matched":
		default:
			http.Error(w, "invalid deviation state", http.StatusBadRequest)
			return
		}
		page := 1
		if raw := q.Get("page"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > 1000000 {
				http.Error(w, "invalid page", http.StatusBadRequest)
				return
			}
			page = n
		}
		sourceFound := data.SourceFilter == ""
		for _, source := range sources {
			if source.available() && source.Key() == data.SourceFilter {
				sourceFound = true
			}
		}
		if !sourceFound {
			http.Error(w, "unknown discovery source", http.StatusBadRequest)
			return
		}
		tx, err := db.BeginTx(req.Context(), &sql.TxOptions{ReadOnly: true})
		if err != nil {
			serverError(w, req, err)
			return
		}
		defer tx.Rollback()
		hosts, err := store.NewHostRepo(db).WithTx(tx).List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		services, err := store.NewServiceRepo(db).WithTx(tx).List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		bindings, err := store.NewDiscoveryBindingRepo(db).WithTx(tx).List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		now := time.Now()
		var rows []discoveryDeviationRow
		for _, source := range sources {
			if !source.available() {
				continue
			}
			summary := discoveryDeviationSource{DiscoverySource: source, Health: "Not scanned"}
			snapshot, err := store.NewDiscoverySnapshotRepo(db).WithTx(tx).Get(source.Key())
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				serverError(w, req, err)
				return
			}
			if err == nil {
				summary.Attempted = snapshot.AttemptedAt.Format(time.RFC3339)
				summary.Error = snapshot.LastError
				summary.HasSnapshot = !snapshot.SucceededAt.IsZero()
				if summary.HasSnapshot {
					summary.Succeeded = snapshot.SucceededAt.Format(time.RFC3339)
					summary.Stale = now.Sub(snapshot.SucceededAt) > discoverySnapshotFreshness || snapshot.LastError != ""
					summary.Health = "Recent observation"
					if summary.Stale {
						summary.Health = "Stale observation"
					}
				}
				if snapshot.LastError != "" {
					summary.Health = "Latest scan failed"
				}
				if summary.HasSnapshot && (data.SourceFilter == "" || data.SourceFilter == source.Key()) {
					seen, err := store.NewDiscoveryObservationRepo(db).WithTx(tx).ListSource(source.Key())
					if err != nil {
						serverError(w, req, err)
						return
					}
					sourceRows := discoveryDeviationRows(source, snapshot.Candidates, hosts, services, bindings, seen, cat, summary.Stale)
					rows = append(rows, sourceRows...)
				}
			}
			data.Sources = append(data.Sources, summary)
		}
		if err := tx.Commit(); err != nil {
			serverError(w, req, err)
			return
		}
		query := strings.ToLower(data.Query)
		start := (page - 1) * discoveryDeviationPageSize
		for _, row := range rows {
			if data.StateFilter != "" && row.State != data.StateFilter {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(row.Name+" "+row.ExternalID+" "+row.Source.Name+" "+row.EntityType), query) {
				continue
			}
			if data.Total >= start && len(data.Rows) < discoveryDeviationPageSize {
				data.Rows = append(data.Rows, row)
			}
			data.Total++
		}
		pageURL := func(n int) string {
			values := url.Values{}
			values.Set("page", strconv.Itoa(n))
			if data.SourceFilter != "" {
				values.Set("source", data.SourceFilter)
			}
			if data.StateFilter != "" {
				values.Set("state", data.StateFilter)
			}
			if data.Query != "" {
				values.Set("q", data.Query)
			}
			return "/discovery/deviations?" + values.Encode()
		}
		if page > 1 {
			data.PreviousURL = pageURL(page - 1)
		}
		if start+len(data.Rows) < data.Total {
			data.NextURL = pageURL(page + 1)
		}
		render(w, req, "discovery_deviations.html", data)
	}
}

func discoveryDeviationRows(source DiscoverySource, candidates []discovery.Candidate, hosts []domain.Host, services []domain.Service, bindings []domain.DiscoveryBinding, seen map[string]string, cat entityCatalog, stale bool) []discoveryDeviationRow {
	rows := []discoveryDeviationRow{}
	bound := make(map[string]bool)
	for _, b := range bindings {
		if b.Source == source.Key() {
			bound[b.ExternalID] = true
		}
	}
	for _, change := range buildDiscoveryChanges(source, candidates, hosts, services, bindings, seen, cat) {
		state := "changed"
		if change.Missing {
			state = "missing"
		} else if change.Conflict {
			state = "conflict"
		} else if len(change.Fields) == 0 {
			continue
		}
		rows = append(rows, discoveryDeviationRow{discoveryChangeRow: change, Source: source, State: state, Stale: stale})
	}
	for _, m := range discovery.MatchCandidates(source.Key(), candidates, hosts, services, bindings) {
		if bound[m.ID] {
			continue
		}
		name := m.Service.Name
		if m.EntityType == "host" {
			name = m.Host.Name
		}
		state := "new"
		if m.Ambiguous {
			state = "conflict"
		} else if m.ExistingID > 0 {
			state = "matched"
		}
		row := discoveryChangeRow{ExternalID: m.ID, EntityType: m.EntityType, EntityID: m.ExistingID, Name: name, Conflict: m.Ambiguous}
		if m.ExistingID > 0 {
			row.Path = cat.path(m.EntityType, m.ExistingID)
		}
		rows = append(rows, discoveryDeviationRow{discoveryChangeRow: row, Source: source, State: state, Stale: stale})
	}
	return rows
}
