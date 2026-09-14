package officeapp

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestOfficePatchCASAndBundleAtomicity(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()

	t.Run("casTwoWritersSameRevision", func(t *testing.T) {
		base := generatedWord(t, svc, task, "cas-base")
		rev := artifactRevision(t, store, task.ID, base.ArtifactID)
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for _, label := range []string{"cas-writer-a", "cas-writer-b"} {
			req := textPatchFor(t, base, label)
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := svc.Patch(ctx, task.ID, base.ID, rev, req, label)
				results <- err
			}()
		}
		wg.Wait()
		close(results)
		success, conflict := 0, 0
		for err := range results {
			if err == nil {
				success++
			} else if errors.Is(err, domain.ErrConflict) {
				conflict++
			} else {
				t.Fatalf("unexpected CAS error: %v", err)
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatalf("CAS success=%d conflict=%d", success, conflict)
		}
		versions, err := store.ListOfficeVersions(ctx, task.ID, base.ArtifactID)
		if err != nil || len(versions) != 2 {
			t.Fatalf("winner not persisted once: n=%d err=%v", len(versions), err)
		}
		if _, err = svc.Patch(ctx, task.ID, base.ID, rev, textPatchFor(t, base, "late-same-revision"), "cas-late"); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("stale expectedRevision accepted: %v", err)
		}
	})

	xlsx, err := svc.Generate(ctx, task.ID, "经营.xlsx", casExcelSpec(), "bundle-xlsx")
	if err != nil {
		t.Fatal(err)
	}
	ppt, err := svc.Generate(ctx, task.ID, "汇报.pptx", casPptSpec(), "bundle-pptx")
	if err != nil {
		t.Fatal(err)
	}
	docx, err := svc.Generate(ctx, task.ID, "说明.docx", shortWordSpec(), "bundle-docx")
	if err != nil {
		t.Fatal(err)
	}
	pdfBytes, err := content.Generate(content.Spec{SchemaVersion: 1, Kind: content.PDF, Title: "同源阅读稿", Body: "独立 PDF 只承诺已支持的 title/body。"})
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := svc.Import(ctx, task.ID, "", "阅读稿.pdf", pdfBytes, "", 0, "bundle-pdf")
	if err != nil {
		t.Fatal(err)
	}
	bind := content.BindSameSourcePDF(content.DOCX, docx.SHA256, pdfBytes)
	if bind.SourceSHA256 != docx.SHA256 || bind.Stale || !bind.ValidFor(docx.SHA256) {
		t.Fatalf("same-source bind: %#v", bind)
	}
	if _, err = store.AddOfficeValidation(ctx, domain.Validation{
		VersionID: docx.ID, SHA256: docx.SHA256, Validator: "t17-same-source",
		Evidence: encode(map[string]any{"pdfRef": pdf.SHA256, "sameSourcePdf": bind}),
	}); err != nil {
		t.Fatal(err)
	}

	formal, err := svc.CreateBundle(ctx, task.ID, "经营交付", []string{xlsx.ID, ppt.ID, docx.ID, pdf.ID}, "formal-bundle")
	if err != nil {
		t.Fatal(err)
	}
	before := snapshotBundle(formal)
	excelBefore, err := store.ListOfficeVersions(ctx, task.ID, xlsx.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}

	_, xlsxBytes, err := svc.ReadVersion(ctx, task.ID, xlsx.ID)
	if err != nil {
		t.Fatal(err)
	}
	insp, err := content.Inspect(content.XLSX, xlsxBytes)
	if err != nil {
		t.Fatal(err)
	}
	fact := content.Fact{FactID: "amt", Value: "100", Currency: "CNY", Period: "2026-01"}
	node, ok := content.LocateFactNode(insp, fact)
	if !ok {
		t.Fatal("BindFact must locate the typed Excel cell; do not invent a second fact algebra")
	}
	if _, ok = content.BindFact(insp, content.Fact{FactID: "amt", Value: "100", Currency: "USD", Period: "2026-02"}); ok {
		t.Fatal("same number with different currency/period must not bind")
	}
	excelReq := excelRangePatch(t, xlsx, insp, node, content.Cell{Type: "number", Value: "200"})
	pptReq := textNodePatch(t, svc, task.ID, ppt, "初始状态", "应失败的修订")
	pptReq.Operations[0].ExpectedDigest = strings.Repeat("0", 64)

	members := []BundleMemberUpdate{
		{VersionID: xlsx.ID, ExpectedRevision: artifactRevision(t, store, task.ID, xlsx.ArtifactID), Request: excelReq},
		{VersionID: ppt.ID, ExpectedRevision: artifactRevision(t, store, task.ID, ppt.ArtifactID), Request: pptReq},
	}
	updateKey := "bundle-update-partial"
	got, err := svc.UpdateBundle(ctx, task.ID, formal.ID, members, updateKey)
	if err == nil || got.Published {
		t.Fatalf("Excel-ok + PPT-fail published: %#v err=%v", got, err)
	}
	if !errors.Is(err, domain.ErrConflict) && !errors.Is(err, content.ErrConflict) {
		t.Fatalf("PPT fail must be an explicit conflict, got %v", err)
	}

	after, err := svc.ListBundles(ctx, task.ID)
	if err != nil || len(after) != 1 || after[0].ID != formal.ID {
		t.Fatalf("formal bundle set changed: %#v %v", after, err)
	}
	reloaded, err := store.GetOfficeBundle(ctx, formal.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertFormalBundleUnchanged(t, before, reloaded)
	excelAfter, err := store.ListOfficeVersions(ctx, task.ID, xlsx.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if extra := successfulExcelNotInFormal(reloaded, excelAfter); extra > 1 {
		t.Fatalf("partial update minted %d extra Excel successes", extra)
	}

	retry, retryErr := svc.UpdateBundle(ctx, task.ID, formal.ID, members, updateKey)
	if retryErr == nil || retry.Published {
		t.Fatalf("retry published a partial bundle: %#v err=%v", retry, retryErr)
	}
	excelRetry, err := store.ListOfficeVersions(ctx, task.ID, xlsx.ArtifactID)
	if err != nil {
		t.Fatal(err)
	}
	if len(excelRetry) != len(excelBefore) && len(excelRetry) != len(excelAfter) {
		t.Fatalf("retry minted another Excel version: before=%d after=%d retry=%d", len(excelBefore), len(excelAfter), len(excelRetry))
	}
	if len(excelRetry) > len(excelAfter) {
		t.Fatal("retry created a second Excel success version")
	}
	reloaded, err = store.GetOfficeBundle(ctx, formal.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertFormalBundleUnchanged(t, before, reloaded)

	if !bind.ValidFor(docx.SHA256) {
		t.Fatal("same-source PDF unbound from pre-update DOCX SHA")
	}
	stale := content.InvalidateSameSourcePDF(bind, docx.SHA256)
	if stale.Stale || !stale.ValidFor(docx.SHA256) {
		t.Fatalf("failed bundle update staled an unchanged source: %#v", stale)
	}
	preview, err := svc.Preview(ctx, task.ID, docx.ID)
	if err != nil || !preview.PDFReady {
		t.Fatalf("unchanged DOCX lost same-source PDF: %#v %v", preview, err)
	}
	if !svc.SameSourceExport(ctx, docx) {
		t.Fatal("same-source export dropped after failed bundle update")
	}
	for _, v := range excelRetry {
		if v.ID == xlsx.ID {
			continue
		}
		if content.InvalidateSameSourcePDF(bind, v.SHA256).ValidFor(v.SHA256) {
			t.Fatal("same-source PDF auto-rebound to an Excel candidate")
		}
		candPreview, e := svc.Preview(ctx, task.ID, v.ID)
		if e != nil {
			t.Fatal(e)
		}
		if candPreview.PDFReady {
			t.Fatal("partial Excel candidate inherited the DOCX same-source PDF")
		}
		if svc.SameSourceExport(ctx, v) {
			t.Fatal("partial set treated as same-source formal")
		}
	}
}

func casExcelSpec() content.Spec {
	return content.Spec{
		SchemaVersion: 1, Kind: content.XLSX, Title: "经营",
		Sheets: []content.Sheet{{Name: "数据", FreezeHeader: true, Rows: [][]content.Cell{
			{{Type: "text", Value: "指标"}, {Type: "text", Value: "数值"}, {Type: "text", Value: "币种"}, {Type: "text", Value: "期间"}},
			{{Type: "text", Value: "收入"}, {Type: "number", Value: "100"}, {Type: "text", Value: "CNY"}, {Type: "text", Value: "2026-01"}},
		}}},
	}
}

func casPptSpec() content.Spec {
	return content.Spec{SchemaVersion: 1, Kind: content.PPTX, Title: "收入汇报", Slides: []content.Slide{{Title: "收入", Bullets: []string{"初始状态"}}}}
}

func artifactRevision(t *testing.T, store *sqlite.Store, taskID, artifactID string) int64 {
	t.Helper()
	heads, err := store.ListOfficeHeads(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range heads {
		if h.ArtifactID == artifactID {
			return h.Revision
		}
	}
	t.Fatalf("missing head for %s", artifactID)
	return 0
}

func snapshotBundle(b domain.Bundle) domain.Bundle {
	out := b
	out.Files = append([]domain.BundleFile(nil), b.Files...)
	return out
}

func assertFormalBundleUnchanged(t *testing.T, before, after domain.Bundle) {
	t.Helper()
	if after.ID != before.ID {
		t.Fatalf("formal bundle ID changed: %s -> %s", before.ID, after.ID)
	}
	if len(after.Files) != len(before.Files) {
		t.Fatalf("formal file set changed: %#v", after.Files)
	}
	for i, f := range before.Files {
		got := after.Files[i]
		if got.VersionID != f.VersionID || got.SHA256 != f.SHA256 || got.Kind != f.Kind || got.ArtifactID != f.ArtifactID {
			t.Fatalf("formal member %d changed: before=%#v after=%#v", i, f, got)
		}
	}
}

func successfulExcelNotInFormal(formal domain.Bundle, versions []domain.Version) int {
	inFormal := map[string]bool{}
	for _, f := range formal.Files {
		inFormal[f.VersionID] = true
	}
	extra := 0
	for _, v := range versions {
		if v.Kind == "xlsx" && !inFormal[v.ID] {
			extra++
		}
	}
	return extra
}

func excelRangePatch(t *testing.T, v domain.Version, insp content.Inspection, node content.Node, cell content.Cell) content.PatchRequest {
	t.Helper()
	partDigest := ""
	for _, p := range insp.Parts {
		if p.Name == node.Part {
			partDigest = p.SHA256
		}
	}
	if partDigest == "" {
		t.Fatalf("missing part digest for %s", node.Part)
	}
	addr := strings.TrimPrefix(node.Locator, "cell:")
	if addr == "" || addr == node.Locator {
		t.Fatalf("expected A1 locator, got %#v", node)
	}
	return content.PatchRequest{
		Kind: content.XLSX, BaseSHA256: v.SHA256,
		Ranges: []content.RangePatch{{
			Part: node.Part, Range: addr, ExpectedDigest: partDigest,
			Rows: [][]content.Cell{{cell}},
		}},
	}
}

func textNodePatch(t *testing.T, svc *Service, taskID string, v domain.Version, from, to string) content.PatchRequest {
	t.Helper()
	n := metricNode(t, svc, taskID, v, from)
	return content.PatchRequest{
		Kind: content.Kind(v.Kind), BaseSHA256: v.SHA256,
		Operations: []content.TextPatch{{NodeID: n.ID, ExpectedDigest: n.Digest, Text: to}},
	}
}
