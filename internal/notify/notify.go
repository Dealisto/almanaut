// Package notify sends expiry notifications (ntfy) for certificates, hardware
// warranties, and subscription renewals, at most once per item until it is
// renewed or deleted.
package notify

// Kind classifies an expiring entity.
type Kind string

const (
	KindCertificate  Kind = "certificate"
	KindHardware     Kind = "hardware"
	KindSubscription Kind = "subscription"
)

// Item is one entity expiring within the configured window.
type Item struct {
	Kind  Kind
	ID    int64
	Label string // certificate subject, hardware/subscription name
	Date  string // YYYY-MM-DD expiry/warranty/renewal date
}

// Key identifies an item independent of its label or date.
type Key struct {
	Kind Kind
	ID   int64
}

// Key returns the item's identity key.
func (i Item) Key() Key { return Key{Kind: i.Kind, ID: i.ID} }

// decide compares the currently-expiring items against the set already
// notified. It returns the items to notify now (expiring but not yet notified)
// and the keys to clear (previously notified but no longer expiring — renewed
// or deleted).
func decide(expiring []Item, sent map[Key]bool) (toNotify []Item, toClear []Key) {
	current := make(map[Key]bool, len(expiring))
	for _, it := range expiring {
		current[it.Key()] = true
		if !sent[it.Key()] {
			toNotify = append(toNotify, it)
		}
	}
	for k := range sent {
		if !current[k] {
			toClear = append(toClear, k)
		}
	}
	return toNotify, toClear
}
