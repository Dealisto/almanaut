package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Dealisto/almanaut/internal/domain"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	if hash == "correct horse battery staple" {
		t.Fatal("password stored in plaintext")
	}
	if !verifyPassword(hash, "correct horse battery staple") {
		t.Fatal("correct password rejected")
	}
	if verifyPassword(hash, "wrong") {
		t.Fatal("wrong password accepted")
	}
}

func TestNewSessionTokenIsRandomAndHashable(t *testing.T) {
	a, err := newSessionToken()
	if err != nil {
		t.Fatalf("newSessionToken: %v", err)
	}
	b, _ := newSessionToken()
	if a == "" || a == b {
		t.Fatalf("tokens not unique: %q %q", a, b)
	}
	if hashToken(a) == a || len(hashToken(a)) != 64 {
		t.Fatalf("hashToken should be a 64-char sha256 hex, got %q", hashToken(a))
	}
	if hashToken(a) != hashToken(a) {
		t.Fatal("hashToken not deterministic")
	}
}

func TestUserContextRoundTrip(t *testing.T) {
	ctx := withUser(context.Background(), domain.User{ID: 1, Username: "alice"})
	u, ok := userFrom(ctx)
	if !ok || u.Username != "alice" {
		t.Fatalf("userFrom = %+v, %v", u, ok)
	}
	if _, ok := userFrom(context.Background()); ok {
		t.Fatal("empty context must report no user")
	}
}

func TestActorReturnsSessionUsername(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(withUser(req.Context(), domain.User{Username: "bob"}))
	if got := actor(req); got != "bob" {
		t.Fatalf("actor = %q, want bob", got)
	}
	// No user in context → empty actor (unchanged legacy behaviour).
	if got := actor(httptest.NewRequest(http.MethodGet, "/", nil)); got != "" {
		t.Fatalf("actor without user = %q, want empty", got)
	}
}
