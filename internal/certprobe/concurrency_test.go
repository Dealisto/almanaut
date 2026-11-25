package certprobe

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestRunBoundsConcurrencyAndResumesUnstartedCertificates(t *testing.T) {
	db := testDB(t)
	certs := store.NewCertificateRepo(db)
	for i := range 6 {
		if _, err := certs.Create(domain.Certificate{Subject: fmt.Sprintf("cert%d", i), ProbeTarget: fmt.Sprintf("cert%d:443", i)}); err != nil {
			t.Fatal(err)
		}
	}
	var slow atomic.Bool
	slow.Store(true)
	entered := make(chan string, 6)
	var active, maxActive atomic.Int32
	var mu sync.Mutex
	var resumed []string
	dial := func(ctx context.Context, addr string) (*x509.Certificate, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maxActive.Load(); n > old && !maxActive.CompareAndSwap(old, n); old = maxActive.Load() {
		}
		if slow.Load() {
			entered <- addr
			<-ctx.Done()
			return nil, ctx.Err()
		}
		mu.Lock()
		resumed = append(resumed, addr)
		mu.Unlock()
		return nil, errors.New("ordinary probe failure")
	}
	p := New(certs, store.NewCertProbeRepo(db), db, dial, time.Second, nil, nil)
	p.workers = 2
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	<-entered
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run=%v", err)
	}
	if maxActive.Load() != 2 {
		t.Fatalf("max concurrency=%d", maxActive.Load())
	}
	slow.Store(false)
	if err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(resumed) != 6 {
		t.Fatalf("resumed=%v", resumed)
	}
	if (resumed[0] != "cert2:443" && resumed[0] != "cert3:443") || (resumed[1] != "cert2:443" && resumed[1] != "cert3:443") {
		t.Fatalf("did not resume unstarted targets: %v", resumed)
	}
	if maxActive.Load() > 2 {
		t.Fatalf("max concurrency=%d", maxActive.Load())
	}
}

func TestRunRecoversWorkerPanicAndChecksOtherCertificates(t *testing.T) {
	db := testDB(t)
	certs := store.NewCertificateRepo(db)
	for _, subject := range []string{"bad", "good"} {
		if _, err := certs.Create(domain.Certificate{Subject: subject, ProbeTarget: subject + ":443"}); err != nil {
			t.Fatal(err)
		}
	}
	var checked atomic.Bool
	p := New(certs, store.NewCertProbeRepo(db), db, func(_ context.Context, addr string) (*x509.Certificate, error) {
		if addr == "bad:443" {
			panic("bad target")
		}
		checked.Store(true)
		return nil, errors.New("ordinary dial error")
	}, time.Second, nil, nil)
	if err := p.Run(context.Background()); err == nil || !checked.Load() {
		t.Fatalf("Run=%v checked=%v", err, checked.Load())
	}
}

func TestConcurrentRunAdmissionHonorsCancellation(t *testing.T) {
	db := testDB(t)
	certs := store.NewCertificateRepo(db)
	if _, err := certs.Create(domain.Certificate{Subject: "web", ProbeTarget: "web:443"}); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	p := New(certs, store.NewCertProbeRepo(db), db, func(ctx context.Context, _ string) (*x509.Certificate, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}, time.Second, nil, nil)
	firstCtx, stopFirst := context.WithCancel(context.Background())
	defer stopFirst()
	firstDone := make(chan error, 1)
	go func() { firstDone <- p.Run(firstCtx) }()
	<-entered
	waitingCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	waitingDone := make(chan error, 1)
	go func() { waitingDone <- p.Run(waitingCtx) }()
	select {
	case err := <-waitingDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("waiting Run=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiting Run ignored cancellation")
	}
	stopFirst()
	<-firstDone
}
