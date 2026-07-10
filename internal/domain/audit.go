package domain

import "time"

// This file holds the fixed audit rules behind the inventory health page. Each
// rule is a pure function over already-loaded slices so it can be unit-tested
// without a database and reused by the dashboard summary counter.

// HostsWithoutBackup returns the hosts that are not linked to any backup entity
// by a relationship, in either direction — the host analogue of
// ServicesWithoutBackup.
func HostsWithoutBackup(hosts []Host, rels []Relationship) []Host {
	backedUp := map[int64]bool{}
	for _, r := range rels {
		if r.FromType == "host" && r.ToType == "backup" {
			backedUp[r.FromID] = true
		}
		if r.ToType == "host" && r.FromType == "backup" {
			backedUp[r.ToID] = true
		}
	}
	out := []Host{}
	for _, h := range hosts {
		if !backedUp[h.ID] {
			out = append(out, h)
		}
	}
	return out
}

// ServicesWithoutHost returns the services that are not linked to any host by a
// relationship, in either direction — a service floating without the host it
// runs on.
func ServicesWithoutHost(services []Service, rels []Relationship) []Service {
	hosted := map[int64]bool{}
	for _, r := range rels {
		if r.FromType == "service" && r.ToType == "host" {
			hosted[r.FromID] = true
		}
		if r.ToType == "service" && r.FromType == "host" {
			hosted[r.ToID] = true
		}
	}
	out := []Service{}
	for _, s := range services {
		if !hosted[s.ID] {
			out = append(out, s)
		}
	}
	return out
}

// ExpiredCertificates returns the certificates whose ExpiresOn date has already
// passed (on or before now), sorted by ExpiresOn ascending. Certificates with
// an empty or unparseable ExpiresOn are skipped. Unlike ExpiringSoon this is
// backward-looking: only certificates that are already expired, never those
// merely due soon.
func ExpiredCertificates(certs []Certificate, now time.Time) []Certificate {
	return expiringOnOrBefore(certs, now, 0, func(c Certificate) string { return c.ExpiresOn })
}

// HardwareWithoutWarranty returns the hardware with no WarrantyEnd recorded —
// items whose warranty status cannot be tracked at all.
func HardwareWithoutWarranty(hw []Hardware) []Hardware {
	out := []Hardware{}
	for _, h := range hw {
		if h.WarrantyEnd == "" {
			out = append(out, h)
		}
	}
	return out
}

// SubscriptionsWithoutRenewal returns the subscriptions with no RenewalDate
// recorded — items whose renewal can never surface in the renewals-due check.
func SubscriptionsWithoutRenewal(subs []Subscription) []Subscription {
	out := []Subscription{}
	for _, s := range subs {
		if s.RenewalDate == "" {
			out = append(out, s)
		}
	}
	return out
}

// LinkedRefs returns the set of entity references that appear as an endpoint of
// at least one relationship, in either direction. It is the basis for detecting
// entities linked to nothing.
func LinkedRefs(rels []Relationship) map[EntityRef]bool {
	linked := map[EntityRef]bool{}
	for _, r := range rels {
		linked[EntityRef{Type: r.FromType, ID: r.FromID}] = true
		linked[EntityRef{Type: r.ToType, ID: r.ToID}] = true
	}
	return linked
}

// UnlinkedRefs returns the subset of refs that participate in no relationship,
// preserving the input order. Callers pass whichever references they consider
// candidates (e.g. every entity, or every certificate).
func UnlinkedRefs(refs []EntityRef, rels []Relationship) []EntityRef {
	linked := LinkedRefs(rels)
	out := []EntityRef{}
	for _, ref := range refs {
		if !linked[ref] {
			out = append(out, ref)
		}
	}
	return out
}
