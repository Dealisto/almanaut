// Package web wires HTTP routes and renders the server-side UI.
package web

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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

type networksPageData struct {
	Title    string
	Networks []domain.Network
}

type networkFormData struct {
	Title, Heading, Action, SubmitLabel, Error string
	Network domain.Network
}

type domainsPageData struct {
	Title   string
	Domains []domain.Domain
}

type domainFormData struct {
	Title, Heading, Action, SubmitLabel, Error string
	Domain domain.Domain
}

type certificatesPageData struct {
	Title        string
	Certificates []domain.Certificate
}

type certificateFormData struct {
	Title, Heading, Action, SubmitLabel, Error string
	Certificate domain.Certificate
}

type backupsPageData struct {
	Title   string
	Backups []domain.Backup
}

type backupFormData struct {
	Title, Heading, Action, SubmitLabel, Error string
	Backup domain.Backup
}

// New builds the HTTP handler with all routes wired to the given repos.
func New(
	hosts *store.HostRepo,
	services *store.ServiceRepo,
	networks *store.NetworkRepo,
	domains *store.DomainRepo,
	certificates *store.CertificateRepo,
	backups *store.BackupRepo,
	relationships *store.RelationshipRepo,
	tags *store.TagRepo,
	db *sql.DB,
) http.Handler {
	cat := entityCatalog{
		hosts: hosts, services: services, networks: networks,
		domains: domains, certificates: certificates, backups: backups,
	}
	r := chi.NewRouter()
	r.Get("/", dashboard(cat, relationships))
	r.Get("/hosts", listHosts(hosts))
	r.Get("/hosts/new", newHostForm())
	r.Post("/hosts", createHost(hosts))
	r.Get("/hosts/{id}", showHost(hosts, cat, tags, relationships))
	r.Get("/hosts/{id}/edit", editHostForm(hosts))
	r.Post("/hosts/{id}", updateHost(hosts))
	r.Post("/hosts/{id}/delete", deleteHost(hosts, relationships, tags))
	r.Post("/tags", addTag(tags))
	r.Post("/tags/delete", removeTag(tags))
	r.Get("/tags", tagsOverview(tags, cat))

	r.Get("/services", listServices(services))
	r.Get("/services/new", newServiceForm())
	r.Post("/services", createService(services))
	r.Get("/services/{id}", showService(services, cat, tags, relationships))
	r.Get("/services/{id}/edit", editServiceForm(services))
	r.Post("/services/{id}", updateService(services))
	r.Post("/services/{id}/delete", deleteService(services, relationships, tags))

	r.Get("/networks", listNetworks(networks))
	r.Get("/networks/new", newNetworkForm())
	r.Post("/networks", createNetwork(networks))
	r.Get("/networks/{id}", showNetwork(networks, cat, tags, relationships))
	r.Get("/networks/{id}/edit", editNetworkForm(networks))
	r.Post("/networks/{id}", updateNetwork(networks))
	r.Post("/networks/{id}/delete", deleteNetwork(networks, relationships, tags))

	r.Get("/domains", listDomains(domains))
	r.Get("/domains/new", newDomainForm())
	r.Post("/domains", createDomain(domains))
	r.Get("/domains/{id}", showDomain(domains, cat, tags, relationships))
	r.Get("/domains/{id}/edit", editDomainForm(domains))
	r.Post("/domains/{id}", updateDomain(domains))
	r.Post("/domains/{id}/delete", deleteDomain(domains, relationships, tags))

	r.Get("/certificates", listCertificates(certificates))
	r.Get("/certificates/new", newCertificateForm())
	r.Post("/certificates", createCertificate(certificates))
	r.Get("/certificates/{id}", showCertificate(certificates, cat, tags, relationships))
	r.Get("/certificates/{id}/edit", editCertificateForm(certificates))
	r.Post("/certificates/{id}", updateCertificate(certificates))
	r.Post("/certificates/{id}/delete", deleteCertificate(certificates, relationships, tags))

	r.Get("/backups", listBackups(backups))
	r.Get("/backups/new", newBackupForm())
	r.Post("/backups", createBackup(backups))
	r.Get("/backups/{id}", showBackup(backups, cat, tags, relationships))
	r.Get("/backups/{id}/edit", editBackupForm(backups))
	r.Post("/backups/{id}", updateBackup(backups))
	r.Post("/backups/{id}/delete", deleteBackup(backups, relationships, tags))

	r.Get("/relationships", listRelationships(relationships, cat))
	r.Post("/relationships", createRelationship(relationships, cat))
	r.Post("/relationships/{id}/delete", deleteRelationship(relationships))
	r.Get("/impact", impactView(relationships, cat))
	r.Get("/checks", healthChecks(services, certificates, relationships))
	r.Get("/search", searchEntities(cat, tags))
	r.Get("/data", showData())
	r.Get("/export", exportData(db))
	r.Post("/import", importData(db))
	return r
}

