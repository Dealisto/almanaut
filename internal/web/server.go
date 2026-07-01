// Package web wires HTTP routes and renders the server-side UI.
package web

import (
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Config bundles everything New needs to build the HTTP handler. Using a struct
// instead of positional parameters prevents transposing same-typed repos.
type Config struct {
	Hosts         *store.HostRepo
	Services      *store.ServiceRepo
	Networks      *store.NetworkRepo
	Domains       *store.DomainRepo
	Certificates  *store.CertificateRepo
	Backups       *store.BackupRepo
	Hardware      *store.HardwareRepo
	Subscriptions *store.SubscriptionRepo
	Accounts      *store.AccountRepo
	Relationships *store.RelationshipRepo
	Tags          *store.TagRepo
	DB            *sql.DB
	Logger        *log.Logger // nil → log.Default()
	Docker        dockerScanner
	NetScan       networkScanner
	NetOpts       NetDiscoveryOptions
	Proxmox       proxmoxScanner
	PVEOpts       ProxmoxOptions
	AuthUser      string // when set with AuthPass, enables HTTP basic auth
	AuthPass      string
	SecureCookies bool   // force the Secure flag on cookies (TLS-terminating proxy)
	Version       string // build version, surfaced at /version (defaults to "dev")
}

// New builds the HTTP handler with all routes wired to the given repos.
func New(cfg Config) http.Handler {
	hosts, services, networks := cfg.Hosts, cfg.Services, cfg.Networks
	domains, certificates, backups := cfg.Domains, cfg.Certificates, cfg.Backups
	hardware, subscriptions, accounts := cfg.Hardware, cfg.Subscriptions, cfg.Accounts
	relationships, tags, db := cfg.Relationships, cfg.Tags, cfg.DB
	docker, netscan, netOpts := cfg.Docker, cfg.NetScan, cfg.NetOpts
	proxmox, pveOpts := cfg.Proxmox, cfg.PVEOpts
	resources := []mountable{
		resource[domain.Host]{
			name: "hosts", sing: "host", title: "Hosts", heading: "Host",
			repo:  hosts,
			parse: parseHost,
			label: func(h domain.Host) string { return h.Name },
			id:    func(h domain.Host) int64 { return h.ID },
			notes: func(h domain.Host) string { return h.Notes },
			fields: func(h domain.Host) []fieldRow {
				return []fieldRow{
					{"Type", h.Type}, {"OS", h.OS}, {"CPU", h.CPU}, {"RAM", h.RAM},
					{"Disk", h.Disk}, {"Status", h.Status}, {"IPs", strings.Join(h.IPs, ", ")},
				}
			},
			search: func(h domain.Host) []string {
				return []string{h.Name, h.OS, h.CPU, h.RAM, h.Disk, h.Status, h.Notes, strings.Join(h.IPs, " ")}
			},
			newItem:  domain.Host{Type: "physical"},
			listTmpl: "hosts.html", formTmpl: "host_form.html",
			extras: func() map[string]any { return map[string]any{"Types": domain.HostTypes} },
		},
		resource[domain.Service]{
			name: "services", sing: "service", title: "Services", heading: "Service",
			repo:  services,
			parse: parseService,
			label: func(s domain.Service) string { return s.Name },
			id:    func(s domain.Service) int64 { return s.ID },
			notes: func(s domain.Service) string { return s.Notes },
			fields: func(s domain.Service) []fieldRow {
				return []fieldRow{
					{"Kind", s.Kind},
					{"URL", s.URL},
					{"Ports", s.Ports},
					{"Category", s.Category},
				}
			},
			search: func(s domain.Service) []string {
				return []string{s.Name, s.Kind, s.URL, s.Ports, s.Category, s.Notes}
			},
			newItem:  domain.Service{Kind: "container"},
			listTmpl: "services.html", formTmpl: "service_form.html",
			extras: func() map[string]any { return map[string]any{"Kinds": domain.ServiceKinds} },
		},
		resource[domain.Network]{
			name: "networks", sing: "network", title: "Networks", heading: "Network",
			repo:  networks,
			parse: parseNetwork,
			label: func(n domain.Network) string { return n.Name },
			id:    func(n domain.Network) int64 { return n.ID },
			notes: func(n domain.Network) string { return n.Notes },
			fields: func(n domain.Network) []fieldRow {
				return []fieldRow{
					{"CIDR", n.CIDR},
					{"VLAN", n.VLAN},
					{"Gateway", n.Gateway},
				}
			},
			search: func(n domain.Network) []string {
				return []string{n.Name, n.CIDR, n.VLAN, n.Gateway, n.Notes}
			},
			newItem:  domain.Network{},
			listTmpl: "networks.html", formTmpl: "network_form.html",
			ipam: func(n domain.Network) *ipamSection {
				nets, err := networks.List()
				if err != nil {
					return nil
				}
				hostList, err := hosts.List()
				if err != nil {
					return nil
				}
				if u, ok := domain.BuildNetworkUsage(n.ID, nets, hostList); ok {
					s := buildIPAMSection(u)
					return &s
				}
				return nil
			},
		},
		resource[domain.Domain]{
			name: "domains", sing: "domain", title: "Domains", heading: "Domain",
			repo:  domains,
			parse: parseDomain,
			label: func(d domain.Domain) string { return d.FQDN },
			id:    func(d domain.Domain) int64 { return d.ID },
			notes: func(d domain.Domain) string { return d.Notes },
			fields: func(d domain.Domain) []fieldRow {
				return []fieldRow{
					{"Provider", d.Provider},
				}
			},
			search: func(d domain.Domain) []string {
				return []string{d.FQDN, d.Provider, d.Notes}
			},
			newItem:  domain.Domain{},
			listTmpl: "domains.html", formTmpl: "domain_form.html",
		},
		resource[domain.Certificate]{
			name: "certificates", sing: "certificate", title: "Certificates", heading: "Certificate",
			repo:  certificates,
			parse: parseCertificate,
			label: func(c domain.Certificate) string { return c.Subject },
			id:    func(c domain.Certificate) int64 { return c.ID },
			notes: func(c domain.Certificate) string { return c.Notes },
			fields: func(c domain.Certificate) []fieldRow {
				autoRenew := "no"
				if c.AutoRenew {
					autoRenew = "yes"
				}
				return []fieldRow{
					{"Issuer", c.Issuer},
					{"Expires on", c.ExpiresOn},
					{"Auto-renew", autoRenew},
				}
			},
			search: func(c domain.Certificate) []string {
				return []string{c.Subject, c.Issuer, c.Notes}
			},
			newItem:  domain.Certificate{},
			listTmpl: "certificates.html", formTmpl: "certificate_form.html",
		},
		resource[domain.Backup]{
			name: "backups", sing: "backup", title: "Backups", heading: "Backup",
			repo:  backups,
			parse: parseBackup,
			label: func(b domain.Backup) string { return b.Source },
			id:    func(b domain.Backup) int64 { return b.ID },
			notes: func(b domain.Backup) string { return b.Notes },
			fields: func(b domain.Backup) []fieldRow {
				return []fieldRow{
					{"Destination", b.Destination},
					{"Frequency", b.Frequency},
					{"Last run", b.LastRun},
				}
			},
			search: func(b domain.Backup) []string {
				return []string{b.Source, b.Destination, b.Frequency, b.LastRun, b.Notes}
			},
			newItem:  domain.Backup{},
			listTmpl: "backups.html", formTmpl: "backup_form.html",
		},
		resource[domain.Hardware]{
			name: "hardware", sing: "hardware", title: "Hardware", heading: "Hardware",
			repo:  hardware,
			parse: parseHardware,
			label: func(h domain.Hardware) string { return h.Name },
			id:    func(h domain.Hardware) int64 { return h.ID },
			notes: func(h domain.Hardware) string { return h.Notes },
			fields: func(h domain.Hardware) []fieldRow {
				return []fieldRow{
					{"Kind", h.Kind},
					{"Manufacturer", h.Manufacturer},
					{"Model", h.Model},
					{"Serial", h.Serial},
					{"Location", h.Location},
					{"Purchase date", h.PurchaseDate},
					{"Warranty end", h.WarrantyEnd},
					{"Status", h.Status},
				}
			},
			search: func(h domain.Hardware) []string {
				return []string{h.Name, h.Kind, h.Manufacturer, h.Model, h.Serial, h.Location, h.Status, h.Notes}
			},
			newItem:  domain.Hardware{},
			listTmpl: "hardware.html", formTmpl: "hardware_form.html",
		},
		resource[domain.Subscription]{
			name: "subscriptions", sing: "subscription", title: "Subscriptions", heading: "Subscription",
			repo:  subscriptions,
			parse: parseSubscription,
			label: func(s domain.Subscription) string { return s.Name },
			id:    func(s domain.Subscription) int64 { return s.ID },
			notes: func(s domain.Subscription) string { return s.Notes },
			fields: func(s domain.Subscription) []fieldRow {
				price := s.Amount
				if s.Amount != "" && s.Currency != "" {
					price = s.Amount + " " + s.Currency
				}
				autoRenew := "no"
				if s.AutoRenew {
					autoRenew = "yes"
				}
				return []fieldRow{
					{"Kind", s.Kind},
					{"Provider", s.Provider},
					{"Amount", price},
					{"Billing cycle", s.BillingCycle},
					{"Renewal date", s.RenewalDate},
					{"Auto-renew", autoRenew},
					{"Status", s.Status},
				}
			},
			search: func(s domain.Subscription) []string {
				return []string{s.Name, s.Kind, s.Provider, s.Currency, s.BillingCycle, s.Status, s.Notes}
			},
			newItem:  domain.Subscription{},
			listTmpl: "subscriptions.html", formTmpl: "subscription_form.html",
		},
		resource[domain.Account]{
			name: "accounts", sing: "account", title: "Accounts", heading: "Account",
			repo:  accounts,
			parse: parseAccount,
			label: func(a domain.Account) string { return a.Name },
			id:    func(a domain.Account) int64 { return a.ID },
			notes: func(a domain.Account) string { return a.Notes },
			fields: func(a domain.Account) []fieldRow {
				return []fieldRow{
					{"Kind", a.Kind},
					{"Username", a.Username},
					{"Password manager", a.PasswordManager},
					{"Secret ref", a.SecretRef},
					{"URL", a.URL},
					{"Status", a.Status},
				}
			},
			search: func(a domain.Account) []string {
				return []string{a.Name, a.Kind, a.Username, a.PasswordManager, a.SecretRef, a.URL, a.Status, a.Notes}
			},
			newItem:  domain.Account{},
			listTmpl: "accounts.html", formTmpl: "account_form.html",
		},
	}
	cat := entityCatalog{resources: resources}
	deps := handlerDeps{cat: cat, tags: tags, rels: relationships, db: db}
	r := chi.NewRouter()
	logger := cfg.Logger
	if logger == nil {
		logger = log.Default()
	}
	r.Use(middleware.RequestID)
	r.Use(requestLogger(logger))
	r.Use(injectLogger(logger))
	r.Use(recoverer(logger))
	r.Use(securityHeaders)

	// Unauthenticated operational endpoints, registered before the auth/CSRF
	// group so a container HEALTHCHECK or probe can reach them without creds.
	r.Get("/healthz", healthz(db))
	r.Get("/version", versionInfo(cfg.Version))

	repos := entityRepos{
		hosts: hosts, services: services, networks: networks,
		domains: domains, certificates: certificates, backups: backups,
		hardware: hardware, subscriptions: subscriptions, accounts: accounts,
	}
	// Everything else is the application UI: optionally behind basic auth and
	// always behind CSRF. Grouping scopes those middlewares to these routes
	// while inheriting the request-id/logging/recovery stack above.
	r.Group(func(r chi.Router) {
		if cfg.AuthUser != "" && cfg.AuthPass != "" {
			r.Use(basicAuth(cfg.AuthUser, cfg.AuthPass))
		}
		// Bound the request body before csrfProtect reads the form of every
		// unsafe request, so an oversize upload is rejected up front.
		r.Use(limitBody)
		r.Use(csrfProtect(cfg.SecureCookies))
		r.Get("/", dashboard(repos, relationships))
		for _, rs := range resources {
			rs.mount(r, deps)
			rs.mountAPI(r)
		}
		r.Post("/tags", addTag(tags, cat))
		r.Post("/tags/delete", removeTag(tags, cat))
		r.Get("/tags", tagsOverview(tags, cat))

		r.Get("/relationships", listRelationships(relationships, cat))
		r.Post("/relationships", createRelationship(relationships, cat))
		r.Post("/relationships/{id}/delete", deleteRelationship(relationships))
		r.Get("/impact", impactView(relationships, cat))
		r.Get("/checks", healthChecks(services, certificates, hardware, subscriptions, relationships))
		r.Get("/search", searchEntities(cat, tags))
		r.Get("/api/search", apiSearch(cat))
		r.Get("/api/relationships", apiRelationships(relationships))
		r.Get("/data", showData())
		r.Get("/export", exportData(db))
		r.Post("/import", importData(db))
		r.Get("/discovery", discoveryLanding(netOpts, pveOpts))
		r.Get("/discovery/docker", scanDocker(docker, services, hosts))
		r.Post("/discovery/docker/import", importDocker(docker, services, relationships, db))
		r.Get("/discovery/network", networkForm(netOpts))
		r.Post("/discovery/network/scan", scanNetwork(netscan, hosts, netOpts))
		r.Post("/discovery/network/import", importNetwork(hosts, netOpts, db))
		r.Get("/discovery/proxmox", scanProxmox(proxmox, hosts, pveOpts))
		r.Post("/discovery/proxmox/import", importProxmox(proxmox, hosts, relationships, pveOpts, db))
	})
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
			serverError(w, req, err)
			return
		}
		data := impactPageData{Title: "Impact", Options: opts}

		ref := req.URL.Query().Get("ref")
		if ref != "" {
			typ, id, perr := parseRef(ref)
			if perr != nil {
				data.Error = perr.Error()
				render(w, req, "impact.html", data)
				return
			}
			labels := labelMap(opts)
			data.Selected = ref
			data.SelectedLabel = labelOrFallback(labels, typ, id)
			refs, err := store.Impact(rels, typ, id)
			if err != nil {
				serverError(w, req, err)
				return
			}
			data.Computed = true
			for _, r := range refs {
				data.Impacted = append(data.Impacted, labelOrFallback(labels, r.Type, r.ID))
			}
		}
		render(w, req, "impact.html", data)
	}
}

