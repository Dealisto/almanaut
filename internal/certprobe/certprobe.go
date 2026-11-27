// Package certprobe reads a certificate's live TLS endpoint, updates the tracked
// expiry/issuer, and records probe diagnostics and mismatches.
package certprobe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"runtime/debug"
	"sort"
	"sync"
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
	workers int
	runGate chan struct{}
	nextID  int64 // first certificate not started during the previous pass
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
	return &Prober{certs: certs, state: state, db: db, dial: dial, timeout: timeout, log: log, now: now, workers: 8, runGate: make(chan struct{}, 1)}
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
// storage error is logged and does not abort the pass. Storage failures are
// returned together after the remaining certificates have been probed.
func (p *Prober) Run(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case p.runGate <- struct{}{}:
	}
	defer func() { <-p.runGate }()
	if err := ctx.Err(); err != nil {
		return err
	}
	certs, err := p.certs.List()
	if err != nil {
		return fmt.Errorf("list certificates: %w", err)
	}
	targets := certs[:0]
	for _, c := range certs {
		if c.ProbeTarget != "" {
			targets = append(targets, c)
		}
	}
	// IDs provide a stable rotation even if names change. An interrupted pass
	// resumes with targets it did not start, rather than starving the same tail.
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	start := sort.Search(len(targets), func(i int) bool { return targets[i].ID >= p.nextID })
	if start == len(targets) {
		start = 0
	}
	sem := make(chan struct{}, max(1, p.workers))
	failures := make([]error, len(targets))
	var wg sync.WaitGroup
dispatch:
	for i := range targets {
		c := targets[(start+i)%len(targets)]
		p.nextID = c.ID
		if ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			break dispatch
		case sem <- struct{}{}:
			if ctx.Err() != nil {
				<-sem
				break dispatch
			}
			p.nextID = targets[(start+i+1)%len(targets)].ID
			wg.Go(func() {
				defer func() { <-sem }()
				failures[i] = p.safeProbe(ctx, c)
			})
		}
	}
	wg.Wait()
	return errors.Join(append(failures, ctx.Err())...)
}

// Worker panics need their own recovery; the scheduler can only recover the
// Run goroutine. A malformed target must not terminate the whole process.
func (p *Prober) safeProbe(ctx context.Context, c domain.Certificate) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			p.log.Error("cert probe panic", "id", c.ID, "panic", recovered, "stack", string(debug.Stack()))
			err = fmt.Errorf("probe certificate %d panicked: %v", c.ID, recovered)
		}
	}()
	if err := p.ProbeOne(ctx, c); err != nil {
		p.log.Error("cert probe", "id", c.ID, "err", err)
		return fmt.Errorf("probe certificate %d: %w", c.ID, err)
	}
	return nil
}

// ProbeOne probes one certificate: on success it updates ExpiresOn/Issuer and
// records diagnostics; on dial failure it records the failure (never fatal) and
// leaves the certificate row unchanged. Returns a non-nil error only on a
// storage failure.
func (p *Prober) ProbeOne(ctx context.Context, cert domain.Certificate) error {
	startedAt := p.now()
	dialCtx, cancel := context.WithTimeout(ctx, p.timeout)
	leaf, probeErr := p.dial(dialCtx, cert.ProbeTarget)
	cancel()
	if ctx.Err() != nil {
		return nil // parent ended mid-dial; don't record a spurious result (the #90 lesson)
	}

	return store.WithTx(p.db, func(tx *sql.Tx) error {
		// Network I/O runs outside the transaction. Re-read before writing so
		// an in-flight probe cannot undo an edit or update a different target.
		certs := p.certs.WithTx(tx)
		current, err := certs.Get(cert.ID)
		if errors.Is(err, store.ErrNotFound) {
			return nil // deleted while the probe was in flight
		}
		if err != nil {
			return err
		}
		if current.ProbeTarget != cert.ProbeTarget || ctx.Err() != nil {
			return nil
		}
		state := p.state.WithTx(tx)
		var prev *domain.CertProbeStatus
		previous, err := state.Get(cert.ID)
		if err == nil {
			// A later-started probe may finish first, especially when a manual
			// probe overlaps the scheduled pass. Preserve its result; identical
			// start times keep the first committed result deterministically.
			if !previous.ProbedAt.Before(startedAt) {
				return nil
			}
			prev = &previous
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if probeErr != nil {
			st := domain.CertProbeStatus{ProbedAt: startedAt, Success: false, LastError: probeErr.Error()}
			if prev != nil {
				st.Serial, st.Issuer, st.SANs, st.NotAfter = prev.Serial, prev.Issuer, prev.SANs, prev.NotAfter
			}
			return state.Upsert(cert.ID, st)
		}
		serial := fmt.Sprintf("%x", leaf.SerialNumber)
		issuer := leaf.Issuer.String()
		sans := append([]string(nil), leaf.DNSNames...)
		for _, ip := range leaf.IPAddresses {
			sans = append(sans, ip.String())
		}
		notAfter := leaf.NotAfter.UTC().Format(domain.DateLayout)
		mismatches := domain.ComputeCertMismatches(current.Subject, prev, serial, issuer, leaf.DNSNames, leaf.IPAddresses...)
		current.ExpiresOn = notAfter
		current.Issuer = issuer
		if err := certs.Update(current); err != nil {
			return err
		}
		return state.Upsert(cert.ID, domain.CertProbeStatus{
			ProbedAt: startedAt, Success: true, Serial: serial, Issuer: issuer,
			SANs: sans, NotAfter: notAfter, Mismatches: mismatches,
		})
	})
}
