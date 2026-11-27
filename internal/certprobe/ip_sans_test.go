package certprobe

import (
	"context"
	"crypto/x509"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestProbeOneRecordsAndMatchesIPSubjectAlternativeNames(t *testing.T) {
	for _, subject := range []string{"192.0.2.10", "2001:db8:0:0:0:0:0:10"} {
		t.Run(subject, func(t *testing.T) {
			db := testDB(t)
			certs, state := store.NewCertificateRepo(db), store.NewCertProbeRepo(db)
			id, err := certs.Create(domain.Certificate{Subject: subject, ProbeTarget: net.JoinHostPort(subject, "443")})
			if err != nil {
				t.Fatal(err)
			}
			served := leaf(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), "Private CA", 42, "admin.example.com")
			served.IPAddresses = []net.IP{net.ParseIP(subject)}
			p := New(certs, state, db, func(context.Context, string) (*x509.Certificate, error) { return served, nil }, time.Second, nil, nil)
			cert, err := certs.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if err := p.ProbeOne(context.Background(), cert); err != nil {
				t.Fatal(err)
			}
			got, err := state.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Mismatches) != 0 {
				t.Errorf("valid IP SAN reported a mismatch: %v", got.Mismatches)
			}
			if !slices.Equal(got.SANs, []string{"admin.example.com", net.ParseIP(subject).String()}) {
				t.Errorf("IP SAN omitted from stored diagnostics: %v", got.SANs)
			}
		})
	}
}
