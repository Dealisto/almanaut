package notify

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMultiSenderFansOutToAll(t *testing.T) {
	a, b := &fakeSender{}, &fakeSender{}
	err := NewMultiSender(a, b).Send(context.Background(), Notification{Title: "t", Body: "x"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(a.sent) != 1 || len(b.sent) != 1 {
		t.Fatalf("both channels should receive it: a=%d b=%d", len(a.sent), len(b.sent))
	}
}

func TestMultiSenderTriesEveryChannelDespiteFailure(t *testing.T) {
	failing := &fakeSender{err: errors.New("boom")}
	healthy := &fakeSender{}
	// Failing channel first: the healthy one must still be attempted.
	err := NewMultiSender(failing, healthy).Send(context.Background(), Notification{Body: "x"})
	if err == nil {
		t.Fatal("want error when a channel fails")
	}
	if len(healthy.sent) != 1 {
		t.Fatalf("healthy channel must still receive it, got %d", len(healthy.sent))
	}
}

func TestMultiSenderNoSendersIsNoOp(t *testing.T) {
	if err := NewMultiSender().Send(context.Background(), Notification{Body: "x"}); err != nil {
		t.Fatalf("empty multi-sender should succeed, got %v", err)
	}
}

func TestMultiSenderContainsChannelPanic(t *testing.T) {
	healthy := &fakeSender{}
	panicking := senderFunc(func(context.Context, Notification) error {
		panic("private endpoint credentials")
	})
	err := NewMultiSender(panicking, healthy).Send(context.Background(), Notification{Body: "alert"})
	if err == nil || err.Error() != "notification channel panicked" {
		t.Fatalf("want sanitized panic error, got %v", err)
	}
	if len(healthy.sent) != 1 {
		t.Fatal("channel panic prevented healthy delivery")
	}
}

func TestMultiSenderSlowChannelDoesNotDelayHealthyChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	healthy := make(chan struct{})
	slow := senderFunc(func(ctx context.Context, _ Notification) error {
		<-ctx.Done()
		return ctx.Err()
	})
	fast := senderFunc(func(ctx context.Context, _ Notification) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		close(healthy)
		return nil
	})
	done := make(chan error, 1)
	go func() { done <- NewMultiSender(slow, fast).Send(ctx, Notification{Body: "alert"}) }()
	select {
	case <-healthy:
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("slow channel prevented delivery to a healthy channel")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("slow channel failure lost: %v", err)
	}
}