type impactPageData struct {
	Title         string
	Options       []entityOption
	Selected      string
	SelectedLabel string
	Impacted      []string
	Computed      bool
	Error         string
}

func impactView(rels *store.RelationshipRepo, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		opts, err := cat.options()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		data := impactPageData{Title: "Impact", Options: opts}

		ref := req.URL.Query().Get("ref")
		if ref != "" {
			typ, id, perr := parseRef(ref)
			if perr != nil {
				data.Error = perr.Error()
				render(w, "impact.html", data)
				return
			}
			labels := labelMap(opts)
			data.Selected = ref
			data.SelectedLabel = labelOrFallback(labels, typ, id)
			refs, err := store.Impact(rels, typ, id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			data.Computed = true
			for _, r := range refs {
				data.Impacted = append(data.Impacted, labelOrFallback(labels, r.Type, r.ID))
			}
		}
		render(w, "impact.html", data)
	}
}

type checksPageData struct {
	Title            string
	WithinDays       int
	UnbackedServices []domain.Service
	ExpiringCerts    []domain.Certificate
}

func healthChecks(services *store.ServiceRepo, certs *store.CertificateRepo, rels *store.RelationshipRepo) http.HandlerFunc {
	const withinDays = 30
	return func(w http.ResponseWriter, req *http.Request) {
		svcList, err := services.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		certList, err := certs.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		relList, err := rels.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		render(w, "checks.html", checksPageData{
			Title:            "Checks",
			WithinDays:       withinDays,
			UnbackedServices: domain.ServicesWithoutBackup(svcList, relList),
			ExpiringCerts:    domain.ExpiringSoon(certList, time.Now(), withinDays),
		})
	}
}

type relationshipView struct {
	ID                       int64
	FromLabel, Kind, ToLabel string
}

type relationshipsPageData struct {
	Title         string
	Relationships []relationshipView
	Options       []entityOption
	Kinds         []string
	Error         string
}

func renderRelationships(w http.ResponseWriter, rels *store.RelationshipRepo, cat entityCatalog, errMsg string) {
	opts, err := cat.options()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	labels := labelMap(opts)
	all, err := rels.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	views := make([]relationshipView, 0, len(all))
	for _, rel := range all {
		views = append(views, relationshipView{
			ID:        rel.ID,
			FromLabel: labelOrFallback(labels, rel.FromType, rel.FromID),
			Kind:      rel.Kind,
			ToLabel:   labelOrFallback(labels, rel.ToType, rel.ToID),
		})
	}
	render(w, "relationships.html", relationshipsPageData{
		Title: "Relationships", Relationships: views, Options: opts,
		Kinds: domain.RelationshipKinds, Error: errMsg,
	})
}

func listRelationships(rels *store.RelationshipRepo, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		renderRelationships(w, rels, cat, "")
	}
}

func createRelationship(rels *store.RelationshipRepo, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		fromType, fromID, errFrom := parseRef(req.FormValue("from"))
		toType, toID, errTo := parseRef(req.FormValue("to"))
		rel := domain.Relationship{
			FromType: fromType, FromID: fromID,
			ToType: toType, ToID: toID, Kind: req.FormValue("kind"),
		}
		if errFrom != nil {
			renderRelationships(w, rels, cat, errFrom.Error())
			return
		}
		if errTo != nil {
			renderRelationships(w, rels, cat, errTo.Error())
			return
		}
		if err := rel.Validate(); err != nil {
			renderRelationships(w, rels, cat, err.Error())
			return
		}
		if _, err := rels.Create(rel); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/relationships", http.StatusSeeOther)
	}
}

