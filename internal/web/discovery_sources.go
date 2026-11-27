package web

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/job"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

// DiscoverySource identifies one configured engine without exposing its credentials.
type DiscoverySource struct {
	ID, Name, Kind string
	Docker         dockerScanner
	Proxmox        proxmoxScanner
	XenOrchestra   xenOrchestraScanner
}

func (s DiscoverySource) Key() string { return s.Kind + ":" + s.ID }
func (s DiscoverySource) Path() string {
	if s.ID == "default" && s.Kind != "podman" {
		return "/discovery/" + s.Kind
	}
	return "/discovery/sources/" + s.Kind + "/" + s.ID
}
func (s DiscoverySource) Containers() bool { return s.Kind == "docker" || s.Kind == "podman" }
func (s DiscoverySource) available() bool {
	switch s.Kind {
	case "docker", "podman":
		return s.Docker != nil
	case "proxmox":
		return s.Proxmox != nil
	case "xen-orchestra":
		return s.XenOrchestra != nil
	}
	return false
}
func (s DiscoverySource) errorPrefix() string {
	switch s.Kind {
	case "docker", "podman":
		return "Could not reach the Docker / Podman socket: "
	case "proxmox":
		return "Could not reach the Proxmox API: "
	default:
		return "Could not reach the Xen Orchestra API: "
	}
}

func configuredDiscoverySources(cfg Config) []DiscoverySource {
	if cfg.DiscoverySources != nil {
		return cfg.DiscoverySources
	}
	sources := []DiscoverySource{{ID: "default", Name: "Docker / Podman containers", Kind: "docker", Docker: cfg.Docker}}
	if cfg.PVEOpts.Enabled {
		sources = append(sources, DiscoverySource{ID: "default", Name: "Proxmox", Kind: "proxmox", Proxmox: cfg.Proxmox})
	}
	if cfg.XOOpts.Enabled {
		sources = append(sources, DiscoverySource{ID: "default", Name: "XCP-ng / Xen Orchestra", Kind: "xen-orchestra", XenOrchestra: cfg.XenOrchestra})
	}
	return sources
}

