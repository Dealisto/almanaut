package web

import (
	"bytes"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestPathsCannotInjectLogLines(t *testing.T) {
	for _, mode := range []string{"request", "server error", "API error"} {
		t.Run(mode, func(t *testing.T) {
			var output bytes.Buffer
			logger := log.New(&output, "", 0)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "server error":
					serverError(w, r, errors.New("database unavailable"))
				case "API error":
					apiServerError(w, r, errors.New("database unavailable"))
				default:
					w.WriteHeader(http.StatusNoContent)
				}
			})
			var h http.Handler = injectLogger(logger)(handler)
			if mode == "request" {
				h = requestLogger(logger)(h)
			}
			path := "/hosts/%0Aforged%20entry%0D%1B%5B2J"
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path+"?token=secret", nil))
			line := output.String()
			if strings.Count(line, "\n") != 1 || strings.ContainsAny(line, "\r\x1b") {
				t.Fatalf("untrusted path introduced log controls: %q", line)
			}
			if !strings.Contains(line, path) {
				t.Errorf("log lost escaped request path: %q", line)
			}
			if strings.Contains(line, "secret") {
				t.Errorf("log exposed query string: %q", line)
			}
		})
	}
}
