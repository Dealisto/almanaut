package web

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func prepareHostCSV(t *testing.T, rs resource[domain.Host], d handlerDeps, doc string, mode csvImportMode) *csvPreparedPreview {
	t.Helper()
	p, rows, err := rs.prepareCSVPreview(context.Background(), d, strings.NewReader(doc), mode)
	if err != nil || len(rows) > 0 {
		t.Fatalf("preview err=%v rows=%v", err, rows)
	}
	return p
}

func TestCSVPreviewPatchPreservesOmittedAndExplicitlyClears(t *testing.T) {
	rs, d := hostResourceForTest(t)
	rec := &recordingDispatcher{}
	d.webhooks = rec
	id, err := rs.repo.Create(domain.Host{Name: "original", Type: "vm", OS: "Linux", Notes: "retained", IPs: []string{"10.0.0.1"}, AgentProtectedFields: []string{"os"}})
	if err != nil {
		t.Fatal(err)
	}
	plan := prepareHostCSV(t, rs, d, "id,name,ips\n"+itoa(id)+",updated,\n", csvPatch)
	old, _ := rs.repo.Get(id)
	if old.Name != "original" || len(old.IPs) != 1 {
		t.Fatalf("preview persisted: %+v", old)
	}
	history, _ := d.changelog.ListRecent(100)
	if len(history) != 0 || len(rec.all()) != 0 {
		t.Fatal("preview leaked history or webhooks")
	}
	if plan.Summary.Updated != 1 || plan.Summary.Created != 0 {
		t.Fatalf("summary=%+v", plan.Summary)
	}
	result, err := plan.apply(context.Background(), "reviewer")
	if err != nil || result.Updated != 1 {
		t.Fatalf("apply=%+v err=%v", result, err)
	}
	got, _ := rs.repo.Get(id)
	if got.Name != "updated" || got.OS != "Linux" || got.Type != "vm" || got.Notes != "retained" || len(got.IPs) != 0 || len(got.AgentProtectedFields) != 1 {
		t.Fatalf("patch=%+v", got)
	}
	history, _ = d.changelog.ListRecent(100)
	if len(history) != 1 || history[0].Actor != "reviewer" || len(rec.all()) != 1 {
		t.Fatalf("history=%+v events=%+v", history, rec.all())
	}
	clear := prepareHostCSV(t, rs, d, "id,agent_protected_fields\n"+itoa(id)+",\n", csvPatch)
	if _, err := clear.apply(context.Background(), "reviewer"); err != nil {
		t.Fatal(err)
	}
	got, _ = rs.repo.Get(id)
	if len(got.AgentProtectedFields) != 0 {
		t.Fatalf("policy not cleared: %+v", got)
	}
}

func TestCSVPreviewReplaceAndNoop(t *testing.T) {
	rs, d := hostResourceForTest(t)
	id, err := rs.repo.Create(domain.Host{Name: "old", Type: "vm", OS: "Linux", AgentProtectedFields: []string{"os"}})
	if err != nil {
		t.Fatal(err)
	}
	noop := prepareHostCSV(t, rs, d, "id,name\n"+itoa(id)+",old\n", csvPatch)
	if noop.Summary.Unchanged != 1 {
		t.Fatalf("summary=%+v", noop.Summary)
	}
	if _, err := noop.apply(context.Background(), "reviewer"); err != nil {
		t.Fatal(err)
	}
	history, _ := d.changelog.ListRecent(10)
	if len(history) != 0 {
		t.Fatal("noop wrote history")
	}
	plan := prepareHostCSV(t, rs, d, "id,name,type\n"+itoa(id)+",new,vm\n", csvReplace)
	if _, err := plan.apply(context.Background(), "reviewer"); err != nil {
		t.Fatal(err)
	}
	got, _ := rs.repo.Get(id)
	if got.OS != "" || len(got.AgentProtectedFields) != 1 {
		t.Fatalf("replacement=%+v", got)
	}
}

func TestCSVPreviewStaleBatchRollsBack(t *testing.T) {
	rs, d := hostResourceForTest(t)
	id, _ := rs.repo.Create(domain.Host{Name: "old", Type: "vm"})
	plan := prepareHostCSV(t, rs, d, "id,name,type\n,created,vm\n"+itoa(id)+",new,vm\n", csvPatch)
	current, _ := rs.repo.Get(id)
	current.Notes = "concurrent edit"
	if err := rs.repo.Update(current); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.apply(context.Background(), "reviewer"); !errors.Is(err, errCSVPreviewStale) {
		t.Fatalf("want stale, got %v", err)
	}
	hosts, _ := rs.repo.List()
	history, _ := d.changelog.ListRecent(10)
	if len(hosts) != 1 || hosts[0].Name != "old" || len(history) != 0 {
		t.Fatalf("stale apply leaked: %+v %+v", hosts, history)
	}
}

