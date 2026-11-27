package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestImportClassifiesValidationAndStorageFailures(t *testing.T) {
	db := newTestDB(t)
	for _, snap := range []Snapshot{
		{Version: 0},
		{Version: 999},
		{Version: 1, Hosts: []domain.Host{{ID: 1, Name: "invalid", Type: "vm", IPs: []string{"bad address"}}}},
	} {
		var validation *ImportValidationError
		if err := Import(db, snap); !errors.As(err, &validation) {
			t.Errorf("invalid input not classified: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	var validation *ImportValidationError
	if err := Import(db, Snapshot{Version: 1}); err == nil || errors.As(err, &validation) {
		t.Errorf("storage failure classified as input error: %v", err)
	}
}

func TestImportRejectsConflictingNaturalKeys(t *testing.T) {
	for _, kind := range []string{"tag", "custom field definition", "custom field value"} {
		t.Run(kind, func(t *testing.T) {
			db := newTestDB(t)
			if _, err := NewHostRepo(db).Create(domain.Host{Name: "keep", Type: "vm"}); err != nil {
				t.Fatal(err)
			}
			snap := Snapshot{
				Version: 1,
				Hosts:   []domain.Host{{ID: 99, Name: "replacement", Type: "vm"}},
			}
			switch kind {
			case "tag":
				snap.Tags = []domain.Tag{
					{ID: 1, EntityType: "host", EntityID: 99, Name: "prod"},
					{ID: 2, EntityType: "host", EntityID: 99, Name: "#PROD"},
				}
			case "custom field definition":
				snap.CustomFieldDefs = []domain.CustomFieldDef{
					{ID: 1, EntityType: "host", Name: "owner", Label: "Owner", Kind: domain.KindText},
					{ID: 2, EntityType: "host", Name: "owner", Label: "Other owner", Kind: domain.KindText},
				}
			case "custom field value":
				snap.CustomFieldDefs = []domain.CustomFieldDef{{ID: 1, EntityType: "host", Name: "owner", Label: "Owner", Kind: domain.KindText}}
				snap.CustomFieldValues = []domain.CustomFieldValueRow{
					{ID: 1, EntityType: "host", EntityID: 99, DefID: 1, Value: "alice"},
					{ID: 2, EntityType: "host", EntityID: 99, DefID: 1, Value: "bob"},
				}
			}
			var validation *ImportValidationError
			err := Import(db, snap)
			if !errors.As(err, &validation) || !strings.Contains(err.Error(), kind+" 2 duplicates "+kind+" 1") {
				t.Fatalf("conflicting keys need an actionable validation error: %v", err)
			}
			hosts, err := NewHostRepo(db).List()
			if err != nil || len(hosts) != 1 || hosts[0].Name != "keep" {
				t.Fatalf("conflicting keys replaced inventory: %+v, %v", hosts, err)
			}
		})
	}
}
