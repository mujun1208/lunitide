package app

import (
	"encoding/json"
	"strings"
	"testing"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
)

func TestOfficeSourceTextPagingPreservesAllRunesAndEvidence(t *testing.T) {
	source := officeText{text: strings.Repeat("中文商业计划书\"数据\"\n<引用>\\\t", 600), method: "windows-ocr", pages: 27}
	version := domain.Version{ID: "v1", SHA256: "immutable-hash", Kind: "pdf"}
	var restored strings.Builder
	offset := 0
	for i := 0; i < 100; i++ {
		raw, err := officeSourceTextPage("task", version, source, offset)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > officeToolPageLimit {
			t.Fatalf("oversized tool page: %d", len(raw))
		}
		var page struct {
			Text           string
			NextTextOffset int
			HasMore        bool
			Notice         string
			Method         string
			PageCount      int
		}
		if err = json.Unmarshal([]byte(raw), &page); err != nil {
			t.Fatal(err)
		}
		if page.Method != "windows-ocr" || page.PageCount != 27 || !strings.Contains(page.Notice, "misread") {
			t.Fatal("missing OCR uncertainty", raw)
		}
		restored.WriteString(page.Text)
		if !page.HasMore {
			break
		}
		if page.NextTextOffset <= offset {
			t.Fatal("paging made no progress")
		}
		offset = page.NextTextOffset
	}
	if restored.String() != source.text {
		t.Fatal("source text was truncated or duplicated")
	}
	if _, err := officeSourceTextPage("task", version, source, -1); err == nil {
		t.Fatal("negative offset accepted")
	}
}
