package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestCSRFTokenMustComeFromRequestBody(t *testing.T) {
	for _, encoding := range []string{"urlencoded", "multipart"} {
		for _, tc := range []struct {
			name, body, query string
			allowed           bool
		}{
			{"query only", "", "valid-token", false},
			{"query cannot repair body", "wrong", "valid-token", false},
			{"query cannot override body", "valid-token", "wrong", true},
			{"body only", "valid-token", "", true},
		} {
			t.Run(encoding+"/"+tc.name, func(t *testing.T) {
				var body bytes.Buffer
				contentType := "application/x-www-form-urlencoded"
				if encoding == "multipart" {
					writer := multipart.NewWriter(&body)
					if tc.body != "" {
						if err := writer.WriteField(csrfFieldName, tc.body); err != nil {
							t.Fatal(err)
						}
					}
					if err := writer.Close(); err != nil {
						t.Fatal(err)
					}
					contentType = writer.FormDataContentType()
				} else if tc.body != "" {
					body.WriteString(url.Values{csrfFieldName: {tc.body}}.Encode())
				}
				path := "/hosts"
				if tc.query != "" {
					path += "?" + url.Values{csrfFieldName: {tc.query}}.Encode()
				}
				req := httptest.NewRequest(http.MethodPost, path, &body)
				req.Header.Set("Content-Type", contentType)
				req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "valid-token"})
				var reached bool
				rec := httptest.NewRecorder()
				csrfProtect(false)(passThrough(&reached)).ServeHTTP(rec, req)
				want := http.StatusForbidden
				if tc.allowed {
					want = http.StatusOK
				}
				if reached != tc.allowed || rec.Code != want {
					t.Fatalf("reached=%t status=%d, want reached=%t status=%d", reached, rec.Code, tc.allowed, want)
				}
			})
		}
	}
}
