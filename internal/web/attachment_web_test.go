package web

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

var attachmentHostLinkRE = regexp.MustCompile(`/hosts/(\d+)`)

// uploadAttachment posts a multipart file to path, with the CSRF field/cookie
// pair the double-submit check requires. Models the CSV-import test's
// multipart-builder (internal/web/csvimport_handler_test.go).
func uploadAttachment(t *testing.T, srv http.Handler, path, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField(csrfFieldName, csrfTestToken); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfTestToken})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

// TestAttachmentUploadDownloadDelete drives the full HTTP path for Tasks 4-5:
// upload -> the filename appears on the entity detail page -> download
// (forced, with the anti-XSS headers) -> delete -> the attachment is gone.
func TestAttachmentUploadDownloadDelete(t *testing.T) {
	srv, db := newTestServerDB(t)

	// 1. Create a host; creation redirects to the list, not the detail page.
	hostRec := postForm(t, srv, "/hosts", url.Values{"name": {"nas"}, "type": {"physical"}})
	if hostRec.Code != http.StatusSeeOther {
		t.Fatalf("POST /hosts = %d, want 303, body: %s", hostRec.Code, hostRec.Body.String())
	}
	listReq := httptest.NewRequest(http.MethodGet, "/hosts", nil)
	listRec := httptest.NewRecorder()
	srv.ServeHTTP(listRec, listReq)
	match := attachmentHostLinkRE.FindStringSubmatch(listRec.Body.String())
	if match == nil {
		t.Fatalf("could not find host detail link on the hosts list:\n%s", listRec.Body.String())
	}
	hostID := match[1]

	// 2. Upload a file -> 303.
	content := []byte("hello attachment world")
	rec := uploadAttachment(t, srv, "/hosts/"+hostID+"/attachments", "notes.txt", content)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /hosts/%s/attachments = %d, want 303, body: %s", hostID, rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/hosts/"+hostID {
		t.Errorf("redirect Location = %q, want /hosts/%s", loc, hostID)
	}

	// The detail page's Attachments panel should now show the uploaded file.
	detailReq := httptest.NewRequest(http.MethodGet, "/hosts/"+hostID, nil)
	detailRec := httptest.NewRecorder()
	srv.ServeHTTP(detailRec, detailReq)
	if !strings.Contains(detailRec.Body.String(), "notes.txt") {
		t.Fatalf("GET /hosts/%s body does not contain uploaded filename %q:\n%s", hostID, "notes.txt", detailRec.Body.String())
	}

	hid, _ := strconv.ParseInt(hostID, 10, 64)
	atts, err := store.NewAttachmentRepo(db).ListForEntity("host", hid)
	if err != nil {
		t.Fatalf("ListForEntity: %v", err)
	}
	if len(atts) != 1 {
		t.Fatalf("attachments for host %s = %d, want 1", hostID, len(atts))
	}
	attID := atts[0].ID

	// 3. Download -> 200, exact bytes, and the two security headers.
	dlReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/attachments/%d", attID), nil)
	dlRec := httptest.NewRecorder()
	srv.ServeHTTP(dlRec, dlReq)
	if dlRec.Code != http.StatusOK {
		t.Fatalf("GET /attachments/%d = %d, want 200", attID, dlRec.Code)
	}
	if !bytes.Equal(dlRec.Body.Bytes(), content) {
		t.Errorf("downloaded body = %q, want %q", dlRec.Body.Bytes(), content)
	}
	if cd := dlRec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want it to start with %q", cd, "attachment")
	}
	if xcto := dlRec.Header().Get("X-Content-Type-Options"); xcto != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", xcto)
	}

	// 4. Oversize upload is rejected before it's persisted.
	oversized := make([]byte, domain.MaxAttachmentBytes+1)
	overRec := uploadAttachment(t, srv, "/hosts/"+hostID+"/attachments", "big.bin", oversized)
	if overRec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize upload = %d, want 413, body: %s", overRec.Code, overRec.Body.String())
	}
	atts, err = store.NewAttachmentRepo(db).ListForEntity("host", hid)
	if err != nil {
		t.Fatalf("ListForEntity after oversize upload: %v", err)
	}
	if len(atts) != 1 {
		t.Fatalf("attachments for host %s after rejected oversize upload = %d, want still 1", hostID, len(atts))
	}

	// 5. Delete -> 303; the attachment is then gone (download 404).
	delRec := postForm(t, srv, fmt.Sprintf("/attachments/%d/delete", attID), nil)
	if delRec.Code != http.StatusSeeOther {
		t.Fatalf("POST /attachments/%d/delete = %d, want 303, body: %s", attID, delRec.Code, delRec.Body.String())
	}
	if loc := delRec.Header().Get("Location"); loc != "/hosts/"+hostID {
		t.Errorf("delete redirect Location = %q, want /hosts/%s", loc, hostID)
	}
	dlReq2 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/attachments/%d", attID), nil)
	dlRec2 := httptest.NewRecorder()
	srv.ServeHTTP(dlRec2, dlReq2)
	if dlRec2.Code != http.StatusNotFound {
		t.Fatalf("GET /attachments/%d after delete = %d, want 404", attID, dlRec2.Code)
	}
}
