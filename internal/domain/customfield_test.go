package domain

import "testing"

func TestSlugifyCustomField(t *testing.T) {
	cases := map[string]string{
		"Asset Tag":    "asset_tag",
		"  Watts (W) ": "watts_w",
		"is-monitored": "is_monitored",
		"already_ok":   "already_ok",
		"###":          "",
	}
	for in, want := range cases {
		if got := SlugifyCustomField(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCustomFieldDefValidate(t *testing.T) {
	ok := CustomFieldDef{EntityType: "host", Name: "asset_tag", Label: "Asset tag", Kind: KindText}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid def rejected: %v", err)
	}
	bad := []CustomFieldDef{
		{EntityType: "nope", Name: "x", Label: "X", Kind: KindText},
		{EntityType: "host", Name: "Bad Name", Label: "X", Kind: KindText},
		{EntityType: "host", Name: "ok", Label: "  ", Kind: KindText},
		{EntityType: "host", Name: "ok", Label: "X", Kind: "weird"},
	}
	for i, d := range bad {
		if err := d.Validate(); err == nil {
			t.Errorf("bad def %d accepted", i)
		}
	}
}

func TestValidateCustomFieldValue(t *testing.T) {
	if v, err := ValidateCustomFieldValue(KindText, "  hi "); err != nil || v != "hi" {
		t.Fatalf("text: got %q, %v", v, err)
	}
	if v, err := ValidateCustomFieldValue(KindNumber, "12.5"); err != nil || v != "12.5" {
		t.Fatalf("number: got %q, %v", v, err)
	}
	if _, err := ValidateCustomFieldValue(KindNumber, "abc"); err == nil {
		t.Fatalf("number abc should fail")
	}
	if v, _ := ValidateCustomFieldValue(KindNumber, ""); v != "" {
		t.Fatalf("empty number should be empty, got %q", v)
	}
	if v, _ := ValidateCustomFieldValue(KindBool, "on"); v != "true" {
		t.Fatalf("bool on should be true, got %q", v)
	}
	if v, _ := ValidateCustomFieldValue(KindBool, ""); v != "false" {
		t.Fatalf("bool empty should be false, got %q", v)
	}
	if v, err := ValidateCustomFieldValue(KindDate, "2026-07-08"); err != nil || v != "2026-07-08" {
		t.Fatalf("date: got %q, %v", v, err)
	}
	if _, err := ValidateCustomFieldValue(KindDate, "07/08/2026"); err == nil {
		t.Fatalf("bad date should fail")
	}
}
