package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestThemeJSONUpdatesCookieWithoutRedirect(t *testing.T) {
	srv := newTestServer(t)
	for _, theme := range []string{"dark", "light", "system", "invalid"} {
		form := url.Values{"theme": {theme}, "return_to": {"/hosts/new"}, csrfFieldName: {csrfTestToken}}
		req := httptest.NewRequest(http.MethodPost, "/theme", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfTestToken})
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		want := theme
		if theme == "invalid" {
			want = "system"
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK || body["theme"] != want || cookieValue(rec, themeCookieName) != want || rec.Header().Get("Location") != "" {
			t.Fatalf("theme %q: %d %s cookies=%v", theme, rec.Code, rec.Body, rec.Result().Cookies())
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Error("theme confirmation must not be cached")
		}
	}
}

func TestThemeJSONStillRequiresCSRF(t *testing.T) {
	srv := newTestServer(t)
	for _, token := range []string{"", "wrong"} {
		req := httptest.NewRequest(http.MethodPost, "/theme", strings.NewReader(url.Values{"theme": {"dark"}, csrfFieldName: {token}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Accept", "application/json")
		req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrfTestToken})
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden || cookieValue(rec, themeCookieName) != "" {
			t.Fatalf("invalid CSRF changed theme: %d %s", rec.Code, rec.Body)
		}
	}
}
