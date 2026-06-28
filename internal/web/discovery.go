package web

import (
	"context"
	"net/http"
	"strconv"

	"github.com/Dealisto/almanaut/internal/discovery"
	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// dockerScanner is the subset of the Docker discovery client the web layer uses.
type dockerScanner interface {
	Containers(ctx context.Context) ([]discovery.Container, error)
}

type discoveryLandingData struct {
	Title string
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

func discoveryLanding() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, "discovery.html", discoveryLandingData{Title: "Discover"})
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
