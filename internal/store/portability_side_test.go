package store

import (
	"database/sql"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

// notInSnapshot lists every table a Snapshot deliberately does not carry, with
// the reason. A table must be here, in inventoryTables, or in
// derivedStateTables.
var notInSnapshot = map[string]string{
	"schema_migrations":   "migration bookkeeping",
	"users":               "accounts and credentials are per instance",
	"sessions":            "credentials",
	"api_tokens":          "credentials",
	"user_totp":           "credentials",
	"totp_recovery_codes": "credentials",
	"totp_pending":        "credentials",
	"auth_events":         "per-instance audit log",
	"webhooks":            "outbound config with secrets",
	"saved_views":         "per-user preferences",
	"changelog":           "history; kept across an import (reconcileSideTables)",
	"attachments":         "binary content; orphans removed on import (reconcileSideTables)",
	"kuma_monitors":       "reconciled by the Kuma syncer",
	"host_agents":         "agent bindings; cascade with hosts",
	"agent_reports":       "agent reports; cascade with hosts",
	"discovery_runs":      "run log",
}

// TestEveryTableIsClassified fails when a migration adds a table nobody has
// decided the export/import fate of — the way a new entity would otherwise
// silently drop out of backups.
func TestEveryTableIsClassified(t *testing.T) {
	db := newTestDB(t)
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		_, excluded := notInSnapshot[name]
		if !excluded && !slices.Contains(inventoryTables, name) && !slices.Contains(derivedStateTables, name) {
			t.Errorf("table %q is not classified: add it to Snapshot/Export/Import (inventoryTables), "+
				"to derivedStateTables, or to notInSnapshot with a reason", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// TestSnapshotHasOneListPerInventoryTable ties the table list Import clears to
// the lists Export fills: a table added to one without the other fails here.
func TestSnapshotHasOneListPerInventoryTable(t *testing.T) {
	lists := 0
	typ := reflect.TypeOf(Snapshot{})
	for i := range typ.NumField() {
		if typ.Field(i).Type.Kind() == reflect.Slice {
			lists++
		}
	}
	if lists != len(inventoryTables) {
		t.Fatalf("Snapshot has %d lists but inventoryTables has %d tables", lists, len(inventoryTables))
	}
}

func TestEntityTablesCoverEntityTypes(t *testing.T) {
	got := make([]string, 0, len(entityTables))
	for typ, table := range entityTables {
		got = append(got, typ)
		if !slices.Contains(inventoryTables, table) {
			t.Errorf("entityTables[%q] = %q, which is not an inventory table", typ, table)
		}
	}
	want := slices.Clone(domain.EntityTypes)
	sort.Strings(got)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Fatalf("entityTables types = %v, want domain.EntityTypes %v", got, want)
	}
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// TestImportReconcilesSideTables: an import must not leave per-entity state
// describing whatever entity used to have an id. Derived state is cleared,
// attachments of entities the snapshot drops are deleted, and attachments of
// entities it keeps follow their id (restoring this instance's own export).
func TestImportReconcilesSideTables(t *testing.T) {
	db := newTestDB(t)
	hosts := NewHostRepo(db)
	keepID, _ := hosts.Create(domain.Host{Name: "nas", Type: "physical"})
	goneID, _ := hosts.Create(domain.Host{Name: "old", Type: "physical"})
	for _, id := range []int64{keepID, goneID} {
		if _, err := NewAttachmentRepo(db).Create(domain.Attachment{EntityType: "host", EntityID: id, Filename: "a.txt",
			ContentType: "text/plain", Size: 1, Content: []byte("a"), UploadedAt: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		`INSERT INTO liveness_state (entity_type, entity_id, status, checked_at, changed_at) VALUES ('host', 1, 'down', 't', 't')`,
		`INSERT INTO cert_probe_state (certificate_id, probed_at, success) VALUES (1, 't', 0)`,
		`INSERT INTO notification_state (kind, entity_id, notified_at) VALUES ('certificate', 1, 't')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	historyBefore := countRows(t, db, `SELECT COUNT(*) FROM changelog`)

	// The snapshot keeps id keepID (under a new name) and drops goneID.
	snap := Snapshot{Version: 1, Hosts: []domain.Host{{ID: keepID, Name: "router", Type: "physical"}}}
	if err := Import(db, snap); err != nil {
		t.Fatalf("Import: %v", err)
	}

	if n := countRows(t, db, `SELECT COUNT(*) FROM attachments WHERE entity_id = ?`, keepID); n != 1 {
		t.Errorf("attachments of a host still in the snapshot = %d, want 1 (kept)", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM attachments WHERE entity_id = ?`, goneID); n != 0 {
		t.Errorf("attachments of a host the snapshot dropped = %d, want 0", n)
	}
	for _, table := range derivedStateTables {
		if n := countRows(t, db, `SELECT COUNT(*) FROM `+table); n != 0 {
			t.Errorf("%s has %d rows after import, want 0", table, n)
		}
	}
	// History is kept, plus the one import event.
	if n := countRows(t, db, `SELECT COUNT(*) FROM changelog`); n != historyBefore+1 {
		t.Errorf("changelog rows = %d, want %d", n, historyBefore+1)
	}
}

// TestImportSkipsDanglingRefs: rows pointing at an entity or definition the
// snapshot lacks are skipped (and counted) instead of being inserted to
// attach to a future entity, or failing the whole restore.
func TestImportSkipsDanglingRefs(t *testing.T) {
	db := newTestDB(t)
	snap := Snapshot{
		Version: 1,
		Hosts:   []domain.Host{{ID: 1, Name: "nas", Type: "physical"}},
		Relationships: []domain.Relationship{
			{ID: 1, FromType: "host", FromID: 1, ToType: "host", ToID: 9, Kind: "depends on"},
		},
		Tags: []domain.Tag{
			{ID: 1, EntityType: "host", EntityID: 1, Name: "keep"},
			{ID: 2, EntityType: "host", EntityID: 9, Name: "dangling"},
		},
		JournalEntries: []domain.JournalEntry{
			{ID: 1, EntityType: "host", EntityID: 9, Kind: domain.JournalInfo, Body: "x", CreatedAt: "2026-01-01T00:00:00Z"},
		},
		CustomFieldDefs: []domain.CustomFieldDef{
			{ID: 1, EntityType: "service", Name: "owner", Label: "Owner", Kind: domain.KindText, CreatedAt: "t"},
		},
		CustomFieldValues: []domain.CustomFieldValueRow{
			{ID: 1, EntityType: "host", EntityID: 1, DefID: 7, Value: "no such def"},
			{ID: 2, EntityType: "host", EntityID: 1, DefID: 1, Value: "def is for services"},
		},
	}
	tags := slices.Clone(snap.Tags)
	if err := Import(db, snap); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !slices.Equal(snap.Tags, tags) {
		t.Error("Import modified the caller's snapshot")
	}
	for table, want := range map[string]int{
		"relationships": 0, "tags": 1, "journal_entries": 0, "custom_field_values": 0, "hosts": 1,
	} {
		if n := countRows(t, db, `SELECT COUNT(*) FROM `+table); n != want {
			t.Errorf("%s rows = %d, want %d", table, n, want)
		}
	}
	var changes string
	if err := db.QueryRow(`SELECT changes FROM changelog WHERE action = ?`, domain.ActionImport).Scan(&changes); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(changes, "dangling references skipped") || !strings.Contains(changes, `"5"`) {
		t.Errorf("import event changes = %s, want 5 dangling references recorded", changes)
	}
}

// TestImportNormalizesTagNames: a hand-edited tag must be stored the way the
// UI stores it, or search and tag filters never match it.
func TestImportNormalizesTagNames(t *testing.T) {
	db := newTestDB(t)
	snap := Snapshot{
		Version: 1,
		Hosts:   []domain.Host{{ID: 1, Name: "nas", Type: "physical"}},
		Tags:    []domain.Tag{{ID: 1, EntityType: "host", EntityID: 1, Name: " #Prod "}},
	}
	if err := Import(db, snap); err != nil {
		t.Fatalf("Import: %v", err)
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM tags WHERE id = 1`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "prod" {
		t.Fatalf("tag name = %q, want %q", name, "prod")
	}
}

func TestImportRejectsNewerSnapshotVersion(t *testing.T) {
	db := newTestDB(t)
	if _, err := NewHostRepo(db).Create(domain.Host{Name: "nas", Type: "physical"}); err != nil {
		t.Fatal(err)
	}
	if err := Import(db, Snapshot{Version: snapshotVersion + 1}); err == nil {
		t.Fatal("Import accepted a snapshot from a newer format")
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM hosts`); n != 1 {
		t.Fatalf("hosts = %d after a rejected import, want 1 (untouched)", n)
	}
}