func deleteRelationship(rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := rels.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/relationships", http.StatusSeeOther)
	}
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

func deleteHost(repo *store.HostRepo, rels *store.RelationshipRepo, tags *store.TagRepo) http.HandlerFunc {
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
		if err := rels.DeleteByEntity("host", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tags.DeleteByEntity("host", id); err != nil {
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

func deleteService(repo *store.ServiceRepo, rels *store.RelationshipRepo, tags *store.TagRepo) http.HandlerFunc {
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
		if err := rels.DeleteByEntity("service", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tags.DeleteByEntity("service", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/services", http.StatusSeeOther)
	}
}

func listNetworks(repo *store.NetworkRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		networks, err := repo.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		render(w, "networks.html", networksPageData{Title: "Networks", Networks: networks})
	}
}

func newNetworkForm() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, "network_form.html", networkFormData{
			Title: "New network", Heading: "New network", Action: "/networks", SubmitLabel: "Create",
		})
	}
}

func networkFromForm(req *http.Request) domain.Network {
	return domain.Network{
		Name:    strings.TrimSpace(req.FormValue("name")),
		CIDR:    strings.TrimSpace(req.FormValue("cidr")),
		VLAN:    req.FormValue("vlan"),
		Gateway: strings.TrimSpace(req.FormValue("gateway")),
		Notes:   req.FormValue("notes"),
	}
}

func createNetwork(repo *store.NetworkRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		n := networkFromForm(req)
		if err := n.Validate(); err != nil {
			render(w, "network_form.html", networkFormData{
				Title: "New network", Heading: "New network", Action: "/networks",
				SubmitLabel: "Create", Network: n, Error: err.Error(),
			})
			return
		}
		if _, err := repo.Create(n); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/networks", http.StatusSeeOther)
	}
}

func editNetworkForm(repo *store.NetworkRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		n, err := repo.Get(id)
		if err != nil {
			http.Error(w, "network not found", http.StatusNotFound)
			return
		}
		render(w, "network_form.html", networkFormData{
			Title: "Edit network", Heading: "Edit network", Action: fmt.Sprintf("/networks/%d", id),
			SubmitLabel: "Save", Network: n,
		})
	}
}

func updateNetwork(repo *store.NetworkRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		n := networkFromForm(req)
		n.ID = id
		if err := n.Validate(); err != nil {
			render(w, "network_form.html", networkFormData{
				Title: "Edit network", Heading: "Edit network", Action: fmt.Sprintf("/networks/%d", id),
				SubmitLabel: "Save", Network: n, Error: err.Error(),
			})
			return
		}
		if err := repo.Update(n); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/networks", http.StatusSeeOther)
	}
}

func deleteNetwork(repo *store.NetworkRepo, rels *store.RelationshipRepo, tags *store.TagRepo) http.HandlerFunc {
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
		if err := rels.DeleteByEntity("network", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tags.DeleteByEntity("network", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/networks", http.StatusSeeOther)
	}
}

func listDomains(repo *store.DomainRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		domains, err := repo.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		render(w, "domains.html", domainsPageData{Title: "Domains", Domains: domains})
	}
}

func newDomainForm() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, "domain_form.html", domainFormData{
			Title: "New domain", Heading: "New domain", Action: "/domains", SubmitLabel: "Create",
		})
	}
}

func domainFromForm(req *http.Request) domain.Domain {
	return domain.Domain{
		FQDN:     strings.TrimSpace(req.FormValue("fqdn")),
		Provider: strings.TrimSpace(req.FormValue("provider")),
		Notes:    req.FormValue("notes"),
	}
}

