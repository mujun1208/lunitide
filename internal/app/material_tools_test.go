package app

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jung-kurt/gofpdf"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

func TestDataProcessAndImageBatchWriteCopies(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(runtime)
	session := ulid.Make().String()
	root := filepath.Join(runtime.WorkspaceRoot(), session)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rows.csv"), []byte("name,amount\nA,1\nA,1\nB,2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := e.executeUserTool(context.Background(), executionModeFullAccess, session, "data.process", json.RawMessage(`{"path":"rows.csv","op":"dedup","keys":["name","amount"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "processed.csv") {
		t.Fatalf("data.process receipt: %s", out.Output)
	}
	if _, err := e.executeUserTool(context.Background(), executionModeFullAccess, session, "data.process", json.RawMessage(`{"path":"rows.csv","op":"filter","column":"name","equals":"=1+1"}`)); err != nil {
		t.Fatalf("plain filter equals must not trip the formula guard: %v", err)
	}
	if _, err := e.executeUserTool(context.Background(), executionModeFullAccess, session, "data.process", json.RawMessage(`{"path":"bad.csv","op":"export"}`)); err == nil {
		t.Fatal("missing table must fail")
	}
	stats, err := e.executeUserTool(context.Background(), executionModeFullAccess, session, "data.process", json.RawMessage(`{"path":"rows.csv","op":"stats","column":"amount"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stats.Output, `"types"`) || !strings.Contains(stats.Output, `"amount":"number"`) {
		t.Fatalf("stats must expose inferred types: %s", stats.Output)
	}
	if err := os.WriteFile(filepath.Join(root, "more.csv"), []byte("name,amount\nC,3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	merged, err := e.executeUserTool(context.Background(), executionModeFullAccess, session, "data.process", json.RawMessage(`{"path":"rows.csv","op":"merge","otherPath":"more.csv","out":"merged.csv"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(merged.Output, "merged.csv") {
		t.Fatalf("merge receipt: %s", merged.Output)
	}
	body, err := os.ReadFile(filepath.Join(root, "merged.csv"))
	if err != nil || !strings.Contains(string(body), "C,3") || !strings.Contains(string(body), "A,1") {
		t.Fatalf("merged table: %q %v", body, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.executeUserTool(canceled, executionModeFullAccess, session, "data.process", json.RawMessage(`{"path":"rows.csv","op":"dedup","keys":["name"]}`)); err == nil {
		t.Fatal("canceled data.process must stop")
	}

	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 20, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 20; x++ {
			img.Set(x, y, color.RGBA{B: 180, A: 255})
		}
	}
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src.png"), buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := e.executeUserTool(context.Background(), executionModeFullAccess, session, "image.batch", json.RawMessage(`{"path":"src.png","op":"crop","width":8,"height":6}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Output, "crop.png") {
		t.Fatalf("image.batch receipt: %s", got.Output)
	}
	if _, err := e.executeUserTool(context.Background(), executionModeFullAccess, session, "image.batch", json.RawMessage(`{"path":"anim.gif","op":"crop","width":8,"height":6}`)); err == nil {
		t.Fatal("gif must be rejected")
	}
}

func TestMaterialToolDefinitionsArePresent(t *testing.T) {
	e := NewEngine(nil, "test")
	foundData, foundImage, foundPDF := false, false, false
	for _, d := range e.engineToolDefinitionsFor(executionModeFullAccess) {
		if d.Name == "data.process" {
			foundData = true
		}
		if d.Name == "image.batch" {
			foundImage = true
		}
		if d.Name == "pdf.copy" {
			foundPDF = true
		}
	}
	if !foundData || !foundImage || !foundPDF {
		t.Fatal("data.process, image.batch and pdf.copy must be on the chat tool surface")
	}
}

func TestPDFCopyToolDeclaresLossyAndRefusesForms(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(runtime)
	session := ulid.Make().String()
	root := filepath.Join(runtime.WorkspaceRoot(), session)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	p := gofpdf.New("P", "mm", "A4", "")
	p.AddPage()
	p.SetFont("Helvetica", "", 12)
	p.Cell(40, 10, "page-one")
	p.AddPage()
	p.SetFont("Helvetica", "", 12)
	p.Cell(40, 10, "page-two")
	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src.pdf"), buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := e.executeUserTool(context.Background(), executionModeFullAccess, session, "pdf.copy", json.RawMessage(`{"op":"split","path":"src.pdf"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Output, `"lossy":true`) || !strings.Contains(got.Output, "lossy_text_rerender") {
		t.Fatalf("HAT-10 must declare lossy copy: %s", got.Output)
	}
	if _, err := os.Stat(filepath.Join(root, "src-p1.pdf")); err != nil {
		t.Fatalf("split page missing: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "form.pdf"), []byte("%PDF-1.4\n1 0 obj<</AcroForm 2 0 R>>endobj\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.executeUserTool(context.Background(), executionModeFullAccess, session, "pdf.copy", json.RawMessage(`{"op":"split","path":"form.pdf"}`)); err == nil || !strings.Contains(err.Error(), "表单") {
		t.Fatalf("forms must be refused: %v", err)
	}
}
