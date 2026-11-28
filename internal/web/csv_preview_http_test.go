package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func uploadCSVPreview(t *testing.T, srv http.Handler, typ, doc, mode string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for key, value := range map[string]string{"type": typ, "mode": mode, csrfFieldName: csrfTestToken} {
		if err := form.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	file, err := form.CreateFormFile("file", "import.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte(doc)); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/import-csv/preview", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfTestToken})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func csvReviewToken(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", rec.Code, rec.Body)
	}
	match := regexp.MustCompile(`name="preview_token" value="([a-f0-9]{64})"`).FindStringSubmatch(rec.Body.String())
	if len(match) != 2 {
		t.Fatalf("no preview token: %s", rec.Body)
	}
	return match[1]
}

func confirmCSVPreview(t *testing.T, srv http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"preview_token": {token}, "confirm": {"on"}, csrfFieldName: {csrfTestToken}}
	req := httptest.NewRequest(http.MethodPost, "/import-csv/apply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfTestToken})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestCSVPreviewHTTPReviewApplyAndReplay(t *testing.T) {
	srv, db, dispatcher := newTestServerWHDB(t)
	token := csvReviewToken(t, uploadCSVPreview(t, srv, "host", "name,type\nreviewed,vm\n", "patch"))
	hosts, _ := store.NewHostRepo(db).List()
	if len(hosts) != 0 || len(dispatcher.all()) != 0 {
		t.Fatal("preview leaked changes")
	}
	if rec := confirmCSVPreview(t, srv, token); rec.Code != http.StatusSeeOther {
		t.Fatalf("apply=%d %s", rec.Code, rec.Body)
	}
	if rec := confirmCSVPreview(t, srv, token); rec.Code != http.StatusConflict {
		t.Fatalf("replay=%d", rec.Code)
	}
	hosts, _ = store.NewHostRepo(db).List()
	history, _ := store.NewChangelogRepo(db).ListRecent(10)
	if len(hosts) != 1 || len(history) != 1 || len(dispatcher.all()) != 1 {
		t.Fatalf("hosts=%+v history=%+v events=%+v", hosts, history, dispatcher.all())
	}
}

func TestCSVPreviewIndirectPortStaleRollsBack(t *testing.T) {
	srv, db, dispatcher := portEffectsFixture(t, false)
	token := csvReviewToken(t, uploadCSVPreview(t, srv, "port", "id,peer_port_id\n1,2\n", "patch"))
	ports := store.NewPortRepo(db)
	b, _ := ports.Get(2)
	b.PeerPortID = 3
	if err := ports.Update(b); err != nil {
		t.Fatal(err)
	}
	rec := confirmCSVPreview(t, srv, token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale indirect=%d %s", rec.Code, rec.Body)
	}
	a, _ := ports.Get(1)
	b, _ = ports.Get(2)
	c, _ := ports.Get(3)
	if a.PeerPortID != 0 || b.PeerPortID != 3 || c.PeerPortID != 2 {
		t.Fatalf("connections changed %+v %+v %+v", a, b, c)
	}
	history, _ := store.NewChangelogRepo(db).ListRecent(10)
	if len(history) != 0 || len(dispatcher.all()) != 0 {
		t.Fatal("stale indirect apply leaked audit/events")
	}
}

func TestCSVPreviewCreatedPortIDsRemainStableAcrossUnrelatedCreate(t *testing.T) {
	srv, db, dispatcher := portEffectsFixture(t, false)
	preview := uploadCSVPreview(t, srv, "port", "name,owner_type,owner_id,peer_port_id\nnew,host,1,2\n", "patch")
	token := csvReviewToken(t, preview)
	if !strings.Contains(preview.Body.String(), "Row 2") || !strings.Contains(preview.Body.String(), "new record from CSV row 2") {
		t.Fatalf("indirect link missing from preview: %s", preview.Body)
	}
	ports := store.NewPortRepo(db)
	if _, err := ports.Create(domain.Port{Name: "unrelated", OwnerType: "host", OwnerID: 1}); err != nil {
		t.Fatal(err)
	}
	rec := confirmCSVPreview(t, srv, token)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("apply=%d %s", rec.Code, rec.Body)
	}
	b, _ := ports.Get(2)
	created, _ := ports.Get(6)
	if b.PeerPortID != 6 || created.PeerPortID != 2 || len(dispatcher.all()) != 2 {
		t.Fatalf("peer=%+v created=%+v events=%+v", b, created, dispatcher.all())
	}
}

func TestCSVPreviewDurableQuotaFailureIsAtomic(t *testing.T) {
	srv, db, dispatcher := newOutboxWebServer(t, 1)
	token := csvReviewToken(t, uploadCSVPreview(t, srv, "host", "name,type\none,vm\ntwo,vm\n", "patch"))
	if count, err := store.NewWebhookOutboxRepo(db).PendingCount(); err != nil || count != 0 {
		t.Fatalf("preview outbox=%d err=%v", count, err)
	}
	rec := confirmCSVPreview(t, srv, token)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("quota=%d %s", rec.Code, rec.Body)
	}
	hosts, _ := store.NewHostRepo(db).List()
	history, _ := store.NewChangelogRepo(db).ListRecent(10)
	count, err := store.NewWebhookOutboxRepo(db).PendingCount()
	if err != nil || len(hosts) != 0 || len(history) != 0 || count != 0 || len(dispatcher.all()) != 0 {
		t.Fatalf("quota failure persisted hosts=%v history=%v pending=%d events=%v err=%v", hosts, history, count, dispatcher.all(), err)
	}
}
