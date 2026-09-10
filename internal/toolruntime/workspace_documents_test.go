package toolruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jung-kurt/gofpdf"
	"github.com/lunitide/lunitide/internal/officetools"
)

func blankScanPDF(t *testing.T) []byte {
	t.Helper()
	doc := gofpdf.New("P", "mm", "A4", "")
	doc.AddPage()
	var buf bytes.Buffer
	if err := doc.Output(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const workspaceDocumentSession = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

func TestWorkspaceReadDocumentPagesKeepFullTail(t *testing.T) {
	runtime, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	folder, _ := runtime.SessionFolder(workspaceDocumentSession)
	prose := strings.Repeat("可完整读取的中文正文。", 700) + "最后一段校验唯一标记"
	docx, err := officetools.GenDocx("读取测试", []officetools.DocxBlock{{Type: "heading", Text: "正文"}, {Type: "paragraph", Text: prose}})
	if err != nil {
		t.Fatal(err)
	}
	pptx, err := officetools.GenPptx("读取测试", []officetools.SlideSpec{{Title: "内容", Layout: "content", Bullets: []string{"幻灯片正文唯一标记"}}})
	if err != nil {
		t.Fatal(err)
	}
	xlsx, err := officetools.GenXLSX([]officetools.SheetSpec{{Name: "测试", Headers: []string{"编号", "内容"}, Rows: [][]any{{"00001", "工作表正文唯一标记"}}}})
	if err != nil {
		t.Fatal(err)
	}
	pdf, err := officetools.GenPDF("中文 PDF / Read test", strings.Repeat("中文文字层需要逐页完整读取，不能因工具输出上限而丢掉后半部分。\n", 80)+"PDF 最后一段唯一标记")
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		name, marker string
		data         []byte
	}{{"完整.docx", "最后一段校验唯一标记", docx}, {"演示.pptx", "幻灯片正文唯一标记", pptx}, {"表格.xlsx", "工作表正文唯一标记", xlsx}, {"报告.pdf", "PDF 最后一段唯一标记", pdf}, {"长文件.txt", "最后一段校验唯一标记", []byte(prose)}} {
		t.Run(fixture.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(folder, fixture.name), fixture.data, 0600); err != nil {
				t.Fatal(err)
			}
			var body strings.Builder
			offset := 0
			for page := 0; ; page++ {
				if page > 100 {
					t.Fatal("pagination did not finish")
				}
				args, _ := json.Marshal(map[string]any{"path": fixture.name, "offset": offset, "limit": 3000})
				result, err := runtime.Execute(context.Background(), AutoEdit, workspaceDocumentSession, "workspace.read", args, false)
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Output) > 4096 || !utf8.ValidString(result.Output) {
					t.Fatal("page cannot pass intact through the chat tool summary")
				}
				header, content, ok := strings.Cut(result.Output, "\n")
				if !ok || !strings.HasPrefix(header, "[file-read ") {
					t.Fatalf("missing progress metadata: %q", result.Output)
				}
				var next int
				for _, token := range strings.Fields(header) {
					if strings.HasPrefix(token, "nextOffset=") {
						_, _ = fmt.Sscanf(token, "nextOffset=%d", &next)
					}
				}
				if next != offset+utf8.RuneCountInString(content) {
					t.Fatalf("cursor loses or repeats characters: offset=%d next=%d", offset, next)
				}
				body.WriteString(content)
				if strings.Contains(header, "complete=true") {
					break
				}
				if next <= offset {
					t.Fatal("pagination cannot make progress")
				}
				offset = next
			}
			if !strings.Contains(body.String(), fixture.marker) {
				t.Fatal("document tail was lost")
			}
			if strings.HasSuffix(fixture.name, ".txt") && body.String() != prose {
				t.Fatal("plain text changed during pagination")
			}
		})
	}
}

