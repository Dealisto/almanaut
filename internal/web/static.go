package web

import (
	"crypto/sha256"
	"fmt"
	"net/http"
)

// staticCSS serves the embedded stylesheet, revalidating cached copies.
func staticCSS(version string) http.HandlerFunc {
	return staticAsset("static/app.css", "text/css; charset=utf-8", version)
}

func staticAsset(path, contentType, version string) http.HandlerFunc {
	body, err := templatesFS.ReadFile(path)
	if err != nil {
		panic("embed: " + path + " missing: " + err.Error())
	}
	// Include the asset content so rebuilding the same development version
	// cannot leave browsers using an earlier stylesheet or enhancement script.
	digest := sha256.Sum256(body)
	etag := fmt.Sprintf(`"%s-%x"`, version, digest[:12])
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		_, _ = w.Write(body)
	}
}
