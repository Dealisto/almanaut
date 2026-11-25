package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/webhook"
)

func TestAgentReportRetriesDispatchOnlyCommittedEvents(t *testing.T) {
	for _, update := range []bool{false, true} {
		action := webhook.ActionCreated
		if update {
			action = webhook.ActionUpdated
		}
		t.Run(action, func(t *testing.T) {
			remaining := 0
			d, _, dispatcher := agentDepsWithFake(t, fakeAgentRepo{busyRemaining: &remaining})
			report := baseReport()
			if update {
				if rec := postReportDirect(d, report); rec.Code != http.StatusOK {
					t.Fatalf("initial report = %d: %s", rec.Code, rec.Body)
				}
				report.RAM = "128 GB"
			}
			before := len(dispatcher.all())
			remaining = 2 // Both rolled-back attempts have already queued an event.
			if rec := postReportDirect(d, report); rec.Code != http.StatusOK {
				t.Fatalf("retried report = %d: %s", rec.Code, rec.Body)
			}
			if remaining != 0 {
				t.Fatalf("remaining failures = %d, want 0", remaining)
			}
			hosts, err := d.hosts.List()
			if err != nil || len(hosts) != 1 {
				t.Fatalf("hosts = %+v, err = %v; want one committed host", hosts, err)
			}
			events := dispatcher.all()[before:]
			if len(events) != 1 {
				t.Fatalf("dispatched %d events for one committed change, want 1", len(events))
			}
			event := events[0]
			if event.Action != action || event.ID != hosts[0].ID {
				t.Fatalf("event = %+v, want %s host/%d", event, action, hosts[0].ID)
			}
			var payload domain.Host
			if err := json.Unmarshal(event.Data, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.ID != hosts[0].ID || payload.RAM != report.RAM || hosts[0].RAM != report.RAM {
				t.Fatalf("event payload = %+v, committed host = %+v", payload, hosts[0])
			}
		})
	}
}
