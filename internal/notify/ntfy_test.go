package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNtfyClientSendsTitleTagsAndBody(t *testing.T) {
	var gotTitle, gotTags, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTitle = r.Header.Get("Title")
		gotTags = r.Header.Get("Tags")
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewNtfyClient(srv.URL, "tok123")
	err := c.Send(context.Background(), Notification{
		Title: "Certificate expiring", Body: "a.com expires in 7 days", Tags: "warning",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotTitle != "Certificate expiring" {
		t.Errorf("Title = %q", gotTitle)
	}
	if gotTags != "warning" {
		t.Errorf("Tags = %q", gotTags)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotBody != "a.com expires in 7 days" {
		t.Errorf("Body = %q", gotBody)
	}
}

func TestNtfyClientNoTokenOmitsAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := NewNtfyClient(srv.URL, "").Send(context.Background(), Notification{Body: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization should be empty, got %q", gotAuth)
	}
}

func TestNtfyClientErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	err := NewNtfyClient(srv.URL, "").Send(context.Background(), Notification{Body: "x"})
	if err == nil {
		t.Fatal("expected error on 403")
	}
}
