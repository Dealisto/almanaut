package kuma

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestConnectRejectsInvalidMonitorList(t *testing.T) {
	for _, payload := range []string{
		`null`,
		`{"invalid":{"name":"existing"}}`,
		`{"0":{"name":"existing"}}`,
		`{"-1":{"name":"existing"}}`,
		`{"1":null}`,
		`{"1":{"name":42}}`,
		`{"1":{"name":"existing","url":42}}`,
		`[{"id":1,"name":"existing"}]`,
	} {
		t.Run(payload, func(t *testing.T) {
			f := newFakeKuma(t)
			f.monitorListOverride = payload
			s, err := NewClient(f.url(), f.user, f.pass, false).Connect(testCtx(t))
			if s != nil {
				s.Close()
			}
			if err == nil || errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "monitorList") {
				t.Fatalf("invalid list must fail before reconciliation: session=%v err=%v", s != nil, err)
			}
		})
	}
}

func TestConnectAcceptsEmptyMonitorLists(t *testing.T) {
	for _, payload := range []string{`{}`, `[]`, "[ \n ]"} {
		t.Run(payload, func(t *testing.T) {
			f := newFakeKuma(t)
			f.monitorListOverride = payload
			s, err := NewClient(f.url(), f.user, f.pass, false).Connect(testCtx(t))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if len(s.Monitors()) != 0 {
				t.Fatal("empty list contains monitors")
			}
		})
	}
}
