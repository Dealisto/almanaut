// Package webhook delivers outbound HTTP notifications when almanaut entities
// change. Delivery is in-memory and non-blocking; see Queue.
package webhook

import (
	"encoding/json"
	"fmt"
)

// Wire values for the "event" field and webhook.Event.Action.
const (
	ActionCreated = "created"
	ActionUpdated = "updated"
	ActionDeleted = "deleted"
)

// Event is a single committed entity change awaiting delivery.
type Event struct {
	Type      string          // entity type, e.g. "host"
	ID        int64           // entity id
	Action    string          // ActionCreated | ActionUpdated | ActionDeleted
	Actor     string          // changelog attribution (username / token label)
	Timestamp string          // RFC3339, same instant recorded in the changelog
	Data      json.RawMessage // entity JSON; nil for deletes (payload omits it)
}

// NewEvent builds an Event, marshalling data to JSON. Pass data == nil for a
// delete so the delivered payload omits the "data" field.
func NewEvent(entityType string, id int64, action, actor, timestamp string, data any) (Event, error) {
	e := Event{Type: entityType, ID: id, Action: action, Actor: actor, Timestamp: timestamp}
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return Event{}, fmt.Errorf("marshal webhook data: %w", err)
		}
		e.Data = b
	}
	return e, nil
}
