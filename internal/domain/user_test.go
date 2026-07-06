package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUserValidate(t *testing.T) {
	if err := (User{Username: "admin"}).Validate(); err != nil {
		t.Fatalf("valid user rejected: %v", err)
	}
	if err := (User{Username: "  "}).Validate(); err == nil {
		t.Fatal("blank username must be rejected")
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("short"); err == nil {
		t.Fatal("too-short password must be rejected")
	}
	if err := ValidatePassword("longenough"); err != nil {
		t.Fatalf("valid password rejected: %v", err)
	}
}

func TestUserPasswordHashNotSerialised(t *testing.T) {
	b, err := json.Marshal(User{Username: "admin", PasswordHash: "secret-hash"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "secret-hash") {
		t.Fatalf("password hash leaked into JSON: %s", b)
	}
}
