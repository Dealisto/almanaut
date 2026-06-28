package web

import (
	"context"
	"net/http"

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
