package web

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
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

type discoveryLandingData struct {
	Title          string
	NetworkEnabled bool
	ProxmoxEnabled bool
}

type proposalRow struct {
	ContainerID    string
	Name           string
	Ports          string
	Category       string
	AlreadyTracked bool
}

type dockerReviewData struct {
	Title     string
	Proposals []proposalRow
	Hosts     []domain.Host
	Error     string
	NewCount  int
}

func discoveryLanding(opts NetDiscoveryOptions, pve ProxmoxOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, req, "discovery.html", discoveryLandingData{
			Title: "Discover", NetworkEnabled: opts.Enabled, ProxmoxEnabled: pve.Enabled,
		})
	}
}

func scanDocker(scanner dockerScanner, services *store.ServiceRepo, hosts *store.HostRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		data := dockerReviewData{Title: "Docker discovery"}
		hostList, err := hosts.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		data.Hosts = hostList

		containers, err := scanner.Containers(req.Context())
		if err != nil {
			data.Error = "Could not reach the Docker socket: " + err.Error()
			render(w, req, "discovery_docker.html", data)
			return
		}
		existing, err := services.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		for _, p := range discovery.ProposeServices(containers, existing) {
			if !p.AlreadyTracked {
				data.NewCount++
			}
			data.Proposals = append(data.Proposals, proposalRow{
				ContainerID:    p.ContainerID,
				Name:           p.Service.Name,
				Ports:          p.Service.Ports,
				Category:       p.Service.Category,
				AlreadyTracked: p.AlreadyTracked,
			})
		}
		render(w, req, "discovery_docker.html", data)
	}
}

type netHostRow struct {
	IP, Name, Ports string
	AlreadyTracked  bool
}

type networkDiscoveryData struct {
	Title        string
	Subnet       string
	PortsInput   string
	Types        []string
	SelectedType string
	Scanned      bool
	Rows         []netHostRow
	NewCount     int
	Error        string
}

func networkForm(opts NetDiscoveryOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !opts.Enabled {
			http.NotFound(w, req)
			return
		}
		render(w, req, "discovery_network.html", networkDiscoveryData{
			Title: "Network discovery", Subnet: opts.DefaultSubnet,
			Types: domain.HostTypes, SelectedType: "physical",
		})
	}
}

