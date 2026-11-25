package web

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

func ssoLink(t *testing.T, body string) *url.URL {
	t.Helper()
	_, rest, ok := strings.Cut(body, `class="btn login-sso" href="`)
	if !ok {
		t.Fatal("SSO button missing")
	}
	href, _, _ := strings.Cut(rest, `"`)
	u, err := url.Parse(html.UnescapeString(href))
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestLoginSSOButtonPreservesSafeDestination(t *testing.T) {
	h, _ := newProxyTestHandler(t, false)
	for _, next := range []string{"/hosts?tag=prod&sort=Name", "/hosts/7#connections", "https://example.invalid", "//example.invalid", "/\t/example.invalid", ""} {
		rec := getProxy(t, h, "/login?next="+url.QueryEscape(next), "10.0.0.5:1234", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("login = %d", rec.Code)
		}
		link := ssoLink(t, rec.Body.String())
		if link.Path != "/login/sso" || link.Query().Get("next") != safeNext(next) {
			t.Errorf("next=%q produced link %q", next, link)
		}
		if !strings.Contains(rec.Body.String(), `action="/login"`) {
			t.Error("password login must remain available")
		}
	}
}

func TestLoginHidesSSOWhenDisabled(t *testing.T) {
	h, session := authTestServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "Sign in with SSO") {
		t.Fatalf("disabled SSO should not appear: status=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, withCookie(httptest.NewRequest(http.MethodGet, "/login/sso", nil), session))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled SSO route = %d, want 404", rec.Code)
	}
}

func TestSSOLoginUsesTrustedProxyIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, peer, username, next string
		provision                  bool
		want                       int
	}{
		{"trusted", "10.0.0.5:1234", "alice", "/hosts?tag=prod", false, http.StatusSeeOther},
		{"external destination", "10.0.0.5:1234", "alice", "https://example.invalid", false, http.StatusSeeOther},
		{"missing identity", "10.0.0.5:1234", "", "/hosts", false, http.StatusUnauthorized},
		{"untrusted peer", "192.168.1.1:1234", "alice", "/hosts", false, http.StatusUnauthorized},
		{"unknown identity", "10.0.0.5:1234", "ghost", "/hosts", false, http.StatusUnauthorized},
		{"provision", "10.0.0.5:1234", "new-user", "/hosts", true, http.StatusSeeOther},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, db := newProxyTestHandler(t, tc.provision)
			if _, err := store.NewUserRepo(db).Create(domain.User{Username: "alice", Role: domain.RoleViewer, PasswordHash: "x", CreatedAt: "t", UpdatedAt: "t"}); err != nil {
				t.Fatal(err)
			}
			rec := getProxy(t, h, "/login/sso?next="+url.QueryEscape(tc.next), tc.peer, tc.username)
			if rec.Code != tc.want {
				t.Fatalf("SSO = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == http.StatusSeeOther {
				if got := rec.Header().Get("Location"); got != safeNext(tc.next) {
					t.Errorf("redirect = %q", got)
				}
			} else {
				if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), `role="alert"`) || !strings.Contains(rec.Body.String(), "SSO sign-in could not be completed") {
					t.Fatal("failed SSO must show an accessible login error")
				}
				if link := ssoLink(t, rec.Body.String()); link.Query().Get("next") != tc.next {
					t.Errorf("retry lost destination: %s", link)
				}
			}
			if cookieValue(rec, sessionCookieName) != "" {
				t.Error("proxy SSO must not create a standalone password session")
			}
			if tc.provision {
				u, err := store.NewUserRepo(db).GetByUsername(tc.username)
				if err != nil || u.Role != domain.RoleViewer {
					t.Fatalf("provisioning did not retain default role: user=%+v err=%v", u, err)
				}
			}
		})
	}
}

func TestLoginKeepsSSOAfterPasswordFailureAndThrottling(t *testing.T) {
	h, _ := newProxyTestHandler(t, false)
	for attempt := 0; attempt < 6; attempt++ {
		rec := postForm(t, h, "/login", url.Values{"username": {"missing"}, "password": {"wrong"}, "next": {"/hosts"}})
		want := http.StatusUnauthorized
		if attempt == 5 {
			want = http.StatusTooManyRequests
		}
		if rec.Code != want {
			t.Fatalf("attempt %d = %d, want %d", attempt, rec.Code, want)
		}
		if link := ssoLink(t, rec.Body.String()); link.Query().Get("next") != "/hosts" {
			t.Errorf("password failure lost SSO destination: %s", link)
		}
	}
}
