package deckfill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelPlanClonesAPageWithoutPhraseRules(t *testing.T) {
	raw := mustZip(t, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Override PartName="/ppt/slides/slide1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>` +
			`</Types>`,
		"_rels/.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>` +
			`</Relationships>`,
		"ppt/presentation.xml": `<?xml version="1.0"?><p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<p:sldIdLst><p:sldId id="256" r:id="rId2"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": rels(`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>`),
		"ppt/slides/slide1.xml": slideXML(
			`<p:sp><p:nvSpPr><p:cNvPr id="7" name="标题"/></p:nvSpPr><p:spPr><a:xfrm><a:ext cx="4000000" cy="400000"/></a:xfrm></p:spPr><p:txBody><a:p><a:r><a:rPr sz="1800"/><a:t>随便写的封面标题</a:t></a:r></a:p></p:txBody></p:sp>`,
			`<p:sp><p:nvSpPr><p:cNvPr id="8" name="正文"/></p:nvSpPr><p:spPr><a:xfrm><a:ext cx="5000000" cy="400000"/></a:xfrm></p:spPr><p:txBody><a:p><a:r><a:rPr sz="1400"/><a:t>这里是设计师写的说明，不是固定口令</a:t></a:r></a:p></p:txBody></p:sp>`,
		),
	})
	dir := t.TempDir()
	folder := filepath.Join(dir, "新分类")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "新模板.pptx")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := ListLibrary(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list, "新分类/新模板.pptx") {
		t.Fatalf("new file was not listed: %s", list)
	}
	desc, err := DescribeTemplate(dir, "新分类/新模板.pptx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(desc, "id=7") || !strings.Contains(desc, "随便写的封面标题") {
		t.Fatalf("catalog missing shape: %s", desc)
	}
	out, notes, err := ApplyFile(dir, "新分类/新模板.pptx", []PageUse{
		{From: 1, Texts: []TextPut{{ID: "7", Text: "第一页主张"}, {ID: "8", Text: "第一页正文"}}},
		{From: 1, Texts: []TextPut{{ID: "7", Text: "第二页主张"}, {ID: "8", Text: "第二页正文"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, out)
	s1, s2 := files["ppt/slides/slide1.xml"], files["ppt/slides/slide2.xml"]
	if !strings.Contains(s1, "第一页主张") || !strings.Contains(s2, "第二页主张") {
		t.Fatalf("clone did not keep both copies notes=%v\n%s\n%s", notes, s1, s2)
	}
	if strings.Contains(s1+s2, "随便写的封面标题") || strings.Contains(s1+s2, "设计师写的说明") {
		t.Fatal("original sample survived")
	}
}

func TestDescribeMarksImageOnlyPage(t *testing.T) {
	raw := mustZip(t, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"></Types>`,
		"_rels/.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>` +
			`</Relationships>`,
		"ppt/presentation.xml": `<?xml version="1.0"?><p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<p:sldIdLst><p:sldId id="256" r:id="rId2"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": rels(`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>`),
		"ppt/slides/slide1.xml":           `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"></p:sld>`,
	})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "图.pptx"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	desc, err := DescribeTemplate(dir, "图.pptx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(desc, "纯图") {
		t.Fatalf("image page not marked: %s", desc)
	}
}
