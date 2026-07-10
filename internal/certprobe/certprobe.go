// Package certprobe reads a certificate's live TLS endpoint, updates the tracked
// expiry/issuer, and records probe diagnostics and mismatches.
package certprobe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

// DialTLS opens a TLS connection to addr and returns the served leaf certificate.
type DialTLS func(ctx context.Context, addr string) (*x509.Certificate, error)

// Prober TLS-dials each certificate's probe target and records what it finds.
type Prober struct {
	certs   *store.CertificateRepo
	state   *store.CertProbeRepo
	db      *sql.DB
	dial    DialTLS
	timeout time.Duration
	log     *slog.Logger
	now     func() time.Time
}

// New builds a Prober. dial may be nil to use the default TLS dialer; log may
// be nil (defaults to slog.Default); now may be nil (defaults to time.Now).
func New(certs *store.CertificateRepo, state *store.CertProbeRepo, db *sql.DB,
	dial DialTLS, timeout time.Duration, log *slog.Logger, now func() time.Time) *Prober {
	if dial == nil {
		dial = defaultDialTLS
	}
	if log == nil {
		log = slog.Default()
	}
	if now == nil {
		now = time.Now
	}
	return &Prober{certs, state, db, dial, timeout, log, now}
}

// defaultDialTLS dials with verification disabled so metadata is readable even
// for expired/self-signed certs — we're inventorying what's actually served,
// not trusting it, so skipping verification here is intentional.
func defaultDialTLS(ctx context.Context, addr string) (*x509.Certificate, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("split host: %w", err)
	}
	d := tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true, ServerName: host}} //nolint:gosec // reading metadata, not trusting it
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	state := conn.(*tls.Conn).ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return nil, fmt.Errorf("no certificate served")
	}
	return state.PeerCertificates[0], nil
}

// Run probes every certificate that has a probe target. A single certificate's
// error is logged and does not abort the pass. It returns nil unless the
// context ended.
func (p *Prober) Run(ctx context.Context) error {
	certs, err := p.certs.List()
	if err != nil {
		return fmt.Errorf("list certificates: %w", err)
	}
	for _, c := range certs {
		if c.ProbeTarget == "" {
			continue
		}
		if err := p.ProbeOne(ctx, c); err != nil {
			p.log.Error("cert probe", "id", c.ID, "err", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

// ProbeOne probes one certificate: on success it updates ExpiresOn/Issuer and
// records diagnostics; on dial failure it records the failure (never fatal) and
// leaves the certificate row unchanged. Returns a non-nil error only on a
// storage failure.
func (p *Prober) ProbeOne(ctx context.Context, cert domain.Certificate) error {
	prev := p.priorState(cert.ID)

	dialCtx, cancel := context.WithTimeout(ctx, p.timeout)
	leaf, probeErr := p.dial(dialCtx, cert.ProbeTarget)
	cancel()
	if ctx.Err() != nil {
		return nil // parent ended mid-dial; don't record a spurious result (the #90 lesson)
	}

	now := p.now()
	if probeErr != nil {
		st := domain.CertProbeStatus{ProbedAt: now, Success: false, LastError: probeErr.Error()}
		if prev != nil { // carry last-known metadata so the UI keeps showing it
			st.Serial, st.Issuer, st.SANs, st.NotAfter = prev.Serial, prev.Issuer, prev.SANs, prev.NotAfter
		}
		return p.state.Upsert(cert.ID, st)
	}

	serial := fmt.Sprintf("%x", leaf.SerialNumber)
	issuer := leaf.Issuer.String()
	sans := leaf.DNSNames
	notAfter := leaf.NotAfter.UTC().Format(domain.DateLayout)
	mismatches := domain.ComputeCertMismatches(cert.Subject, prev, serial, issuer, sans)

	return store.WithTx(p.db, func(tx *sql.Tx) error {
		cert.ExpiresOn = notAfter
		cert.Issuer = issuer
		if err := p.certs.WithTx(tx).Update(cert); err != nil {
			return err
		}
		return p.state.WithTx(tx).Upsert(cert.ID, domain.CertProbeStatus{
			ProbedAt: now, Success: true, Serial: serial, Issuer: issuer,
			SANs: sans, NotAfter: notAfter, Mismatches: mismatches,
		})
	})
}

// priorState returns the previously stored probe status for certID, or nil if
// none exists yet (first probe) or it can't be read.
func (p *Prober) priorState(certID int64) *domain.CertProbeStatus {
	prev, err := p.state.Get(certID)
	if err != nil {
		return nil // store.ErrNotFound (or unreadable) => treat as first probe
	}
	return &prev
}