func createDomain(repo *store.DomainRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		d := domainFromForm(req)
		if err := d.Validate(); err != nil {
			render(w, "domain_form.html", domainFormData{
				Title: "New domain", Heading: "New domain", Action: "/domains",
				SubmitLabel: "Create", Domain: d, Error: err.Error(),
			})
			return
		}
		if _, err := repo.Create(d); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/domains", http.StatusSeeOther)
	}
}

func editDomainForm(repo *store.DomainRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		d, err := repo.Get(id)
		if err != nil {
			http.Error(w, "domain not found", http.StatusNotFound)
			return
		}
		render(w, "domain_form.html", domainFormData{
			Title: "Edit domain", Heading: "Edit domain", Action: fmt.Sprintf("/domains/%d", id),
			SubmitLabel: "Save", Domain: d,
		})
	}
}

func updateDomain(repo *store.DomainRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		d := domainFromForm(req)
		d.ID = id
		if err := d.Validate(); err != nil {
			render(w, "domain_form.html", domainFormData{
				Title: "Edit domain", Heading: "Edit domain", Action: fmt.Sprintf("/domains/%d", id),
				SubmitLabel: "Save", Domain: d, Error: err.Error(),
			})
			return
		}
		if err := repo.Update(d); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/domains", http.StatusSeeOther)
	}
}

func deleteDomain(repo *store.DomainRepo, rels *store.RelationshipRepo, tags *store.TagRepo) http.HandlerFunc {
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
		if err := rels.DeleteByEntity("domain", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tags.DeleteByEntity("domain", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/domains", http.StatusSeeOther)
	}
}

func listCertificates(repo *store.CertificateRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		certs, err := repo.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		render(w, "certificates.html", certificatesPageData{Title: "Certificates", Certificates: certs})
	}
}

func newCertificateForm() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, "certificate_form.html", certificateFormData{
			Title: "New certificate", Heading: "New certificate", Action: "/certificates", SubmitLabel: "Create",
		})
	}
}

func certificateFromForm(req *http.Request) domain.Certificate {
	return domain.Certificate{
		Subject:   strings.TrimSpace(req.FormValue("subject")),
		Issuer:    strings.TrimSpace(req.FormValue("issuer")),
		ExpiresOn: strings.TrimSpace(req.FormValue("expires_on")),
		AutoRenew: req.FormValue("auto_renew") == "on",
		Notes:     req.FormValue("notes"),
	}
}

func createCertificate(repo *store.CertificateRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		c := certificateFromForm(req)
		if err := c.Validate(); err != nil {
			render(w, "certificate_form.html", certificateFormData{
				Title: "New certificate", Heading: "New certificate", Action: "/certificates",
				SubmitLabel: "Create", Certificate: c, Error: err.Error(),
			})
			return
		}
		if _, err := repo.Create(c); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/certificates", http.StatusSeeOther)
	}
}

func editCertificateForm(repo *store.CertificateRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		c, err := repo.Get(id)
		if err != nil {
			http.Error(w, "certificate not found", http.StatusNotFound)
			return
		}
		render(w, "certificate_form.html", certificateFormData{
			Title: "Edit certificate", Heading: "Edit certificate", Action: fmt.Sprintf("/certificates/%d", id),
			SubmitLabel: "Save", Certificate: c,
		})
	}
}

func updateCertificate(repo *store.CertificateRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		c := certificateFromForm(req)
		c.ID = id
		if err := c.Validate(); err != nil {
			render(w, "certificate_form.html", certificateFormData{
				Title: "Edit certificate", Heading: "Edit certificate", Action: fmt.Sprintf("/certificates/%d", id),
				SubmitLabel: "Save", Certificate: c, Error: err.Error(),
			})
			return
		}
		if err := repo.Update(c); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/certificates", http.StatusSeeOther)
	}
}

func deleteCertificate(repo *store.CertificateRepo, rels *store.RelationshipRepo, tags *store.TagRepo) http.HandlerFunc {
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
		if err := rels.DeleteByEntity("certificate", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tags.DeleteByEntity("certificate", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/certificates", http.StatusSeeOther)
	}
}

func listBackups(repo *store.BackupRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		backups, err := repo.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		render(w, "backups.html", backupsPageData{Title: "Backups", Backups: backups})
	}
}

