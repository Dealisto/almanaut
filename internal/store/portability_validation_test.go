package store

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestImportRejectsInvalidCustomFieldValuesWithoutReplacingInventory(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  domain.CustomFieldKind
		value string
	}{
		{"nonnumeric", domain.KindNumber, "not a number"},
		{"nan", domain.KindNumber, "NaN"},
		{"infinite", domain.KindNumber, "+Inf"},
		{"invalid date", domain.KindDate, "2026-02-30"},
		{"boolean typo", domain.KindBool, "tru"},
		{"invalid boolean number", domain.KindBool, "2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			if _, err := NewHostRepo(db).Create(domain.Host{Name: "keep", Type: "vm"}); err != nil {
				t.Fatal(err)
			}
			before, err := Export(db)
			if err != nil {
				t.Fatal(err)
			}
			snap := Snapshot{
				Version:           1,
				Hosts:             []domain.Host{{ID: 99, Name: "replacement", Type: "vm"}},
				CustomFieldDefs:   []domain.CustomFieldDef{{ID: 1, EntityType: "host", Name: "value", Label: "Value", Kind: tc.kind}},
				CustomFieldValues: []domain.CustomFieldValueRow{{ID: 1, EntityType: "host", EntityID: 99, DefID: 1, Value: tc.value}},
			}
			if err := Import(db, snap); err == nil || !strings.Contains(err.Error(), "custom field value 1") {
				t.Errorf("invalid custom field import = %v, want contextual validation error", err)
			}
			after, err := Export(db)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("invalid custom field import changed inventory: err=%v", err)
			}
		})
	}
}

func TestImportInvalidCustomBooleanPreservesStoredTrue(t *testing.T) {
	db := newTestDB(t)
	snap := Snapshot{
		Version:           1,
		Hosts:             []domain.Host{{ID: 1, Name: "keep", Type: "vm"}},
		CustomFieldDefs:   []domain.CustomFieldDef{{ID: 1, EntityType: "host", Name: "enabled", Label: "Enabled", Kind: domain.KindBool}},
		CustomFieldValues: []domain.CustomFieldValueRow{{ID: 1, EntityType: "host", EntityID: 1, DefID: 1, Value: "true"}},
	}
	if err := Import(db, snap); err != nil {
		t.Fatal(err)
	}
	before, err := Export(db)
	if err != nil {
		t.Fatal(err)
	}
	snap.CustomFieldValues[0].Value = "tru"
	if err := Import(db, snap); err == nil || !strings.Contains(err.Error(), "custom field value 1") {
		t.Fatalf("import typo must fail with field context: %v", err)
	}
	after, err := Export(db)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("boolean typo changed existing inventory: error=%v values=%+v", err, after.CustomFieldValues)
	}
}

func TestImportNormalizesCustomFieldValuesWithoutChangingInput(t *testing.T) {
	db := newTestDB(t)
	snap := Snapshot{Version: 1, Hosts: []domain.Host{{ID: 1, Name: "host", Type: "vm"}}}
	for i, tc := range []struct {
		kind domain.CustomFieldKind
		raw  string
	}{
		{domain.KindText, "  description  "},
		{domain.KindNumber, "  42.5  "},
		{domain.KindBool, " YES "},
		{domain.KindDate, " 2026-11-25 "},
	} {
		id := int64(i + 1)
		snap.CustomFieldDefs = append(snap.CustomFieldDefs, domain.CustomFieldDef{ID: id, EntityType: "host", Name: string(tc.kind), Label: string(tc.kind), Kind: tc.kind})
		snap.CustomFieldValues = append(snap.CustomFieldValues, domain.CustomFieldValueRow{ID: id, EntityType: "host", EntityID: 1, DefID: id, Value: tc.raw})
	}
	before := append([]domain.CustomFieldValueRow(nil), snap.CustomFieldValues...)
	if err := Import(db, snap); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snap.CustomFieldValues) {
		t.Fatal("import mutated caller-owned custom field values")
	}
	values, err := NewCustomFieldRepo(db).ListAllValues()
	if err != nil || len(values) != 4 {
		t.Fatalf("imported custom values = %+v, %v", values, err)
	}
	for i, want := range []string{"description", "42.5", "true", "2026-11-25"} {
		if values[i].Value != want {
			t.Errorf("custom value %d = %q, want %q", i+1, values[i].Value, want)
		}
	}
}

