package web

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

type discoveryChangeField struct {
	Name, Label, Current, Observed, Expected string
	Editable                                 bool
}

type discoveryChangeRow struct {
	ExternalID, EntityType, Name, Path, LastSeen string
	EntityID                                     int64
	Missing, Conflict                            bool
	Fields                                       []discoveryChangeField
	Editable                                     int
}

type discoveryChangesPage struct {
	Title, Error string
	Source       DiscoverySource
	Rows         []discoveryChangeRow
	Applied      int
}

// discoveryChangeFingerprint is an optimistic concurrency token, not an
// authorization token. POST always rescans the configured source and accepts
// only allowlisted source values, never values supplied by the browser.
func discoveryChangeFingerprint(source string, row discoveryChangeRow, field discoveryChangeField) string {
	raw, _ := json.Marshal([]any{source, row.ExternalID, row.EntityType, row.EntityID, field.Name, field.Current, field.Observed})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func discoveryImage(notes string) string {
	// Only recognize the exact discovery-generated provenance line. Image
	// differences are informational: free-form notes are never rewritten.
	for _, line := range strings.Split(notes, "\n") {
		for _, prefix := range []string{"Discovered from a Docker-compatible API. Image: ", "Discovered from Docker. Image: "} {
			if strings.HasPrefix(line, prefix) {
				return strings.TrimPrefix(line, prefix)
			}
		}
	}
	return ""
}

func discoveryChangeFields(c discovery.Candidate, h domain.Host, s domain.Service) []discoveryChangeField {
	var fields []discoveryChangeField
	add := func(name, label, current, observed string, editable, available bool) {
		if available && current != observed {
			fields = append(fields, discoveryChangeField{Name: name, Label: label, Current: current, Observed: observed, Editable: editable})
		}
	}
	if c.EntityType == "service" {
		add("name", "Name", s.Name, c.Service.Name, true, c.Service.Name != "")
		add("ports", "Published ports", s.Ports, c.Service.Ports, true, true)
		add("category", "Compose project / category", s.Category, c.Service.Category, true, c.Service.Category != "")
		add("image", "Container image (informational)", discoveryImage(s.Notes), discoveryImage(c.Service.Notes), false, discoveryImage(c.Service.Notes) != "")
		return fields
	}
	add("name", "Name", h.Name, c.Host.Name, true, c.Host.Name != "")
	add("cpu", "CPU", h.CPU, c.Host.CPU, true, c.Host.CPU != "")
	add("ram", "RAM", h.RAM, c.Host.RAM, true, c.Host.RAM != "")
	add("disk", "Disk", h.Disk, c.Host.Disk, true, c.Host.Disk != "")
	add("status", "Status", h.Status, c.Host.Status, true, c.Host.Status != "")
	oldIPs, newIPs := append([]string(nil), h.IPs...), append([]string(nil), c.Host.IPs...)
	sort.Strings(oldIPs)
	sort.Strings(newIPs)
	// An absent IP list may reflect missing guest tools/API permissions. It
	// must not offer to erase manually maintained addresses.
	add("ips", "IP addresses", strings.Join(oldIPs, ", "), strings.Join(newIPs, ", "), true, len(newIPs) > 0)
	return fields
}

func buildDiscoveryChanges(source DiscoverySource, candidates []discovery.Candidate, hosts []domain.Host, services []domain.Service, bindings []domain.DiscoveryBinding, seen map[string]string, cat entityCatalog) []discoveryChangeRow {
	hostByID, serviceByID := make(map[int64]domain.Host), make(map[int64]domain.Service)
	for _, h := range hosts {
		hostByID[h.ID] = h
	}
	for _, s := range services {
		serviceByID[s.ID] = s
	}
	matches := make(map[string]discovery.Match)
	for _, m := range discovery.MatchCandidates(source.Key(), candidates, hosts, services, bindings) {
		matches[m.ID] = m
	}
	rows := []discoveryChangeRow{}
	for _, b := range bindings {
		if b.Source != source.Key() {
			continue
		}
		h, s := hostByID[b.EntityID], serviceByID[b.EntityID]
		name := s.Name
		if b.EntityType == "host" {
			name = h.Name
		}
		row := discoveryChangeRow{ExternalID: b.ExternalID, EntityType: b.EntityType, EntityID: b.EntityID, Name: name, Path: cat.path(b.EntityType, b.EntityID), LastSeen: seen[b.ExternalID]}
		m, found := matches[b.ExternalID]
		row.Missing = !found
		row.Conflict = found && (m.Ambiguous || !m.Bound || m.ExistingID != b.EntityID || m.EntityType != b.EntityType)
		if found && !row.Conflict {
			row.Fields = discoveryChangeFields(m.Candidate, h, s)
			for i := range row.Fields {
				field := &row.Fields[i]
				field.Expected = discoveryChangeFingerprint(source.Key(), row, *field)
				if field.Editable {
					row.Editable++
				}
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].ExternalID < rows[j].ExternalID
	})
	return rows
}

func reviewDiscoveryChanges(source DiscoverySource, db *sql.DB, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !source.available() {
			http.NotFound(w, req)
			return
		}
		data := discoveryChangesPage{Title: source.Name + " changes", Source: source}
		scannedAt := time.Now().UTC()
		candidates, err := source.candidates(req.Context())
		if err != nil {
			data.Error = source.errorPrefix() + err.Error()
			render(w, req, "discovery_changes.html", data)
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
			observations := store.NewDiscoveryObservationRepo(db).WithTx(tx)
			var ids []string
			for _, m := range discovery.MatchCandidates(source.Key(), candidates, hosts, services, bindings) {
				if m.Bound && !m.Ambiguous {
					ids = append(ids, m.ID)
				}
			}
			if err := observations.RecordBound(source.Key(), ids, scannedAt); err != nil {
				return err
			}
			seen, err := observations.ListSource(source.Key())
			if err != nil {
				return err
			}
			data.Rows = buildDiscoveryChanges(source, candidates, hosts, services, bindings, seen, cat)
			return nil
		})
		if err != nil {
			serverError(w, req, err)
			return
		}
		render(w, req, "discovery_changes.html", data)
	}
}

