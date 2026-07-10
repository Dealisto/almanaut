package domain

import (
	"testing"
	"time"
)

func TestTOTPRoundTrip(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	code, err := TOTPCode(secret, now)
	if err != nil {
		t.Fatalf("TOTPCode: %v", err)
	}
	if len(code) != totpDigits {
		t.Fatalf("code %q length = %d, want %d", code, len(code), totpDigits)
	}
	if !VerifyTOTP(secret, code, now) {
		t.Error("freshly generated code should verify")
	}
	// A code from two steps ago is outside the ±1 skew window.
	old, _ := TOTPCode(secret, now.Add(-90*time.Second))
	if VerifyTOTP(secret, old, now) {
		t.Error("code from 3 steps ago should not verify")
	}
	if VerifyTOTP(secret, "000000", now.Add(24*time.Hour)) && code == "000000" {
		t.Skip("astronomically unlikely collision")
	}
}

func TestTOTPKnownVector(t *testing.T) {
	// RFC 6238 test vector: secret "12345678901234567890" (ASCII) as base32,
	// at T=59s the SHA1 8-digit code is 94287082 → 6-digit 287082.
	secret := totpEnc.EncodeToString([]byte("12345678901234567890"))
	code, err := TOTPCode(secret, time.Unix(59, 0).UTC())
	if err != nil {
		t.Fatalf("TOTPCode: %v", err)
	}
	if code != "287082" {
		t.Errorf("RFC6238 vector = %s, want 287082", code)
	}
}

func TestVerifyTOTPSkew(t *testing.T) {
	secret, _ := GenerateTOTPSecret()
	now := time.Now().UTC()
	prev, _ := TOTPCode(secret, now.Add(-30*time.Second))
	next, _ := TOTPCode(secret, now.Add(30*time.Second))
	if !VerifyTOTP(secret, prev, now) {
		t.Error("previous step code should verify (±1 skew)")
	}
	if !VerifyTOTP(secret, next, now) {
		t.Error("next step code should verify (±1 skew)")
	}
	if VerifyTOTP(secret, "12345", now) {
		t.Error("wrong-length code must not verify")
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, err := GenerateRecoveryCodes(10)
	if err != nil {
		t.Fatalf("GenerateRecoveryCodes: %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("got %d codes, want 10", len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if seen[c] {
			t.Errorf("duplicate recovery code %q", c)
		}
		seen[c] = true
		if NormalizeRecoveryCode(c) != NormalizeRecoveryCode(" "+c+" ") {
			t.Errorf("normalize not idempotent for %q", c)
		}
	}
	// Formatting variants normalize to the same value.
	if NormalizeRecoveryCode("ABCDE-FGHIJ") != "abcdefghij" {
		t.Errorf("normalize = %q", NormalizeRecoveryCode("ABCDE-FGHIJ"))
	}
}