func TestImportEmptyCustomFieldsMatchNormalWrites(t *testing.T) {
	db := newTestDB(t)
	snap := Snapshot{Version: 1, Hosts: []domain.Host{{ID: 1, Name: "real host name", Type: "vm"}}}
	for i, kind := range []domain.CustomFieldKind{domain.KindText, domain.KindNumber, domain.KindDate, domain.KindBool} {
		id := int64(i + 1)
		label := string(kind)
		if kind == domain.KindText {
			label = "Name" // A present custom value would override the standard column.
		}
		snap.CustomFieldDefs = append(snap.CustomFieldDefs, domain.CustomFieldDef{ID: id, EntityType: "host", Name: string(kind), Label: label, Kind: kind})
		snap.CustomFieldValues = append(snap.CustomFieldValues, domain.CustomFieldValueRow{ID: id, EntityType: "host", EntityID: 1, DefID: id, Value: " \t "})
	}
	before := append([]domain.CustomFieldValueRow(nil), snap.CustomFieldValues...)
	if err := Import(db, snap); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snap.CustomFieldValues) {
		t.Fatal("import modified caller-owned values")
	}
	repo := NewCustomFieldRepo(db)
	values, err := repo.ListForEntity("host", 1)
	if err != nil || len(values) != 1 || values[0].Kind != domain.KindBool || values[0].Value != "false" {
		t.Fatalf("empty imported values must be absent except canonical false: %+v, %v", values, err)
	}
	// Re-applying the same values through the normal write path must be a no-op.
	if err := repo.SetForEntity("host", 1, map[int64]string{1: "", 2: "", 3: "", 4: "false"}); err != nil {
		t.Fatal(err)
	}
	after, err := repo.ListForEntity("host", 1)
	if err != nil || !reflect.DeepEqual(values, after) {
		t.Fatalf("import and normal write differ: before=%+v after=%+v error=%v", values, after, err)
	}
}

func TestImportStillSkipsDanglingCustomFieldValuesBeforeTypeValidation(t *testing.T) {
	db := newTestDB(t)
	snap := Snapshot{
		Version:         1,
		Hosts:           []domain.Host{{ID: 1, Name: "host", Type: "vm"}},
		Services:        []domain.Service{{ID: 1, Name: "service", Kind: "container"}},
		CustomFieldDefs: []domain.CustomFieldDef{{ID: 1, EntityType: "host", Name: "cost", Label: "Cost", Kind: domain.KindNumber}},
		CustomFieldValues: []domain.CustomFieldValueRow{
			{ID: 1, EntityType: "host", EntityID: 99, DefID: 1, Value: "invalid"},
			{ID: 2, EntityType: "host", EntityID: 1, DefID: 99, Value: "invalid"},
			{ID: 3, EntityType: "service", EntityID: 1, DefID: 1, Value: "invalid"},
		},
	}
	if err := Import(db, snap); err != nil {
		t.Fatal(err)
	}
	if values, err := NewCustomFieldRepo(db).ListAllValues(); err != nil || len(values) != 0 {
		t.Fatalf("dangling custom values survived: %+v, %v", values, err)
	}
}

func TestImportRejectsInvalidRecordIDs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hosts []domain.Host
	}{
		{"zero", []domain.Host{{ID: 0, Name: "invalid", Type: "vm"}}},
		{"negative", []domain.Host{{ID: -1, Name: "invalid", Type: "vm"}}},
		{"duplicate", []domain.Host{{ID: 2, Name: "one", Type: "vm"}, {ID: 2, Name: "two", Type: "vm"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			repo := NewHostRepo(db)
			id, err := repo.Create(domain.Host{Name: "keep", Type: "vm"})
			if err != nil {
				t.Fatal(err)
			}
			if err := Import(db, Snapshot{Version: 1, Hosts: tc.hosts}); err == nil {
				t.Error("import accepted invalid record IDs")
			}
			hosts, err := repo.List()
			if err != nil || len(hosts) != 1 || hosts[0].ID != id || hosts[0].Name != "keep" {
				t.Fatalf("inventory changed: hosts=%+v err=%v", hosts, err)
			}
		})
	}
}

func TestImportRequiresSupportedVersion(t *testing.T) {
	for _, version := range []int{-1, 0, snapshotVersion + 1} {
		db := newTestDB(t)
		if _, err := NewHostRepo(db).Create(domain.Host{Name: "keep", Type: "vm"}); err != nil {
			t.Fatal(err)
		}
		if err := Import(db, Snapshot{Version: version}); err == nil {
			t.Errorf("import accepted version %d", version)
		}
		if n := countRows(t, db, "SELECT COUNT(*) FROM hosts"); n != 1 {
			t.Errorf("version %d changed inventory", version)
		}
	}
}