func scanNetwork(netscan networkScanner, hosts *store.HostRepo, opts NetDiscoveryOptions) http.HandlerFunc {
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
		data := networkDiscoveryData{
			Title: "Network discovery", Subnet: subnet, PortsInput: portsInput,
			Types: domain.HostTypes, SelectedType: "physical",
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

func importNetwork(hosts *store.HostRepo, opts NetDiscoveryOptions, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !opts.Enabled {
			http.NotFound(w, req)
			return
		}
		if err := req.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		hostType := req.FormValue("type")
		existing, err := hosts.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		// Re-check tracked IPs against live data; guards double-submit and
		// concurrent manual entry.
		tracked := make(map[string]bool)
		for _, h := range existing {
			for _, ip := range h.IPs {
				tracked[ip] = true
			}
		}
		txErr := store.WithTx(db, func(tx *sql.Tx) error {
			hr := hosts.WithTx(tx)
			for _, v := range req.Form["host"] {
				// Each value is "ip|name|ports". PTR hostnames and IPs are LDH/
				// numeric (no "|"), and SplitN caps at 3 so a "|" in ports is kept.
				// Malformed or empty-field rows fall through to Host.Validate below.
				parts := strings.SplitN(v, "|", 3)
				if len(parts) != 3 {
					continue
				}
				ip, name, ports := parts[0], parts[1], parts[2]
				if tracked[ip] {
					continue
				}
				h := domain.Host{
					Name: name, Type: hostType,
					IPs: []string{ip}, Notes: discovery.NetworkHostNotes(ports),
				}
				// Discovery must not write a Host the manual UI would reject.
				if err := h.Validate(); err != nil {
					continue
				}
				if _, err := hr.Create(h); err != nil {
					return err
				}
				tracked[ip] = true // avoid a duplicate within the same submit
			}
			return nil
		})
		if txErr != nil {
			serverError(w, req, txErr)
			return
		}
		http.Redirect(w, req, "/hosts", http.StatusSeeOther)
	}
}

type proxmoxRow struct {
	ID, Name, Type, Status, CPU, RAM, Disk string
	AlreadyTracked                         bool
}

type proxmoxReviewData struct {
	Title    string
	Rows     []proxmoxRow
	NewCount int
	Error    string
}

func scanProxmox(scanner proxmoxScanner, hosts *store.HostRepo, opts ProxmoxOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !opts.Enabled {
			http.NotFound(w, req)
			return
		}
		data := proxmoxReviewData{Title: "Proxmox discovery"}
		res, err := scanner.Resources(req.Context())
		if err != nil {
			data.Error = "Could not reach the Proxmox API: " + err.Error()
			render(w, req, "discovery_proxmox.html", data)
			return
		}
		existing, err := hosts.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		for _, p := range discovery.ProposeProxmoxHosts(res, existing) {
			if !p.AlreadyTracked {
				data.NewCount++
			}
			data.Rows = append(data.Rows, proxmoxRow{
				ID: p.ID, Name: p.Host.Name, Type: p.Host.Type, Status: p.Host.Status,
				CPU: p.Host.CPU, RAM: p.Host.RAM, Disk: p.Host.Disk, AlreadyTracked: p.AlreadyTracked,
			})
		}
		render(w, req, "discovery_proxmox.html", data)
	}
}

func importProxmox(scanner proxmoxScanner, hosts *store.HostRepo, rels *store.RelationshipRepo, opts ProxmoxOptions, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if !opts.Enabled {
			http.NotFound(w, req)
			return
		}
		if err := req.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		selected := make(map[string]bool)
		for _, id := range req.Form["id"] {
			selected[id] = true
		}
		linkToNode := req.FormValue("link") != ""

		// Re-query so we never round-trip proposal data through hidden fields; a
		// guest that vanished since the review is simply skipped.
		res, err := scanner.Resources(req.Context())
		if err != nil {
			http.Error(w, "could not reach the Proxmox API: "+err.Error(), http.StatusBadGateway)
			return
		}
		existing, err := hosts.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		// resource id -> node name, to resolve a guest's node after import.
		nodeOf := make(map[string]string)
		for _, r := range res {
			nodeOf[r.ID] = r.Node
		}

		proposals := discovery.ProposeProxmoxHosts(res, existing)

		// One transaction covers host creation and guest linking, so a link
		// failure rolls back the hosts created in the same submit too.
		txErr := store.WithTx(db, func(tx *sql.Tx) error {
			hr := hosts.WithTx(tx)
			rl := rels.WithTx(tx)

			// createdID maps each imported host's Proxmox resource id to its new DB
			// id. Keying on the resource id (not the name) keeps guest linking exact:
			// guest names can collide across nodes, but resource ids are unique.
			createdID := make(map[string]int64)
			for _, p := range proposals {
				// ProposeProxmoxHosts recomputes AlreadyTracked against the freshly
				// listed hosts, so skipping tracked rows also guards double-submit.
				if p.AlreadyTracked || !selected[p.ID] {
					continue
				}
				// Discovery must not write a Host the manual UI would reject.
				if err := p.Host.Validate(); err != nil {
					continue
				}
				newID, err := hr.Create(p.Host)
				if err != nil {
					return err
				}
				createdID[p.ID] = newID
			}
			if linkToNode {
				// Resolve a guest's node by name. Proxmox node names are unique within
				// a cluster, so name resolution is safe on the node side (existing node
				// hosts plus any imported this submit). A manual host that happens to
				// share a node's name is the only residual collision: at worst one edge
				// points at the wrong host, never data loss.
				nodeIDByName := make(map[string]int64)
				for _, h := range existing {
					nodeIDByName[discovery.NormalizeName(h.Name)] = h.ID
				}
				for _, p := range proposals {
					if id, ok := createdID[p.ID]; ok && p.Host.Type == "physical" {
						nodeIDByName[discovery.NormalizeName(p.Host.Name)] = id
					}
				}
				// Link only freshly-imported guests, so re-import never duplicates edges.
				for _, p := range proposals {
					guestID, ok := createdID[p.ID]
					if !ok || (p.Host.Type != "vm" && p.Host.Type != "lxc") {
						continue
					}
					nodeID, ok := nodeIDByName[discovery.NormalizeName(nodeOf[p.ID])]
					if !ok || nodeID == guestID {
						continue
					}
					rel := domain.Relationship{
						FromType: "host", FromID: guestID,
						ToType: "host", ToID: nodeID, Kind: "runs on",
					}
					if err := rel.Validate(); err != nil {
						continue
					}
					if _, err := rl.Create(rel); err != nil {
						return err
					}
				}
			}
			return nil
		})
		if txErr != nil {
			serverError(w, req, txErr)
			return
		}
		http.Redirect(w, req, "/hosts", http.StatusSeeOther)
	}
}

