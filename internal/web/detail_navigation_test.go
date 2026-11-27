package web

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/store"
)

func TestDetailMutationsPreserveListContext(t *testing.T) {
	for _, entity := range []string{"host", "hardware"} {
		t.Run(entity, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			base := "/" + entity
			if entity == "host" {
				base += "s"
			}
			if rec := postForm(t, srv, base, url.Values{"name": {"Parent"}, "type": {"physical"}}); rec.Code != http.StatusSeeOther {
				t.Fatal(rec.Code)
			}
			origin := base + "?" + url.Values{"q": {"edge & core"}, "tag": {"production"}, "sort": {"name"}, "dir": {"desc"}, "field": {"Notes"}, "value": {"rack"}, "page": {"2"}, "per_page": {"25"}}.Encode()
			detail := base + "/1?" + url.Values{"return_to": {origin}}.Encode()
			assertRedirect := func(rec *httptest.ResponseRecorder, activity bool) {
				t.Helper()
				want := detail
				if activity {
					want += "#activity"
				}
				if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
					t.Fatalf("mutation = %d %q want %q: %s", rec.Code, rec.Header().Get("Location"), want, rec.Body)
				}
			}
			assertRedirect(postForm(t, srv, "/tags", url.Values{"entity_type": {entity}, "entity_id": {"1"}, "tag": {"keep"}, "return_to": {origin}}), false)
			assertRedirect(postForm(t, srv, base+"/1/journal", url.Values{"kind": {"info"}, "body": {"Keep context"}, "return_to": {origin}, "return_section": {"activity"}}), true)
			assertRedirect(uploadAttachment(t, srv, base+"/1/attachments", "context.txt", []byte("Keep context"), url.Values{"return_to": {origin}, "return_section": {"activity"}}), true)
			body := getPage(t, srv, detail)
			forms := regexp.MustCompile(`(?s)<form\b.*?</form>`).FindAllString(body, -1)
			found := 0
			for _, form := range forms {
				if !strings.Contains(form, `action="/tags`) && !strings.Contains(form, "/journal") && !strings.Contains(form, "/attachments") {
					continue
				}
				match := regexp.MustCompile(`name="return_to" value="([^"]*)"`).FindStringSubmatch(form)
				if len(match) != 2 || html.UnescapeString(match[1]) != origin {
					t.Fatalf("form lost context: %s", form)
				}
				found++
			}
			if found != 6 {
				t.Fatalf("checked %d child mutation forms, want6", found)
			}
			tags, err := store.NewTagRepo(db).ListForEntity(entity, 1)
			if err != nil || len(tags) != 1 {
				t.Fatalf("tags=%v err=%v", tags, err)
			}
			assertRedirect(postForm(t, srv, "/tags/delete", url.Values{"id": {fmt.Sprint(tags[0].ID)}, "entity_type": {entity}, "entity_id": {"1"}, "return_to": {origin}}), false)
			entries, err := store.NewJournalRepo(db).ListForEntity(entity, 1)
			if err != nil || len(entries) != 1 {
				t.Fatalf("entries=%v err=%v", entries, err)
			}
			assertRedirect(postForm(t, srv, fmt.Sprintf("/journal/%d/delete", entries[0].ID), url.Values{"return_to": {origin}, "return_section": {"activity"}}), true)
			attachments, err := store.NewAttachmentRepo(db).ListForEntity(entity, 1)
			if err != nil || len(attachments) != 1 {
				t.Fatalf("attachments=%v err=%v", attachments, err)
			}
			assertRedirect(postForm(t, srv, fmt.Sprintf("/attachments/%d/delete", attachments[0].ID), url.Values{"return_to": {origin}, "return_section": {"activity"}}), true)
		})
	}
}

func TestDetailReturnPathAcceptsOnlySameListBodyContext(t *testing.T) {
	for _, origin := range []string{"https://evil.test/hosts?q=x", "//evil.test/hosts", "/hardware?q=x", "/hosts/2", "/hosts?q=%zz", "/hosts?q=good&unknown=bad&return_to=evil#discard"} {
		req := httptest.NewRequest(http.MethodPost, "/hosts/1/journal?return_to=%2Fhosts%3Fq%3Dquery&return_section=activity", strings.NewReader(url.Values{"return_to": {origin}, "return_section": {"evil"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		want := "/hosts/1"
		if strings.Contains(origin, "q=good") {
			want += "?" + url.Values{"return_to": {"/hosts?q=good"}}.Encode()
		}
		if got := detailReturnPath(req, "/hosts/1"); got != want {
			t.Errorf("origin %q got %q want %q", origin, got, want)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/hosts/1/journal?return_to=%2Fhosts%3Fq%3Dquery&return_section=activity", nil)
	if got := detailReturnPath(req, "/hosts/1"); got != "/hosts/1" {
		t.Fatalf("query leaked into context: %q", got)
	}
}
