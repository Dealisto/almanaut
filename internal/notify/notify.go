// Package notify sends expiry notifications for certificates, hardware
// warranties, and subscription renewals to one or more channels (ntfy,
// Discord), at most once per item until it is renewed or deleted.
package notify

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
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

func (k Key) before(other Key) bool {
	return k.Kind < other.Kind || (k.Kind == other.Kind && k.ID < other.ID)
}

// Key returns the item's identity key.
func (i Item) Key() Key { return Key{Kind: i.Kind, ID: i.ID} }

// decide compares the currently-expiring items against the set already
// notified. It returns the items to notify now (new or changed deadlines)
// and the keys to clear (previously notified but no longer expiring — renewed
// or deleted).
func decide(expiring []Item, sent map[Key]string) (toNotify []Item, toClear []Key) {
	current := make(map[Key]bool, len(expiring))
	for _, it := range expiring {
		current[it.Key()] = true
		date, notified := sent[it.Key()]
		if !notified || strings.TrimSpace(date) != strings.TrimSpace(it.Date) {
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
	runGate    chan struct{}
	next       Key // first item to attempt after an interrupted pass
}

// New builds a Notifier. withinDays is the "expiring soon" window.
func New(certs *store.CertificateRepo, hw *store.HardwareRepo, subs *store.SubscriptionRepo, state *store.NotificationRepo, sender Sender, withinDays int) *Notifier {
	return &Notifier{certs: certs, hw: hw, subs: subs, state: state, sender: sender, withinDays: withinDays, runGate: make(chan struct{}, 1)}
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

// current refreshes a queued candidate after earlier network deliveries. Its
// deadline or label may have changed, or the entity may have been deleted.
// Keep the same domain cutoff rules as gather without holding a transaction
// across notification I/O.
func (n *Notifier) current(key Key, now time.Time) (Item, bool, error) {
	switch key.Kind {
	case KindCertificate:
		c, err := n.certs.Get(key.ID)
		if err != nil {
			return Item{}, false, err
		}
		return Item{Kind: key.Kind, ID: key.ID, Label: c.Subject, Date: c.ExpiresOn}, len(domain.ExpiringSoon([]domain.Certificate{c}, now, n.withinDays)) != 0, nil
	case KindHardware:
		h, err := n.hw.Get(key.ID)
		if err != nil {
			return Item{}, false, err
		}
		return Item{Kind: key.Kind, ID: key.ID, Label: h.Name, Date: h.WarrantyEnd}, len(domain.WarrantyExpiring([]domain.Hardware{h}, now, n.withinDays)) != 0, nil
	case KindSubscription:
		s, err := n.subs.Get(key.ID)
		if err != nil {
			return Item{}, false, err
		}
		return Item{Kind: key.Kind, ID: key.ID, Label: s.Name, Date: s.RenewalDate}, len(domain.RenewalsDue([]domain.Subscription{s}, now, n.withinDays)) != 0, nil
	default:
		return Item{}, false, fmt.Errorf("unknown notification kind %q", key.Kind)
	}
}

// Run performs one full pass: notify newly-expiring items, clear items that are
// no longer expiring. A single failed send is logged and skipped (its state is
// left unmarked so it retries next pass); it does not abort the pass.
// Delivery errors are returned after the remaining items have been attempted.
//
// Passes are serialized without holding a database transaction across Send.
// An interrupted pass resumes at the next stable item identity so slow early
// deliveries cannot indefinitely starve later certificates or other kinds.
func (n *Notifier) Run(ctx context.Context, now time.Time) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case n.runGate <- struct{}{}:
	}
	defer func() { <-n.runGate }()
	if err := ctx.Err(); err != nil {
		return err
	}
	expiring, err := n.gather(now)
	if err != nil {
		return err
	}
	sentRaw, err := n.state.Sent()
	if err != nil {
		return err
	}
	sent := make(map[Key]string, len(sentRaw))
	for k, date := range sentRaw {
		sent[Key{Kind: Kind(k.Kind), ID: k.ID}] = date
	}

	toNotify, toClear := decide(expiring, sent)

	for _, k := range toClear {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := n.state.Clear(string(k.Kind), k.ID); err != nil {
			return err
		}
	}
	sort.Slice(toNotify, func(i, j int) bool { return toNotify[i].Key().before(toNotify[j].Key()) })
	start := sort.Search(len(toNotify), func(i int) bool { return !toNotify[i].Key().before(n.next) })
	if start == len(toNotify) {
		start = 0
	}
	var runErr error
	for i := range toNotify {
		if err := ctx.Err(); err != nil {
			return errors.Join(runErr, err)
		}
		candidate := toNotify[(start+i)%len(toNotify)]
		n.next = toNotify[(start+i+1)%len(toNotify)].Key()
		it, expiring, err := n.current(candidate.Key(), now)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("refresh %s %d notification: %w", candidate.Kind, candidate.ID, err))
			continue
		}
		if !expiring {
			continue
		}
		if date, notified := sent[it.Key()]; notified && strings.TrimSpace(date) == strings.TrimSpace(it.Date) {
			continue // an edit restored the deadline that was already notified
		}
		if err := ctx.Err(); err != nil {
			return errors.Join(runErr, err)
		}
		if err := n.sender.Send(ctx, message(it, now)); err != nil {
			log.Printf("notify: send %s %d: %v", it.Kind, it.ID, err)
			runErr = errors.Join(runErr, fmt.Errorf("send %s %d notification: %w", it.Kind, it.ID, err))
			continue
		}
		if err := n.state.Mark(string(it.Kind), it.ID, strings.TrimSpace(it.Date), now); err != nil {
			return errors.Join(runErr, err)
		}
	}
	return errors.Join(runErr, ctx.Err())
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
	d, err := time.Parse(domain.DateLayout, strings.TrimSpace(date))
	if err != nil {
		return "expiring soon"
	}
	// Compare calendar dates in the caller's timezone, independent of the
	// time of day and daylight-saving transitions.
	year, month, day := now.Date()
	today := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	days := int(d.Sub(today).Hours() / 24)
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
