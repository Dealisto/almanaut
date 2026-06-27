package domain

import (
	"testing"
	"time"
)

func TestExpiringSoon(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	certs := []Certificate{
		{Subject: "expired", ExpiresOn: "2026-05-01"}, // already expired -> included
		{Subject: "soon", ExpiresOn: "2026-06-20"},    // within 30 days -> included
		{Subject: "far", ExpiresOn: "2026-12-31"},     // beyond 30 days -> excluded
		{Subject: "bad", ExpiresOn: "not-a-date"},     // unparseable -> skipped
	}
	got := ExpiringSoon(certs, now, 30)
	if len(got) != 2 {
		t.Fatalf("got %d certs, want 2: %+v", len(got), got)
	}
	// sorted ascending by date: expired (05-01) before soon (06-20)
	if got[0].Subject != "expired" || got[1].Subject != "soon" {
		t.Errorf("wrong order/content: %+v", got)
	}
}

func TestServicesWithoutBackup(t *testing.T) {
	services := []Service{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}, {ID: 3, Name: "c"}}
	rels := []Relationship{
		// service 1 is backed up (service -> backup)
		{FromType: "service", FromID: 1, ToType: "backup", ToID: 10, Kind: "backed up by"},
		// service 2 is backed up (backup -> service, other direction)
		{FromType: "backup", FromID: 11, ToType: "service", ToID: 2, Kind: "backed up by"},
		// an unrelated edge that must NOT count
		{FromType: "service", FromID: 3, ToType: "host", ToID: 5, Kind: "runs on"},
	}
	got := ServicesWithoutBackup(services, rels)
	if len(got) != 1 || got[0].ID != 3 {
		t.Errorf("want only service 3 without backup, got %+v", got)
	}
}

func TestHostsDown(t *testing.T) {
	hosts := []Host{
		{ID: 1, Name: "a", Status: "running"},
		{ID: 2, Name: "b", Status: "DOWN"},
		{ID: 3, Name: "c", Status: " offline "},
		{ID: 4, Name: "d", Status: "stopped"},
		{ID: 5, Name: "e", Status: ""},
	}
	down := HostsDown(hosts)
	if len(down) != 3 {
		t.Fatalf("got %d down, want 3: %+v", len(down), down)
	}
	if down[0].Name != "b" || down[1].Name != "c" || down[2].Name != "d" {
		t.Errorf("wrong hosts or order: %+v", down)
	}
}
