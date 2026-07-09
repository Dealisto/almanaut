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
	return inSetOrEmpty(w.EntityTypes, entityType) && inSetOrEmpty(w.Events, action)
}

func inSetOrEmpty(set []string, v string) bool {
	if len(set) == 0 {
		return true
	}
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}