func newBackupForm() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		render(w, "backup_form.html", backupFormData{
			Title: "New backup", Heading: "New backup", Action: "/backups", SubmitLabel: "Create",
		})
	}
}

func backupFromForm(req *http.Request) domain.Backup {
	return domain.Backup{
		Source:      strings.TrimSpace(req.FormValue("source")),
		Destination: strings.TrimSpace(req.FormValue("destination")),
		Frequency:   strings.TrimSpace(req.FormValue("frequency")),
		LastRun:     strings.TrimSpace(req.FormValue("last_run")),
		Notes:       req.FormValue("notes"),
	}
}

func createBackup(repo *store.BackupRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		b := backupFromForm(req)
		if err := b.Validate(); err != nil {
			render(w, "backup_form.html", backupFormData{
				Title: "New backup", Heading: "New backup", Action: "/backups",
				SubmitLabel: "Create", Backup: b, Error: err.Error(),
			})
			return
		}
		if _, err := repo.Create(b); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/backups", http.StatusSeeOther)
	}
}

func editBackupForm(repo *store.BackupRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		b, err := repo.Get(id)
		if err != nil {
			http.Error(w, "backup not found", http.StatusNotFound)
			return
		}
		render(w, "backup_form.html", backupFormData{
			Title: "Edit backup", Heading: "Edit backup", Action: fmt.Sprintf("/backups/%d", id),
			SubmitLabel: "Save", Backup: b,
		})
	}
}

func updateBackup(repo *store.BackupRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		b := backupFromForm(req)
		b.ID = id
		if err := b.Validate(); err != nil {
			render(w, "backup_form.html", backupFormData{
				Title: "Edit backup", Heading: "Edit backup", Action: fmt.Sprintf("/backups/%d", id),
				SubmitLabel: "Save", Backup: b, Error: err.Error(),
			})
			return
		}
		if err := repo.Update(b); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/backups", http.StatusSeeOther)
	}
}

func deleteBackup(repo *store.BackupRepo, rels *store.RelationshipRepo, tags *store.TagRepo) http.HandlerFunc {
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
		if err := rels.DeleteByEntity("backup", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := tags.DeleteByEntity("backup", id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, "/backups", http.StatusSeeOther)
	}
}

func showBackup(repo *store.BackupRepo, cat entityCatalog, tags *store.TagRepo, rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		b, err := repo.Get(id)
		if err != nil {
			http.Error(w, "backup not found", http.StatusNotFound)
			return
		}
		fields := []fieldRow{
			{"Destination", b.Destination},
			{"Frequency", b.Frequency},
			{"Last run", b.LastRun},
		}
		renderDetail(w, cat, tags, rels, "backup", id,
			"Backup: "+b.Source, b.Notes, fmt.Sprintf("/backups/%d/edit", id), fields)
	}
}

func showHost(repo *store.HostRepo, cat entityCatalog, tags *store.TagRepo, rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		h, err := repo.Get(id)
		if err != nil {
			http.Error(w, "host not found", http.StatusNotFound)
			return
		}
		fields := []fieldRow{
			{"Type", h.Type},
			{"OS", h.OS},
			{"CPU", h.CPU},
			{"RAM", h.RAM},
			{"Disk", h.Disk},
			{"Status", h.Status},
			{"IPs", strings.Join(h.IPs, ", ")},
		}
		renderDetail(w, cat, tags, rels, "host", id,
			"Host: "+h.Name, h.Notes, fmt.Sprintf("/hosts/%d/edit", id), fields)
	}
}

func showService(repo *store.ServiceRepo, cat entityCatalog, tags *store.TagRepo, rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		s, err := repo.Get(id)
		if err != nil {
			http.Error(w, "service not found", http.StatusNotFound)
			return
		}
		fields := []fieldRow{
			{"Kind", s.Kind},
			{"URL", s.URL},
			{"Ports", s.Ports},
			{"Category", s.Category},
		}
		renderDetail(w, cat, tags, rels, "service", id,
			"Service: "+s.Name, s.Notes, fmt.Sprintf("/services/%d/edit", id), fields)
	}
}