func (s DiscoverySource) candidates(ctx context.Context) ([]discovery.Candidate, error) {
	var candidates []discovery.Candidate
	switch s.Kind {
	case "docker", "podman":
		containers, err := s.Docker.Containers(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range discovery.ProposeServices(containers, nil) {
			candidates = append(candidates, discovery.Candidate{ID: p.ContainerID, EntityType: "service", Service: p.Service})
		}
	case "proxmox":
		resources, err := s.Proxmox.Resources(ctx)
		if err != nil {
			return nil, err
		}
		nodes := make(map[string][]string)
		parentNames := make(map[string]string)
		for _, r := range resources {
			parentNames[r.ID] = r.Node
			if r.Type == "node" {
				nodes[discovery.NormalizeName(r.Node)] = append(nodes[discovery.NormalizeName(r.Node)], r.ID)
			}
		}
		for _, p := range discovery.ProposeProxmoxHosts(resources, nil) {
			parent := ""
			if p.Host.Type != "physical" {
				if ids := nodes[discovery.NormalizeName(parentNames[p.ID])]; len(ids) == 1 {
					parent = ids[0]
				}
			}
			candidates = append(candidates, discovery.Candidate{ID: p.ID, ParentID: parent, EntityType: "host", Host: p.Host})
		}
	case "xen-orchestra":
		resources, err := s.XenOrchestra.Resources(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range discovery.ProposeXenOrchestraHosts(resources, nil) {
			candidates = append(candidates, discovery.Candidate{ID: p.ID, ParentID: p.HostID, EntityType: "host", Host: p.Host})
		}
	default:
		return nil, fmt.Errorf("unknown discovery source type")
	}
	return candidates, nil
}

type discoverySourceRow struct {
	discovery.Match
	Name, Type, Status, CPU, RAM, Disk, Ports, Category, IPs, ParentName string
	InventoryPath, ImportStatus                                          string
}
type discoverySourcePage struct {
	Title      string
	Source     DiscoverySource
	Rows       []discoverySourceRow
	Hosts      []domain.Host
	Error      string
	Selectable int
	Total      int
	Filter     string
	Query      string
}
type discoveryImportResult struct {
	Title                            string
	Source                           DiscoverySource
	Added, Existing, Skipped, Linked int
	InventoryPath                    string
}

type discoverySourceHealth struct {
	DiscoverySource
	Latest, Successful *store.DiscoveryRun
	Task               *job.Status
}

func discoverySourcesLanding(opts NetDiscoveryOptions, sources []DiscoverySource, runs *store.DiscoveryRunRepo, runner jobRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		jobs := make(map[string]job.Status)
		if runner != nil {
			for _, status := range runner.Statuses() {
				jobs[status.Name] = status
			}
		}
		visible := make([]discoverySourceHealth, 0, len(sources))
		for _, s := range sources {
			if !s.available() {
				continue
			}
			health := discoverySourceHealth{DiscoverySource: s}
			if runs != nil {
				latest, err := runs.Latest(s.Key())
				if err != nil && !errors.Is(err, store.ErrNotFound) {
					serverError(w, req, err)
					return
				}
				if err == nil {
					health.Latest = &latest
				}
				successful, err := runs.LatestSuccessful(s.Key())
				if err != nil && !errors.Is(err, store.ErrNotFound) {
					serverError(w, req, err)
					return
				}
				if err == nil {
					health.Successful = &successful
				}
			}
			jobName := "discovery-" + s.Kind
			if s.ID != "default" {
				jobName += "-" + s.ID
			}
			if status, ok := jobs[jobName]; ok {
				health.Task = &status
			}
			visible = append(visible, health)
		}
		render(w, req, "discovery.html", struct {
			Title          string
			NetworkEnabled bool
			Sources        []discoverySourceHealth
			TasksEnabled   bool
			HistoryEnabled bool
		}{"Discover", opts.Enabled, visible, runner != nil, runs != nil})
	}
}

func scanDiscoverySource(source DiscoverySource, db *sql.DB, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !source.available() {
			http.NotFound(w, req)
			return
		}
		filter := req.URL.Query().Get("status")
		switch filter {
		case "", "all":
			filter = "all"
		case "new", "matched", "conflict", "tracked":
		default:
			http.Error(w, "invalid discovery status filter", http.StatusBadRequest)
			return
		}
		data := discoverySourcePage{Title: source.Name + " discovery", Source: source, Filter: filter, Query: strings.TrimSpace(req.URL.Query().Get("q"))}
		scannedAt := time.Now().UTC()
		candidates, err := source.candidates(req.Context())
		if err != nil {
			data.Error = source.errorPrefix() + err.Error()
			render(w, req, "discovery_source.html", data)
			return
		}
		err = store.WithTx(db, func(tx *sql.Tx) error {
			hosts, err := store.NewHostRepo(db).WithTx(tx).List()
			if err != nil {
				return err
			}
			services, err := store.NewServiceRepo(db).WithTx(tx).List()
			if err != nil {
				return err
			}
			bindings, err := store.NewDiscoveryBindingRepo(db).WithTx(tx).List()
			if err != nil {
				return err
			}
			data.Hosts = hosts
			parents := make(map[string]string)
			for _, c := range candidates {
				if c.EntityType == "host" && c.Host.Type == "physical" {
					parents[c.ID] = c.Host.Name
				}
			}
			var observed []string
			for _, m := range discovery.MatchCandidates(source.Key(), candidates, hosts, services, bindings) {
				if m.Bound && !m.Ambiguous {
					observed = append(observed, m.ID)
				}
				row := discoverySourceRow{Match: m, ParentName: parents[m.ParentID]}
				if m.EntityType == "host" {
					row.Name = m.Host.Name
					row.Type = m.Host.Type
					row.Status = m.Host.Status
					row.CPU = m.Host.CPU
					row.RAM = m.Host.RAM
					row.Disk = m.Host.Disk
					row.IPs = strings.Join(m.Host.IPs, ", ")
				} else {
					row.Name = m.Service.Name
					row.Type = m.Service.Kind
					row.Ports = m.Service.Ports
					row.Category = m.Service.Category
				}
				switch {
				case m.Ambiguous:
					row.ImportStatus = "conflict"
				case m.Bound:
					row.ImportStatus = "tracked"
				case m.ExistingID > 0:
					row.ImportStatus = "matched"
				default:
					row.ImportStatus = "new"
				}
				if m.ExistingID > 0 {
					row.InventoryPath = cat.path(m.EntityType, m.ExistingID)
				}
				data.Total++
				if data.Filter != "all" && data.Filter != row.ImportStatus {
					continue
				}
				searchable := strings.Join([]string{row.Name, row.Type, row.ParentName, row.IPs, row.Ports, row.Category}, " ")
				if data.Query != "" && !strings.Contains(strings.ToLower(searchable), strings.ToLower(data.Query)) {
					continue
				}
				if !m.Bound && !m.Ambiguous {
					data.Selectable++
				}
				data.Rows = append(data.Rows, row)
			}
			return store.NewDiscoveryObservationRepo(db).WithTx(tx).RecordBound(source.Key(), observed, scannedAt)
		})
		if err != nil {
			serverError(w, req, err)
			return
		}
		render(w, req, "discovery_source.html", data)
	}
}