// errInvalidRel marks a relationship that failed Validate during an import, so
// the handler can map it to 400 (vs 500 for DB errors) after the transaction.
var errInvalidRel = errors.New("invalid relationship")

func importDocker(scanner dockerScanner, services *store.ServiceRepo, rels *store.RelationshipRepo, db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if err := req.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		selected := make(map[string]bool)
		for _, id := range req.Form["id"] {
			selected[id] = true
		}

		var hostID int64
		if raw := req.FormValue("host"); raw != "" {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				http.Error(w, "invalid host id", http.StatusBadRequest)
				return
			}
			hostID = id
		}

		// Re-scan so we never round-trip proposal data through hidden fields; a
		// container that vanished since the review is simply skipped.
		containers, err := scanner.Containers(req.Context())
		if err != nil {
			http.Error(w, "could not reach the Docker socket: "+err.Error(), http.StatusBadGateway)
			return
		}
		existing, err := services.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		proposals := discovery.ProposeServices(containers, existing)

		// The whole import is one transaction: any failure rolls back every
		// service/relationship written in this submit, so no orphan Service
		// can survive a failed relationship.
		txErr := store.WithTx(db, func(tx *sql.Tx) error {
			svc := services.WithTx(tx)
			rl := rels.WithTx(tx)
			for _, p := range proposals {
				// ProposeServices recomputes AlreadyTracked against the freshly-listed
				// services, so skipping tracked rows also guards against double-submit.
				if p.AlreadyTracked || !selected[p.ContainerID] {
					continue
				}
				// Discovery must not write a Service that the manual UI would reject
				// (e.g. a container with no name). Skip invalid proposals.
				if err := p.Service.Validate(); err != nil {
					continue
				}
				newID, err := svc.Create(p.Service)
				if err != nil {
					return err
				}
				if hostID > 0 {
					rel := domain.Relationship{
						FromType: "service", FromID: newID,
						ToType: "host", ToID: hostID, Kind: "runs on",
					}
					if err := rel.Validate(); err != nil {
						return fmt.Errorf("%w: %v", errInvalidRel, err)
					}
					if _, err := rl.Create(rel); err != nil {
						return err
					}
				}
			}
			return nil
		})
		if txErr != nil {
			if errors.Is(txErr, errInvalidRel) {
				http.Error(w, txErr.Error(), http.StatusBadRequest)
			} else {
				serverError(w, req, txErr)
			}
			return
		}
		http.Redirect(w, req, "/services", http.StatusSeeOther)
	}
}
