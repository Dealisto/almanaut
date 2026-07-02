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
	for _, page := range []string{"hosts.html", "host_form.html", "services.html", "service_form.html", "networks.html", "network_form.html", "domains.html", "domain_form.html", "certificates.html", "certificate_form.html", "backups.html", "backup_form.html", "hardware.html", "hardware_form.html", "subscriptions.html", "subscription_form.html", "accounts.html", "account_form.html", "relationships.html", "impact.html", "checks.html", "detail.html", "tags_overview.html", "search.html", "data.html", "dashboard.html", "discovery.html", "discovery_docker.html", "discovery_network.html", "discovery_proxmox.html"} {
		m[page] = template.Must(
			template.New("layout.html").
				Funcs(template.FuncMap{
					"csrfField": func() template.HTML { return "" },
					"isActive":  func(string) bool { return false },
					"theme":     func() string { return "system" },
				}).
				ParseFS(templatesFS, "templates/layout.html", "templates/"+page),
		)
	}
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
	})
	var buf bytes.Buffer
	if err := clone.ExecuteTemplate(&buf, "layout", data); err != nil {
		serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}
