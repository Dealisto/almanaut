// Package web wires HTTP routes and renders the server-side UI.
package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/go-chi/chi/v5"
)

type hostsPageData struct {
	Title string
	Hosts []domain.Host
}

type hostFormData struct {
	Title, Heading, Action, SubmitLabel, Error string
	Host  domain.Host
	Types []string
}

// New builds the HTTP handler with all routes wired to repo.
func New(repo *store.HostRepo) http.Handler {
	r := chi.NewRouter()
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/hosts", http.StatusSeeOther)
	})
	r.Get("/hosts", listHosts(repo))
	r.Get("/hosts/new", newHostForm())
	r.Post("/hosts", createHost(repo))
	r.Post("/hosts/{id}/delete", deleteHost(repo))
	return r
}

func listHosts(repo *store.HostRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		hosts, err := repo.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		render(w, "hosts.html", hostsPageData{Title: "Hosts", Hosts: hosts})
	}
}

func newHostForm() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, "host_form.html", hostFormData{
			Title:       "New host",
			Heading:     "New host",
			Action:      "/hosts",
			SubmitLabel: "Create",
			Host:        domain.Host{Type: "physical"},
			Types:       domain.HostTypes,
		})
	}
}

func createHost(repo *store.HostRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		host := domain.Host{
			Name:   strings.TrimSpace(req.FormValue("name")),
			Type:   req.FormValue("type"),
			OS:     req.FormValue("os"),
			Status: req.FormValue("status"),
			Notes:  req.FormValue("notes"),
			IPs:    parseIPs(req.FormValue("ips")),
		}
		if err := host.Validate(); err != nil {
			render(w, "host_form.html", hostFormData{
				Title:       "New host",
				Heading:     "New host",
				Action:      "/hosts",
				SubmitLabel: "Create",
				Host:        host,
				Types:       domain.HostTypes,
				Error:       err.Error(),
			})
			return
		}
		if _, err := repo.Create(host); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/hosts", http.StatusSeeOther)
	}
}

func deleteHost(repo *store.HostRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := repo.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/hosts", http.StatusSeeOther)
	}
}

// parseIPs splits a comma-separated field into trimmed, non-empty values.
func parseIPs(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
