package notify

import (
	"sort"
	"testing"
	"time"
)

func keys(items []Item) []Key {
	out := make([]Key, len(items))
	for i, it := range items {
		out[i] = it.Key()
	}
	return out
}

func TestHumanizeWhenUsesCalendarDays(t *testing.T) {
	for _, tc := range []struct {
		name string
		now  time.Time
		date string
		want string
	}{
		{"tomorrow at noon", time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC), "2026-07-02", "expires in 1 day"},
		{"two days at night", time.Date(2026, 7, 1, 23, 59, 0, 0, time.UTC), "2026-07-03", "expires in 2 days"},
		{"today", time.Date(2026, 7, 1, 23, 59, 0, 0, time.UTC), "2026-07-01", "expires today"},
		{"yesterday", time.Date(2026, 7, 1, 0, 1, 0, 0, time.UTC), "2026-06-30", "expired"},
		{"east of UTC", time.Date(2026, 7, 1, 0, 1, 0, 0, time.FixedZone("east", 14*3600)), "2026-06-30", "expired"},
		{"west of UTC", time.Date(2026, 7, 1, 23, 59, 0, 0, time.FixedZone("west", -10*3600)), "2026-07-02", "expires in 1 day"},
		{"surrounding whitespace", time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC), " 2026-07-02 ", "expires in 1 day"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := humanizeWhen(tc.date, tc.now); got != tc.want {
				t.Fatalf("humanizeWhen = %q, want %q", got, tc.want)
			}
		})
	}
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
		notify, clear := decide([]Item{cert1}, map[Key]string{})
		if len(notify) != 1 || notify[0].Key() != cert1.Key() {
			t.Fatalf("want cert1 notified, got %v", notify)
		}
		if len(clear) != 0 {
			t.Fatalf("want nothing cleared, got %v", clear)
		}
	})

	t.Run("already-notified item is left alone", func(t *testing.T) {
		notify, clear := decide([]Item{cert1}, map[Key]string{cert1.Key(): cert1.Date})
		if len(notify) != 0 {
			t.Fatalf("want nothing notified, got %v", notify)
		}
		if len(clear) != 0 {
			t.Fatalf("want nothing cleared, got %v", clear)
		}
	})

	t.Run("changed deadline is notified", func(t *testing.T) {
		notify, clear := decide([]Item{cert1}, map[Key]string{cert1.Key(): "2026-07-01"})
		if len(notify) != 1 || notify[0].Date != cert1.Date || len(clear) != 0 {
			t.Fatalf("want new deadline notified without clearing state, got %v / %v", notify, clear)
		}
	})

	t.Run("date whitespace does not re-arm", func(t *testing.T) {
		notify, clear := decide([]Item{cert1}, map[Key]string{cert1.Key(): " 2026-07-08\t"})
		if len(notify) != 0 || len(clear) != 0 {
			t.Fatalf("want unchanged calendar date suppressed, got %v / %v", notify, clear)
		}
	})

	t.Run("no-longer-expiring item is cleared", func(t *testing.T) {
		notify, clear := decide([]Item{}, map[Key]string{cert1.Key(): cert1.Date})
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
			map[Key]string{cert1.Key(): cert1.Date, cert3: "2026-07-10"},
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
