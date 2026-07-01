package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestMetricsEndpoint(t *testing.T) {
	h, db := newTestServerDockerDB(t, fakeScanner{})
	hosts := store.NewHostRepo(db)
	if _, err := hosts.Create(domain.Host{Name: "up-1", Type: "vm", Status: "running"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := hosts.Create(domain.Host{Name: "down-1", Type: "vm", Status: "down"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	soon := time.Now().Add(5 * 24 * time.Hour).Format("2006-01-02")
	if _, err := store.NewCertificateRepo(db).Create(domain.Certificate{Subject: "x", ExpiresOn: soon}); err != nil {
		t.Fatalf("create cert: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content-type = %q", ct)
	}
	body := rec.Body.String()
	wants := []string{
		"# TYPE almanaut_entities_total gauge",
		`almanaut_entities_total{type="host"} 2`,
		`almanaut_entities_total{type="account"} 0`,
		"# TYPE almanaut_hosts_down_total gauge",
		"almanaut_hosts_down_total 1",
		"almanaut_certificates_expiring_total 1",
		"almanaut_relationships_total 0",
		"almanaut_services_without_backup_total 0",
	}
	for _, w := range wants {
		if !strings.Contains(body, w) {
			t.Errorf("metrics body missing %q\n---\n%s", w, body)
		}
	}
}
