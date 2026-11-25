package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Dealisto/almanaut/internal/domain"
	"github.com/Dealisto/almanaut/internal/store"
)

var rawTokenRE = regexp.MustCompile(`alm_[A-Za-z0-9_-]+`)

func TestTokenUIExpiryAndAuthentication(t *testing.T) {
	h, db := totpTestHandler(t)
	session := loginAs(t, h, "admin", "password123")
	rec := postAuthForm(t, h, session, "/account/tokens", map[string]string{"label": "expiring", "scope": "read-only", "expiry_days": "7"})
	if rec.Code != http.StatusOK {
		t.Fatalf("create=%d %s", rec.Code, rec.Body)
	}
	raw := rawTokenRE.FindString(rec.Body.String())
	if raw == "" {
		t.Fatal("new token missing")
	}
	tokens := store.NewTokenRepo(db)
	list, err := tokens.ListByUser(1)
	if err != nil || len(list) != 1 {
		t.Fatalf("tokens=%+v err=%v", list, err)
	}
	expires, err := time.Parse(time.RFC3339, list[0].ExpiresAt)
	if err != nil || time.Until(expires) < 6*24*time.Hour || time.Until(expires) > 8*24*time.Hour {
		t.Fatalf("expiry=%q", list[0].ExpiresAt)
	}
	req := httptest.NewRequest("GET", "/api/hosts", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid token=%d", rec.Code)
	}
	list, _ = tokens.ListByUser(1)
	if list[0].LastUsedAt == "" {
		t.Fatal("first use not recorded")
	}
	if _, err := db.Exec(`UPDATE api_tokens SET expires_at=? WHERE id=?`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), list[0].ID); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired token=%d", rec.Code)
	}
	rec = postAuthForm(t, h, session, "/account/tokens", map[string]string{"label": "bad", "expiry_days": "-1"})
	if !strings.Contains(rec.Body.String(), "invalid token lifetime") {
		t.Fatalf("invalid expiry accepted: %s", rec.Body)
	}
	list, _ = tokens.ListByUser(1)
	if len(list) != 1 {
		t.Fatal("invalid expiry created token")
	}
}

