package web

import "testing"

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
