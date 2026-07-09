package kuma

import (
	"context"
	"testing"
	"time"
)

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestConnectAndLogin(t *testing.T) {
	f := newFakeKuma(t)
	f.putMonitor("pre-existing", "http://pre.lan")

	c := NewClient(f.url(), "admin", "s3cret", false)
	s, err := c.Connect(testCtx(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer s.Close()

	mons := s.Monitors()
	if len(mons) != 1 {
		t.Fatalf("Monitors() = %v, want the 1 seeded monitor", mons)
	}
	for _, m := range mons {
		if m.Name != "pre-existing" || m.URL != "http://pre.lan" || m.ID == 0 {
			t.Fatalf("monitor = %+v", m)
		}
	}
}

func TestConnectBadPassword(t *testing.T) {
	f := newFakeKuma(t)
	c := NewClient(f.url(), "admin", "wrong", false)
	if _, err := c.Connect(testCtx(t)); err == nil {
		t.Fatal("Connect succeeded with a bad password")
	}
}

func TestConnectServerDown(t *testing.T) {
	f := newFakeKuma(t)
	url := f.url()
	f.srv.Close()
	c := NewClient(url, "admin", "s3cret", false)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.Connect(ctx); err == nil {
		t.Fatal("Connect succeeded against a dead server")
	}
}
