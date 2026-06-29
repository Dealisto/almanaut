package domain

import (
	"sort"
	"strings"
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

// downStatuses are the free-text Host.Status values treated as "not running".
var downStatuses = []string{"down", "offline", "stopped"}

// HostsDown returns the hosts whose Status (trimmed, lowercased) marks them as
// not running — "down", "offline", or "stopped" — in input order. Status is
// free text, so this is a best-effort heuristic.
func HostsDown(hosts []Host) []Host {
	out := []Host{}
	for _, h := range hosts {
		if contains(downStatuses, strings.ToLower(strings.TrimSpace(h.Status))) {
			out = append(out, h)
		}
	}
	return out
}

// WarrantyExpiring returns the hardware whose WarrantyEnd date is on or before
// now+withinDays (including already-expired), sorted by WarrantyEnd ascending.
// Hardware with an empty or unparseable WarrantyEnd is skipped.
func WarrantyExpiring(hw []Hardware, now time.Time, withinDays int) []Hardware {
	cutoff := now.AddDate(0, 0, withinDays)
	out := []Hardware{}
	for _, h := range hw {
		end, err := time.Parse(DateLayout, h.WarrantyEnd)
		if err != nil {
			continue
		}
		if !end.After(cutoff) { // end <= cutoff
			out = append(out, h)
		}
	}
	// WarrantyEnd is YYYY-MM-DD, which sorts lexically in chronological order.
	sort.Slice(out, func(i, j int) bool { return out[i].WarrantyEnd < out[j].WarrantyEnd })
	return out
}
