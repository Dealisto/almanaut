package web

import (
	"fmt"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func TestCSRFRejectsMalformedFormsBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, body string
		limit                   int64
		want                    int
	}{
		{"bad escape", "application/x-www-form-urlencoded", "csrf_token=valid&name=%ZZ", 1024, http.StatusBadRequest},
		{"bad separator", "application/x-www-form-urlencoded", "csrf_token=valid&name=before;after", 1024, http.StatusBadRequest},
		{"truncated multipart", "multipart/form-data; boundary=example", "--example\r\nContent-Disposition: form-data; name=\"csrf_token\"\r\n\r\nvalid\r\n--example\r\nContent-Disposition: form-data; name=\"name\"\r\n\r\ntruncated", 1024, http.StatusBadRequest},
		{"chunked oversized form", "application/x-www-form-urlencoded", "csrf_token=valid&name=" + strings.Repeat("x", 128), 64, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reached := false
			h := limitBodyBytes(tc.limit, csrfProtect(false)(passThrough(&reached)))
			req := httptest.NewRequest(http.MethodPost, "/hosts?csrf_token=valid", strings.NewReader(tc.body))
			req.ContentLength = -1
			req.Header.Set("Content-Type", tc.contentType)
			req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "valid"})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if reached || rec.Code != tc.want {
				t.Fatalf("reached=%v status=%d, want no mutation and status %d", reached, rec.Code, tc.want)
			}
		})
	}
}

func TestLogoutDeletionFailureDoesNotReportSuccess(t *testing.T) {
	h, db := totpTestHandler(t)
	session := loginAs(t, h, "admin", "password123")
	if _, err := db.Exec(`CREATE TRIGGER fail_logout BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT, 'logout unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	rec := postCSRF(t, h, session, "/logout", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("logout status=%d, want 500", rec.Code)
	}
	if c := cookieNamed(rec.Result().Cookies(), sessionCookieName); c != nil {
		t.Fatal("failed logout discarded the cookie needed to retry revocation")
	}
	if _, err := store.NewSessionRepo(db).UserByToken(hashToken(session.Value), nowRFC3339()); err != nil {
		t.Fatalf("failure injection did not preserve the session: %v", err)
	}
	events, err := store.NewAuthEventRepo(db).List(store.AuthEventFilter{Type: domain.AuthLogout})
	if err != nil || len(events) != 0 {
		t.Fatalf("failed logout recorded success: events=%v err=%v", events, err)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_logout`); err != nil {
		t.Fatal(err)
	}
	rec = postCSRF(t, h, session, "/logout", "")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("logout retry status=%d, want 303", rec.Code)
	}
	if _, err := store.NewSessionRepo(db).UserByToken(hashToken(session.Value), nowRFC3339()); err == nil {
		t.Fatal("successful retry left session valid")
	}
}

func TestChallengeFormDistinguishesStorageFailureFromMissingChallenge(t *testing.T) {
	db := rbacDB(t)
	h := challenge2FAForm(store.NewTOTPRepo(db))
	req := httptest.NewRequest(http.MethodGet, "/login/2fa", nil)
	req.AddCookie(&http.Cookie{Name: pending2FACookieName, Value: "unknown"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("missing challenge status=%d, want 303", rec.Code)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError || rec.Header().Get("Location") != "" {
		t.Fatalf("storage failure status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAttachmentDownloadEncodesInternationalFilename(t *testing.T) {
	h, db := newTestServerDB(t)
	hostID, err := store.NewHostRepo(db).Create(domain.Host{Name: "nas", Type: "physical"})
	if err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"résumé 中文.txt", "quote\"and;semicolon.txt"} {
		t.Run(filename, func(t *testing.T) {
			id, err := store.NewAttachmentRepo(db).Create(domain.Attachment{
				EntityType: "host", EntityID: hostID, Filename: filename,
				ContentType: "text/plain", Content: []byte("hello"), Size: 5,
				UploadedAt: nowRFC3339(),
			})
			if err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/attachments/%d", id), nil))
			header := rec.Header().Get("Content-Disposition")
			kind, params, err := mime.ParseMediaType(header)
			if rec.Code != http.StatusOK || err != nil || kind != "attachment" || params["filename"] != filename {
				t.Fatalf("download status=%d disposition=%q err=%v", rec.Code, header, err)
			}
			for _, c := range header {
				if c > 127 {
					t.Fatalf("filename must be encoded in an ASCII header: %q", header)
				}
			}
		})
	}
}
