package officestudio

import (
	"strings"
	"testing"
)

func TestVisualManifestRejectsFirstPageOnly(t *testing.T) {
	man := VisualManifest{PageCount: 3, PageDigests: []string{"aaa"}}
	if err := ValidateVisualManifest(man, 3); err == nil {
		t.Fatal("one digest for three pages must fail")
	}
	if err := ValidateVisualProcess(0, "", nil); err == nil {
		t.Fatal("exit 0 with empty JSON must fail")
	}
}

func TestVisualManifestRequiresEveryIndex(t *testing.T) {
	man := VisualManifest{
		SchemaVersion: 2,
		PageCount:     3,
		PageDigests:   []string{"aaa", "bbb", "ccc"},
		Pages: []VisualPage{
			{Index: 0, Path: "page-000.bin", SHA256: "aaa"},
			{Index: 1, Path: "page-001.bin", SHA256: "bbb"},
			{Index: 2, Path: "page-002.bin", SHA256: "ccc"},
		},
	}
	if err := ValidateVisualManifest(man, 3); err != nil {
		t.Fatal(err)
	}
	man.Pages = man.Pages[:2]
	if err := ValidateVisualManifest(man, 3); err == nil {
		t.Fatal("missing last page must fail")
	}
}

func TestValidateVisualProcessRejectsExitZeroWithoutObject(t *testing.T) {
	if err := ValidateVisualProcess(0, "   ", []byte("libreoffice ok")); err == nil {
		t.Fatal("whitespace stdout is not JSON")
	}
	if err := ValidateVisualProcess(0, "not-json", nil); err == nil {
		t.Fatal("non-JSON stdout must fail")
	}
	stdout := `{"reviewer":"fixture","version":"1","reviewedPages":[0,1],"issues":[]}`
	if err := ValidateVisualProcess(0, stdout, nil); err != nil {
		t.Fatal(err)
	}
}

func TestVisualPageCoverageRejectsPartialReview(t *testing.T) {
	c := VisualPageCoverageCheck(3, []int{0})
	if c.ID != "page-coverage" || c.Status == "passed" {
		t.Fatalf("first page only must not mark full coverage: %#v", c)
	}
	if strings.Contains(c.Message, "高端商用") && !strings.Contains(c.Message, "不能") {
		t.Fatalf("must not claim 高端商用: %q", c.Message)
	}
	ok := VisualPageCoverageCheck(2, []int{1, 0})
	if ok.Status != "passed" {
		t.Fatalf("complete coverage: %#v", ok)
	}
	if strings.Contains(ok.Message, "高端商用") && !strings.Contains(ok.Message, "不能") {
		t.Fatalf("passed coverage must not claim 高端商用: %q", ok.Message)
	}
}
