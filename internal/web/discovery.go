package web

import (
	"context"
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

type discoveryLandingData struct {
	Title          string
	NetworkEnabled bool
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

func discoveryLanding(opts NetDiscoveryOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, "discovery.html", discoveryLandingData{Title: "Discover", NetworkEnabled: opts.Enabled})
	}
}

func scanDocker(scanner dockerScanner, services *store.ServiceRepo, hosts *store.HostRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		data := dockerReviewData{Title: "Docker discovery"}
		hostList, err := hosts.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data.Hosts = hostList

		containers, err := scanner.Containers(req.Context())
		if err != nil {
			data.Error = "Could not reach the Docker socket: " + err.Error()
			render(w, "discovery_docker.html", data)
			return
		}
		existing, err := services.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
		render(w, "discovery_docker.html", data)
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
		render(w, "discovery_network.html", networkDiscoveryData{
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
			render(w, "discovery_network.html", data)
			return
		}
		var ports []int
		if portsInput != "" {
			p, err := discovery.ParsePorts(portsInput)
			if err != nil {
				data.Error = "Invalid ports: " + err.Error()
				render(w, "discovery_network.html", data)
				return
			}
			ports = p
		}
		scanned, err := netscan.Scan(req.Context(), subnet, ports)
		if err != nil {
			data.Error = "Scan failed: " + err.Error()
			render(w, "discovery_network.html", data)
			return
		}
		existing, err := hosts.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
		render(w, "discovery_network.html", data)
	}
}

func importNetwork(hosts *store.HostRepo, opts NetDiscoveryOptions) http.HandlerFunc {
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
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
			if _, err := hosts.Create(h); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			tracked[ip] = true // avoid a duplicate within the same submit
		}
		http.Redirect(w, req, "/hosts", http.StatusSeeOther)
	}
}

func importDocker(scanner dockerScanner, services *store.ServiceRepo, rels *store.RelationshipRepo) http.HandlerFunc {
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
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// ProposeServices recomputes AlreadyTracked against the freshly-listed
		// services, so skipping tracked rows also guards against double-submit.
		for _, p := range discovery.ProposeServices(containers, existing) {
			if p.AlreadyTracked || !selected[p.ContainerID] {
				continue
			}
			// Discovery must not write a Service that the manual UI would reject
			// (e.g. a container with no name). Skip invalid proposals.
			if err := p.Service.Validate(); err != nil {
				continue
			}
			newID, err := services.Create(p.Service)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if hostID > 0 {
				rel := domain.Relationship{
					FromType: "service", FromID: newID,
					ToType: "host", ToID: hostID, Kind: "runs on",
				}
				if err := rel.Validate(); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				if _, err := rels.Create(rel); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
		}
		http.Redirect(w, req, "/services", http.StatusSeeOther)
	}
}
