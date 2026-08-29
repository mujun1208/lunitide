package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/officetools"
)

const officeSession = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

func styledDocxArgs(path, title string, extra map[string]any) json.RawMessage {
	args := map[string]any{
		"path": path, "title": title, "blocks": officetools.SampleStyledDocxBlocks(),
	}
	for k, v := range extra {
		args[k] = v
	}
	raw, _ := json.Marshal(args)
	return raw
}

func TestOfficeGenToolsApprovalGating(t *testing.T) {
	r, _ := New(t.TempDir())
	defer r.Close()
	gen := json.RawMessage(`{"path":"report.xlsx","sheets":[{"name":"S","headers":["A","B"],"rows":[["x",1],["y",2]]}]}`)
	// Approval mode gates every generator like workspace.write.
	if _, err := r.Execute(context.Background(), Approval, officeSession, "excel.gen", gen, false); err == nil || !strings.Contains(err.Error(), "approval") {
		t.Fatalf("excel.gen ungated: %v", err)
	}
	if _, err := r.Execute(context.Background(), Approval, officeSession, "excel.gen", gen, true); err != nil {
		t.Fatalf("approved excel.gen failed: %v", err)
	}
	// Auto-edit mode rides without approval (file creation class).
	docx := styledDocxArgs("r.docx", "T", nil)
	if _, err := r.Execute(context.Background(), AutoEdit, officeSession, "docx.gen", docx, false); err != nil {
		t.Fatalf("auto-edit docx.gen failed: %v", err)
	}
	// Plan mode stays disabled.
	if _, err := r.Execute(context.Background(), Plan, officeSession, "pdf.gen", json.RawMessage(`{"path":"r.pdf","title":"t","body":"b"}`), true); err == nil {
		t.Fatal("plan mode allowed pdf.gen")
	}
}

