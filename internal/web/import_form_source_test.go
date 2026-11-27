package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func uploadWithFormFields(t *testing.T, srv http.Handler, target, document string, fields url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for key, values := range fields {
		for _, value := range values {
			if err := mw.WriteField(key, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := mw.WriteField(csrfFieldName, csrfTestToken); err != nil {
		t.Fatal(err)
	}
	file, err := mw.CreateFormFile("file", "inventory.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte(document)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, target, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfTestToken})
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestCSVImportUsesSubmittedTypeAndConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		fields      url.Values
		code, count int
	}{
		{"query cannot replace type", "type=service", url.Values{"type": {"hardware"}, "confirm": {"on"}}, 303, 1},
		{"query cannot supply type", "type=hardware", url.Values{"confirm": {"on"}}, 400, 0},
		{"query cannot confirm", "confirm=on", url.Values{"type": {"hardware"}}, 400, 0},
		{"query cannot override empty confirmation", "confirm=on", url.Values{"type": {"hardware"}, "confirm": {""}}, 400, 0},
		{"query cannot replace confirmation", "confirm=", url.Values{"type": {"hardware"}, "confirm": {"on"}}, 303, 1},
		{"normal upload", "", url.Values{"type": {"hardware"}, "confirm": {"on"}}, 303, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db, recorder := newOutboxWebServer(t, 10)
			rec := uploadWithFormFields(t, srv, "/import-csv?"+tc.query, "name,kind\nImported,container\n", tc.fields)
			if rec.Code != tc.code {
				t.Fatalf("status=%d body=%s; want %d", rec.Code, rec.Body, tc.code)
			}
			for table, want := range map[string]int{"hardware": tc.count, "services": 0} {
				var count int
				if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != want {
					t.Fatalf("%s count=%d want=%d err=%v", table, count, want, err)
				}
			}
			assertBatchEventCounts(t, db, recorder, tc.count)
		})
	}
}

func TestYAMLImportRequiresSubmittedConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		fields      url.Values
		code        int
		wantName    string
	}{
		{"query cannot confirm replacement", "confirm=on", nil, 400, "original"},
		{"query cannot override empty confirmation", "confirm=on", url.Values{"confirm": {""}}, 400, "original"},
		{"query cannot replace confirmation", "confirm=", url.Values{"confirm": {"on"}}, 303, "replacement"},
		{"normal confirmed replacement", "", url.Values{"confirm": {"on"}}, 303, "replacement"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newTestServerDB(t)
			repo := store.NewHostRepo(db)
			if _, err := repo.Create(domain.Host{Name: "original", Type: "physical"}); err != nil {
				t.Fatal(err)
			}
			rec := uploadWithFormFields(t, srv, "/import?"+tc.query, "version: 1\nhosts:\n  - id: 1\n    name: replacement\n    type: physical\n", tc.fields)
			if rec.Code != tc.code {
				t.Fatalf("status=%d body=%s; want %d", rec.Code, rec.Body, tc.code)
			}
			host, err := repo.Get(1)
			if err != nil || host.Name != tc.wantName {
				t.Fatalf("host=%+v err=%v; want name %q", host, err, tc.wantName)
			}
		})
	}
}
