package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Dealisto/almanaut/internal/store"
)

func TestTOTPConcurrentFailuresRespectLoginBudget(t *testing.T) {
	h, db := totpTestHandler(t)
	session := loginAs(t, h, "admin", "password123")
	_, recovery := enroll2FA(t, h, db, session, 1)
	pending := cookieNamed(passwordLogin(t, h, "admin", "password123").Result().Cookies(), pending2FACookieName)
	if pending == nil {
		t.Fatal("password login did not issue a pending challenge")
	}
	const attempts = 20
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, attempts)
	for range attempts {
		go func() {
			req := httptest.NewRequest(http.MethodPost, "/login/2fa", strings.NewReader("code=invalid&csrf_token=concurrent-test"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(pending)
			req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "concurrent-test"})
			rec := httptest.NewRecorder()
			<-start
			h.ServeHTTP(rec, req)
			results <- rec
		}()
	}
	close(start)
	statuses := make(map[int]int)
	for range attempts {
		rec := <-results
		statuses[rec.Code]++
		if cookieValue(rec, sessionCookieName) != "" {
			t.Error("invalid second factor issued a session")
		}
	}
	if statuses[http.StatusUnauthorized] != loginFailureThreshold || statuses[http.StatusTooManyRequests] != attempts-loginFailureThreshold {
		t.Fatalf("concurrent requests exceeded login budget: statuses=%v", statuses)
	}
	if rec := submit2FA(t, h, pending, "recovery="+recovery[0]); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("valid recovery code bypassed lockout: status=%d", rec.Code)
	}
	if left, err := store.NewTOTPRepo(db).CountUnusedRecovery(1); err != nil || left != recoveryCodeCount {
		t.Fatalf("locked request consumed recovery code: remaining=%d err=%v", left, err)
	}
}
