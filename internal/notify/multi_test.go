package notify

import (
	"context"
	"errors"
	"testing"
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
