package officeapp

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
	"github.com/oklog/ulid/v2"
)

func TestDiffRealSnapshotsKeepHashesAndHeadUntouched(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	base := generatedWord(t, svc, task, "diff-base")
	_, original, err := svc.ReadVersion(ctx, task.ID, base.ID)
	if err != nil {
		t.Fatal(err)
	}
	version, err := svc.Patch(ctx, task.ID, base.ID, 1, textPatchFor(t, base, "已完成复核"), "diff-patch")
	if err != nil {
		t.Fatal(err)
	}
	head := headOf(t, store, task.ID)
	r, err := svc.Diff(ctx, task.ID, base.ID, version.ID, DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.BaseVersionID != base.ID || r.VersionID != version.ID || r.BaseSHA256 != base.SHA256 || r.SHA256 != version.SHA256 || r.BytesIdentical || r.PayloadIdentical || !r.PartHashesCompared || r.ComparisonBasis != "structure" {
		t.Fatalf("wrong evidence: %+v", r)
	}
	if r.Summary.ModifiedNodes != 1 || r.Summary.ModifiedParts != 1 || r.Summary.AddedNodes != 0 || r.Summary.DeletedNodes != 0 || len(r.Changes) != 1 || len(r.Parts) != 1 || r.Parts[0].Name != "word/document.xml" {
		t.Fatalf("wrong changes: %+v", r)
	}
	c := r.Changes[0]
	if c.Change != "modified" || c.Before.Text != "初始状态" || c.After.Text != "已完成复核" || c.Before.Digest == c.After.Digest || c.NodeID == "" {
		t.Fatalf("invented delta: %+v", c)
	}
	i, err := content.Inspect(content.DOCX, original)
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary.UnchangedParts != len(i.Parts)-1 {
		t.Fatal("non-target part verification is incomplete", r.Summary)
	}
	var unchangedBytes int64
	for _, p := range i.Parts {
		if p.Name != "word/document.xml" {
			unchangedBytes += int64(p.Size)
		}
	}
	if r.Summary.UnchangedBytes != unchangedBytes {
		t.Fatal("unverified byte count", r.Summary.UnchangedBytes, unchangedBytes)
	}
	if next := headOf(t, store, task.ID); next != head {
		t.Fatal("read-only diff changed version head")
	}
	_, afterRead, err := svc.ReadVersion(ctx, task.ID, base.ID)
	if err != nil || !bytes.Equal(afterRead, original) {
		t.Fatal("diff mutated baseline", err)
	}
	same, err := svc.Diff(ctx, task.ID, base.ID, base.ID, DiffOptions{})
	if err != nil || !same.BytesIdentical || !same.PayloadIdentical || same.TotalNodeChanges != 0 || same.TotalPartChanges != 0 || same.Summary.UnchangedParts != len(i.Parts) {
		t.Fatalf("identity comparison: %+v %v", same, err)
	}
}

func diffZIP(t *testing.T, data []byte, replacements map[string][]byte, removed map[string]bool) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range zr.File {
		if removed[f.Name] {
			continue
		}
		if _, ok := replacements[f.Name]; ok {
			continue
		}
		if err = zw.Copy(f); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range replacements {
		w, e := zw.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(body); e != nil {
			t.Fatal(e)
		}
	}
	if err = zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestDiffDetectsOpaquePartChangesWithoutPretendingNodesChanged(t *testing.T) {
	svc, _, task := studioServiceFixture(t)
	ctx := context.Background()
	source, err := content.Generate(shortWordSpec())
	if err != nil {
		t.Fatal(err)
	}
	first := diffZIP(t, source, map[string][]byte{"custom/untouched.bin": []byte("keep"), "custom/modified.bin": []byte("before"), "custom/deleted.bin": []byte("removed")}, nil)
	second := diffZIP(t, first, map[string][]byte{"custom/modified.bin": []byte("after"), "custom/added.bin": []byte("added")}, map[string]bool{"custom/deleted.bin": true})
	b, err := svc.Import(ctx, task.ID, "", "原稿.docx", first, "", 0, "opaque-base")
	if err != nil {
		t.Fatal(err)
	}
	v, err := svc.Import(ctx, task.ID, b.ArtifactID, "原稿.docx", second, b.ID, 1, "opaque-next")
	if err != nil {
		t.Fatal(err)
	}
	r, err := svc.Diff(ctx, task.ID, b.ID, v.ID, DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.TotalNodeChanges != 0 || r.TotalPartChanges != 3 || r.Summary.AddedParts != 1 || r.Summary.DeletedParts != 1 || r.Summary.ModifiedParts != 1 || r.PayloadIdentical {
		t.Fatalf("opaque changes hidden: %+v", r)
	}
	for _, p := range r.Parts {
		if p.Change == "modified" && p.BeforeSHA256 == p.SHA256 {
			t.Fatal("part comparison lost raw digests")
		}
	}
}

func TestDiffAddsDeletesAndComparesSharedStringValuesAtStableCell(t *testing.T) {
	svc, _, task := studioServiceFixture(t)
	ctx := context.Background()
	spec := content.Spec{SchemaVersion: 1, Kind: content.XLSX, Title: "Data", Sheets: []content.Sheet{{Name: "Data", Rows: [][]content.Cell{{{Type: "text", Value: "old"}, {Type: "text", Value: "removed"}}}}}}
	b, err := svc.Generate(ctx, task.ID, "数据.xlsx", spec, "cells-base")
	if err != nil {
		t.Fatal(err)
	}
	spec.Sheets[0].Rows = [][]content.Cell{{{Type: "text", Value: "new"}, {Type: "blank"}, {Type: "text", Value: "added"}}}
	data, err := content.Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	v, err := svc.Import(ctx, task.ID, b.ArtifactID, "数据.xlsx", data, b.ID, 1, "cells-next")
	if err != nil {
		t.Fatal(err)
	}
	r, err := svc.Diff(ctx, task.ID, b.ID, v.ID, DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary.AddedNodes != 1 || r.Summary.DeletedNodes != 0 || r.Summary.ModifiedNodes != 2 {
		t.Fatalf("cell locations not compared correctly: %+v", r)
	}
	sharedValueCompared := false
	for _, change := range r.Changes {
		if change.Locator == "cell:A1" {
			sharedValueCompared = true
			if change.Before.Text != "old" || change.After.Text != "new" || change.Before.Digest != change.After.Digest {
				t.Fatal("shared-string value should change with the original cell XML retained", change)
			}
		}
	}
	if !sharedValueCompared {
		t.Fatal("shared-string dictionary mutation was not detected at its cell")
	}
	// A styled blank cell still exists at B1. Its value/type changed; it was not
	// physically deleted. Verify deletion separately using an absent row.
	spec.Sheets[0].Rows = [][]content.Cell{{{Type: "text", Value: "new"}}}
	data, err = content.Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	last, err := svc.Import(ctx, task.ID, b.ArtifactID, "数据.xlsx", data, v.ID, 2, "cells-last")
	if err != nil {
		t.Fatal(err)
	}
	del, err := svc.Diff(ctx, task.ID, v.ID, last.ID, DiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if del.Summary.DeletedNodes != 2 || del.Summary.ModifiedNodes != 0 {
		t.Fatalf("deletions not mapped: %+v", del)
	}
}

func TestDiffPagesFullPackageBeyondStoredIndexAndBoundsJSON(t *testing.T) {
	svc, _, task := studioServiceFixture(t)
	ctx := context.Background()
	rows := make([][]content.Cell, 600)
	for i := range rows {
		rows[i] = []content.Cell{{Type: "text", Value: fmt.Sprintf("原始值%04d", i)}, {Type: "text", Value: "保持原样"}}
	}
	spec := content.Spec{SchemaVersion: 1, Kind: content.XLSX, Title: "大表", Sheets: []content.Sheet{{Name: "Data", Rows: rows}}}
	b, err := svc.Generate(ctx, task.ID, "大表.xlsx", spec, "paged-base")
	if err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		rows[i][0].Value = fmt.Sprintf("修改后%04d", i)
	}
	data, err := content.Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	v, err := svc.Import(ctx, task.ID, b.ArtifactID, "大表.xlsx", data, b.ID, 1, "paged-next")
	if err != nil {
		t.Fatal(err)
	}
	opts := DiffOptions{}
	seen := map[string]bool{}
	seenParts := map[string]bool{}
	pages := 0
	for {
		r, e := svc.Diff(ctx, task.ID, b.ID, v.ID, opts)
		if e != nil {
			t.Fatal(e)
		}
		pages++
		if len(encode(r)) > MaxDiffResponseBytes {
			t.Fatal("bridge response limit exceeded")
		}
		if r.TotalNodeChanges != 600 || r.Summary.ModifiedNodes != 600 || r.Summary.UnchangedNodes != 600 {
			t.Fatalf("truncated DB index used as truth: %+v", r.Summary)
		}
		for _, c := range r.Changes {
			if seen[c.NodeID] {
				t.Fatal("duplicate pagination result")
			}
			seen[c.NodeID] = true
		}
		for _, p := range r.Parts {
			if seenParts[p.NameDigest] {
				t.Fatal("duplicate part result")
			}
			seenParts[p.NameDigest] = true
		}
		if !r.Truncated {
			break
		}
		if opts.NodeOffset == r.NextNodeOffset && opts.PartOffset == r.NextPartOffset {
			t.Fatal("pagination cannot advance")
		}
		opts = DiffOptions{NodeOffset: r.NextNodeOffset, PartOffset: r.NextPartOffset}
		if pages > 50 {
			t.Fatal("pagination did not finish")
		}
	}
	if pages < 2 || len(seen) != 600 {
		t.Fatal("not all changes returned", pages, len(seen))
	}
	if _, err = svc.Diff(ctx, task.ID, b.ID, v.ID, DiffOptions{NodeOffset: 601}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("invalid offset accepted", err)
	}
}

func TestDiffScopeDifferentFormatsCancellationAndCorruptBlobs(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	b := generatedWord(t, svc, task, "scope-base")
	other, err := store.CreateOfficeTask(ctx, domain.Task{SessionID: task.SessionID, Title: "其他任务"}, "diff-other")
	if err != nil {
		t.Fatal(err)
	}
	v := generatedWord(t, svc, other, "scope-other")
	if _, err = svc.Diff(ctx, task.ID, b.ID, v.ID, DiffOptions{}); !errors.Is(err, domain.ErrScope) {
		t.Fatal("cross-task comparison accepted", err)
	}
	if _, err = svc.Diff(domain.WithScope(ctx, ulid.Make().String()), task.ID, b.ID, b.ID, DiffOptions{}); err == nil {
		t.Fatal("cross-org comparison accepted")
	}
	pdf, err := svc.Import(ctx, task.ID, "", "纯文本.pdf", []byte("%PDF-1.7\n%%EOF"), "", 0, "pdf-base")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Diff(ctx, task.ID, b.ID, pdf.ID, DiffOptions{}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("cross-format comparison accepted", err)
	}
	next, err := svc.Import(ctx, task.ID, "", "其他.pdf", []byte("%PDF-1.7\n% changed\n%%EOF"), "", 0, "pdf-next")
	if err != nil {
		t.Fatal(err)
	}
	r, err := svc.Diff(ctx, task.ID, pdf.ID, next.ID, DiffOptions{})
	if err != nil || r.ComparisonBasis != "file-digest" || r.BytesIdentical || r.PartHashesCompared {
		t.Fatal("PDF structural evidence invented", r, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = svc.Diff(cancelled, task.ID, b.ID, b.ID, DiffOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel ignored", err)
	}
	if err = os.WriteFile(filepath.Join(svc.Root, "blobs", b.ContentRef), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Diff(ctx, task.ID, b.ID, b.ID, DiffOptions{}); err == nil {
		t.Fatal("corrupt stored index trusted without reading source bytes")
	}
}

func TestDiffLongEscapedRowsAlwaysAdvanceUnderFrameBudget(t *testing.T) {
	before := content.Inspection{Kind: content.DOCX, SHA256: "before"}
	after := content.Inspection{Kind: content.DOCX, SHA256: "after"}
	for i := range 8 {
		part := strings.Repeat("<", 5000) + fmt.Sprint(i)
		before.Parts = append(before.Parts, content.Part{Name: part, SHA256: "old", Size: 10})
		after.Parts = append(after.Parts, content.Part{Name: part, SHA256: "new", Size: 11})
		old := content.Node{ID: fmt.Sprint(i), Part: part, Locator: strings.Repeat("<", 5000), Kind: "text", Text: strings.Repeat("<", 10000), Digest: "old"}
		next := old
		next.Text = strings.Repeat(">", 10000)
		next.Digest = "new"
		before.Nodes = append(before.Nodes, old)
		after.Nodes = append(after.Nodes, next)
	}
	opts := DiffOptions{}
	nodes, parts := 0, 0
	for range 20 {
		r, err := compareInspections(context.Background(), before, after, opts)
		if err != nil {
			t.Fatal(err)
		}
		if len(encode(r)) > MaxDiffResponseBytes {
			t.Fatal("escaped strings exceeded payload budget")
		}
		for _, n := range r.Changes {
			if !n.PartTruncated || !n.LocatorTruncated || !n.Before.TextTruncated || !n.After.TextTruncated {
				t.Fatal("abbreviation unmarked")
			}
		}
		nodes += len(r.Changes)
		parts += len(r.Parts)
		if !r.Truncated {
			if nodes != 8 || parts != 8 {
				t.Fatal("missing long changes", nodes, parts)
			}
			return
		}
		if opts.NodeOffset == r.NextNodeOffset && opts.PartOffset == r.NextPartOffset {
			t.Fatal("long node blocks all pagination")
		}
		opts = DiffOptions{r.NextNodeOffset, r.NextPartOffset}
	}
	t.Fatal("pagination exhausted iteration budget")
}
