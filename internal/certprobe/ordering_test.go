package certprobe

import (
	"context"
	"crypto/x509"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestProbeOneDiscardsOlderConcurrentCompletion(t *testing.T) {
	for _, failure := range []bool{false, true} {
		name := "success"
		if failure {
			name = "failure"
		}
		t.Run(name, func(t *testing.T) {
			db := testDB(t)
			certs := store.NewCertificateRepo(db)
			id, err := certs.Create(domain.Certificate{Subject: "example.com", ProbeTarget: "example.com:443"})
			if err != nil {
				t.Fatal(err)
			}
			cert, err := certs.Get(id)
			if err != nil {
				t.Fatal(err)
			}
			started, release := make(chan struct{}), make(chan struct{})
			done := make(chan error, 1)
			var calls, clock atomic.Int32
			base := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
			now := func() time.Time { return base.Add(time.Duration(clock.Add(1)) * time.Nanosecond) }
			dial := func(context.Context, string) (*x509.Certificate, error) {
				if calls.Add(1) == 1 {
					close(started)
					<-release
					if failure {
						return nil, errors.New("old connection timed out")
					}
					return leaf(base.AddDate(1, 0, 0), "Old CA", 1, "example.com"), nil
				}
				return leaf(base.AddDate(2, 0, 0), "New CA", 2, "example.com"), nil
			}
			state := store.NewCertProbeRepo(db)
			p := New(certs, state, db, dial, time.Second, nil, now)
			go func() { done <- p.ProbeOne(context.Background(), cert) }()
			<-started
			newErr := p.ProbeOne(context.Background(), cert)
			close(release)
			oldErr := <-done
			if newErr != nil || oldErr != nil {
				t.Fatalf("probe errors: new=%v old=%v", newErr, oldErr)
			}
			got, err := certs.Get(id)
			if err != nil || got.ExpiresOn != "2028-11-25" {
				t.Fatalf("older completion overwrote expiry: %+v, %v", got, err)
			}
			status, err := state.Get(id)
			if err != nil || !status.Success || status.Serial != "2" || !status.ProbedAt.Equal(base.Add(2*time.Nanosecond)) {
				t.Fatalf("older completion overwrote probe status: %+v, %v", status, err)
			}
		})
	}
}

func TestProbeOneEqualStartTimePreservesFirstCommittedResult(t *testing.T) {
	db := testDB(t)
	certs := store.NewCertificateRepo(db)
	id, err := certs.Create(domain.Certificate{Subject: "example.com", ProbeTarget: "example.com:443"})
	if err != nil {
		t.Fatal(err)
	}
	cert, err := certs.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 11, 25, 12, 0, 0, 123, time.UTC)
	var serial int64
	dial := func(context.Context, string) (*x509.Certificate, error) {
		serial++
		return leaf(stamp.AddDate(1, 0, 0), "CA", serial, "example.com"), nil
	}
	state := store.NewCertProbeRepo(db)
	p := New(certs, state, db, dial, time.Second, nil, func() time.Time { return stamp })
	for range 2 {
		if err := p.ProbeOne(context.Background(), cert); err != nil {
			t.Fatal(err)
		}
	}
	status, err := state.Get(id)
	if err != nil || status.Serial != "1" {
		t.Fatalf("equal timestamp replaced first result: %+v, %v", status, err)
	}
}
