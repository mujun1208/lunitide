package officerender_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ledongthuc/pdf"
	"github.com/lunitide/lunitide/internal/officerender"
	"github.com/lunitide/lunitide/internal/officestudio"
)

func TestOfficeNativeThirtyChapterDocument(t *testing.T) {
	executable := os.Getenv("OFFICE_STUDIO_TEST_LIBREOFFICE")
	if executable == "" {
		t.Skip("set OFFICE_STUDIO_TEST_LIBREOFFICE for actual long-document validation")
	}
	spec := officestudio.Spec{SchemaVersion: 1, Kind: officestudio.DOCX, Title: "长文目录验收", Document: &officestudio.DocumentOptions{Header: "合成测试材料", Footer: "原文保留", PageNumbers: true}, Blocks: []officestudio.Block{{Type: "toc"}}}
	for i := 1; i <= 30; i++ {
		spec.Blocks = append(spec.Blocks, officestudio.Block{Type: "pagebreak"}, officestudio.Block{Type: "heading", Text: fmt.Sprintf("Chapter-%02d", i)}, officestudio.Block{Type: "paragraph", Text: fmt.Sprintf("本章验证分页、目录与完整正文。编号 2100040404301 保持不变。原文末尾标记-%02d", i)})
	}
	data, err := officestudio.Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	r := &officerender.Renderer{Root: t.TempDir(), Executable: executable, Preflight: func(kind string, body []byte) error {
		result, err := officestudio.Inspect(officestudio.Kind(kind), body)
		if err != nil {
			return err
		}
		if !result.RenderAllowed {
			return errors.New("long document fixture preflight denied")
		}
		return nil
	}}
	started := time.Now()
	result, err := r.RenderWithChecks(context.Background(), "docx", data, officerender.NativeOptions{UpdateFields: true, ExportUpdatedCopy: true})
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(started)
	merged, err := officestudio.MergeNativeCaches(officestudio.DOCX, data, result.UpdatedOffice)
	if err != nil {
		t.Fatal("long-document selective cache merge:", err)
	}
	reader, err := pdf.NewReader(bytes.NewReader(result.PDF), int64(len(result.PDF)))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := reader.GetPlainText()
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(plain)
	if err != nil {
		t.Fatal(err)
	}
	if dir := os.Getenv("OFFICE_STUDIO_TEST_EVIDENCE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		report, err := json.MarshalIndent(map[string]any{"pages": reader.NumPage(), "elapsedMs": elapsed.Milliseconds(), "sourceSHA256": result.SourceDigest, "mergedSHA256": merged.OutputSHA256, "pdfSHA256": result.SHA256, "native": result.Native, "rendererVersion": result.RendererVersion, "changedParts": merged.ChangedParts, "scope": "one synthetic 30-chapter document, not a 30-template golden set or P95"}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		for name, body := range map[string][]byte{"long-toc-source.docx": data, "long-toc-merged.docx": merged.Data, "long-toc-preview.pdf": result.PDF, "long-toc-report.json": report, "long-toc-text.txt": body} {
			if err := os.WriteFile(filepath.Join(dir, name), body, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if reader.NumPage() < 31 || reader.NumPage() > 33 {
		t.Fatalf("unexpected pagination: %d", reader.NumPage())
	}
	// PDF extraction inserts line/run breaks inside this marker. Compare its
	// visible characters; independently check the untouched source XML below.
	visibleText := strings.Join(strings.Fields(string(body)), "")
	for i := 1; i <= 30; i++ {
		if title := fmt.Sprintf("Chapter-%02d", i); strings.Count(string(body), title) != 2 {
			t.Fatalf("TOC/body occurrence missing for %s", title)
		}
		if marker := fmt.Sprintf("原文末尾标记-%02d", i); strings.Count(visibleText, marker) != 1 {
			t.Fatalf("body incomplete at %s", marker)
		}
	}
	if strings.Count(string(body), "2100040404301") != 30 {
		t.Fatal("long identifiers changed")
	}
	if result.Native == nil || !result.Native.FieldsRefreshed || !result.Native.SourceUnchanged || result.Native.UpdatedIndexes != 1 {
		t.Fatalf("invalid native evidence: %+v", result.Native)
	}
	original, refreshed := nativeZipParts(t, data), nativeZipParts(t, merged.Data)
	for i := 1; i <= 30; i++ {
		marker := []byte(fmt.Sprintf("原文末尾标记-%02d", i))
		if bytes.Count(original["word/document.xml"], marker) != 1 || bytes.Count(refreshed["word/document.xml"], marker) != 1 {
			t.Fatalf("source marker changed: %s", marker)
		}
	}
	allowed := map[string]bool{}
	for _, name := range merged.ChangedParts {
		allowed[name] = true
	}
	for name, before := range original {
		if !allowed[name] && !bytes.Equal(before, refreshed[name]) {
			t.Fatalf("unrelated part changed: %s", name)
		}
	}
	if len(original) != len(refreshed) {
		t.Fatal("package member set changed")
	}

	t.Logf("actual native result: %d pages, %d updated fields, %s", reader.NumPage(), merged.UpdatedFields, elapsed)
}
