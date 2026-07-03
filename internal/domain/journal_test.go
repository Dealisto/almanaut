package domain

import "testing"

func TestJournalEntryValidate(t *testing.T) {
	ok := JournalEntry{Kind: JournalIncident, Body: "disk failed"}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid entry rejected: %v", err)
	}
	if err := (JournalEntry{Kind: "bogus", Body: "x"}).Validate(); err == nil {
		t.Error("bad kind accepted")
	}
	if err := (JournalEntry{Kind: JournalInfo, Body: "   "}).Validate(); err == nil {
		t.Error("blank body accepted")
	}
}
