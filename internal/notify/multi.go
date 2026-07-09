package notify

import (
	"context"
	"errors"
)

// multiSender fans a notification out to every configured channel (ntfy,
// Discord, …). It always attempts every sender — a failure in one does not
// short-circuit the others — and returns the joined error of any that failed.
//
// The Notifier's at-most-once state is per item and independent of the channel
// count: Run marks an item sent only when Send returns nil, i.e. once it has
// reached every channel. If one channel fails the item stays unmarked and the
// whole set is retried next pass, so a healthy channel may see a duplicate —
// the same per-send retry policy the single-channel path already has.
type multiSender struct {
	senders []Sender
}

// NewMultiSender returns a Sender that fans out to all of senders. With a single
// sender it behaves as that sender (wrapped); with none it is a no-op success.
func NewMultiSender(senders ...Sender) Sender {
	return &multiSender{senders: senders}
}

func (m *multiSender) Send(ctx context.Context, n Notification) error {
	var errs []error
	for _, s := range m.senders {
		if err := s.Send(ctx, n); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
