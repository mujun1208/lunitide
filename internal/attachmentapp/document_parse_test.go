package attachmentapp

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/attachment"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

func TestOfficeDocumentsBecomeReadableChatMaterials(t *testing.T) {
	for _, kind := range []content.Kind{content.DOCX, content.PPTX, content.XLSX} {
		t.Run(string(kind), func(t *testing.T) {
			s := NewService(newMockStore(), NewDirFileStorage(t.TempDir()))
			spec := content.Spec{SchemaVersion: 1, Kind: kind, Title: "源材料"}
			switch kind {
			case content.DOCX:
				spec.Blocks = []content.Block{{Type: "paragraph", Text: "销售原始数据"}}
			case content.PPTX:
				spec.Slides = []content.Slide{{Title: "销售原始数据"}}
			case content.XLSX:
				spec.Sheets = []content.Sheet{{Name: "数据", Rows: [][]content.Cell{{{Type: "text", Value: "销售原始数据"}, {Type: "text", Value: "001234567890123456"}}}}}
			}
			b, err := content.Generate(spec)
			if err != nil {
				t.Fatal(err)
			}
			a, err := s.IngestFile(context.Background(), IngestFileRequest{ProjectID: mustULID(), SessionID: mustULID(), OriginalName: "source." + string(kind), MIME: "application/octet-stream", Content: b})
			if err != nil || a.ParseStatus != attachment.StatusSucceeded || !strings.Contains(a.ParsedText, "销售原始数据") {
				t.Fatalf("readable office: %+v %v", a, err)
			}
			if kind == content.XLSX && !strings.Contains(a.ParsedText, "001234567890123456") {
				t.Fatal("text identifier changed")
			}
		})
	}
}

func TestLegacyWordDocumentsBecomeReadableChatMaterials(t *testing.T) {
	s := NewService(newMockStore(), NewDirFileStorage(t.TempDir()))
	rtf, err := s.IngestFile(context.Background(), IngestFileRequest{ProjectID: mustULID(), SessionID: mustULID(), OriginalName: "需求.rtf", MIME: "application/octet-stream", Content: []byte("{\\rtf1\\ansi 客户关系 CRM\\par }")})
	if err != nil || rtf.ParseStatus != attachment.StatusSucceeded || !strings.Contains(rtf.ParsedText, "客户关系") {
		t.Fatalf("rtf: %+v %v", rtf, err)
	}
	body := "客户关系管理系统需求说明"
	chars := []rune(body)
	raw := make([]byte, len(chars)*2)
	for i, r := range chars {
		raw[i*2] = byte(r)
		raw[i*2+1] = byte(r >> 8)
	}
	doc, err := s.IngestFile(context.Background(), IngestFileRequest{ProjectID: mustULID(), SessionID: mustULID(), OriginalName: "需求.doc", MIME: "application/msword", Content: raw})
	if err != nil || doc.ParseStatus != attachment.StatusSucceeded || !strings.Contains(doc.ParsedText, body) {
		t.Fatalf("doc: %+v %v", doc, err)
	}
	xls, err := s.IngestFile(context.Background(), IngestFileRequest{ProjectID: mustULID(), SessionID: mustULID(), OriginalName: "表.xls", MIME: "application/vnd.ms-excel", Content: raw})
	if err != nil || xls.ParseStatus != attachment.StatusSucceeded || !strings.Contains(xls.ParsedText, body) {
		t.Fatalf("xls: %+v %v", xls, err)
	}
	wps, err := s.IngestFile(context.Background(), IngestFileRequest{ProjectID: mustULID(), SessionID: mustULID(), OriginalName: "需求.wps", MIME: "application/octet-stream", Content: []byte(body)})
	if err != nil || wps.ParseStatus != attachment.StatusSucceeded || !strings.Contains(wps.ParsedText, body) {
		t.Fatalf("wps: %+v %v", wps, err)
	}
}

func TestBrokenPDFRetainsBytesWithoutReadableGarbage(t *testing.T) {
	s := NewService(newMockStore(), NewDirFileStorage(t.TempDir()))
	a, err := s.IngestFile(context.Background(), IngestFileRequest{ProjectID: mustULID(), SessionID: mustULID(), OriginalName: "broken.pdf", MIME: "application/pdf", Content: []byte("%PDF-1.4")})
	if err != nil || a.ParseStatus != attachment.StatusFailed || a.ParseErrorCode != "PARSE_FAILED" || a.IsReadable() {
		t.Fatalf("invalid PDF: %+v %v", a, err)
	}
}

func TestSourceMaterialsHandleWindowsMIMEWithoutAcceptingBinary(t *testing.T) {
	for _, tc := range []struct {
		name, mime, body string
		readable         bool
	}{
		{"materials.json", "application/octet-stream", `{"id":"001234567890123456","name":"项目材料"}`, true},
		{"component.ts", "video/mp2t", `export const title: string = "项目材料";`, true},
		{"component.ts", "application/typescript", `export const amount = 123;`, true},
		{"materials.json", "application/json; charset=utf-8", `{"name":"项目材料"}`, true},
		{"component.ts", "video/mp2t", "\x47\x00\x12\xff", false},
		{"data.json", "application/octet-stream", "x\x00y", false},
		{"unknown.bin", "application/octet-stream", "opaque data", false},
	} {
		t.Run(tc.name+tc.mime, func(t *testing.T) {
			s := NewService(newMockStore(), NewDirFileStorage(t.TempDir()))
			a, err := s.IngestFile(context.Background(), IngestFileRequest{ProjectID: mustULID(), SessionID: mustULID(), OriginalName: tc.name, MIME: tc.mime, Content: []byte(tc.body)})
			if err != nil {
				t.Fatal(err)
			}
			if a.IsReadable() != tc.readable {
				t.Fatalf("readability=%t: %+v", tc.readable, a)
			}
			if tc.readable && a.ParsedText != tc.body {
				t.Fatal("source changed")
			}
		})
	}
}