func TestCSVPreviewCreatesAfterUnrelatedInsert(t *testing.T) {
	rs, d := hostResourceForTest(t)
	plan := prepareHostCSV(t, rs, d, "name,type\ncreated,vm\n", csvPatch)
	if _, err := rs.repo.Create(domain.Host{Name: "unrelated", Type: "physical"}); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.apply(context.Background(), "reviewer"); err != nil {
		t.Fatal(err)
	}
	hosts, _ := rs.repo.List()
	if len(hosts) != 2 {
		t.Fatalf("hosts=%+v", hosts)
	}
}

func TestCSVPreviewValidationLeavesNoWrites(t *testing.T) {
	for _, doc := range []string{"name,type\nvalid,vm\n,vm\n", "id,name\n9999,missing\n", "id,name\n1,a\n1,b\n", "name,type\nvalid,unknown-catalog-type\n", "name,name\na,b\n"} {
		t.Run(doc, func(t *testing.T) {
			rs, d := hostResourceForTest(t)
			p, rows, err := rs.prepareCSVPreview(context.Background(), d, strings.NewReader(doc), csvPatch)
			if p != nil || len(rows) == 0 || err != nil {
				t.Fatalf("plan=%v rows=%v err=%v", p, rows, err)
			}
			hosts, _ := rs.repo.List()
			history, _ := d.changelog.ListRecent(10)
			if len(hosts) != 0 || len(history) != 0 {
				t.Fatal("invalid preview wrote data")
			}
		})
	}
}

func TestCSVPreviewCancellation(t *testing.T) {
	rs, d := hostResourceForTest(t)
	plan := prepareHostCSV(t, rs, d, "name,type\ncreated,vm\n", csvPatch)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := plan.apply(ctx, "reviewer"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	p, _, err := rs.prepareCSVPreview(ctx, d, strings.NewReader("name,type\ncreated,vm\n"), csvPatch)
	if p != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("plan=%v err=%v", p, err)
	}
	hosts, _ := rs.repo.List()
	if len(hosts) != 0 {
		t.Fatal("cancellation wrote data")
	}
}

func TestCSVPreviewTokenOwnerExpiryReplayAndCapacity(t *testing.T) {
	cache := newCSVPreviewStore()
	now := time.Now()
	cache.now = func() time.Time { return now }
	token, err := cache.add(1, &csvPreparedPreview{Bytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.take(2, token); ok {
		t.Fatal("other user took token")
	}
	if _, ok := cache.take(1, token); !ok {
		t.Fatal("owner lost token")
	}
	if _, ok := cache.take(1, token); ok {
		t.Fatal("token replayed")
	}
	token, _ = cache.add(1, &csvPreparedPreview{Bytes: 1})
	now = now.Add(csvPreviewLifetime)
	if _, ok := cache.take(1, token); ok {
		t.Fatal("expired token accepted")
	}
	for i := 0; i < 4; i++ {
		if _, err := cache.add(1, &csvPreparedPreview{Bytes: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cache.add(1, &csvPreparedPreview{Bytes: 1}); err == nil {
		t.Fatal("per-user capacity ignored")
	}
	if _, err := cache.add(2, &csvPreparedPreview{Bytes: maxCSVPreviewCacheBytes}); err == nil {
		t.Fatal("byte capacity ignored")
	}
	now = now.Add(csvPreviewLifetime)
	if _, err := cache.add(1, &csvPreparedPreview{Bytes: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestCSVPreviewForeignKeyRevalidation(t *testing.T) {
	rs, d := hostResourceForTest(t)
	if err := store.NewHostTypeRepo(d.db).Create(domain.HostType{Name: "custom", Label: "Custom"}); err != nil {
		t.Fatal(err)
	}
	plan := prepareHostCSV(t, rs, d, "name,type\ncreated,custom\n", csvPatch)
	if _, err := d.db.Exec("DELETE FROM host_types WHERE name = 'custom'"); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.apply(context.Background(), "reviewer"); err == nil {
		t.Fatal("deleted dependency accepted")
	}
	hosts, _ := rs.repo.List()
	history, _ := d.changelog.ListRecent(10)
	if len(hosts) != 0 || len(history) != 0 {
		t.Fatal("failed apply wrote data")
	}
}

func TestCSVPreviewPreservesConcurrentCustomFieldChanges(t *testing.T) {
	rs, d := hostResourceForTest(t)
	d.customFields = store.NewCustomFieldRepo(d.db)
	id, err := rs.repo.Create(domain.Host{Name: "old", Type: "vm"})
	if err != nil {
		t.Fatal(err)
	}
	field, err := d.customFields.CreateDef(domain.CustomFieldDef{EntityType: "host", Name: "asset", Label: "Asset", Kind: domain.KindText})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.customFields.SetForEntity("host", id, map[int64]string{field: "before"}); err != nil {
		t.Fatal(err)
	}
	plan := prepareHostCSV(t, rs, d, "id,name\n"+itoa(id)+",old\n", csvPatch)
	if err := d.customFields.SetForEntity("host", id, map[int64]string{field: "concurrent"}); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.apply(context.Background(), "reviewer"); err != nil {
		t.Fatal(err)
	}
	values, err := d.customFields.ListForEntity("host", id)
	if err != nil || len(values) != 1 || values[0].Value != "concurrent" {
		t.Fatalf("values=%+v err=%v", values, err)
	}
}
