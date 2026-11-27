package kuma

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEditPreservesUnmanagedJSONNumbers(t *testing.T) {
	f := newFakeKuma(t)
	id := f.putMonitor("old", "http://old.lan")
	f.monitorListOverride = `{"1":{"name":"old","url":"http://old.lan","externalSettings":{"identifier":9007199254740993,"ratio":0.1234567890123456789}}}`
	f.editPayload = make(chan json.RawMessage, 1)
	s, err := NewClient(f.url(), f.user, f.pass, false).Connect(testCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	monitor := s.Monitors()[id]
	monitor.Name = "new"
	if err := s.Edit(testCtx(t), monitor); err != nil {
		t.Fatal(err)
	}
	body := <-f.editPayload
	for _, expected := range []string{`"identifier":9007199254740993`, `"ratio":0.1234567890123456789`, `"name":"new"`} {
		if !strings.Contains(string(body), expected) {
			t.Errorf("edit changed unmanaged values: %s; want %s", body, expected)
		}
	}
}

func TestMonitorListRejectsAmbiguousNumericIDs(t *testing.T) {
	f := newFakeKuma(t)
	f.monitorListOverride = `{"1":{"name":"first"},"01":{"name":"second"}}`
	s, err := NewClient(f.url(), f.user, f.pass, false).Connect(testCtx(t))
	if s != nil {
		s.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "monitorList") {
		t.Fatalf("ambiguous IDs accepted: %v", err)
	}
}

func TestAddRejectsNegativeMonitorID(t *testing.T) {
	f := newFakeKuma(t)
	f.nextID = -1
	s, err := NewClient(f.url(), f.user, f.pass, false).Connect(testCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if id, err := s.Add(testCtx(t), Monitor{Name: "test", URL: "http://test.lan"}); err == nil || id != 0 {
		t.Fatalf("invalid monitor ID accepted: id=%d err=%v", id, err)
	}
}
