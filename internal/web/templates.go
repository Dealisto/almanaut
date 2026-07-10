package web

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"
)

//go:embed templates/*.html static/app.css
var templatesFS embed.FS

// pages maps each content page to a template set that combines the shared
// layout with that page's "content" block. A stub csrfField is registered so
// templates referencing {{ csrfField }} parse; render rebinds it per request.
var pages = func() map[string]*template.Template {
	m := map[string]*template.Template{}
	for _, page := range []string{"hosts.html", "host_form.html", "services.html", "service_form.html", "networks.html", "network_form.html", "vlans.html", "vlan_form.html", "domains.html", "domain_form.html", "certificates.html", "certificate_form.html", "backups.html", "backup_form.html", "hardware.html", "hardware_form.html", "subscriptions.html", "subscription_form.html", "accounts.html", "account_form.html", "sites.html", "site_form.html", "locations.html", "location_form.html", "racks.html", "rack_form.html", "contacts.html", "contact_form.html", "relationships.html", "impact.html", "checks.html", "health_report.html", "saved_views.html", "detail.html", "tags_overview.html", "search.html", "data.html", "dashboard.html", "discovery.html", "discovery_docker.html", "discovery_network.html", "discovery_proxmox.html", "history.html", "users.html", "password.html", "tokens.html", "reservations.html", "reservation_form.html", "custom_fields.html", "webhooks.html", "webhook_edit.html", "kuma.html", "tasks.html", "discovery_runs.html", "audit.html"} {
		m[page] = template.Must(
			template.New("layout.html").
				Funcs(template.FuncMap{
					"csrfField":   func() template.HTML { return "" },
					"isActive":    func(string) bool { return false },
					"theme":       func() string { return "system" },
					"currentUser": func() string { return "" },
					"canWrite":    func() bool { return false },
					"isAdmin":     func() bool { return false },
					"kumaEnabled": func() bool { return false },
					"savedViews":  func() []savedViewGroup { return nil },
				}).
				ParseFS(templatesFS, "templates/layout.html", "templates/custom_fields_form.html", "templates/webhook_form_fields.html", "templates/list_controls.html", "templates/bulk_actions.html", "templates/"+page),
		)
	}
	// login.html is standalone (no app shell): it defines its own "layout".
	m["login.html"] = template.Must(
		template.New("login.html").
			Funcs(template.FuncMap{
				"csrfField":   func() template.HTML { return "" },
				"isActive":    func(string) bool { return false },
				"theme":       func() string { return "system" },
				"currentUser": func() string { return "" },
			}).
			ParseFS(templatesFS, "templates/login.html"),
	)
	return m
}()

// render executes the shared layout for the given content page with data,
// binding a per-request csrfField that emits the hidden CSRF input.
// It renders into a buffer first so a mid-render error never produces a
// half-written response with a 200 status.
func render(w http.ResponseWriter, r *http.Request, page string, data any) {
	t, ok := pages[page]
	if !ok {
		http.Error(w, "unknown page: "+page, http.StatusInternalServerError)
		return
	}
	clone, err := t.Clone()
	if err != nil {
		serverError(w, r, err)
		return
	}
	token := csrfTokenFrom(r.Context())
	clone.Funcs(template.FuncMap{
		"csrfField": func() template.HTML {
			return template.HTML(`<input type="hidden" name="` + csrfFieldName +
				`" value="` + template.HTMLEscapeString(token) + `">`)
		},
		"isActive": func(base string) bool { return navIsActive(r.URL.Path, base) },
		"theme":    func() string { return themeFromCookie(r) },
		"currentUser": func() string {
			if u, ok := userFrom(r.Context()); ok {
				return u.Username
			}
			return ""
		},
		// When auth is disabled there is no user in context and thus no RBAC:
		// show every control rather than hiding them behind a false default.
		"canWrite": func() bool {
			if _, ok := userFrom(r.Context()); !ok {
				return true
			}
			return effectiveCanWrite(r.Context())
		},
		"isAdmin": func() bool {
			if _, ok := userFrom(r.Context()); !ok {
				return true
			}
			return effectiveIsAdmin(r.Context())
		},
		"kumaEnabled": func() bool { return kumaEnabledFrom(r.Context()) },
		"savedViews":  func() []savedViewGroup { return savedViewsFrom(r.Context()) },
	})
	var buf bytes.Buffer
	if err := clone.ExecuteTemplate(&buf, "layout", data); err != nil {
		serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}
