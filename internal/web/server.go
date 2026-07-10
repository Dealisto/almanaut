// Package web wires HTTP routes and renders the server-side UI.
package web

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
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
	Sites         *store.SiteRepo
	Locations     *store.LocationRepo
	Racks         *store.RackRepo
	Contacts      *store.ContactRepo
	Relationships *store.RelationshipRepo
	Tags          *store.TagRepo
	VLANs         *store.VLANRepo
	Reservations  *store.ReservationRepo
	DB            *sql.DB
	Logger        *log.Logger // nil → log.Default()
	Docker        dockerScanner
	NetScan       networkScanner
	NetOpts       NetDiscoveryOptions
	Proxmox       proxmoxScanner
	PVEOpts       ProxmoxOptions
	AuthEnabled   bool   // require login (main.go always sets this true; tests leave it false)
	SecureCookies bool   // force the Secure flag on cookies (TLS-terminating proxy)
	Version       string // build version, surfaced at /version (defaults to "dev")
	// Webhooks receives entity-change events for outbound delivery. Nil defaults
	// to a no-op dispatcher (webhooks disabled).
	Webhooks webhook.Dispatcher
	// Kuma wires the Uptime Kuma sync admin page. Zero value = disabled.
	Kuma KumaOptions
	// Tasks exposes the background job runner to the Scheduled-tasks admin
	// page. Nil => the page and routes are not mounted.
	Tasks jobRunner
	// CertProber runs the "Probe now" TLS check from a certificate's detail
	// page (implemented by *certprobe.Prober).
	CertProber CertProber
	// CertProbes stores/reads each certificate's latest TLS probe result,
	// surfaced on the certificate detail page.
	CertProbes *store.CertProbeRepo
}

