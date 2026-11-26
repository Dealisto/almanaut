package kuma

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestConnectDoesNotSendCredentialsToRedirectTarget(t *testing.T) {
	for _, insecure := range []bool{false, true} {
		name := "verified TLS"
		if insecure {
			name = "insecure TLS"
		}
		t.Run(name, func(t *testing.T) {
			fake := newFakeKuma(t)
			var hits atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				fake.handle(w, r)
			}))
			defer target.Close()
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL+r.URL.RequestURI(), http.StatusTemporaryRedirect)
			}))
			defer source.Close()

			session, err := NewClient(source.URL, fake.user, fake.pass, insecure).Connect(testCtx(t))
			if session != nil {
				session.Close()
			}
			if err == nil {
				t.Error("redirect was accepted as an authenticated Kuma connection")
			}
			if got := hits.Load(); got != 0 {
				t.Errorf("redirect target received %d request(s); credentials must stay at the configured endpoint", got)
			}
		})
	}
}