func TestWorkspaceDocumentReadKeepsScopeAndSmallTextCompatibility(t *testing.T) {
	runtime, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	folder, _ := runtime.SessionFolder(workspaceDocumentSession)
	text := "  short text\n保留空白\n"
	if err := os.WriteFile(filepath.Join(folder, "small.txt"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Execute(context.Background(), AutoEdit, workspaceDocumentSession, "workspace.read", json.RawMessage(`{"path":"small.txt"}`), false)
	if err != nil || result.Output != text {
		t.Fatalf("legacy text read changed: %q %v", result.Output, err)
	}
	for _, args := range []string{`{"path":"../outside.txt"}`, `{"path":"small.txt","offset":-1}`, `{"path":"small.txt","offset":999}`, `{"path":"small.txt","limit":0}`, `{"path":"small.txt","limit":99999}`} {
		if _, err := runtime.Execute(context.Background(), AutoEdit, workspaceDocumentSession, "workspace.read", json.RawMessage(args), false); err == nil {
			t.Fatalf("invalid read accepted: %s", args)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runtime.Execute(ctx, AutoEdit, workspaceDocumentSession, "workspace.read", json.RawMessage(`{"path":"small.txt"}`), false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read did work: %v", err)
	}
}

func TestWorkspaceReadImageUsesInjectedOCR(t *testing.T) {
	runtime, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 1, 2, 0}
	folder, _ := runtime.SessionFolder(workspaceDocumentSession)
	if err := os.WriteFile(filepath.Join(folder, "scan.png"), png, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Execute(context.Background(), AutoEdit, workspaceDocumentSession, "workspace.read", json.RawMessage(`{"path":"scan.png"}`), false); err == nil || strings.Contains(err.Error(), "scanned PDFs") {
		t.Fatalf("image without OCR must fail as OCR routing, not a scanned-PDF hint: %v", err)
	}
	runtime.SetDocumentText(func(context.Context, string, []byte, string) (string, string, string, int, error) {
		return "识别到发票 88", "image", "provider-ocr", 1, nil
	})
	got, err := runtime.Execute(context.Background(), AutoEdit, workspaceDocumentSession, "workspace.read", json.RawMessage(`{"path":"scan.png"}`), false)
	if err != nil || !strings.Contains(got.Output, "识别到发票 88") {
		t.Fatalf("injected OCR not used: %q %v", got.Output, err)
	}
}

func TestWorkspaceReadScanPDFWithoutOCRUsesRoutingFailClosed(t *testing.T) {
	runtime, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	folder, _ := runtime.SessionFolder(workspaceDocumentSession)
	if err := os.WriteFile(filepath.Join(folder, "scan.pdf"), blankScanPDF(t), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Execute(context.Background(), AutoEdit, workspaceDocumentSession, "workspace.read", json.RawMessage(`{"path":"scan.pdf"}`), false)
	if err == nil || !strings.Contains(err.Error(), "OCR 未装配") {
		t.Fatalf("scan PDF without documentText must fail as OCR routing: %v", err)
	}
	if strings.Contains(err.Error(), "OCR routing is not configured") {
		t.Fatalf("must not leak English OCR-routing gap: %v", err)
	}
	if strings.Contains(err.Error(), "scanned PDFs need OCR") {
		t.Fatalf("must not use the old local-OCR hint: %v", err)
	}
}

func TestWorkspaceReadMarksIncompleteOCRCoverage(t *testing.T) {
	runtime, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	folder, _ := runtime.SessionFolder(workspaceDocumentSession)
	if err := os.WriteFile(filepath.Join(folder, "mixed.pdf"), []byte("%PDF-1.4 mixed"), 0600); err != nil {
		t.Fatal(err)
	}
	runtime.SetDocumentText(func(context.Context, string, []byte, string) (string, string, string, int, error) {
		return "Cover layer only", "pdf", "text-layer incomplete-coverage", 3, nil
	})
	got, err := runtime.Execute(context.Background(), AutoEdit, workspaceDocumentSession, "workspace.read", json.RawMessage(`{"path":"mixed.pdf"}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Output, "incomplete") || !strings.Contains(got.Output, "Cover layer only") {
		t.Fatalf("incomplete OCR coverage must stay visible: %q", got.Output)
	}
	if strings.Contains(got.Output, "kind=pdf]") && !strings.Contains(got.Output, "incomplete") {
		t.Fatalf("must not present a bare pdf kind as complete coverage: %q", got.Output)
	}
}

func TestWorkspaceReadBinaryRejectsEnglishAndUsesChinese(t *testing.T) {
	runtime, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	folder, _ := runtime.SessionFolder(workspaceDocumentSession)
	if err := os.WriteFile(filepath.Join(folder, "blob.bin"), []byte{0x00, 0x01, 0x02, 0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Execute(context.Background(), AutoEdit, workspaceDocumentSession, "workspace.read", json.RawMessage(`{"path":"blob.bin"}`), false)
	if err == nil || !strings.Contains(err.Error(), "二进制") {
		t.Fatalf("binary read must be Chinese fail-closed: %v", err)
	}
	if strings.Contains(err.Error(), "file is binary") {
		t.Fatalf("must not leak English binary-read gap: %v", err)
	}
}

func TestWorkspaceReadInvalidOffsetUsesChinese(t *testing.T) {
	runtime, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	folder, _ := runtime.SessionFolder(workspaceDocumentSession)
	if err := os.WriteFile(filepath.Join(folder, "note.txt"), []byte("短文本"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Execute(context.Background(), AutoEdit, workspaceDocumentSession, "workspace.read", json.RawMessage(`{"path":"note.txt","offset":-1}`), false)
	if err == nil || !strings.Contains(err.Error(), "offset 不能为负") {
		t.Fatalf("invalid offset must be Chinese fail-closed: %v", err)
	}
	if strings.Contains(err.Error(), "must be nonnegative") {
		t.Fatalf("must not leak English offset bounds: %v", err)
	}
}

func TestWorkspaceReadOffsetBeyondTextUsesChinese(t *testing.T) {
	runtime, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	folder, _ := runtime.SessionFolder(workspaceDocumentSession)
	if err := os.WriteFile(filepath.Join(folder, "note.txt"), []byte("短文本"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Execute(context.Background(), AutoEdit, workspaceDocumentSession, "workspace.read", json.RawMessage(`{"path":"note.txt","offset":99}`), false)
	if err == nil || !strings.Contains(err.Error(), "偏移已超出") {
		t.Fatalf("offset overflow must be Chinese fail-closed: %v", err)
	}
	if strings.Contains(err.Error(), "offset is beyond") {
		t.Fatalf("must not leak English offset overflow: %v", err)
	}
}

func TestWorkspaceEditReplaceFailurePreservesOriginalAndCleansTemporary(t *testing.T) {
	folder := t.TempDir()
	path := filepath.Join(folder, "existing.txt")
	if err := os.WriteFile(path, []byte("原内容，替换失败时仍须保留"), 0600); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("replacement unavailable")
	err := writeFileReplaceUsing(path, "修改后的内容", func(string, string) error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "原内容，替换失败时仍须保留" {
		t.Fatalf("failed replacement deleted original: %q %v", data, err)
	}
	entries, _ := os.ReadDir(folder)
	if len(entries) != 1 {
		t.Fatalf("temporary edit leaked: %v", entries)
	}
}

func TestWorkspaceWriteEditAndNestedHTMLArtifactCanBeRead(t *testing.T) {
	runtime, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	ctx := context.Background()
	args := json.RawMessage(`{"path":"reports/demo.html","content":"<!doctype html><title>文档</title><p>原内容</p>"}`)
	written, err := runtime.Execute(ctx, AutoEdit, workspaceDocumentSession, "workspace.write", args, false)
	if err != nil || written.Artifact == nil || written.Artifact.Path != "reports/demo.html" {
		t.Fatalf("nested artifact path lost: %+v %v", written, err)
	}
	_, err = runtime.Execute(ctx, AutoEdit, workspaceDocumentSession, "workspace.edit", json.RawMessage(`{"path":"reports/demo.html","oldText":"原内容","newText":"实际修改后的内容"}`), false)
	if err != nil {
		t.Fatal(err)
	}
	data, err := runtime.ReadWorkspaceFile(workspaceDocumentSession, written.Artifact.Path, 4096)
	if err != nil || !strings.Contains(string(data), "实际修改后的内容") {
		t.Fatalf("artifact cannot open its edited file: %q %v", data, err)
	}
	folder, _ := runtime.SessionFolder(workspaceDocumentSession)
	binary := []byte("%PDF- binary text")
	if err := os.WriteFile(filepath.Join(folder, "existing.pdf"), binary, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Execute(ctx, AutoEdit, workspaceDocumentSession, "workspace.edit", json.RawMessage(`{"path":"existing.pdf","oldText":"text","newText":"changed"}`), false)
	if err == nil {
		t.Fatal("binary document was edited as plain text")
	}
	if !strings.Contains(err.Error(), "不能直接改") || strings.Contains(err.Error(), "requires plain UTF-8") {
		t.Fatalf("Office/PDF edit must be Chinese fail-closed: %v", err)
	}
	unchanged, err := os.ReadFile(filepath.Join(folder, "existing.pdf"))
	if err != nil || string(unchanged) != string(binary) {
		t.Fatal("unsupported edit corrupted the document")
	}
}

func TestWorkspaceEditMissingFileUsesChinese(t *testing.T) {
	runtime, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	_, err = runtime.Execute(context.Background(), AutoEdit, workspaceDocumentSession, "workspace.edit", json.RawMessage(`{"path":"missing.txt","oldText":"a","newText":"b"}`), false)
	if err == nil || !strings.Contains(err.Error(), "文件不存在") {
		t.Fatalf("missing edit target must be Chinese fail-closed: %v", err)
	}
	if strings.Contains(err.Error(), "file missing or exceeds") {
		t.Fatalf("must not leak English edit-missing gap: %v", err)
	}
}
