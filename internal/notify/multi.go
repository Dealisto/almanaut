package notify

import (
	"context"
	"errors"
	"sync"
)

// multiSender fans a notification out to every configured channel (ntfy,
// Discord, …). Channels start independently so a slow receiver cannot exhaust
// the shared deadline before a healthy receiver is attempted. It waits for all
// senders and returns the joined error of any that failed.
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
	errs := make([]error, len(m.senders))
	var workers sync.WaitGroup
	for i, s := range m.senders {
		workers.Go(func() {
			defer func() {
				if recover() != nil {
					// Channel work is outside the task runner's panic boundary.
					// A panic value may contain credentials; do not reflect it.
					errs[i] = errors.New("notification channel panicked")
				}
			}()
			errs[i] = s.Send(ctx, n)
		})
	}
	workers.Wait()
	return errors.Join(errs...)
}