func TestOfficeGenWritesRealFilesAndParseRoundTrips(t *testing.T) {
	r, _ := New(t.TempDir())
	defer r.Close()
	ctx := context.Background()
	// xlsx with chart lands as a valid zip and parses back.
	xlsx := json.RawMessage(`{"path":"out/sales.xlsx","sheets":[{"name":"销售","headers":["月份","销量"],"rows":[["1月",10],["2月",20],["3月",30]],"chart":{"type":"col","title":"销量"}}]}`)
	if _, err := r.Execute(ctx, FullAccess, officeSession, "excel.gen", xlsx, false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(r.root, officeSession, "out", "sales.xlsx"))
	if err != nil || len(b) < 4 || string(b[:2]) != "PK" {
		t.Fatalf("xlsx missing or not zip: %v %d", err, len(b))
	}
	out, err := r.Execute(ctx, FullAccess, officeSession, "excel.parse", json.RawMessage(`{"path":"out/sales.xlsx"}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, `"name":"销售"`) || !strings.Contains(out.Output, "1月") {
		t.Fatalf("parse summary = %s", out.Output)
	}
	// pptx + pdf land too.
	pptx := json.RawMessage(`{"path":"deck.pptx","title":"汇报","slides":[{"title":"A","bullets":["x","y"]}]}`)
	if _, err := r.Execute(ctx, FullAccess, officeSession, "pptx.gen", pptx, false); err != nil {
		t.Fatal(err)
	}
	pdf := json.RawMessage(`{"path":"r.pdf","title":"Report","body":"line one\nline two"}`)
	if _, err := r.Execute(ctx, FullAccess, officeSession, "pdf.gen", pdf, false); err != nil {
		t.Fatal(err)
	}
	pb, _ := os.ReadFile(filepath.Join(r.root, officeSession, "r.pdf"))
	if len(pb) < 5 || string(pb[:5]) != "%PDF-" {
		t.Fatal("pdf missing or invalid header")
	}
}

func TestOfficeGenGuardsAndContainment(t *testing.T) {
	r, _ := New(t.TempDir())
	defer r.Close()
	ctx := context.Background()
	// Extension mismatch is refused.
	if _, err := r.Execute(ctx, FullAccess, officeSession, "excel.gen", json.RawMessage(`{"path":"a.txt","sheets":[{"rows":[["x"]]},{"rows":[["y"]]}]}`), false); err == nil {
		t.Fatal("wrong extension accepted")
	}
	// Path escape is refused.
	if _, err := r.Execute(ctx, FullAccess, officeSession, "pdf.gen", json.RawMessage(`{"path":"../r.pdf","title":"t","body":"b"}`), false); err == nil {
		t.Fatal("path escape accepted")
	}
	// Empty payload fails.
	if _, err := r.Execute(ctx, FullAccess, officeSession, "docx.gen", json.RawMessage(`{"path":"a.docx","title":"t","blocks":[]}`), false); err == nil {
		t.Fatal("empty blocks accepted")
	}
	// Parse of a missing file fails without creating anything.
	if _, err := r.Execute(ctx, FullAccess, officeSession, "excel.parse", json.RawMessage(`{"path":"missing.xlsx"}`), false); err == nil {
		t.Fatal("missing parse accepted")
	}
	if _, err := os.Stat(filepath.Join(r.root, officeSession)); !os.IsNotExist(err) {
		t.Fatal("read-only office tool created workspace")
	}
}

func TestOfficeGenDesktopNeedsUnconfined(t *testing.T) {
	r, _ := New(t.TempDir())
	defer r.Close()
	args := json.RawMessage(`{"path":"半年财报.xlsx","desktop":true,"sheets":[{"name":"S","headers":["月"],"rows":[["1月"]]}]}`)
	if _, err := r.Execute(context.Background(), FullAccess, officeSession, "excel.gen", args, false); err == nil {
		t.Fatal("desktop write escaped confined runtime")
	}
}

func TestOfficeGenDesktopArtifactPath(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	enableFullDisk(t, r)
	desktop, err := userDesktopDir()
	if err != nil {
		t.Skip("desktop folder not found:", err)
	}
	cases := []struct {
		tool string
		name string
		kind string
		args map[string]any
	}{
		{"excel.gen", "半年财报.xlsx", "xlsx", map[string]any{
			"path": "半年财报.xlsx", "desktop": true,
			"sheets": []any{map[string]any{"name": "S", "headers": []any{"月"}, "rows": []any{[]any{"1月", 10}}}},
		}},
		{"docx.gen", "半年报告.docx", "docx", map[string]any{
			"path": "半年报告.docx", "desktop": true, "title": "半年报告",
			"blocks": officetools.SampleStyledDocxBlocks(),
		}},
		{"pptx.gen", "结构.pptx", "pptx", map[string]any{
			"path": "结构.pptx", "desktop": true, "title": "结构",
			"slides": []any{map[string]any{"title": "封面", "layout": "title"}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			target := filepath.Join(desktop, tc.name)
			t.Cleanup(func() { _ = os.Remove(target) })
			raw, _ := json.Marshal(tc.args)
			out, err := r.ExecuteUnconfined(context.Background(), officeSession, tc.tool, raw, false)
			if err != nil {
				t.Fatal(err)
			}
			want := "desktop/" + tc.name
			if out.Artifact == nil || out.Artifact.Kind != tc.kind || out.Artifact.Path != want {
				t.Fatalf("artifact = %+v want %s %s", out.Artifact, tc.kind, want)
			}
			if strings.Contains(out.Artifact.Path, `:\`) || strings.Contains(out.Artifact.Path, `\`) {
				t.Fatalf("absolute path leaked: %+v", out.Artifact)
			}
			resolved, err := r.ResolveSessionArtifact(officeSession, out.Artifact.Path)
			if err != nil || resolved != target {
				t.Fatalf("ResolveSessionArtifact = %q err=%v want %q", resolved, err, target)
			}
			if _, err := os.Stat(target); err != nil {
				t.Fatalf("desktop write failed: %v", err)
			}
		})
	}
}

func TestDesktopPreviewPathStripsDrive(t *testing.T) {
	got := desktopPreviewPath(`C:\Users\a\Desktop\半年财报.xlsx`, true, "workbook.xlsx")
	if !strings.HasPrefix(got, "desktop/") || strings.Contains(got, `\`) {
		t.Fatalf("preview = %q", got)
	}
}
