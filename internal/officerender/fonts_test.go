package officerender

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func fontPackage(t *testing.T, parts map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, body := range parts {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestFontReportChecksDeclarationsWithoutClaimingSubstitution(t *testing.T) {
	data := fontPackage(t, map[string]string{
		"word/document.xml":     `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:rFonts w:ascii="Arial" w:eastAsia="Missing Example Font"/><w:rFonts w:ascii="Arial"/></w:document>`,
		"word/theme/theme1.xml": `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:majorFont><a:latin typeface="Unused Theme Family"/><a:font script="Jpan" typeface="Unused Japanese Font"/></a:majorFont></a:theme>`,
	})
	before := append([]byte{}, data...)
	r := Renderer{ListFonts: func(context.Context) (FontInventory, error) {
		return FontInventory{Families: []string{"Arial", "Noto Sans SC"}, Complete: true, Basis: "fixture-real-inventory-contract"}, nil
	}}
	report, err := r.FontReport(context.Background(), "docx", data)
	if err != nil || len(report.Families) != 2 || report.MissingCount != 1 || report.UnknownCount != 0 || report.SubstitutionVerified || !bytes.Equal(before, data) {
		t.Fatalf("font report: %+v %v", report, err)
	}
	for _, family := range report.Families {
		if strings.HasPrefix(family.Family, "Unused") {
			t.Fatal("unused theme declaration treated as used font")
		}
		if family.Family == "Arial" && (family.Status != "available" || family.ReferenceCount != 2) {
			t.Fatal("available declaration wrong", family)
		}
		if family.Status == "missing" && family.SuggestedFallback != "Noto Sans SC" {
			t.Fatal("fallback not backed by inventory", family)
		}
	}
}

func TestFontReportThemeMappingAmbiguityAndUnknownInventory(t *testing.T) {
	parts := map[string]string{
		"ppt/slides/slide1.xml": `<a:p xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:latin typeface="+mn-lt"/><a:ea typeface="+mn-ea"/></a:p>`,
		"ppt/theme/theme1.xml":  `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:minorFont><a:latin typeface="Arial"/><a:ea typeface=""/></a:minorFont></a:theme>`,
	}
	r := Renderer{ListFonts: func(context.Context) (FontInventory, error) {
		return FontInventory{Families: []string{"Arial"}, Complete: true}, nil
	}}
	report, err := r.FontReport(context.Background(), "pptx", fontPackage(t, parts))
	if err != nil || report.UnknownCount != 1 || report.MissingCount != 0 {
		t.Fatalf("unresolved CJK theme invented: %+v %v", report, err)
	}
	parts["ppt/theme/theme2.xml"] = `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:minorFont><a:latin typeface="Different Master Font"/></a:minorFont></a:theme>`
	report, err = r.FontReport(context.Background(), "pptx", fontPackage(t, parts))
	if err != nil || report.UnknownCount != 2 {
		t.Fatalf("ambiguous theme silently resolved: %+v %v", report, err)
	}
	r.ListFonts = func(context.Context) (FontInventory, error) {
		return FontInventory{Basis: "unavailable"}, errors.New("inventory unavailable")
	}
	report, err = r.FontReport(context.Background(), "pptx", fontPackage(t, parts))
	if err != nil || report.InventoryComplete || report.MissingCount != 0 {
		t.Fatalf("unknown inventory claimed missing: %+v %v", report, err)
	}
}

func TestFontReportSheetRichTextAndLimits(t *testing.T) {
	r := Renderer{ListFonts: func(context.Context) (FontInventory, error) {
		return FontInventory{Families: []string{"Arial"}, Complete: true}, nil
	}}
	data := fontPackage(t, map[string]string{"xl/sharedStrings.xml": `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><r><rPr><rFont val="Missing Rich Font"/></rPr><t>Text</t></r></si></sst>`})
	report, err := r.FontReport(context.Background(), "xlsx", data)
	if err != nil || report.MissingCount != 1 {
		t.Fatalf("rich string fonts ignored: %+v %v", report, err)
	}
	data = fontPackage(t, map[string]string{"word/document.xml": `<!DOCTYPE test [<!ENTITY external SYSTEM "file:///secret">]><document/>`})
	if _, err = r.FontReport(context.Background(), "docx", data); err == nil {
		t.Fatal("external declaration accepted")
	}
	data = fontPackage(t, map[string]string{"word/document.xml": `<w:rFonts xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" w:ascii="` + strings.Repeat("x", 300) + `"/>`})
	report, err = r.FontReport(context.Background(), "docx", data)
	if err != nil || report.DeclarationScanComplete {
		t.Fatalf("oversized font silently discarded: %+v %v", report, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = r.FontReport(ctx, "docx", data); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel ignored", err)
	}
}
