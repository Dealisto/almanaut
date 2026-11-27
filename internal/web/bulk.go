package web

import (
	"database/sql"
	"encoding/json"
	"errors"
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
		// Query parameters must not add unchecked rows or supply an action
		// that was absent from the submitted form.
		action := req.PostFormValue("action")
		ids, err := parseIDs(req.PostForm["ids"])
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(ids) == 0 {
			// Nothing selected: nothing to do, just return to the list.
			http.Redirect(w, req, rs.listReturnPath(req), http.StatusSeeOther)
			return
		}

		// Validate action-specific inputs up front so bad input is a 400, not a
		// rolled-back 500.
		tag := strings.TrimSpace(req.PostFormValue("tag"))
		field := strings.TrimSpace(req.PostFormValue("field"))
		value := req.PostFormValue("value")
		switch action {
		case "tag-add", "tag-remove":
			if domain.NormalizeTag(tag) == "" {
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
		err = store.WithTx(d.db, func(tx *sql.Tx) error {
			batchDeps := d
			batchDeps.webhooks = webhook.ForTransaction(tx, d.webhooks)
			for _, id := range ids {
				var err error
				switch action {
				case "delete":
					err = rs.deleteEntityTx(tx, batchDeps, id, actor, &events)
				case "tag-add", "tag-remove":
					err = rs.bulkTag(tx, batchDeps, id, action, tag, actor)
				case "set-field":
					err = rs.bulkSetField(tx, batchDeps, id, field, value, actor, &events)
				}
				if err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			var validation bulkValidationError
			if errors.As(err, &validation) {
				http.Error(w, validation.Error(), http.StatusBadRequest)
			} else if c := constraintError(err); c != nil {
				http.Error(w, c.Error(), http.StatusBadRequest)
			} else {
				notFoundOrServerError(w, req, rs.sing, err)
			}
			return
		}
		d.webhooks.Dispatch(events...)
		http.Redirect(w, req, rs.listReturnPath(req), http.StatusSeeOther)
	}
}

// listReturnPath preserves the originating list's filters, sort and page after
// a bulk action. Only query state on this resource's canonical route is accepted;
// submitted external URLs or another resource's route cannot become redirects.
func (rs resource[T]) listReturnPath(req *http.Request) string {
	return inventoryListReturnPath(rs.basePath(), req.PostFormValue("return_to"))
}

// bulkValidationError distinguishes invalid submitted values from storage
// failures while preserving the all-or-nothing transaction boundary.
type bulkValidationError struct{ error }

// bulkTag adds or removes a tag on one entity and records the change in history.
func (rs resource[T]) bulkTag(tx *sql.Tx, d handlerDeps, id int64, action, tag, actor string) error {
	item, err := rs.repo.GetTx(tx, id)
	if err != nil {
		return err
	}
	canonicalTag := domain.NormalizeTag(tag)
	tags := d.tags.WithTx(tx)
	existing, err := tags.ListForEntity(rs.sing, id)
	if err != nil {
		return err
	}
	present := slices.ContainsFunc(existing, func(t domain.Tag) bool { return t.Name == canonicalTag })
	if (action == "tag-add" && present) || (action == "tag-remove" && !present) {
		return nil // No change: preserve history and the entity's activity clock.
	}
	var old, new string
	if action == "tag-add" {
		t := domain.Tag{EntityType: rs.sing, EntityID: id, Name: tag}
		if err := t.Validate(); err != nil {
			return err
		}
		if err := tags.Add(t); err != nil {
			return err
		}
		new = canonicalTag
	} else {
		if err := tags.RemoveByName(rs.sing, id, tag); err != nil {
			return err
		}
		old = canonicalTag
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
		return bulkValidationError{err}
	}
	// nil custom-field map: SetForEntity only touches keys it is given, so the
	// entity's existing custom-field values are preserved.
	return rs.updateEntityTx(tx, d, updated, nil, actor, events)
}

// parseIDs validates the entire selection before any mutation and removes
// duplicates so every selected entity is changed exactly once.
func parseIDs(raw []string) ([]int64, error) {
	out := make([]int64, 0, len(raw))
	seen := make(map[int64]bool, len(raw))
	for _, s := range raw {
		id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("selected entity ids must be positive integers")
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

// mergeField returns a copy of item with one JSON field set to value, via a
// JSON round-trip that preserves the exact encoding of untouched fields. Only
// string fields succeed: assigning a string to a numeric,
// boolean, or slice field fails to unmarshal and returns an error, which is why
// the handler restricts the field picker to string fields.
func mergeField[T any](item T, key, value string) (T, error) {
	var out T
	raw, err := json.Marshal(item)
	if err != nil {
		return out, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return out, err
	}
	if _, ok := m[key]; !ok {
		return out, fmt.Errorf("unknown field %q", key)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return out, err
	}
	m[key] = encoded
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
