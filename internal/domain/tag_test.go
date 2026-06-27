package domain

import "testing"

func TestNormalizeTag(t *testing.T) {
	cases := map[string]string{
		"  #Media ": "media",
		"Critical":  "critical",
		"#prod":     "prod",
		"   ":       "",
	}
	for in, want := range cases {
		if got := NormalizeTag(in); got != want {
			t.Errorf("NormalizeTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTagValidate(t *testing.T) {
	if err := (Tag{EntityType: "host", EntityID: 1, Name: "media"}).Validate(); err != nil {
		t.Errorf("valid tag rejected: %v", err)
	}
	if err := (Tag{EntityType: "widget", EntityID: 1, Name: "media"}).Validate(); err == nil {
		t.Error("bad entity type accepted")
	}
	if err := (Tag{EntityType: "host", EntityID: 1, Name: "  "}).Validate(); err == nil {
		t.Error("empty tag name accepted")
	}
	if err := (Tag{EntityType: "host", EntityID: 0, Name: "media"}).Validate(); err == nil {
		t.Error("zero entity id accepted")
	}
}