func TestTokenRotationPreservesAccessUntilReplacementIsUsed(t *testing.T) {
	h, db := totpTestHandler(t)
	session := loginAs(t, h, "admin", "password123")
	tokens := store.NewTokenRepo(db)
	oldRaw := "alm_original"
	oldID, err := tokens.Create(store.APIToken{TokenHash: hashToken(oldRaw), UserID: 1, Label: "integration", Scope: "read-only", CreatedAt: nowRFC3339()})
	if err != nil {
		t.Fatal(err)
	}
	rec := postAuthForm(t, h, session, fmt.Sprintf("/account/tokens/%d/rotate", oldID), map[string]string{"expiry_days": "30", "scope": "read-write"})
	if rec.Code != http.StatusOK {
		t.Fatalf("prepare=%d %s", rec.Code, rec.Body)
	}
	raw := rawTokenRE.FindString(rec.Body.String())
	if raw == "" {
		t.Fatal("replacement secret missing")
	}
	list, _ := tokens.ListByUser(1)
	if len(list) != 2 {
		t.Fatalf("rotation removed old token: %+v", list)
	}
	replacement := list[0]
	if replacement.Scope != "read-only" || replacement.RotatedFromID != oldID || replacement.Label != "integration" {
		t.Fatalf("replacement changed owner/scope/label: %+v", replacement)
	}
	if _, _, err := tokens.UserByToken(hashToken(oldRaw)); err != nil {
		t.Fatalf("old token stopped working during rotation: %v", err)
	}
	rec = postAuthForm(t, h, session, fmt.Sprintf("/account/tokens/%d/rotate", oldID), nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate prepare=%d", rec.Code)
	}
	finish := fmt.Sprintf("/account/tokens/%d/finish-rotation", replacement.ID)
	rec = postAuthForm(t, h, session, finish, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("unused replacement retired original: %d", rec.Code)
	}
	req := httptest.NewRequest("GET", "/account/tokens", nil)
	req.AddCookie(session)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), raw) {
		t.Fatal("replacement secret leaked on GET")
	}
	apiReq := httptest.NewRequest("GET", "/api/hosts", nil)
	apiReq.Header.Set("Authorization", "Bearer "+raw)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, apiReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("replacement request=%d", rec.Code)
	}
	if _, err := db.Exec(`UPDATE api_tokens SET expires_at=? WHERE id=?`, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), replacement.ID); err != nil {
		t.Fatal(err)
	}
	rec = postAuthForm(t, h, session, finish, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expired replacement retired original: %d", rec.Code)
	}
	if _, err := db.Exec(`UPDATE api_tokens SET expires_at=? WHERE id=?`, replacement.ExpiresAt, replacement.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_retirement BEFORE DELETE ON api_tokens BEGIN SELECT RAISE(ABORT,'retirement failed'); END`); err != nil {
		t.Fatal(err)
	}
	rec = postAuthForm(t, h, session, finish, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("failed retirement=%d", rec.Code)
	}
	pending, err := tokens.GetByOwner(replacement.ID, 1)
	if err != nil || pending.RotatedFromID != oldID {
		t.Fatalf("failed rotation partially committed: %+v err=%v", pending, err)
	}
	if _, _, err := tokens.UserByToken(hashToken(oldRaw)); err != nil {
		t.Fatal("failed rotation revoked old token")
	}
	if _, err := db.Exec(`DROP TRIGGER fail_retirement`); err != nil {
		t.Fatal(err)
	}
	rec = postAuthForm(t, h, session, finish, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("finish=%d %s", rec.Code, rec.Body)
	}
	if _, _, err := tokens.UserByToken(hashToken(oldRaw)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("old token survives: %v", err)
	}
	pending, err = tokens.GetByOwner(replacement.ID, 1)
	if err != nil || pending.RotatedFromID != 0 {
		t.Fatalf("completed rotation still pending: %+v err=%v", pending, err)
	}
	if _, _, err := tokens.UserByToken(hashToken(raw)); err != nil {
		t.Fatalf("replacement revoked: %v", err)
	}
}

func TestTokenRotationIsOwnerScopedAndRollsBackPreparation(t *testing.T) {
	h, db := totpTestHandler(t)
	session := loginAs(t, h, "admin", "password123")
	other, err := store.NewUserRepo(db).Create(domain.User{Username: "bob", Role: domain.RoleViewer, PasswordHash: "h", CreatedAt: nowRFC3339(), UpdatedAt: nowRFC3339()})
	if err != nil {
		t.Fatal(err)
	}
	tokens := store.NewTokenRepo(db)
	otherID, err := tokens.Create(store.APIToken{TokenHash: "other", UserID: other, Label: "private", Scope: "agent", CreatedAt: nowRFC3339()})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"rotate", "finish-rotation"} {
		rec := postAuthForm(t, h, session, fmt.Sprintf("/account/tokens/%d/%s", otherID, action), nil)
		if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), "private") {
			t.Fatalf("cross-owner %s: %d %s", action, rec.Code, rec.Body)
		}
	}
	ownID, err := tokens.Create(store.APIToken{TokenHash: "own", UserID: 1, Label: "own", Scope: "read-only", CreatedAt: nowRFC3339()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TRIGGER fail_rotation BEFORE INSERT ON api_tokens BEGIN SELECT RAISE(ABORT,'rotation unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	rec := postAuthForm(t, h, session, fmt.Sprintf("/account/tokens/%d/rotate", ownID), nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("prepare failure=%d", rec.Code)
	}
	list, _ := tokens.ListByUser(1)
	if len(list) != 1 || list[0].ID != ownID {
		t.Fatalf("failed preparation changed credentials: %+v", list)
	}
}
