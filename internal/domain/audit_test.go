package domain

import (
	"testing"
	"time"
)

func TestHostsWithoutBackup(t *testing.T) {
	hosts := []Host{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}, {ID: 3, Name: "c"}}
	rels := []Relationship{
		{FromType: "host", FromID: 1, ToType: "backup", ToID: 10, Kind: "backed up by"},
		{FromType: "backup", FromID: 11, ToType: "host", ToID: 2, Kind: "backed up by"},
		{FromType: "host", FromID: 3, ToType: "service", ToID: 5, Kind: "runs on"}, // must not count
	}
	got := HostsWithoutBackup(hosts, rels)
	if len(got) != 1 || got[0].ID != 3 {
		t.Errorf("want only host 3 without backup, got %+v", got)
	}
}

func TestServicesWithoutHost(t *testing.T) {
	services := []Service{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}, {ID: 3, Name: "c"}}
	rels := []Relationship{
		{FromType: "service", FromID: 1, ToType: "host", ToID: 10, Kind: "runs on"},
		{FromType: "host", FromID: 11, ToType: "service", ToID: 2, Kind: "runs on"},
		{FromType: "service", FromID: 3, ToType: "backup", ToID: 5, Kind: "backed up by"}, // must not count
	}
	got := ServicesWithoutHost(services, rels)
	if len(got) != 1 || got[0].ID != 3 {
		t.Errorf("want only service 3 without host, got %+v", got)
	}
}

func TestExpiredCertificates(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	certs := []Certificate{
		{Subject: "old", ExpiresOn: "2026-01-01"},    // expired -> included
		{Subject: "today", ExpiresOn: "2026-06-01"},  // parses to midnight, before noon -> included
		{Subject: "future", ExpiresOn: "2026-12-31"}, // not yet expired -> excluded
		{Subject: "bad", ExpiresOn: "nope"},          // unparseable -> skipped
		{Subject: "none", ExpiresOn: ""},             // empty -> skipped
	}
	got := ExpiredCertificates(certs, now)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2: %+v", len(got), got)
	}
	if got[0].Subject != "old" || got[1].Subject != "today" {
		t.Errorf("unexpected order/content: %+v", got)
	}
}

func TestHardwareWithoutWarranty(t *testing.T) {
	hw := []Hardware{
		{ID: 1, Name: "tracked", WarrantyEnd: "2027-01-01"},
		{ID: 2, Name: "untracked", WarrantyEnd: ""},
	}
	got := HardwareWithoutWarranty(hw)
	if len(got) != 1 || got[0].ID != 2 {
		t.Errorf("want only hardware 2, got %+v", got)
	}
}

func TestSubscriptionsWithoutRenewal(t *testing.T) {
	subs := []Subscription{
		{ID: 1, Name: "tracked", RenewalDate: "2027-01-01"},
		{ID: 2, Name: "untracked", RenewalDate: ""},
	}
	got := SubscriptionsWithoutRenewal(subs)
	if len(got) != 1 || got[0].ID != 2 {
		t.Errorf("want only subscription 2, got %+v", got)
	}
}

func TestUnlinkedRefs(t *testing.T) {
	refs := []EntityRef{
		{Type: "host", ID: 1},
		{Type: "host", ID: 2},
		{Type: "certificate", ID: 3},
		{Type: "certificate", ID: 4},
	}
	rels := []Relationship{
		{FromType: "host", FromID: 1, ToType: "service", ToID: 9, Kind: "runs on"},
		{FromType: "service", FromID: 8, ToType: "certificate", ToID: 4, Kind: "secured by"},
	}
	got := UnlinkedRefs(refs, rels)
	// host 1 and cert 4 are linked; host 2 and cert 3 are orphans, order preserved.
	want := []EntityRef{{Type: "host", ID: 2}, {Type: "certificate", ID: 3}}
	if len(got) != len(want) {
		t.Fatalf("got %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("at %d got %+v want %+v", i, got[i], want[i])
		}
	}
}