func importDiscoverySource(source DiscoverySource, db *sql.DB, dispatcher webhook.Dispatcher) http.HandlerFunc {
	if dispatcher == nil {
		dispatcher = webhook.Noop{}
	}
	return func(w http.ResponseWriter, req *http.Request) {
		if !source.available() {
			http.NotFound(w, req)
			return
		}
		if err := req.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		selected := make(map[string]bool)
		for _, id := range req.PostForm["id"] {
			selected[id] = true
		}
		var hostID int64
		if source.Containers() && req.PostFormValue("host") != "" {
			id, err := strconv.ParseInt(req.PostFormValue("host"), 10, 64)
			if err != nil || id <= 0 {
				http.Error(w, "invalid host id", http.StatusBadRequest)
				return
			}
			hostID = id
		}
		candidates, err := source.candidates(req.Context())
		if err != nil {
			http.Error(w, source.errorPrefix()+err.Error(), http.StatusBadGateway)
			return
		}
		result := discoveryImportResult{Title: "Discovery import complete", Source: source, InventoryPath: "/hosts"}
		if source.Containers() {
			result.InventoryPath = "/services"
		}
		var events []webhook.Event
		err = store.WithTx(db, func(tx *sql.Tx) error {
			staged := webhook.ForTransaction(tx, dispatcher)
			hr := store.NewHostRepo(db).WithTx(tx)
			sr := store.NewServiceRepo(db).WithTx(tx)
			br := store.NewDiscoveryBindingRepo(db).WithTx(tx)
			rr := store.NewRelationshipRepo(db).WithTx(tx)
			cr := store.NewChangelogRepo(db).WithTx(tx)
			if source.Containers() && hostID > 0 {
				// The host selector may have been open while another request
				// deleted its target. Check under the same write lock as links.
				if _, err := hr.Get(hostID); err != nil {
					if errors.Is(err, store.ErrNotFound) {
						return &store.ConstraintError{Reason: "selected host no longer exists"}
					}
					return err
				}
			}
			hosts, err := hr.List()
			if err != nil {
				return err
			}
			services, err := sr.List()
			if err != nil {
				return err
			}
			bindings, err := br.List()
			if err != nil {
				return err
			}
			matches := discovery.MatchCandidates(source.Key(), candidates, hosts, services, bindings)
			resolved := make(map[string]int64)
			boundIDs := make(map[string]bool)
			created := make(map[string]int64)
			seen := make(map[string]bool)
			for _, m := range matches {
				if m.Bound && !m.Ambiguous {
					resolved[m.ID] = m.ExistingID
					boundIDs[m.ID] = true
				}
			}
			for _, m := range matches {
				if !selected[m.ID] || seen[m.ID] {
					continue
				}
				seen[m.ID] = true
				if m.Ambiguous || strings.TrimSpace(m.ID) == "" {
					result.Skipped++
					continue
				}
				if m.Bound {
					result.Existing++
					continue
				}
				id := m.ExistingID
				if id == 0 {
					name := ""
					var data any
					if m.EntityType == "host" {
						if err := m.Host.Validate(); err != nil {
							result.Skipped++
							continue
						}
						id, err = hr.Create(m.Host)
						if err == nil {
							m.Host, err = hr.Get(id)
						}
						data = m.Host
						name = m.Host.Name
					} else {
						if err := m.Service.Validate(); err != nil {
							result.Skipped++
							continue
						}
						id, err = sr.Create(m.Service)
						if err == nil {
							m.Service, err = sr.Get(id)
						}
						data = m.Service
						name = m.Service.Name
					}
					if err != nil {
						return err
					}
					if err := recordDiscoveryMutation(tx, cr, staged, store.ChangeEvent{
						EntityType: m.EntityType, EntityID: id, Label: name,
						Action: domain.ActionImport, Actor: actor(req),
					}, data, &events); err != nil {
						return err
					}
					created[m.ID] = id
					result.Added++
				} else {
					result.Existing++
				}
				if err := br.Create(domain.DiscoveryBinding{Source: source.Key(), ExternalID: m.ID, EntityType: m.EntityType, EntityID: id}); err != nil {
					return err
				}
				resolved[m.ID] = id
				boundIDs[m.ID] = true
			}
			for id := range selected {
				if !seen[id] {
					result.Skipped++
				}
			}
			if source.Containers() && hostID > 0 {
				for _, id := range created {
					if _, err := rr.Create(domain.Relationship{FromType: "service", FromID: id, ToType: "host", ToID: hostID, Kind: "runs on"}); err != nil {
						return err
					}
					result.Linked++
				}
			} else if req.PostFormValue("link") != "" {
				// Opting into host links permits adopting an unambiguous existing parent.
				// It never creates an unselected parent or changes existing guest relations.
				for _, m := range matches {
					if m.EntityType == "host" && m.Host.Type == "physical" && !m.Ambiguous && m.ExistingID > 0 {
						resolved[m.ID] = m.ExistingID
					}
				}
				for _, m := range matches {
					id := created[m.ID]
					parent := resolved[m.ParentID]
					if id == 0 || parent == 0 || id == parent || m.Host.Type == "physical" {
						continue
					}
					if !boundIDs[m.ParentID] {
						if err := br.Create(domain.DiscoveryBinding{Source: source.Key(), ExternalID: m.ParentID, EntityType: "host", EntityID: parent}); err != nil {
							return err
						}
						boundIDs[m.ParentID] = true
					}
					if _, err := rr.Create(domain.Relationship{FromType: "host", FromID: id, ToType: "host", ToID: parent, Kind: "runs on"}); err != nil {
						return err
					}
					result.Linked++
				}
			}
			return nil
		})
		if c := constraintError(err); c != nil {
			http.Error(w, c.Error(), http.StatusBadRequest)
			return
		}
		if err != nil {
			serverError(w, req, err)
			return
		}
		dispatcher.Dispatch(events...)
		render(w, req, "discovery_import.html", result)
	}
}