var errDiscoveryChangesStale = errors.New("discovery or inventory changed; review the source again before applying changes")
var errDiscoveryChangesSelection = errors.New("select at least one available field to apply")

func applyDiscoveryChanges(source DiscoverySource, db *sql.DB, cat entityCatalog, dispatcher webhook.Dispatcher) http.HandlerFunc {
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
		id, err := strconv.ParseInt(req.PostFormValue("entity_id"), 10, 64)
		if err != nil || id <= 0 || req.PostFormValue("external_id") == "" {
			http.Error(w, "invalid discovery resource", http.StatusBadRequest)
			return
		}
		selected := make(map[string]bool)
		for _, field := range req.PostForm["field"] {
			selected[field] = true
		}
		if len(selected) == 0 {
			http.Error(w, errDiscoveryChangesSelection.Error(), http.StatusBadRequest)
			return
		}
		candidates, err := source.candidates(req.Context())
		if err != nil {
			http.Error(w, source.errorPrefix()+err.Error(), http.StatusBadGateway)
			return
		}
		applied := 0
		var events []webhook.Event
		err = store.WithTx(db, func(tx *sql.Tx) error {
			hostRepo, serviceRepo := store.NewHostRepo(db).WithTx(tx), store.NewServiceRepo(db).WithTx(tx)
			hosts, err := hostRepo.List()
			if err != nil {
				return err
			}
			services, err := serviceRepo.List()
			if err != nil {
				return err
			}
			bindings, err := store.NewDiscoveryBindingRepo(db).WithTx(tx).List()
			if err != nil {
				return err
			}
			rows := buildDiscoveryChanges(source, candidates, hosts, services, bindings, nil, cat)
			var row *discoveryChangeRow
			for i := range rows {
				r := &rows[i]
				if r.ExternalID == req.PostFormValue("external_id") && r.EntityID == id && r.EntityType == req.PostFormValue("entity_type") && !r.Missing && !r.Conflict {
					row = r
					break
				}
			}
			if row == nil {
				return errDiscoveryChangesStale
			}
			fields := make(map[string]discoveryChangeField)
			for _, f := range row.Fields {
				fields[f.Name] = f
			}
			for name := range selected {
				f, ok := fields[name]
				if !ok || !f.Editable || req.PostFormValue("expected_"+name) != f.Expected {
					return errDiscoveryChangesStale
				}
			}
			var changes []domain.FieldChange
			var data any
			if row.EntityType == "service" {
				s, err := serviceRepo.Get(id)
				if err != nil {
					return err
				}
				old := s
				for name := range selected {
					switch name {
					case "name":
						s.Name = fields[name].Observed
					case "ports":
						s.Ports = fields[name].Observed
					case "category":
						s.Category = fields[name].Observed
					}
				}
				if err := s.Validate(); err != nil {
					return err
				}
				if err := serviceRepo.Update(s); err != nil {
					return err
				}
				s, err = serviceRepo.Get(id)
				if err != nil {
					return err
				}
				data = s
				changes, err = domain.Diff(old, s)
				if err != nil {
					return err
				}
			} else {
				h, err := hostRepo.Get(id)
				if err != nil {
					return err
				}
				old := h
				for name := range selected {
					switch name {
					case "name":
						h.Name = fields[name].Observed
					case "cpu":
						h.CPU = fields[name].Observed
					case "ram":
						h.RAM = fields[name].Observed
					case "disk":
						h.Disk = fields[name].Observed
					case "status":
						h.Status = fields[name].Observed
					case "ips":
						h.IPs = strings.Split(fields[name].Observed, ", ")
					}
				}
				if err := h.Validate(); err != nil {
					return err
				}
				if err := hostRepo.Update(h); err != nil {
					return err
				}
				h, err = hostRepo.Get(id)
				if err != nil {
					return err
				}
				data = h
				changes, err = domain.Diff(old, h)
				if err != nil {
					return err
				}
			}
			if len(changes) == 0 {
				return nil
			}
			if err := recordDiscoveryMutation(tx, store.NewChangelogRepo(db).WithTx(tx), dispatcher, store.ChangeEvent{
				EntityType: row.EntityType, EntityID: id, Label: row.Name,
				Action: domain.ActionUpdate, Actor: actor(req), Changes: changes,
			}, data, &events); err != nil {
				return err
			}
			applied = len(selected)
			return nil
		})
		if errors.Is(err, errDiscoveryChangesStale) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if err != nil {
			serverError(w, req, err)
			return
		}
		dispatcher.Dispatch(events...)
		render(w, req, "discovery_changes.html", discoveryChangesPage{Title: "Discovery changes applied", Source: source, Applied: applied})
	}
}
