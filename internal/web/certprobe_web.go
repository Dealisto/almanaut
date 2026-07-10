package web

import (
	"context"
	"net/http"
	"strconv"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/go-chi/chi/v5"
)

// CertProber runs a single-certificate probe (implemented by *certprobe.Prober).
type CertProber interface {
	ProbeOne(ctx context.Context, cert domain.Certificate) error
}

// probeSection is the certificate detail-page probe block.
type probeSection struct {
	Target string
	Status *domain.CertProbeStatus // nil => never probed
}

// certProbeSection builds the hook closure for the certificate resource.
// probes may be nil (e.g. in tests that don't wire cert probing); the section
// then always reports "never probed" instead of panicking.
func certProbeSection(probes *store.CertProbeRepo) func(domain.Certificate) *probeSection {
	return func(c domain.Certificate) *probeSection {
		sec := &probeSection{Target: c.ProbeTarget}
		if probes == nil {
			return sec
		}
		if st, err := probes.Get(c.ID); err == nil {
			sec.Status = &st
		}
		return sec
	}
}

// probeCertificate handles the "Probe now" POST, then redirects to the detail
// page. It is not a resource[T] method (probeSection/CertProber live outside
// the generic CRUD surface), so it parses the id param directly, matching
// attachment.go's non-resource sub-handlers.
func probeCertificate(prober CertProber, certs *store.CertificateRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		cert, err := certs.Get(id)
		if err != nil {
			notFoundOrServerError(w, r, "certificate", err)
			return
		}
		if cert.ProbeTarget != "" && prober != nil {
			if err := prober.ProbeOne(r.Context(), cert); err != nil {
				serverError(w, r, err)
				return
			}
		}
		http.Redirect(w, r, "/certificates/"+chi.URLParam(r, "id"), http.StatusSeeOther)
	}
}
