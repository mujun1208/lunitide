package ocrapp

import "testing"

func TestBatchOCRKeepsUnknownConfidenceAndRetriesOnlyFailed(t *testing.T) {
	pages := []BatchPage{
		{File: "a.pdf", Page: 1, HasText: true, Status: "confirmed"},
		{File: "a.pdf", Page: 2, HasText: false, Status: "failed"},
		{File: "b.pdf", Page: 1, HasText: true, Status: "confirmed"},
	}
	got := IncompletePages(pages)
	if len(got) != 1 || got[0].Page != 2 {
		t.Fatalf("only failed pages retry: %+v", got)
	}
	if ConfidenceOrUnknown("") != "unknown" {
		t.Fatal("missing confidence must stay unknown, not a percentage")
	}
}
