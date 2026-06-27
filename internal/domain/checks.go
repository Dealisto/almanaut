package domain

import (
	"sort"
	"time"
)

// ExpiringSoon returns the certificates whose ExpiresOn date is on or before
// now+withinDays (including already-expired certificates), sorted by ExpiresOn
// ascending. Certificates whose ExpiresOn does not parse are skipped.
func ExpiringSoon(certs []Certificate, now time.Time, withinDays int) []Certificate {
	cutoff := now.AddDate(0, 0, withinDays)
	out := []Certificate{}
	for _, c := range certs {
		expiry, err := time.Parse(DateLayout, c.ExpiresOn)
		if err != nil {
			continue
		}
		if !expiry.After(cutoff) { // expiry <= cutoff
			out = append(out, c)
		}
	}
	// ExpiresOn is YYYY-MM-DD, which sorts lexically in chronological order.
	sort.Slice(out, func(i, j int) bool { return out[i].ExpiresOn < out[j].ExpiresOn })
	return out
}

// ServicesWithoutBackup returns the services that are not linked to any backup
// entity by a relationship, in either direction.
func ServicesWithoutBackup(services []Service, rels []Relationship) []Service {
	backedUp := map[int64]bool{}
	for _, r := range rels {
		if r.FromType == "service" && r.ToType == "backup" {
			backedUp[r.FromID] = true
		}
		if r.ToType == "service" && r.FromType == "backup" {
			backedUp[r.ToID] = true
		}
	}
	out := []Service{}
	for _, s := range services {
		if !backedUp[s.ID] {
			out = append(out, s)
		}
	}
	return out
}
