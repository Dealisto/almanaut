// Package notify sends expiry notifications for certificates, hardware
// warranties, and subscription renewals to one or more channels (ntfy,
// Discord), at most once per item until it is renewed or deleted.
package notify

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

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

// Notifier gathers expiring certificates, hardware warranties, and subscription
// renewals, and sends each a notification (via the configured Sender) at most
// once until it is renewed or deleted.
type Notifier struct {
	certs      *store.CertificateRepo
	hw         *store.HardwareRepo
	subs       *store.SubscriptionRepo
	state      *store.NotificationRepo
	sender     Sender
	withinDays int
}

// New builds a Notifier. withinDays is the "expiring soon" window.
func New(certs *store.CertificateRepo, hw *store.HardwareRepo, subs *store.SubscriptionRepo, state *store.NotificationRepo, sender Sender, withinDays int) *Notifier {
	return &Notifier{certs: certs, hw: hw, subs: subs, state: state, sender: sender, withinDays: withinDays}
}

// gather returns every entity expiring within the window, across all kinds.
func (n *Notifier) gather(now time.Time) ([]Item, error) {
	var items []Item

	certs, err := n.certs.List()
	if err != nil {
		return nil, fmt.Errorf("list certificates: %w", err)
	}
	for _, c := range domain.ExpiringSoon(certs, now, n.withinDays) {
		items = append(items, Item{Kind: KindCertificate, ID: c.ID, Label: c.Subject, Date: c.ExpiresOn})
	}

	hw, err := n.hw.List()
	if err != nil {
		return nil, fmt.Errorf("list hardware: %w", err)
	}
	for _, h := range domain.WarrantyExpiring(hw, now, n.withinDays) {
		items = append(items, Item{Kind: KindHardware, ID: h.ID, Label: h.Name, Date: h.WarrantyEnd})
	}

	subs, err := n.subs.List()
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	for _, s := range domain.RenewalsDue(subs, now, n.withinDays) {
		items = append(items, Item{Kind: KindSubscription, ID: s.ID, Label: s.Name, Date: s.RenewalDate})
	}
	return items, nil
}

// Run performs one full pass: notify newly-expiring items, clear items that are
// no longer expiring. A single failed send is logged and skipped (its state is
// left unmarked so it retries next pass); it does not abort the pass.
//
// Deliberately not transactional: Run is the sole writer of notification_state
// and runs single-threaded, so a tx would just hold a DB tx open across Send.
func (n *Notifier) Run(ctx context.Context, now time.Time) error {
	expiring, err := n.gather(now)
	if err != nil {
		return err
	}
	sentRaw, err := n.state.Sent()
	if err != nil {
		return err
	}
	sent := make(map[Key]bool, len(sentRaw))
	for k := range sentRaw {
		sent[Key{Kind: Kind(k.Kind), ID: k.ID}] = true
	}

	toNotify, toClear := decide(expiring, sent)

	for _, k := range toClear {
		if err := n.state.Clear(string(k.Kind), k.ID); err != nil {
			return err
		}
	}
	for _, it := range toNotify {
		if err := n.sender.Send(ctx, message(it, now)); err != nil {
			log.Printf("notify: send %s %d: %v", it.Kind, it.ID, err)
			continue
		}
		if err := n.state.Mark(string(it.Kind), it.ID, now); err != nil {
			return err
		}
	}
	return nil
}

var titles = map[Kind]string{
	KindCertificate:  "Certificate expiring",
	KindHardware:     "Warranty expiring",
	KindSubscription: "Subscription renewal due",
}

// message renders the ntfy Notification for an item.
func message(it Item, now time.Time) Notification {
	return Notification{
		Title: titles[it.Kind],
		Body:  fmt.Sprintf("%s — %s (%s)", it.Label, humanizeWhen(it.Date, now), it.Date),
		Tags:  "warning",
	}
}

// humanizeWhen renders a coarse "expires in N days" phrase from a YYYY-MM-DD
// date relative to now.
func humanizeWhen(date string, now time.Time) string {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return "expiring soon"
	}
	days := int(d.Sub(now).Hours() / 24)
	switch {
	case days < 0:
		return "expired"
	case days == 0:
		return "expires today"
	case days == 1:
		return "expires in 1 day"
	default:
		return fmt.Sprintf("expires in %d days", days)
	}
}
