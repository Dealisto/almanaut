package web

import (
	"net/http"
)

// staticCSS serves the embedded stylesheet. The ETag is the build version, so
// a new release invalidates caches; Cache-Control: no-cache makes browsers
// revalidate cheaply (304 when unchanged).
func staticCSS(version string) http.HandlerFunc {
	body, err := templatesFS.ReadFile("static/app.css")
	if err != nil {
		panic("embed: static/app.css missing: " + err.Error())
	}
	etag := `"` + version + `"`
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(body)
	}
}
