package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/asset"
	"github.com/oklog/ulid/v2"
)

func tinySlide(t *testing.T, text string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	part, err := w.Create("ppt/slides/slide1.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(`<p:sld><a:t>` + text + `</a:t></p:sld>`)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestOfficeImportUsesModelNameAndKeepsTheFileName(t *testing.T) {
	old := completeOfficeTemplateName
	t.Cleanup(func() { completeOfficeTemplateName = old })
	completeOfficeTemplateName = func(_ *Engine, _ context.Context, _, excerpt string) (string, error) {
		if !strings.Contains(excerpt, "经营指标") {
			t.Fatalf("excerpt %q", excerpt)
		}
		return "名称：季度经营汇报\n描述：按季度汇总经营指标的演示稿", nil
	}
	engine := &Engine{assets: &mockTemplateStore{}, templateFiles: &memTemplateFiles{files: map[string][]byte{}}}
	resp := handleTemplateOfficeImport(engine, context.Background(), bridgeRequest("template.office.import", map[string]any{
		"fileName": "Q1.pptx", "contentBase64": base64.StdEncoding.EncodeToString(tinySlide(t, "经营指标")),
	}))
	if !resp.OK {
		t.Fatalf("import %+v", resp.Error)
	}
	saved := engine.assets.(*mockTemplateStore).created
	if saved.Name != "季度经营汇报" || saved.Description != "按季度汇总经营指标的演示稿" || saved.FileName != "Q1.pptx" || saved.TemplateType != asset.TemplateTypePPT || saved.Status != asset.StatusDraft {
		t.Fatalf("saved %#v", saved)
	}
}

func TestOfficeImportFallsBackWhenTheModelIsUnavailable(t *testing.T) {
	engine := &Engine{assets: &mockTemplateStore{}, templateFiles: &memTemplateFiles{files: map[string][]byte{}}}
	resp := handleTemplateOfficeImport(engine, context.Background(), bridgeRequest("template.office.import", map[string]any{
		"fileName": "经营周报.docx", "contentBase64": base64.StdEncoding.EncodeToString([]byte("not-a-docx")),
	}))
	if !resp.OK {
		t.Fatalf("import %+v", resp.Error)
	}
	saved := engine.assets.(*mockTemplateStore).created
	if saved.Name != "经营周报" || saved.Description != asset.OfficeTemplateFallbackDescription || saved.TemplateType != asset.TemplateTypeWord || saved.Status != asset.StatusDraft {
		t.Fatalf("saved %#v", saved)
	}
}

func TestOfficeImportKeepsTheFileWhenTheModelCannotAnswer(t *testing.T) {
	engine := &Engine{assets: &mockTemplateStore{}, templateFiles: &memTemplateFiles{files: map[string][]byte{}}}
	resp := handleTemplateOfficeImport(engine, context.Background(), bridgeRequest("template.office.import", map[string]any{
		"fileName": "经营指标.pptx", "contentBase64": base64.StdEncoding.EncodeToString(tinySlide(t, "经营指标")),
	}))
	if !resp.OK {
		t.Fatalf("import %+v", resp.Error)
	}
	saved := engine.assets.(*mockTemplateStore).created
	if saved.Name != "经营指标" || saved.Description != asset.OfficeTemplateFallbackDescription || saved.FileName != "经营指标.pptx" {
		t.Fatalf("saved %#v", saved)
	}
}

func TestOfficeImportRejectsLegacyPowerPoint(t *testing.T) {
	engine := &Engine{assets: &mockTemplateStore{}, templateFiles: &memTemplateFiles{files: map[string][]byte{}}}
	resp := handleTemplateOfficeImport(engine, context.Background(), bridgeRequest("template.office.import", map[string]any{
		"fileName": "old.ppt", "contentBase64": base64.StdEncoding.EncodeToString([]byte("ppt")),
	}))
	if resp.OK {
		t.Fatal("accepted legacy ppt")
	}
}

func bridgeRequest(method string, payload map[string]any) bridge.Request {
	return bridge.Request{
		ID: ulid.Make().String(), TraceID: ulid.Make().String(), IdempotencyKey: "office-import",
		Method: method, Payload: mustJSON(payload),
	}
}
