package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/artifactreview"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

const artifactSession = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

func newArtifactEngine(t *testing.T) *Engine {
	t.Helper()
	e := NewEngine(nil, "test")
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tools.Close() })
	e.SetToolRuntime(tools)
	reviews, err := artifactreview.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.SetArtifactReviewStore(reviews)
	return e
}

func artifactRequest(payload string) bridge.Request {
	return bridge.Request{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", TraceID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Payload: json.RawMessage(payload)}
}

func TestArtifactReviewAppendListAcceptanceLoop(t *testing.T) {
	e := newArtifactEngine(t)
	ctx := context.Background()
	// Generate one artifact so the loop has a real target.
	if _, err := e.tools.Execute(ctx, toolruntime.FullAccess, artifactSession, "excel.gen", json.RawMessage(`{"path":"reports/q3.xlsx","sheets":[{"name":"S","headers":["A"],"rows":[["1"]] }]}`), false); err != nil {
		t.Fatal(err)
	}
	appendPayload := `{"sessionId":"` + artifactSession + `","callId":"call-1","toolName":"excel.gen","kind":"xlsx","path":"reports/q3.xlsx","action":"comment","note":"补一列环比"}`
	resp := handleWorkspaceArtifactReviewAppend(e, ctx, artifactRequest(appendPayload))
	if !resp.OK {
		t.Fatalf("append failed: %+v", resp)
	}
	// revise then accept flip the accepted set.
	handleWorkspaceArtifactReviewAppend(e, ctx, artifactRequest(`{"sessionId":"`+artifactSession+`","callId":"call-1","kind":"xlsx","path":"reports/q3.xlsx","action":"revise","note":"图表改成折线"}`))
	handleWorkspaceArtifactReviewAppend(e, ctx, artifactRequest(`{"sessionId":"`+artifactSession+`","callId":"call-1","kind":"xlsx","path":"reports/q3.xlsx","action":"accept"}`))
	list := handleWorkspaceArtifactReviewList(e, ctx, artifactRequest(`{"sessionId":"`+artifactSession+`"}`))
	if !list.OK {
		t.Fatalf("list failed: %+v", list)
	}
	raw, _ := json.Marshal(list.Payload)
	if !strings.Contains(string(raw), `"acceptedPaths":["reports/q3.xlsx"]`) {
		t.Fatalf("acceptedPaths missing: %s", raw)
	}
	if !strings.Contains(string(raw), `"note":"补一列环比"`) || strings.Count(string(raw), `"action"`) != 3 {
		t.Fatalf("review log incomplete: %s", raw)
	}
}

func TestArtifactReviewAppendRejectsInvalid(t *testing.T) {
	e := newArtifactEngine(t)
	resp := handleWorkspaceArtifactReviewAppend(e, context.Background(), artifactRequest(`{"sessionId":"nope","callId":"","kind":"zip","path":"","action":"approve"}`))
	if resp.OK {
		t.Fatal("invalid review accepted")
	}
	list := handleWorkspaceArtifactReviewList(e, context.Background(), artifactRequest(`{"sessionId":"bad"}`))
	if list.OK {
		t.Fatal("invalid list accepted")
	}
}

func TestArtifactPreviewKindAware(t *testing.T) {
	e := newArtifactEngine(t)
	ctx := context.Background()
	if _, err := e.tools.Execute(ctx, toolruntime.FullAccess, artifactSession, "docx.gen", json.RawMessage(`{"path":"周报.docx","title":"周报","blocks":[{"type":"heading","text":"进展"},{"type":"paragraph","text":"完成了 P2-2 验收闭环 & 测试 <b>x</b>"}]}`), false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.tools.Execute(ctx, toolruntime.FullAccess, artifactSession, "pdf.gen", json.RawMessage(`{"path":"说明.pdf","title":"t","body":"b"}`), false); err != nil {
		t.Fatal(err)
	}
	ok := handleWorkspaceArtifactPreview(e, ctx, artifactRequest(`{"sessionId":"`+artifactSession+`","path":"周报.docx"}`))
	if !ok.OK {
		t.Fatalf("docx preview failed: %+v", ok)
	}
	var previewPayload struct {
		Content string `json:"content"`
	}
	if raw, err := json.Marshal(ok.Payload); err != nil || json.Unmarshal(raw, &previewPayload) != nil {
		t.Fatalf("preview payload undecodable: %v", err)
	}
	// Entity-escaped text (&lt;b&gt;) must be unescaped back to source characters.
	if !strings.Contains(previewPayload.Content, "进展") || !strings.Contains(previewPayload.Content, "&") || !strings.Contains(previewPayload.Content, "P2-2 验收闭环") {
		t.Fatalf("docx text extraction wrong: %s", previewPayload.Content)
	}
	// pdf has no extractor yet → explicit unsupported failure.
	pdf := handleWorkspaceArtifactPreview(e, ctx, artifactRequest(`{"sessionId":"`+artifactSession+`","path":"说明.pdf"}`))
	if pdf.OK || pdf.Error == nil || pdf.Error.Code != "ARTIFACT_PREVIEW_UNSUPPORTED" {
		t.Fatalf("pdf preview should be unsupported: %+v", pdf)
	}
	// escaping or missing paths fail closed.
	escape := handleWorkspaceArtifactPreview(e, ctx, artifactRequest(`{"sessionId":"`+artifactSession+`","path":"../x.docx"}`))
	if escape.OK {
		t.Fatal("escaping path preview accepted")
	}
	missing := handleWorkspaceArtifactPreview(e, ctx, artifactRequest(`{"sessionId":"`+artifactSession+`","path":"nope.docx"}`))
	if missing.OK || missing.Error == nil || missing.Error.Code != "ARTIFACT_NOT_FOUND" {
		t.Fatalf("missing preview should fail: %+v", missing)
	}
}

func TestOfficeGeneratorsSurfaceArtifactMetadata(t *testing.T) {
	e := newArtifactEngine(t)
	r, err := e.tools.Execute(context.Background(), toolruntime.FullAccess, artifactSession, "pptx.gen", json.RawMessage(`{"path":"deck.pptx","title":"T","slides":[{"title":"A","bullets":["x"]}]}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Artifact == nil || r.Artifact.Kind != "pptx" || r.Artifact.Path != "deck.pptx" || r.Artifact.Content != "" {
		t.Fatalf("pptx artifact metadata wrong: %+v", r.Artifact)
	}
	// workspace.write keeps html-only artifact semantics (no office kind).
	w, err := e.tools.Execute(context.Background(), toolruntime.FullAccess, artifactSession, "workspace.write", json.RawMessage(`{"path":"index.html","content":"<h1>hi</h1>"}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if w.Artifact == nil || w.Artifact.Kind != "html" {
		t.Fatalf("html artifact missing: %+v", w.Artifact)
	}
}

func TestArtifactExportRoundTripAndGuards(t *testing.T) {
	e := newArtifactEngine(t)
	ctx := context.Background()
	if _, err := e.tools.Execute(ctx, toolruntime.FullAccess, artifactSession, "docx.gen", json.RawMessage(`{"path":"交付/周报.docx","title":"周报","blocks":[{"type":"paragraph","text":"交付内容"}]}`), false); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	payload := `{"sessionId":"` + artifactSession + `","path":"交付/周报.docx","target":` + strconv.Quote(filepath.ToSlash(target)) + `}`
	resp := handleWorkspaceArtifactExport(e, ctx, artifactRequest(payload))
	if !resp.OK {
		t.Fatalf("export failed: %+v", resp)
	}
	var out struct {
		ExportedPath string `json:"exportedPath"`
		Size         int    `json:"size"`
	}
	raw, _ := json.Marshal(resp.Payload)
	if json.Unmarshal(raw, &out) != nil || out.Size <= 0 {
		t.Fatalf("export payload wrong: %s", raw)
	}
	written, err := os.ReadFile(filepath.Join(target, "周报.docx"))
	if err != nil || len(written) != out.Size {
		t.Fatalf("exported file mismatch: %v %d vs %d", err, len(written), out.Size)
	}
	// second export without overwrite is refused with a dedicated code.
	again := handleWorkspaceArtifactExport(e, ctx, artifactRequest(payload))
	if again.OK || again.Error == nil || again.Error.Code != "ARTIFACT_EXPORT_EXISTS" {
		t.Fatalf("overwrite guard missing: %+v", again)
	}
	// overwrite=true lands the same bytes.
	withOverwrite := `{"sessionId":"` + artifactSession + `","path":"交付/周报.docx","target":` + strconv.Quote(filepath.ToSlash(target)) + `,"overwrite":true}`
	if resp := handleWorkspaceArtifactExport(e, ctx, artifactRequest(withOverwrite)); !resp.OK {
		t.Fatalf("overwrite export failed: %+v", resp)
	}
	// relative target and traversal source fail closed.
	rel := handleWorkspaceArtifactExport(e, ctx, artifactRequest(`{"sessionId":"`+artifactSession+`","path":"交付/周报.docx","target":"rel/ative"}`))
	if rel.OK || rel.Error == nil || rel.Error.Code != "ARTIFACT_EXPORT_TARGET_INVALID" {
		t.Fatalf("relative target accepted: %+v", rel)
	}
	escape := handleWorkspaceArtifactExport(e, ctx, artifactRequest(`{"sessionId":"`+artifactSession+`","path":"../周报.docx","target":`+strconv.Quote(filepath.ToSlash(target))+`}`))
	if escape.OK {
		t.Fatal("traversal source accepted")
	}
	missing := handleWorkspaceArtifactExport(e, ctx, artifactRequest(`{"sessionId":"`+artifactSession+`","path":"nope.docx","target":`+strconv.Quote(filepath.ToSlash(target))+`}`))
	if missing.OK || missing.Error == nil || missing.Error.Code != "ARTIFACT_NOT_FOUND" {
		t.Fatalf("missing source should fail: %+v", missing)
	}
	// shortcut target resolves under the profile home.
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	desktop := handleWorkspaceArtifactExport(e, ctx, artifactRequest(`{"sessionId":"`+artifactSession+`","path":"交付/周报.docx","target":"desktop"}`))
	if !desktop.OK {
		t.Fatalf("desktop export failed: %+v", desktop)
	}
	if _, err := os.Stat(filepath.Join(home, "Desktop", "周报.docx")); err != nil {
		t.Fatalf("desktop file missing: %v", err)
	}
}

func TestArtifactReviewStoreRoundTripPersistence(t *testing.T) {
	root := t.TempDir()
	s1, err := artifactreview.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s1.Append(artifactSession, "c1", "excel.gen", "xlsx", filepath.ToSlash(filepath.Join("a", "b.xlsx")), "comment", "n"); err != nil {
		t.Fatal(err)
	}
	// Reopen from the same root: the log must persist.
	s2, err := artifactreview.NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	items, accepted, err := s2.ListBySession(artifactSession)
	if err != nil || len(items) != 1 {
		t.Fatalf("persisted reviews wrong: %v %d", err, len(items))
	}
	if len(accepted) != 0 || items[0].Note != "n" {
		t.Fatalf("unexpected state: %+v", items[0])
	}
}
