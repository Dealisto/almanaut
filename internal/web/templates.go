package web

import (
	"bytes"
	"embed"
	"html/template"
	"net/http"
)

//go:embed templates/*.html
var templatesFS embed.FS

// pages maps each content page to a template set that combines the shared
// layout with that page's "content" block.
var pages = func() map[string]*template.Template {
	m := map[string]*template.Template{}
	for _, page := range []string{"hosts.html", "host_form.html", "services.html", "service_form.html", "networks.html", "network_form.html", "domains.html", "domain_form.html", "certificates.html", "certificate_form.html", "backups.html", "backup_form.html"} {
		m[page] = template.Must(
			template.ParseFS(templatesFS, "templates/layout.html", "templates/"+page),
		)
	}
	return m
}()

// render executes the shared layout for the given content page with data.
// It renders into a buffer first so a mid-render error never produces a
// half-written response with a 200 status.
func render(w http.ResponseWriter, page string, data any) {
	t, ok := pages[page]
	if !ok {
		http.Error(w, "unknown page: "+page, http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}