type checksPageData struct {
	Title              string
	WithinDays         int
	UnbackedServices   []domain.Service
	ExpiringCerts      []domain.Certificate
	ExpiringWarranties []domain.Hardware
	RenewalsDue        []domain.Subscription
}

func healthChecks(services *store.ServiceRepo, certs *store.CertificateRepo, hardware *store.HardwareRepo, subscriptions *store.SubscriptionRepo, rels *store.RelationshipRepo) http.HandlerFunc {
	const withinDays = 30
	return func(w http.ResponseWriter, req *http.Request) {
		svcList, err := services.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		certList, err := certs.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		hwList, err := hardware.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		subList, err := subscriptions.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		relList, err := rels.List()
		if err != nil {
			serverError(w, req, err)
			return
		}
		render(w, req, "checks.html", checksPageData{
			Title:              "Checks",
			WithinDays:         withinDays,
			UnbackedServices:   domain.ServicesWithoutBackup(svcList, relList),
			ExpiringCerts:      domain.ExpiringSoon(certList, time.Now(), withinDays),
			ExpiringWarranties: domain.WarrantyExpiring(hwList, time.Now(), withinDays),
			RenewalsDue:        domain.RenewalsDue(subList, time.Now(), withinDays),
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

func renderRelationships(w http.ResponseWriter, r *http.Request, rels *store.RelationshipRepo, cat entityCatalog, errMsg string) {
	opts, err := cat.options()
	if err != nil {
		serverError(w, r, err)
		return
	}
	labels := labelMap(opts)
	all, err := rels.List()
	if err != nil {
		serverError(w, r, err)
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
	render(w, r, "relationships.html", relationshipsPageData{
		Title: "Relationships", Relationships: views, Options: opts,
		Kinds: domain.RelationshipKinds, Error: errMsg,
	})
}

func listRelationships(rels *store.RelationshipRepo, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		renderRelationships(w, req, rels, cat, "")
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
			renderRelationships(w, req, rels, cat, errFrom.Error())
			return
		}
		if errTo != nil {
			renderRelationships(w, req, rels, cat, errTo.Error())
			return
		}
		if err := rel.Validate(); err != nil {
			renderRelationships(w, req, rels, cat, err.Error())
			return
		}
		if _, err := rels.Create(rel); err != nil {
			serverError(w, req, err)
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
			serverError(w, req, err)
			return
		}
		http.Redirect(w, req, "/relationships", http.StatusSeeOther)
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
				serverError(w, req, err)
				return
			}
			render(w, req, "tags_overview.html", tagsOverviewData{Title: "Tags", Counts: counts})
			return
		}

		name := domain.NormalizeTag(raw)
		tagged, err := tags.ListByName(name)
		if err != nil {
			serverError(w, req, err)
			return
		}
		opts, err := cat.options()
		if err != nil {
			serverError(w, req, err)
			return
		}
		labels := labelMap(opts)
		entities := make([]tagEntity, 0, len(tagged))
		for _, tg := range tagged {
			entities = append(entities, tagEntity{
				Label: labelOrFallback(labels, tg.EntityType, tg.EntityID),
				URL:   cat.path(tg.EntityType, tg.EntityID),
			})
		}
		// Keep the view in drilldown mode (Selected non-empty) even when the
		// requested name normalizes away, so it shows "no entities" not the cloud.
		selected := name
		if selected == "" {
			selected = raw
		}
		render(w, req, "tags_overview.html", tagsOverviewData{
			Title: "Tags", Selected: selected, Entities: entities,
		})
	}
}

func addTag(tags *store.TagRepo, cat entityCatalog) http.HandlerFunc {
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
			serverError(w, req, err)
			return
		}
		http.Redirect(w, req, cat.path(entityType, entityID), http.StatusSeeOther)
	}
}

func removeTag(tags *store.TagRepo, cat entityCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		id, err := strconv.ParseInt(req.FormValue("id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := tags.Delete(id); err != nil {
			serverError(w, req, err)
			return
		}
		entityType := req.FormValue("entity_type")
		entityID, err := strconv.ParseInt(req.FormValue("entity_id"), 10, 64)
		if err != nil {
			// The tag is gone; with no valid entity to return to, fall back to the
			// tag overview rather than redirecting to a bogus "/type/0".
			http.Redirect(w, req, "/tags", http.StatusSeeOther)
			return
		}
		http.Redirect(w, req, cat.path(entityType, entityID), http.StatusSeeOther)
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
