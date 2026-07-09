package kuma

import (
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func svc(id int64, name, url string) domain.Service {
	return domain.Service{ID: id, Name: name, Kind: "container", URL: url}
}

// count returns how many planned actions have the given kind.
func count(actions []action, kind actionKind) int {
	n := 0
	for _, a := range actions {
		if a.kind == kind {
			n++
		}
	}
	return n
}

func find(actions []action, kind actionKind) (action, bool) {
	for _, a := range actions {
		if a.kind == kind {
			return a, true
		}
	}
	return action{}, false
}

func TestMonitorURL(t *testing.T) {
	cases := []struct {
		url string
		ok  bool
	}{
		{"http://jellyfin.lan:8096", true},
		{"https://grafana.lan/dashboards", true},
		{"", false},
		{"jellyfin.lan:8096", false}, // no scheme
		{"ssh://host", false},        // wrong scheme
		{"http://", false},           // no host
	}
	for _, c := range cases {
		got, ok := monitorURL(svc(1, "s", c.url))
		if ok != c.ok {
			t.Errorf("monitorURL(%q) ok = %v, want %v", c.url, ok, c.ok)
		}
		if ok && got != c.url {
			t.Errorf("monitorURL(%q) = %q", c.url, got)
		}
	}
}

func TestPlanCreatesMissing(t *testing.T) {
	actions, skipped := plan(
		[]domain.Service{svc(1, "jellyfin", "http://jellyfin.lan:8096"), svc(2, "no-url", "")},
		map[int64]int64{},
		map[int64]Monitor{},
	)
	if skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	if len(actions) != 1 || actions[0].kind != actCreate {
		t.Fatalf("actions = %+v, want one create", actions)
	}
	if actions[0].serviceID != 1 || actions[0].monitor.Name != "jellyfin" || actions[0].monitor.URL != "http://jellyfin.lan:8096" {
		t.Fatalf("create action = %+v", actions[0])
	}
}

func TestPlanNoopWhenInSync(t *testing.T) {
	actions, skipped := plan(
		[]domain.Service{svc(1, "jellyfin", "http://jellyfin.lan:8096")},
		map[int64]int64{1: 100},
		map[int64]Monitor{100: {ID: 100, Name: "jellyfin", URL: "http://jellyfin.lan:8096"}},
	)
	if len(actions) != 0 || skipped != 0 {
		t.Fatalf("want no actions, got %+v (skipped %d)", actions, skipped)
	}
}

func TestPlanEditsChanged(t *testing.T) {
	actions, _ := plan(
		[]domain.Service{svc(1, "jellyfin-new", "http://jellyfin.lan:9999")},
		map[int64]int64{1: 100},
		map[int64]Monitor{100: {ID: 100, Name: "jellyfin", URL: "http://jellyfin.lan:8096"}},
	)
	a, ok := find(actions, actEdit)
	if !ok || len(actions) != 1 {
		t.Fatalf("actions = %+v, want one edit", actions)
	}
	if a.monitor.ID != 100 || a.monitor.Name != "jellyfin-new" || a.monitor.URL != "http://jellyfin.lan:9999" {
		t.Fatalf("edit action = %+v", a)
	}
}

func TestPlanRecreatesManuallyDeleted(t *testing.T) {
	// Mapping exists but the monitor is gone from Kuma: create it again.
	actions, _ := plan(
		[]domain.Service{svc(1, "jellyfin", "http://jellyfin.lan:8096")},
		map[int64]int64{1: 100},
		map[int64]Monitor{},
	)
	if len(actions) != 1 || actions[0].kind != actCreate || actions[0].serviceID != 1 {
		t.Fatalf("actions = %+v, want one create", actions)
	}
}

func TestPlanDeletesOrphans(t *testing.T) {
	// Service 1 was deleted; service 2 lost its URL. Both managed monitors go.
	actions, _ := plan(
		[]domain.Service{svc(2, "no-more-url", "")},
		map[int64]int64{1: 100, 2: 200},
		map[int64]Monitor{
			100: {ID: 100, Name: "gone", URL: "http://gone.lan"},
			200: {ID: 200, Name: "no-more-url", URL: "http://x.lan"},
		},
	)
	if count(actions, actDelete) != 2 {
		t.Fatalf("actions = %+v, want two deletes", actions)
	}
}

func TestPlanForgetsWhenBothGone(t *testing.T) {
	// Service gone AND monitor gone: only the stale mapping row is removed.
	actions, _ := plan(nil, map[int64]int64{1: 100}, map[int64]Monitor{})
	if len(actions) != 1 || actions[0].kind != actForget || actions[0].serviceID != 1 {
		t.Fatalf("actions = %+v, want one forget", actions)
	}
}

func TestPlanNeverTouchesUnmanaged(t *testing.T) {
	// Monitor 999 exists in Kuma but is not in the mapping: invisible.
	actions, _ := plan(
		nil,
		map[int64]int64{},
		map[int64]Monitor{999: {ID: 999, Name: "hand-made", URL: "http://keep.me"}},
	)
	if len(actions) != 0 {
		t.Fatalf("actions = %+v, want none", actions)
	}
}
