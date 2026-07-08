package store

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func newCustomFieldRepo(t *testing.T) *CustomFieldRepo {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db, dbPath); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return NewCustomFieldRepo(db)
}

func TestCustomFieldDefCRUD(t *testing.T) {
	repo := newCustomFieldRepo(t)
	id, err := repo.CreateDef(domain.CustomFieldDef{
		EntityType: "host", Name: "asset_tag", Label: "Asset tag",
		Kind: domain.KindText, CreatedAt: "2026-07-08T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("CreateDef: %v", err)
	}
	defs, err := repo.ListDefs("host")
	if err != nil || len(defs) != 1 || defs[0].Label != "Asset tag" {
		t.Fatalf("ListDefs: %+v, %v", defs, err)
	}
	if err := repo.UpdateDefLabel(id, "Asset ID"); err != nil {
		t.Fatalf("UpdateDefLabel: %v", err)
	}
	defs, _ = repo.ListDefs("host")
	if defs[0].Label != "Asset ID" {
		t.Fatalf("label not updated: %q", defs[0].Label)
	}
	if err := repo.UpdateDefLabel(9999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateDefLabel missing should be ErrNotFound, got %v", err)
	}
	// a def for another entity type must not show up
	if _, err := repo.CreateDef(domain.CustomFieldDef{
		EntityType: "service", Name: "asset_tag", Label: "X", Kind: domain.KindText, CreatedAt: "t",
	}); err != nil {
		t.Fatalf("CreateDef service: %v", err)
	}
	if defs, _ := repo.ListDefs("host"); len(defs) != 1 {
		t.Fatalf("host defs leaked service def: %+v", defs)
	}
}

func TestCustomFieldSetListDelete(t *testing.T) {
	repo := newCustomFieldRepo(t)
	tagID, _ := repo.CreateDef(domain.CustomFieldDef{EntityType: "host", Name: "asset_tag", Label: "Asset tag", Kind: domain.KindText, CreatedAt: "t"})
	wattID, _ := repo.CreateDef(domain.CustomFieldDef{EntityType: "host", Name: "watts", Label: "Watts", Kind: domain.KindNumber, CreatedAt: "t"})

	// set two values
	if err := repo.SetForEntity("host", 1, map[int64]string{tagID: "ABC-1", wattID: "42"}); err != nil {
		t.Fatalf("SetForEntity: %v", err)
	}
	vals, err := repo.ListForEntity("host", 1)
	if err != nil || len(vals) != 2 {
		t.Fatalf("ListForEntity: %+v, %v", vals, err)
	}
	// empty value deletes the row; non-empty upserts
	if err := repo.SetForEntity("host", 1, map[int64]string{tagID: "", wattID: "50"}); err != nil {
		t.Fatalf("SetForEntity 2: %v", err)
	}
	vals, _ = repo.ListForEntity("host", 1)
	if len(vals) != 1 || vals[0].Name != "watts" || vals[0].Value != "50" {
		t.Fatalf("after empty+upsert: %+v", vals)
	}
	// nil map is a no-op
	if err := repo.SetForEntity("host", 1, nil); err != nil {
		t.Fatalf("SetForEntity nil: %v", err)
	}
	if vals, _ := repo.ListForEntity("host", 1); len(vals) != 1 {
		t.Fatalf("nil map changed values: %+v", vals)
	}
	// DeleteByEntity clears remaining
	if err := repo.DeleteByEntity("host", 1); err != nil {
		t.Fatalf("DeleteByEntity: %v", err)
	}
	if vals, _ := repo.ListForEntity("host", 1); len(vals) != 0 {
		t.Fatalf("DeleteByEntity left rows: %+v", vals)
	}
}

func TestCustomFieldDeleteDefCascadesValues(t *testing.T) {
	repo := newCustomFieldRepo(t)
	id, _ := repo.CreateDef(domain.CustomFieldDef{EntityType: "host", Name: "asset_tag", Label: "Asset tag", Kind: domain.KindText, CreatedAt: "t"})
	_ = repo.SetForEntity("host", 1, map[int64]string{id: "ABC"})
	if err := repo.DeleteDef(id); err != nil {
		t.Fatalf("DeleteDef: %v", err)
	}
	if defs, _ := repo.ListDefs("host"); len(defs) != 0 {
		t.Fatalf("def not deleted: %+v", defs)
	}
	if vals, _ := repo.ListForEntity("host", 1); len(vals) != 0 {
		t.Fatalf("values not cascaded: %+v", vals)
	}
}
