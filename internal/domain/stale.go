package domain

import "time"

// StaleRefs returns the entities whose most recent recorded activity is older
// than the staleness window (days). "Activity" is the latest changelog event —
// a create, edit, discovery import, or acknowledgement — so acknowledging an
// entity (which writes a fresh event) resets its clock.
//
// Entities with no recorded activity are skipped: their age is unknown, so
// reporting them would be a false positive. Input order is preserved, and
// days <= 0 disables the rule.
func StaleRefs(refs []EntityRef, lastActivity map[EntityRef]string, now time.Time, days int) []EntityRef {
	if days <= 0 {
		return nil
	}
	cutoff := now.AddDate(0, 0, -days)
	var out []EntityRef
	for _, ref := range refs {
		ts, ok := lastActivity[ref]
		if !ok {
			continue
		}
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			out = append(out, ref)
		}
	}
	return out
}
