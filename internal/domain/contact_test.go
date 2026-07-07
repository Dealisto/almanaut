package domain

import "testing"

func TestContactValidate(t *testing.T) {
	if err := (Contact{Name: "Ada", Email: "ada@example.com"}).Validate(); err != nil {
		t.Errorf("valid contact rejected: %v", err)
	}
	if err := (Contact{Name: "Ada"}).Validate(); err != nil {
		t.Errorf("contact without email should be valid: %v", err)
	}
	if err := (Contact{Name: "  "}).Validate(); err == nil {
		t.Error("blank name should be rejected")
	}
	if err := (Contact{Name: "Ada", Email: "not-an-email"}).Validate(); err == nil {
		t.Error("email without @ should be rejected")
	}
}
