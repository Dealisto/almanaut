package web

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestEntitySaveRejectsCustomFieldDefinitionsChangedAfterParsing(t *testing.T) {
	for _, change := range []string{"deleted", "different entity type", "different value kind"} {
		t.Run(change, func(t *testing.T) {
			rs, deps, dispatcher := canonicalHostResource(t)
			original := domain.Host{Name: "Original", Type: "physical"}
			id, err := rs.createEntity(deps, original, nil, "alice")
			if err != nil {
				t.Fatal(err)
			}
			defID, err := deps.customFields.CreateDef(domain.CustomFieldDef{EntityType: "host", Name: "owner", Label: "Owner", Kind: domain.KindText})
			if err != nil {
				t.Fatal(err)
			}
			values, err := deps.parseJSONCustomFields("host", map[string]json.RawMessage{"owner": json.RawMessage(`"Alice"`)})
			if err != nil {
				t.Fatal(err)
			}
			// This is the real ordering in API/form handlers: parse definitions,
			// then acquire the mutation transaction. A definition can be deleted
			// or replaced by an import between those two steps.
			switch change {
			case "deleted":
				err = deps.customFields.DeleteDef(defID)
			case "different entity type":
				_, err = deps.db.Exec(`UPDATE custom_field_definitions SET entity_type='service' WHERE id=?`, defID)
			case "different value kind":
				_, err = deps.db.Exec(`UPDATE custom_field_definitions SET kind='number' WHERE id=?`, defID)
			}
			if err != nil {
				t.Fatal(err)
			}
			original.ID, original.Name = id, "Must not commit"
			err = rs.updateEntity(deps, original, values, "alice")
			var constraint *store.ConstraintError
			if !errors.As(err, &constraint) {
				t.Fatalf("stale definition accepted: error=%v", err)
			}
			got, err := rs.repo.Get(id)
			if err != nil || got.Name != "Original" {
				t.Fatalf("rejected save changed host: %+v %v", got, err)
			}
			stored, err := deps.customFields.ListAllValues()
			if err != nil || len(stored) != 0 {
				t.Fatalf("stale save left invalid/orphan values: %+v %v", stored, err)
			}
			if events := dispatcher.all(); len(events) != 1 {
				t.Fatalf("rejected save emitted events: %+v", events)
			}
		})
	}
}
