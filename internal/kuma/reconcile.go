package kuma

import (
	"sort"

	"github.com/Dealisto/almanaut/internal/domain"
)

type actionKind int

const (
	actCreate actionKind = iota // create monitor in Kuma, then Put mapping
	actEdit                     // edit monitor in Kuma
	actDelete                   // delete monitor in Kuma, then Delete mapping
	actForget                   // Delete mapping only (monitor already gone)
)

type action struct {
	kind      actionKind
	serviceID int64
	monitor   Monitor // desired state for create/edit; .ID is the Kuma id for edit/delete
}

// plan computes the actions that turn Kuma's current state into the desired
// one. It is a pure function: services is almanaut's truth, mapping is the
// managed serviceID→monitorID table, existing is Kuma's live monitor list.
// Monitors absent from mapping are never touched. skipped counts services
// without a monitorable URL.
func plan(services []domain.Service, mapping map[int64]int64, existing map[int64]Monitor) ([]action, int) {
	desired := map[int64]Monitor{} // serviceID → wanted monitor state
	skipped := 0
	for _, s := range services {
		u, ok := monitorURL(s)
		if !ok {
			skipped++
			continue
		}
		desired[s.ID] = Monitor{Name: s.Name, URL: u}
	}

	var actions []action

	// Managed rows: edit drifted monitors, recreate manually-deleted ones,
	// delete (or forget) the ones whose service is gone or unmonitorable.
	sids := make([]int64, 0, len(mapping))
	for sid := range mapping {
		sids = append(sids, sid)
	}
	sort.Slice(sids, func(i, j int) bool { return sids[i] < sids[j] })
	for _, sid := range sids {
		mid := mapping[sid]
		cur, inKuma := existing[mid]
		want, wanted := desired[sid]
		switch {
		case wanted && inKuma:
			if cur.Name != want.Name || cur.URL != want.URL {
				want.ID = mid
				want.raw = cur.raw
				actions = append(actions, action{kind: actEdit, serviceID: sid, monitor: want})
			}
		case wanted && !inKuma: // deleted by hand in Kuma → create again
			actions = append(actions, action{kind: actCreate, serviceID: sid, monitor: want})
		case !wanted && inKuma:
			actions = append(actions, action{kind: actDelete, serviceID: sid, monitor: Monitor{ID: mid}})
		default: // service and monitor both gone → drop the stale row
			actions = append(actions, action{kind: actForget, serviceID: sid})
		}
		delete(desired, sid)
	}

	// Whatever remains desired has no mapping row yet: create.
	rest := make([]int64, 0, len(desired))
	for sid := range desired {
		rest = append(rest, sid)
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i] < rest[j] })
	for _, sid := range rest {
		actions = append(actions, action{kind: actCreate, serviceID: sid, monitor: desired[sid]})
	}
	return actions, skipped
}
