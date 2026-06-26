// Package web wires HTTP routes and renders the server-side UI.
package web

import (
	"fmt"
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

type servicesPageData struct {
	Title    string
	Services []domain.Service
}

type serviceFormData struct {
	Title, Heading, Action, SubmitLabel, Error string
	Service domain.Service
	Kinds   []string
}

// New builds the HTTP handler with all routes wired to the given repos.
func New(hosts *store.HostRepo, services *store.ServiceRepo) http.Handler {
	r := chi.NewRouter()
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/hosts", http.StatusSeeOther)
	})
	r.Get("/hosts", listHosts(hosts))
	r.Get("/hosts/new", newHostForm())
	r.Post("/hosts", createHost(hosts))
	r.Get("/hosts/{id}/edit", editHostForm(hosts))
	r.Post("/hosts/{id}", updateHost(hosts))
	r.Post("/hosts/{id}/delete", deleteHost(hosts))

	r.Get("/services", listServices(services))
	r.Get("/services/new", newServiceForm())
	r.Post("/services", createService(services))
	r.Get("/services/{id}/edit", editServiceForm(services))
	r.Post("/services/{id}", updateService(services))
	r.Post("/services/{id}/delete", deleteService(services))
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
			CPU:    req.FormValue("cpu"),
			RAM:    req.FormValue("ram"),
			Disk:   req.FormValue("disk"),
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

func editHostForm(repo *store.HostRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		host, err := repo.Get(id)
		if err != nil {
			http.Error(w, "host not found", http.StatusNotFound)
			return
		}
		render(w, "host_form.html", hostFormData{
			Title:       "Edit host",
			Heading:     "Edit host",
			Action:      fmt.Sprintf("/hosts/%d", id),
			SubmitLabel: "Save",
			Host:        host,
			Types:       domain.HostTypes,
		})
	}
}

func updateHost(repo *store.HostRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		host := domain.Host{
			ID:     id,
			Name:   strings.TrimSpace(req.FormValue("name")),
			Type:   req.FormValue("type"),
			OS:     req.FormValue("os"),
			CPU:    req.FormValue("cpu"),
			RAM:    req.FormValue("ram"),
			Disk:   req.FormValue("disk"),
			Status: req.FormValue("status"),
			Notes:  req.FormValue("notes"),
			IPs:    parseIPs(req.FormValue("ips")),
		}
		if err := host.Validate(); err != nil {
			render(w, "host_form.html", hostFormData{
				Title:       "Edit host",
				Heading:     "Edit host",
				Action:      fmt.Sprintf("/hosts/%d", id),
				SubmitLabel: "Save",
				Host:        host,
				Types:       domain.HostTypes,
				Error:       err.Error(),
			})
			return
		}
		if err := repo.Update(host); err != nil {
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

func listServices(repo *store.ServiceRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		services, err := repo.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		render(w, "services.html", servicesPageData{Title: "Services", Services: services})
	}
}

func newServiceForm() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, "service_form.html", serviceFormData{
			Title: "New service", Heading: "New service", Action: "/services",
			SubmitLabel: "Create", Service: domain.Service{Kind: "container"}, Kinds: domain.ServiceKinds,
		})
	}
}

func serviceFromForm(req *http.Request) domain.Service {
	return domain.Service{
		Name:     strings.TrimSpace(req.FormValue("name")),
		Kind:     req.FormValue("kind"),
		URL:      req.FormValue("url"),
		Ports:    req.FormValue("ports"),
		Category: req.FormValue("category"),
		Notes:    req.FormValue("notes"),
	}
}

func createService(repo *store.ServiceRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		svc := serviceFromForm(req)
		if err := svc.Validate(); err != nil {
			render(w, "service_form.html", serviceFormData{
				Title: "New service", Heading: "New service", Action: "/services",
				SubmitLabel: "Create", Service: svc, Kinds: domain.ServiceKinds, Error: err.Error(),
			})
			return
		}
		if _, err := repo.Create(svc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/services", http.StatusSeeOther)
	}
}

func editServiceForm(repo *store.ServiceRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		svc, err := repo.Get(id)
		if err != nil {
			http.Error(w, "service not found", http.StatusNotFound)
			return
		}
		render(w, "service_form.html", serviceFormData{
			Title: "Edit service", Heading: "Edit service", Action: fmt.Sprintf("/services/%d", id),
			SubmitLabel: "Save", Service: svc, Kinds: domain.ServiceKinds,
		})
	}
}

func updateService(repo *store.ServiceRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		svc := serviceFromForm(req)
		svc.ID = id
		if err := svc.Validate(); err != nil {
			render(w, "service_form.html", serviceFormData{
				Title: "Edit service", Heading: "Edit service", Action: fmt.Sprintf("/services/%d", id),
				SubmitLabel: "Save", Service: svc, Kinds: domain.ServiceKinds, Error: err.Error(),
			})
			return
		}
		if err := repo.Update(svc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/services", http.StatusSeeOther)
	}
}

func deleteService(repo *store.ServiceRepo) http.HandlerFunc {
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
		http.Redirect(w, req, "/services", http.StatusSeeOther)
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
