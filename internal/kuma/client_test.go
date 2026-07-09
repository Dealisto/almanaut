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

	// The fake sent a second ping right after monitorList; if the client's
	// read-loop pong handling were removed, this would time out.
	if !f.waitForPong(2, 2*time.Second) {
		t.Fatal("fake never observed a second pong from the client")
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

func TestAddEditDeleteMonitor(t *testing.T) {
	f := newFakeKuma(t)
	c := NewClient(f.url(), "admin", "s3cret", false)
	s, err := c.Connect(testCtx(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer s.Close()

	id, err := s.Add(testCtx(t), Monitor{Name: "jellyfin", URL: "http://jellyfin.lan:8096"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, ok := f.getMonitor(id)
	if !ok || got["name"] != "jellyfin" || got["url"] != "http://jellyfin.lan:8096" || got["type"] != "http" {
		t.Fatalf("stored monitor = %v", got)
	}

	if err := s.Edit(testCtx(t), Monitor{ID: id, Name: "jellyfin-2", URL: "http://jellyfin.lan:9999",
		raw: map[string]any{"id": float64(id), "type": "http", "name": "jellyfin", "url": "http://jellyfin.lan:8096", "interval": float64(120)}}); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	got, _ = f.getMonitor(id)
	if got["name"] != "jellyfin-2" || got["url"] != "http://jellyfin.lan:9999" {
		t.Fatalf("edited monitor = %v", got)
	}
	if got["interval"] != float64(120) {
		t.Fatalf("edit dropped unknown field interval: %v", got)
	}

	if err := s.Delete(testCtx(t), id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if f.monitorCount() != 0 {
		t.Fatalf("monitor not deleted")
	}
}

func TestEditWithoutRawSendsMinimalObject(t *testing.T) {
	f := newFakeKuma(t)
	id := f.putMonitor("old", "http://old.lan")
	c := NewClient(f.url(), "admin", "s3cret", false)
	s, err := c.Connect(testCtx(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer s.Close()

	mons := s.Monitors() // raw comes from the monitorList push
	if err := s.Edit(testCtx(t), Monitor{ID: id, Name: "new", URL: "http://new.lan", raw: mons[id].raw}); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	got, _ := f.getMonitor(id)
	if got["name"] != "new" || got["url"] != "http://new.lan" {
		t.Fatalf("edited monitor = %v", got)
	}
}
