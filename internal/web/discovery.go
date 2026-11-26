package web

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

// dockerScanner is the subset of the Docker discovery client the web layer uses.
type dockerScanner interface {
	Containers(ctx context.Context) ([]discovery.Container, error)
}

// networkScanner is the subset of the network discovery scanner the web layer uses.
type networkScanner interface {
	Scan(ctx context.Context, cidr string, ports []int) ([]discovery.ScannedHost, error)
}

// NetDiscoveryOptions configures the opt-in network scan feature. It is
// exported so package main can construct it when calling web.New.
type NetDiscoveryOptions struct {
	Enabled       bool
	DefaultSubnet string
}

// proxmoxScanner is the subset of the Proxmox discovery client the web layer uses.
type proxmoxScanner interface {
	Resources(ctx context.Context) ([]discovery.ProxmoxResource, error)
}

// ProxmoxOptions configures the opt-in Proxmox import feature. It is exported so
// package main can construct it when calling web.New.
type ProxmoxOptions struct {
	Enabled bool
}

type netHostRow struct {
	IP, Name, Ports string
	AlreadyTracked  bool
}

type networkDiscoveryData struct {
	Title        string
	Subnet       string
	PortsInput   string
	Types        []domain.HostType
	SelectedType string
	Scanned      bool
	Rows         []netHostRow
	NewCount     int
	Error        string
}

func networkForm(opts NetDiscoveryOptions, hostTypes *store.HostTypeRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !opts.Enabled {
			http.NotFound(w, req)
			return
		}
		types, err := hostTypes.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		render(w, req, "discovery_network.html", networkDiscoveryData{
			Title: "Network discovery", Subnet: opts.DefaultSubnet,
			Types: types, SelectedType: "physical",
		})
	}
}

func scanNetwork(netscan networkScanner, hosts *store.HostRepo, opts NetDiscoveryOptions, hostTypes *store.HostTypeRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !opts.Enabled {
			http.NotFound(w, req)
			return
		}
		if err := req.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		subnet := strings.TrimSpace(req.FormValue("subnet"))
		portsInput := strings.TrimSpace(req.FormValue("ports"))
		types, err := hostTypes.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		data := networkDiscoveryData{
			Title: "Network discovery", Subnet: subnet, PortsInput: portsInput,
			Types: types, SelectedType: "physical",
		}
		if subnet == "" {
			data.Error = "Subnet is required."
			render(w, req, "discovery_network.html", data)
			return
		}
		var ports []int
		if portsInput != "" {
			p, err := discovery.ParsePorts(portsInput)
			if err != nil {
				data.Error = "Invalid ports: " + err.Error()
				render(w, req, "discovery_network.html", data)
				return
			}
			ports = p
		}
		scanned, err := netscan.Scan(req.Context(), subnet, ports)
		if err != nil {
			data.Error = "Scan failed: " + err.Error()
			render(w, req, "discovery_network.html", data)
			return
		}
		existing, err := hosts.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		data.Scanned = true
		for _, p := range discovery.ProposeHosts(scanned, existing) {
			if !p.AlreadyTracked {
				data.NewCount++
			}
			data.Rows = append(data.Rows, netHostRow{
				IP: p.IP, Name: p.Host.Name, Ports: p.Ports, AlreadyTracked: p.AlreadyTracked,
			})
		}
		render(w, req, "discovery_network.html", data)
	}
}

// recordDiscoveryMutation keeps discovery's import/update history and the
// corresponding outbound event in the same transaction. Events are delivered
// only after the caller successfully commits the complete discovery operation.
func recordDiscoveryMutation(tx *sql.Tx, cl *store.ChangelogRepo, dispatcher webhook.Dispatcher, change store.ChangeEvent, data any, events *[]webhook.Event) error {
	change.CreatedAt = nowRFC3339()
	if err := cl.Create(change); err != nil {
		return err
	}
	action := webhook.ActionUpdated
	if change.Action == domain.ActionImport {
		action = webhook.ActionCreated
	}
	event, err := webhook.NewEvent(change.EntityType, change.EntityID, action, change.Actor, change.CreatedAt, data)
	if err != nil {
		return err
	}
	if err := webhook.Stage(tx, dispatcher, event); err != nil {
		return err
	}
	*events = append(*events, event)
	return nil
}

func importNetwork(hosts *store.HostRepo, opts NetDiscoveryOptions, db *sql.DB, dispatcher webhook.Dispatcher) http.HandlerFunc {
	if dispatcher == nil {
		dispatcher = webhook.Noop{}
	}
	return func(w http.ResponseWriter, req *http.Request) {
		if !opts.Enabled {
			http.NotFound(w, req)
			return
		}
		if err := req.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		hostType := req.PostFormValue("type")
		result := discoveryImportResult{Title: "Discovery import complete", Source: DiscoverySource{ID: "default", Kind: "network", Name: "Network scan"}, InventoryPath: "/hosts"}
		var events []webhook.Event
		txErr := store.WithTx(db, func(tx *sql.Tx) error {
			if err := store.NewHostTypeRepo(db).WithTx(tx).Require(hostType); err != nil {
				return err
			}
			hr := hosts.WithTx(tx)
			cl := store.NewChangelogRepo(db).WithTx(tx)
			// Build the tracked-IP set inside the transaction so the uniqueness
			// check and the inserts share one consistent view. Listing outside the
			// tx let two concurrent submits (or a double-click) each read "IP absent"
			// and both insert it, creating a duplicate host.
			existing, err := hr.List()
			if err != nil {
				return err
			}
			tracked := make(map[string]bool)
			for _, h := range existing {
				for _, ip := range h.IPs {
					tracked[discovery.NormalizeIP(ip)] = true
				}
			}
			seen := make(map[string]bool)
			for _, v := range req.PostForm["host"] {
				if seen[v] {
					continue
				}
				seen[v] = true
				// Each value is "ip|name|ports". PTR hostnames and IPs are LDH/
				// numeric (no "|"), and SplitN caps at 3 so a "|" in ports is kept.
				// Malformed or empty-field rows fall through to Host.Validate below.
				parts := strings.SplitN(v, "|", 3)
				if len(parts) != 3 {
					result.Skipped++
					continue
				}
				ip, name, ports := parts[0], parts[1], parts[2]
				key := discovery.NormalizeIP(ip)
				if tracked[key] {
					result.Existing++
					continue
				}
				h := domain.Host{
					Name: name, Type: hostType,
					IPs: []string{ip}, Notes: discovery.NetworkHostNotes(ports),
				}
				// Discovery must not write a Host the manual UI would reject.
				if err := h.Validate(); err != nil {
					result.Skipped++
					continue
				}
				newID, err := hr.Create(h)
				if err != nil {
					return err
				}
				h, err = hr.Get(newID)
				if err != nil {
					return err
				}
				if err := recordDiscoveryMutation(tx, cl, dispatcher, store.ChangeEvent{
					EntityType: "host", EntityID: newID, Label: h.Name,
					Action: domain.ActionImport, Actor: actor(req),
				}, h, &events); err != nil {
					return err
				}
				tracked[key] = true // avoid a duplicate within the same submit
				result.Added++
			}
			return nil
		})
		if c := constraintError(txErr); c != nil {
			http.Error(w, c.Error(), http.StatusBadRequest)
			return
		}
		if txErr != nil {
			serverError(w, req, txErr)
			return
		}
		dispatcher.Dispatch(events...)
		render(w, req, "discovery_import.html", result)
	}
}
