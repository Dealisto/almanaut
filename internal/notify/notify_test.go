package notify

import (
	"sort"
	"testing"
)

func keys(items []Item) []Key {
	out := make([]Key, len(items))
	for i, it := range items {
		out[i] = it.Key()
	}
	return out
}

func sortKeys(ks []Key) {
	sort.Slice(ks, func(i, j int) bool {
		if ks[i].Kind != ks[j].Kind {
			return ks[i].Kind < ks[j].Kind
		}
		return ks[i].ID < ks[j].ID
	})
}

func TestDecide(t *testing.T) {
	cert1 := Item{Kind: KindCertificate, ID: 1, Label: "a.com", Date: "2026-07-08"}
	cert2 := Item{Kind: KindCertificate, ID: 2, Label: "b.com", Date: "2026-07-10"}

	t.Run("new item is notified", func(t *testing.T) {
		notify, clear := decide([]Item{cert1}, map[Key]bool{})
		if len(notify) != 1 || notify[0].Key() != cert1.Key() {
			t.Fatalf("want cert1 notified, got %v", notify)
		}
		if len(clear) != 0 {
			t.Fatalf("want nothing cleared, got %v", clear)
		}
	})

	t.Run("already-notified item is left alone", func(t *testing.T) {
		notify, clear := decide([]Item{cert1}, map[Key]bool{cert1.Key(): true})
		if len(notify) != 0 {
			t.Fatalf("want nothing notified, got %v", notify)
		}
		if len(clear) != 0 {
			t.Fatalf("want nothing cleared, got %v", clear)
		}
	})

	t.Run("no-longer-expiring item is cleared", func(t *testing.T) {
		notify, clear := decide([]Item{}, map[Key]bool{cert1.Key(): true})
		if len(notify) != 0 {
			t.Fatalf("want nothing notified, got %v", notify)
		}
		if len(clear) != 1 || clear[0] != cert1.Key() {
			t.Fatalf("want cert1 cleared, got %v", clear)
		}
	})

	t.Run("mixed set", func(t *testing.T) {
		// cert1 already notified (stays), cert2 new (notify), cert3 gone (clear).
		cert3 := Key{Kind: KindCertificate, ID: 3}
		notify, clear := decide(
			[]Item{cert1, cert2},
			map[Key]bool{cert1.Key(): true, cert3: true},
		)
		gotNotify := keys(notify)
		sortKeys(gotNotify)
		if len(gotNotify) != 1 || gotNotify[0] != cert2.Key() {
			t.Fatalf("want only cert2 notified, got %v", gotNotify)
		}
		if len(clear) != 1 || clear[0] != cert3 {
			t.Fatalf("want only cert3 cleared, got %v", clear)
		}
	})
}