func showNetwork(repo *store.NetworkRepo, cat entityCatalog, tags *store.TagRepo, rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		n, err := repo.Get(id)
		if err != nil {
			http.Error(w, "network not found", http.StatusNotFound)
			return
		}
		fields := []fieldRow{
			{"CIDR", n.CIDR},
			{"VLAN", n.VLAN},
			{"Gateway", n.Gateway},
		}
		renderDetail(w, cat, tags, rels, "network", id,
			"Network: "+n.Name, n.Notes, fmt.Sprintf("/networks/%d/edit", id), fields)
	}
}

func showDomain(repo *store.DomainRepo, cat entityCatalog, tags *store.TagRepo, rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		d, err := repo.Get(id)
		if err != nil {
			http.Error(w, "domain not found", http.StatusNotFound)
			return
		}
		fields := []fieldRow{
			{"Provider", d.Provider},
		}
		renderDetail(w, cat, tags, rels, "domain", id,
			"Domain: "+d.FQDN, d.Notes, fmt.Sprintf("/domains/%d/edit", id), fields)
	}
}

func showCertificate(repo *store.CertificateRepo, cat entityCatalog, tags *store.TagRepo, rels *store.RelationshipRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		c, err := repo.Get(id)
		if err != nil {
			http.Error(w, "certificate not found", http.StatusNotFound)
			return
		}
		autoRenew := "no"
		if c.AutoRenew {
			autoRenew = "yes"
		}
		fields := []fieldRow{
			{"Issuer", c.Issuer},
			{"Expires on", c.ExpiresOn},
			{"Auto-renew", autoRenew},
		}
		renderDetail(w, cat, tags, rels, "certificate", id,
			"Certificate: "+c.Subject, c.Notes, fmt.Sprintf("/certificates/%d/edit", id), fields)
	}
}

type tagEntity struct {
	Label string
	URL   string
}

type tagsOverviewData struct {
	Title    string
	Counts   []store.TagCount
	Selected string
	Entities []tagEntity
}

func tagsOverview(tags *store.TagRepo, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		// Decide between the tag cloud and a drilldown on whether a name was
		// actually requested, not on its normalized form — otherwise a query
		// like ?name=# (which normalizes to "") would silently show the cloud.
		raw := strings.TrimSpace(req.URL.Query().Get("name"))
		if raw == "" {
			counts, err := tags.Counts()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			render(w, "tags_overview.html", tagsOverviewData{Title: "Tags", Counts: counts})
			return
		}

		name := domain.NormalizeTag(raw)
		tagged, err := tags.ListByName(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		opts, err := cat.options()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		labels := labelMap(opts)
		entities := make([]tagEntity, 0, len(tagged))
		for _, tg := range tagged {
			entities = append(entities, tagEntity{
				Label: labelOrFallback(labels, tg.EntityType, tg.EntityID),
				URL:   fmt.Sprintf("/%ss/%d", tg.EntityType, tg.EntityID),
			})
		}
		// Keep the view in drilldown mode (Selected non-empty) even when the
		// requested name normalizes away, so it shows "no entities" not the cloud.
		selected := name
		if selected == "" {
			selected = raw
		}
		render(w, "tags_overview.html", tagsOverviewData{
			Title: "Tags", Selected: selected, Entities: entities,
		})
	}
}

func addTag(tags *store.TagRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		entityType := req.FormValue("entity_type")
		entityID, err := strconv.ParseInt(req.FormValue("entity_id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid entity id", http.StatusBadRequest)
			return
		}
		tag := domain.Tag{EntityType: entityType, EntityID: entityID, Name: req.FormValue("tag")}
		if err := tag.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := tags.Add(tag); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, req, fmt.Sprintf("/%ss/%d", entityType, entityID), http.StatusSeeOther)
	}
}

func removeTag(tags *store.TagRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(req.FormValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := tags.Delete(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		entityType := req.FormValue("entity_type")
		entityID, _ := strconv.ParseInt(req.FormValue("entity_id"), 10, 64)
		http.Redirect(w, req, fmt.Sprintf("/%ss/%d", entityType, entityID), http.StatusSeeOther)
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
