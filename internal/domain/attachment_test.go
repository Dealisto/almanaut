package domain

import (
	"strings"
	"testing"
)

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"invoice.pdf":            "invoice.pdf",
		`../../etc/passwd`:       "passwd",
		`C:\Users\x\config.yaml`: "config.yaml",
		"  spaced name.txt  ":    "spaced name.txt",
		"/only/dirs/":            "file", // trailing slash → empty → fallback
		"":                       "file",
	}
	for in, want := range cases {
		if got := SanitizeFilename(in); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
	// control characters are stripped
	if got := SanitizeFilename("a\x00b\x1fc.txt"); got != "abc.txt" {
		t.Errorf("control chars: got %q", got)
	}
}

func TestAttachmentValidate(t *testing.T) {
	ok := Attachment{EntityType: "host", EntityID: 1, Filename: "x.pdf", Size: 10}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid attachment rejected: %v", err)
	}
	bad := []Attachment{
		{EntityType: "nope", EntityID: 1, Filename: "x", Size: 1},
		{EntityType: "host", EntityID: 0, Filename: "x", Size: 1},
		{EntityType: "host", EntityID: 1, Filename: "  ", Size: 1},
		{EntityType: "host", EntityID: 1, Filename: "x", Size: 0},
		{EntityType: "host", EntityID: 1, Filename: "x", Size: MaxAttachmentBytes + 1},
	}
	for i, a := range bad {
		if err := a.Validate(); err == nil {
			t.Errorf("bad attachment %d accepted", i)
		}
	}
	// a filename that sanitises to non-empty passes; whitespace-only fails
	if err := (Attachment{EntityType: "host", EntityID: 1, Filename: strings.Repeat(" ", 3), Size: 1}).Validate(); err == nil {
		t.Errorf("whitespace filename should fail")
	}
}