// New builds the HTTP handler with all routes wired to the given repos.
func New(cfg Config) http.Handler {
	if cfg.Webhooks == nil {
		cfg.Webhooks = webhook.Noop{}
	}
	hosts, services, networks := cfg.Hosts, cfg.Services, cfg.Networks
	domains, certificates, backups := cfg.Domains, cfg.Certificates, cfg.Backups
	hardware, subscriptions, accounts := cfg.Hardware, cfg.Subscriptions, cfg.Accounts
	sites := cfg.Sites
	locations := cfg.Locations
	racks := cfg.Racks
	contacts := cfg.Contacts
	relationships, tags, db := cfg.Relationships, cfg.Tags, cfg.DB
	vlans := cfg.VLANs
	reservations := cfg.Reservations
	docker, netscan, netOpts := cfg.Docker, cfg.NetScan, cfg.NetOpts
	proxmox, pveOpts := cfg.Proxmox, cfg.PVEOpts
	resources := []mountable{
		resource[domain.Host]{
			name: "hosts", sing: "host", title: "Hosts", heading: "Host",
			repo:  hosts,
			parse: parseHost,
			label: func(h domain.Host) string { return h.Name },
			id:    func(h domain.Host) int64 { return h.ID },
			setID: func(h *domain.Host, id int64) { h.ID = id },
			notes: func(h domain.Host) string { return h.Notes },
			fields: func(h domain.Host) []fieldRow {
				return []fieldRow{
					{"Type", h.Type}, {"OS", h.OS}, {"CPU", h.CPU}, {"RAM", h.RAM},
					{"Disk", h.Disk}, {"Status", h.Status}, {"IPs", strings.Join(h.IPs, ", ")},
					{"Rack", rackLabel(racks, h.RackID)}, {"Rack position (U)", rackPosLabel(h.RackID, h.RackPosition, h.UHeight)},
					{"Liveness", livenessLabel(h.Liveness)},
				}
			},
			search: func(h domain.Host) []string {
				return []string{h.Name, h.OS, h.CPU, h.RAM, h.Disk, h.Status, h.Notes, strings.Join(h.IPs, " ")}
			},
			newItem:  domain.Host{Type: "physical", UHeight: 1},
			listTmpl: "hosts.html", formTmpl: "host_form.html",
			extras: func() map[string]any {
				rackList, _ := racks.List()
				return map[string]any{"Types": domain.HostTypes, "Racks": rackList}
			},
		},
		resource[domain.Service]{
			name: "services", sing: "service", title: "Services", heading: "Service",
			repo:  services,
			parse: parseService,
			label: func(s domain.Service) string { return s.Name },
			id:    func(s domain.Service) int64 { return s.ID },
			setID: func(s *domain.Service, id int64) { s.ID = id },
			notes: func(s domain.Service) string { return s.Notes },
			fields: func(s domain.Service) []fieldRow {
				return []fieldRow{
					{"Kind", s.Kind},
					{"URL", s.URL},
					{"Ports", s.Ports},
					{"Category", s.Category},
					{"Liveness", livenessLabel(s.Liveness)},
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
			setID: func(n *domain.Network, id int64) { n.ID = id },
			notes: func(n domain.Network) string { return n.Notes },
			fields: func(n domain.Network) []fieldRow {
				return []fieldRow{
					{"CIDR", n.CIDR},
					{"VLAN", vlanLabel(vlans, n.VLANID)},
					{"Gateway", n.Gateway},
				}
			},
			search: func(n domain.Network) []string {
				return []string{n.Name, n.CIDR, n.Gateway, n.Notes}
			},
			extras: func() map[string]any {
				list, _ := vlans.List()
				return map[string]any{"VLANs": list}
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
				resList, err := reservations.List()
				if err != nil {
					return nil
				}
				if u, ok := domain.BuildNetworkUsage(n.ID, nets, hostList, resList); ok {
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
			setID: func(d *domain.Domain, id int64) { d.ID = id },
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
			setID: func(c *domain.Certificate, id int64) { c.ID = id },
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
					{"Probe target", c.ProbeTarget},
				}
			},
			search: func(c domain.Certificate) []string {
				return []string{c.Subject, c.Issuer, c.Notes}
			},
			probe:    certProbeSection(cfg.CertProbes),
			newItem:  domain.Certificate{},
			listTmpl: "certificates.html", formTmpl: "certificate_form.html",
		},
		resource[domain.Backup]{
			name: "backups", sing: "backup", title: "Backups", heading: "Backup",
			repo:  backups,
			parse: parseBackup,
			label: func(b domain.Backup) string { return b.Source },
			id:    func(b domain.Backup) int64 { return b.ID },
			setID: func(b *domain.Backup, id int64) { b.ID = id },
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
			setID: func(h *domain.Hardware, id int64) { h.ID = id },
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
					{"Rack", rackLabel(racks, h.RackID)},
					{"Rack position (U)", rackPosLabel(h.RackID, h.RackPosition, h.UHeight)},
				}
			},
			search: func(h domain.Hardware) []string {
				return []string{h.Name, h.Kind, h.Manufacturer, h.Model, h.Serial, h.Location, h.Status, h.Notes}
			},
			extras: func() map[string]any {
				rackList, _ := racks.List()
				return map[string]any{"Racks": rackList}
			},
			newItem:  domain.Hardware{UHeight: 1},
			listTmpl: "hardware.html", formTmpl: "hardware_form.html",
		},
		resource[domain.Subscription]{
			name: "subscriptions", sing: "subscription", title: "Subscriptions", heading: "Subscription",
			repo:  subscriptions,
			parse: parseSubscription,
			label: func(s domain.Subscription) string { return s.Name },
			id:    func(s domain.Subscription) int64 { return s.ID },
			setID: func(s *domain.Subscription, id int64) { s.ID = id },
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
			setID: func(a *domain.Account, id int64) { a.ID = id },
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
		resource[domain.Site]{
			name: "sites", sing: "site", title: "Sites", heading: "Site",
			repo:  sites,
			parse: parseSite,
			label: func(s domain.Site) string { return s.Name },
			id:    func(s domain.Site) int64 { return s.ID },
			setID: func(s *domain.Site, id int64) { s.ID = id },
			notes: func(s domain.Site) string { return s.Notes },
			fields: func(s domain.Site) []fieldRow {
				return []fieldRow{{"Address", s.Address}}
			},
			children: func(s domain.Site, cat entityCatalog) (*childrenSection, error) {
				locs, err := locations.List()
				if err != nil {
					return nil, err
				}
				items := []childRef{}
				for _, l := range locs {
					if l.SiteID == s.ID {
						items = append(items, childRef{Label: l.Name, Path: cat.path("location", l.ID)})
					}
				}
				return &childrenSection{Title: "Locations", Items: items}, nil
			},
			search: func(s domain.Site) []string {
				return []string{s.Name, s.Address, s.Notes}
			},
			newItem:  domain.Site{},
			listTmpl: "sites.html", formTmpl: "site_form.html",
		},
		resource[domain.Location]{
			name: "locations", sing: "location", title: "Locations", heading: "Location",
			repo:  locations,
			parse: parseLocation,
			label: func(l domain.Location) string { return l.Name },
			id:    func(l domain.Location) int64 { return l.ID },
			setID: func(l *domain.Location, id int64) { l.ID = id },
			notes: func(l domain.Location) string { return l.Notes },
			fields: func(l domain.Location) []fieldRow {
				return []fieldRow{{"Site", siteLabel(sites, l.SiteID)}}
			},
			children: func(l domain.Location, cat entityCatalog) (*childrenSection, error) {
				rk, err := racks.List()
				if err != nil {
					return nil, err
				}
				items := []childRef{}
				for _, k := range rk {
					if k.LocationID == l.ID {
						items = append(items, childRef{Label: k.Name, Path: cat.path("rack", k.ID)})
					}
				}
				return &childrenSection{Title: "Racks", Items: items}, nil
			},
			search: func(l domain.Location) []string {
				return []string{l.Name, l.Notes}
			},
			extras: func() map[string]any {
				list, _ := sites.List()
				return map[string]any{"Sites": list}
			},
			newItem:  domain.Location{},
			listTmpl: "locations.html", formTmpl: "location_form.html",
		},
		resource[domain.Rack]{
			name: "racks", sing: "rack", title: "Racks", heading: "Rack",
			repo:  racks,
			parse: parseRack,
			label: func(k domain.Rack) string { return k.Name },
			id:    func(k domain.Rack) int64 { return k.ID },
			setID: func(k *domain.Rack, id int64) { k.ID = id },
			notes: func(k domain.Rack) string { return k.Notes },
			fields: func(k domain.Rack) []fieldRow {
				return []fieldRow{
					{"Location", locationLabel(locations, k.LocationID)},
					{"Height (U)", strconv.Itoa(k.UHeight)},
				}
			},
			search: func(k domain.Rack) []string {
				return []string{k.Name, k.Notes}
			},
			extras: func() map[string]any {
				list, _ := locations.List()
				return map[string]any{"Locations": list}
			},
			elevation: func(k domain.Rack, cat entityCatalog) (*elevationSection, error) {
				hs, err := hosts.List()
				if err != nil {
					return nil, err
				}
				hw, err := hardware.List()
				if err != nil {
					return nil, err
				}
				return buildElevationSection(k, hs, hw, cat), nil
			},
			newItem:  domain.Rack{UHeight: 42},
			listTmpl: "racks.html", formTmpl: "rack_form.html",
		},
		resource[domain.Contact]{
			name: "contacts", sing: "contact", title: "Contacts", heading: "Contact",
			repo:  contacts,
			parse: parseContact,
			label: func(c domain.Contact) string { return c.Name },
			id:    func(c domain.Contact) int64 { return c.ID },
			setID: func(c *domain.Contact, id int64) { c.ID = id },
			notes: func(c domain.Contact) string { return c.Notes },
			fields: func(c domain.Contact) []fieldRow {
				return []fieldRow{
					{"Email", c.Email},
					{"Phone", c.Phone},
					{"Role", c.Role},
					{"Organization", c.Organization},
				}
			},
			search: func(c domain.Contact) []string {
				return []string{c.Name, c.Email, c.Phone, c.Role, c.Organization, c.Notes}
			},
			newItem:  domain.Contact{},
			listTmpl: "contacts.html", formTmpl: "contact_form.html",
		},
		resource[domain.VLAN]{
			name: "vlans", sing: "vlan", title: "VLANs", heading: "VLAN",
			repo:  vlans,
			parse: parseVLAN,
			label: func(v domain.VLAN) string { return v.Name },
			id:    func(v domain.VLAN) int64 { return v.ID },
			setID: func(v *domain.VLAN, id int64) { v.ID = id },
			notes: func(v domain.VLAN) string { return v.Notes },
			fields: func(v domain.VLAN) []fieldRow {
				return []fieldRow{{"VLAN ID", strconv.Itoa(v.VID)}}
			},
			search: func(v domain.VLAN) []string {
				return []string{v.Name, strconv.Itoa(v.VID), v.Notes}
			},
			newItem:  domain.VLAN{},
			listTmpl: "vlans.html", formTmpl: "vlan_form.html",
		},
		resource[domain.Reservation]{
			name: "reservations", sing: "reservation", title: "Reservations", heading: "Reservation",
			repo:  reservations,
			parse: parseReservation,
			label: func(v domain.Reservation) string { return v.Name },
			id:    func(v domain.Reservation) int64 { return v.ID },
			setID: func(v *domain.Reservation, id int64) { v.ID = id },
			notes: func(v domain.Reservation) string { return v.Notes },
			fields: func(v domain.Reservation) []fieldRow {
				return []fieldRow{
					{"Network", reservationNetworkLabel(networks, v.NetworkID)},
					{"Start IP", v.StartIP},
					{"End IP", v.EndIP},
				}
			},
			search: func(v domain.Reservation) []string {
				return []string{v.Name, v.StartIP, v.EndIP, v.Notes}
			},
			extras: func() map[string]any {
				list, _ := networks.List()
				return map[string]any{"Networks": list}
			},
			newItem:  domain.Reservation{},
			listTmpl: "reservations.html", formTmpl: "reservation_form.html",
		},
	}
	changelog := store.NewChangelogRepo(db)
	journal := store.NewJournalRepo(db)
	users := store.NewUserRepo(db)
	sessions := store.NewSessionRepo(db)
	tokens := store.NewTokenRepo(db)
	customFields := store.NewCustomFieldRepo(db)
	attachments := store.NewAttachmentRepo(db)
	webhooks := store.NewWebhookRepo(db)
	cat := entityCatalog{resources: resources}
	deps := handlerDeps{cat: cat, tags: tags, rels: relationships, changelog: changelog, journal: journal, customFields: customFields, attachments: attachments, db: db, webhooks: cfg.Webhooks}
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
	if cfg.Kuma.Enabled {
		r.Use(markKumaEnabled)
	}

	// Unauthenticated operational endpoints, registered before the auth/CSRF
	// group so a container HEALTHCHECK or probe can reach them without creds.
	r.Get("/healthz", healthz(db))
	r.Get("/version", versionInfo(cfg.Version))
	// Static CSS is public so the unauthenticated login page can style itself.
	r.Get("/static/app.css", staticCSS(cfg.Version))

	// Login routes: CSRF-protected but not session-gated (you cannot have a
	// session yet). Only present when auth is enabled.
	if cfg.AuthEnabled {
		throttle := newLoginThrottle()
		r.Group(func(r chi.Router) {
			r.Use(limitBody)
			r.Use(csrfProtect(cfg.SecureCookies))
			r.Get("/login", loginForm)
			r.Post("/login", login(users, sessions, throttle, cfg.SecureCookies))
		})
	}

	repos := entityRepos{
		hosts: hosts, services: services, networks: networks,
		domains: domains, certificates: certificates, backups: backups,
		hardware: hardware, subscriptions: subscriptions, accounts: accounts,
		vlans:        vlans,
		reservations: reservations,
		contacts:     contacts,
		sites:        sites, locations: locations, racks: racks,
	}
	// Everything else is the application UI: optionally behind session auth and
	// always behind CSRF. Grouping scopes those middlewares to these routes
	// while inheriting the request-id/logging/recovery stack above.
	r.Group(func(r chi.Router) {
		if cfg.AuthEnabled {
			r.Use(sessionAuth(sessions))
		}
		// Bound the request body before csrfProtect reads the form of every
		// unsafe request, so an oversize upload is rejected up front.
		r.Use(limitBody)
		r.Use(csrfProtect(cfg.SecureCookies))

		// Self-service: available to every authenticated user regardless of role.
		if cfg.AuthEnabled {
			r.Post("/logout", logout(sessions, cfg.SecureCookies))
			r.Get("/account/password", changePasswordForm)
			r.Post("/account/password", changePassword(users))
			r.Get("/account/tokens", listTokens(tokens))
			r.Post("/account/tokens", createToken(tokens))
			r.Post("/account/tokens/{id}/delete", deleteToken(tokens))
		}
		r.Post("/theme", setTheme(cfg.SecureCookies)) // UI preference, any role

		// Admin-only: user management. requireAdmin only meaningful with auth on.
		if cfg.AuthEnabled {
			r.Group(func(r chi.Router) {
				r.Use(requireAdmin)
				r.Get("/users", listUsers(users))
				r.Post("/users", createUser(users))
				r.Post("/users/{id}/delete", deleteUser(users, db))
				r.Post("/users/{id}/password", resetUserPassword(users))
				r.Post("/users/{id}/role", updateUserRole(users))

				r.Get("/webhooks", listWebhooks(webhooks))
				r.Post("/webhooks", createWebhook(webhooks))
				r.Get("/webhooks/{id}/edit", editWebhookForm(webhooks))
				r.Post("/webhooks/{id}", updateWebhook(webhooks))
				r.Post("/webhooks/{id}/toggle", toggleWebhook(webhooks))
				r.Post("/webhooks/{id}/delete", deleteWebhook(webhooks))

				if cfg.Kuma.Enabled {
					r.Get("/kuma", kumaPage(cfg.Kuma))
					r.Post("/kuma/sync", kumaSyncNow(cfg.Kuma))
				}

				if cfg.Tasks != nil {
					r.Get("/tasks", tasksPage(cfg.Tasks))
					r.Post("/tasks/{name}/run", tasksRun(cfg.Tasks))
				}
			})
		}

		// Everything else: readable by all, mutations gated to writers. GET passes
		// requireWrite untouched; POST/PUT/DELETE require an effective writer.
		r.Group(func(r chi.Router) {
			if cfg.AuthEnabled {
				r.Use(requireWrite)
			}
			r.Get("/", dashboard(repos, relationships, cat, changelog))
			for _, rs := range resources {
				rs.mount(r, deps)
			}
			r.Post("/tags", addTag(tags, cat))
			r.Post("/tags/delete", removeTag(tags, cat))
			r.Get("/tags", tagsOverview(tags, cat))

			r.Get("/custom-fields", customFieldsPage(customFields))
			r.Post("/custom-fields", createCustomField(customFields))
			r.Post("/custom-fields/{id}", updateCustomFieldLabel(customFields))
			r.Post("/custom-fields/{id}/delete", deleteCustomField(customFields))

			r.Get("/relationships", listRelationships(relationships, cat))
			r.Post("/relationships", createRelationship(relationships, cat))
			r.Post("/relationships/{id}/delete", deleteRelationship(relationships))
			r.Post("/journal/{id}/delete", deleteJournal(cat, deps))
			r.Get("/attachments/{id}", downloadAttachment(deps))
			r.Post("/attachments/{id}/delete", deleteAttachment(cat, deps))
			r.Get("/impact", impactView(relationships, cat))
			r.Get("/history", history(cat, changelog))
			r.Get("/checks", healthChecks(services, certificates, hardware, subscriptions, relationships))
			r.Get("/search", searchEntities(cat, tags, customFields))
			r.Get("/data", showData(cat))
			r.Get("/export", exportData(db))
			r.Post("/import", importData(db))
			r.Post("/import-csv", importCSV(cat, deps))
			r.Get("/discovery", discoveryLanding(netOpts, pveOpts))
			r.Get("/discovery/docker", scanDocker(docker, services, hosts))
			r.Post("/discovery/docker/import", importDocker(docker, services, relationships, db))
			r.Get("/discovery/network", networkForm(netOpts))
			r.Post("/discovery/network/scan", scanNetwork(netscan, hosts, netOpts))
			r.Post("/discovery/network/import", importNetwork(hosts, netOpts, db))
			r.Get("/discovery/proxmox", scanProxmox(proxmox, hosts, pveOpts))
			r.Post("/discovery/proxmox/import", importProxmox(proxmox, hosts, relationships, pveOpts, db))
			r.Post("/certificates/{id}/probe", probeCertificate(cat, cfg.CertProber, certificates))
		})
	})

	// JSON API: authenticated by a bearer API token, or a session cookie for
	// reads (see apiAuth). Deliberately outside the CSRF group — writes are
	// bearer-only, which a browser never sends automatically, so there is no CSRF
	// vector. limitBody still bounds the request body.
	r.Group(func(r chi.Router) {
		r.Use(limitBody)
		if cfg.AuthEnabled {
			r.Use(apiAuth(tokens, sessions))
			r.Use(requireWrite)
		}
		for _, rs := range resources {
			rs.mountAPI(r, deps)
		}
		r.Get("/api/search", apiSearch(cat, customFields))
		r.Get("/api/relationships", apiRelationships(relationships))
		// /metrics is a GET: reachable with a bearer API token (Prometheus) or a
		// session cookie (logged-in browser). Not under CSRF; no creds → 401.
		r.Get("/metrics", metricsHandler(repos, relationships))
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

// siteLabel resolves a Location's soft site_id to a display name.
func siteLabel(sites *store.SiteRepo, id int64) string {
	if id == 0 {
		return "—"
	}
	s, err := sites.Get(id)
	if err != nil {
		return "(unknown)"
	}
	return s.Name
}

// locationLabel resolves a Rack's soft location_id to a display name.
func locationLabel(locations *store.LocationRepo, id int64) string {
	if id == 0 {
		return "—"
	}
	l, err := locations.Get(id)
	if err != nil {
		return "(unknown)"
	}
	return l.Name
}

// vlanLabel resolves a Network's soft vlan_id to a display name.
func vlanLabel(vlans *store.VLANRepo, id int64) string {
	if id == 0 {
		return "—"
	}
	v, err := vlans.Get(id)
	if err != nil {
		return "(unknown)"
	}
	return fmt.Sprintf("VLAN %d (%s)", v.VID, v.Name)
}

// reservationNetworkLabel resolves a Reservation's soft network_id to a name.
func reservationNetworkLabel(networks *store.NetworkRepo, id int64) string {
	if id == 0 {
		return "—"
	}
	n, err := networks.Get(id)
	if err != nil {
		return "(unknown)"
	}
	return n.Name
}

// rackLabel resolves an occupant's soft rack_id to a display name.
func rackLabel(racks *store.RackRepo, id int64) string {
	if id == 0 {
		return "—"
	}
	k, err := racks.Get(id)
	if err != nil {
		return "(unknown)"
	}
	return k.Name
}

// rackPosLabel formats an occupant's U position + height for the detail page.
func rackPosLabel(rackID int64, position, uHeight int) string {
	if rackID == 0 {
		return "—"
	}
	return fmt.Sprintf("%d (%dU)", position, uHeight)
}

// livenessLabel renders a LivenessStatus for the detail page's plain-text row.
func livenessLabel(s *domain.LivenessStatus) string {
	if s == nil {
		return "—"
	}
	if s.Status == domain.LivenessDown {
		if s.LastError != "" {
			return "down · " + s.LastError
		}
		return "down"
	}
	return "up · checked " + s.CheckedAt.Format("2006-01-02 15:04")
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
