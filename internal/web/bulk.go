package web

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
	"github.com/Dealisto/almanaut/internal/webhook"
)

// bulk applies one action to every selected entity in a single transaction, so
// a failure on any row rolls the whole batch back. Selection comes from repeated
// "ids" form values (the row checkboxes); the action from the submit button's
// name=action value. Writer-gated by the route group; the toolbar is hidden from
// non-writers. Every action records per-entity history.
func (rs resource[T]) bulk(d handlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if err := req.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		action := req.FormValue("action")
		ids := parseIDs(req.Form["ids"])
		if len(ids) == 0 {
			// Nothing selected: nothing to do, just return to the list.
			http.Redirect(w, req, rs.basePath(), http.StatusSeeOther)
			return
		}

		// Validate action-specific inputs up front so bad input is a 400, not a
		// rolled-back 500.
		tag := strings.TrimSpace(req.FormValue("tag"))
		field := strings.TrimSpace(req.FormValue("field"))
		value := req.FormValue("value")
		switch action {
		case "tag-add", "tag-remove":
			if tag == "" {
				http.Error(w, "a tag name is required", http.StatusBadRequest)
				return
			}
		case "set-field":
			if !rs.isStringField(field) {
				http.Error(w, "cannot bulk-set field "+field, http.StatusBadRequest)
				return
			}
		case "delete":
			// no extra input
		default:
			http.Error(w, "unknown bulk action", http.StatusBadRequest)
			return
		}

		actor := actor(req)
		var events []webhook.Event
		err := store.WithTx(d.db, func(tx *sql.Tx) error {
			for _, id := range ids {
				var err error
				switch action {
				case "delete":
					err = rs.deleteEntityTx(tx, d, id, actor, &events)
				case "tag-add", "tag-remove":
					err = rs.bulkTag(tx, d, id, action, tag, actor)
				case "set-field":
					err = rs.bulkSetField(tx, d, id, field, value, actor, &events)
				}
				if err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			serverError(w, req, err)
			return
		}
		d.webhooks.Dispatch(events...)
		http.Redirect(w, req, rs.basePath(), http.StatusSeeOther)
	}
}

// bulkTag adds or removes a tag on one entity and records the change in history.
func (rs resource[T]) bulkTag(tx *sql.Tx, d handlerDeps, id int64, action, tag, actor string) error {
	item, err := rs.repo.GetTx(tx, id)
	if err != nil {
		return err
	}
	var old, new string
	if action == "tag-add" {
		t := domain.Tag{EntityType: rs.sing, EntityID: id, Name: tag}
		if err := t.Validate(); err != nil {
			return err
		}
		if err := d.tags.WithTx(tx).Add(t); err != nil {
			return err
		}
		new = tag
	} else {
		if err := d.tags.WithTx(tx).RemoveByName(rs.sing, id, tag); err != nil {
			return err
		}
		old = tag
	}
	return d.changelog.WithTx(tx).Create(store.ChangeEvent{
		EntityType: rs.sing, EntityID: id, Label: rs.label(item),
		Action: domain.ActionUpdate, Actor: actor,
		Changes:   []domain.FieldChange{{Field: "tags", Old: old, New: new}},
		CreatedAt: nowRFC3339(),
	})
}

// bulkSetField overwrites one string field on an entity, leaving all its other
// typed and custom-field values intact, and records the diff via updateEntityTx.
func (rs resource[T]) bulkSetField(tx *sql.Tx, d handlerDeps, id int64, field, value, actor string, events *[]webhook.Event) error {
	old, err := rs.repo.GetTx(tx, id)
	if err != nil {
		return err
	}
	updated, err := mergeField(old, field, value)
	if err != nil {
		return err
	}
	rs.setID(&updated, id)
	if err := updated.Validate(); err != nil {
		return err
	}
	// nil custom-field map: SetForEntity only touches keys it is given, so the
	// entity's existing custom-field values are preserved.
	return rs.updateEntityTx(tx, d, updated, nil, actor, events)
}

// parseIDs turns the repeated "ids" form values into int64s, skipping malformed
// ones.
func parseIDs(raw []string) []int64 {
	out := make([]int64, 0, len(raw))
	for _, s := range raw {
		if id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// mergeField returns a copy of item with one JSON field set to value, via a
// JSON round-trip. Only string fields succeed: assigning a string to a numeric,
// boolean, or slice field fails to unmarshal and returns an error, which is why
// the handler restricts the field picker to string fields.
func mergeField[T any](item T, key, value string) (T, error) {
	var out T
	raw, err := json.Marshal(item)
	if err != nil {
		return out, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return out, err
	}
	if _, ok := m[key]; !ok {
		return out, fmt.Errorf("unknown field %q", key)
	}
	m[key] = value
	raw2, err := json.Marshal(m)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw2, &out); err != nil {
		return out, fmt.Errorf("cannot set %s to %q: %w", key, value, err)
	}
	return out, nil
}

// stringFields returns the JSON keys of this entity's string-typed fields, the
// only ones bulk set-field can safely write. Derived from the zero value so it
// is stable and DB-free.
func (rs resource[T]) stringFields() []string {
	raw, err := json.Marshal(rs.newItem)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	var keys []string
	for k, v := range m {
		if k == "id" {
			continue
		}
		if _, ok := v.(string); ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func (rs resource[T]) isStringField(key string) bool {
	return slices.Contains(rs.stringFields(), key)
}
