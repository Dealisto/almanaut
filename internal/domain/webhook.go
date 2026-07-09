package domain

// Webhook is a registered outbound endpoint. EntityTypes and Events are filters:
// an empty slice means "match every type" / "match every action".
type Webhook struct {
	ID          int64
	URL         string
	Secret      string
	Enabled     bool
	EntityTypes []string // e.g. {"host","service"}; empty = all types
	Events      []string // subset of {"created","updated","deleted"}; empty = all
	CreatedAt   string
}

// Matches reports whether this webhook should receive an event for the given
// entity type and action ("created"/"updated"/"deleted").
func (w Webhook) Matches(entityType, action string) bool {
	return (len(w.EntityTypes) == 0 || contains(w.EntityTypes, entityType)) &&
		(len(w.Events) == 0 || contains(w.Events, action))
}
