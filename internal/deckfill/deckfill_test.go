package deckfill

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFillReplacesPromptAndDropsUnusedMedia(t *testing.T) {
	raw := mustZip(t, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Override PartName="/ppt/slides/slide1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>` +
			`<Override PartName="/ppt/slides/slide2.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>` +
			`</Types>`,
		"_rels/.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>` +
			`</Relationships>`,
		"ppt/presentation.xml": `<?xml version="1.0"?><p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` +
			`<p:sldIdLst><p:sldId id="256" r:id="rId2"/><p:sldId id="257" r:id="rId3"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>` +
			`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide2.xml"/>` +
			`</Relationships>`,
		"ppt/slides/slide1.xml": slideXML(
			shape("工作总结", 1800, 2000000),
			shape("市场份额稳步提升", 1800, 4000000),
			shape("此处添加详细文本描述，建议与标题相关", 1800, 5000000),
			shape("添加标题", 1800, 4000000),
			shape("汇报人：觅知网", 1400, 3000000),
		),
		"ppt/slides/_rels/slide1.xml.rels": rels(`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/keep.png"/>`),
		"ppt/media/keep.png":               "keep",
		"ppt/slides/slide2.xml":            `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"></p:sld>`,
		"ppt/slides/_rels/slide2.xml.rels": rels(`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/drop.png"/>`),
		"ppt/media/drop.png":               "drop",
	})
	out, notes, err := FillKept(raw, []int{1}, Plan{
		DeckTitle: "2026 上半年经营汇报",
		Points:    []Point{{Title: "营收增长 & 毛利", Body: "营收同比提升，毛利保持稳定。"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, out)
	slide := files["ppt/slides/slide1.xml"]
	if strings.Contains(slide, "添加标题") || strings.Contains(slide, "此处添加") || strings.Contains(slide, "觅知网") {
		t.Fatalf("sample copy survived: %s", slide)
	}
	if !strings.Contains(slide, "2026 上半年经营汇报") || !strings.Contains(slide, "营收增长 &amp; 毛利") {
		t.Fatalf("replacement missing: %s", slide)
	}
	if !strings.Contains(slide, "市场份额稳步提升") {
		t.Fatalf("existing sentence was overwritten: %s", slide)
	}
	if !strings.Contains(slide, "汇报人：") {
		t.Fatalf("presenter label missing: %s", slide)
	}
	if _, ok := files["ppt/media/drop.png"]; ok {
		t.Fatal("unused picture was kept")
	}
	if files["ppt/media/keep.png"] != "keep" {
		t.Fatal("used picture was dropped")
	}
	if strings.Contains(files["[Content_Types].xml"], "slide2.xml") {
		t.Fatal("dropped slide still declared")
	}
	for _, note := range notes {
		if strings.Contains(note, "英文") {
			t.Fatalf("unexpected english note: %v", notes)
		}
	}
}

func TestBuildClonesWhenPointsExceedPage(t *testing.T) {
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
			shape("添加标题", 1800, 3000000),
			shape("此处添加详细文本描述，建议与标题相关", 1400, 5000000),
		),
	})
	out, _, err := BuildBytes(raw, Plan{Points: []Point{
		{Title: "第一点", Body: "甲"},
		{Title: "第二点", Body: "乙"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, out)
	if _, ok := files["ppt/slides/slide2.xml"]; !ok {
		t.Fatalf("expected a cloned slide, got %v", keys(files))
	}
	joined := files["ppt/slides/slide1.xml"] + files["ppt/slides/slide2.xml"]
	if !strings.Contains(joined, "第一点") || !strings.Contains(joined, "第二点") || strings.Contains(joined, "添加标题") {
		t.Fatalf("clone fill failed: %s", joined)
	}
}

func TestClassifyTaskAndKeywordPrompts(t *testing.T) {
	if Classify("任务一") != SlotTitle || Classify("关键词一：提炼本阶段亮点") != SlotBody {
		t.Fatal(Classify("任务一"), Classify("关键词一：提炼本阶段亮点"))
	}
	if Classify("市场份额稳步提升") != SlotConfirm {
		t.Fatal("business sentence should stay")
	}
	if Classify("LOGO") != SlotSkip {
		t.Fatal("logo")
	}
}

func shape(text string, sz, cx int) string {
	return `<p:sp><p:spPr><a:xfrm><a:ext cx="` + itoa(cx) + `" cy="400000"/></a:xfrm></p:spPr>` +
		`<p:txBody><a:p><a:r><a:rPr sz="` + itoa(sz) + `"/><a:t>` + text + `</a:t></a:r></a:p></p:txBody></p:sp>`
}

func slideXML(shapes ...string) string {
	return `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree>` +
		strings.Join(shapes, "") + `</p:spTree></p:cSld></p:sld>`
}

func rels(body string) string {
	return `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + body + `</Relationships>`
}

func mustZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
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
	return buf.Bytes()
}

func unzip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		_, err = buf.ReadFrom(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name] = buf.String()
	}
	return out
}

func keys(files map[string]string) []string {
	out := make([]string, 0, len(files))
	for name := range files {
		out = append(out, name)
	}
	return out
}

func TestFillNamedWorkSummarySlide(t *testing.T) {
	path := filepath.Join(`D:\PPT模板库`, "工作总结", "工作总结.pptx")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip(path)
	}
	plan := proofPlan()
	out, notes, err := FillKept(raw, []int{4}, plan)
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, out)
	slide := files["ppt/slides/slide4.xml"]
	if slide == "" || strings.Contains(slide, "添加标题") || strings.Contains(slide, "此处添加") {
		t.Fatalf("slide 4 prompts remain, notes=%v", notes)
	}
	if !strings.Contains(slide, "营收") {
		t.Fatalf("proof copy missing")
	}
	dir := filepath.Join("..", "..", "_scratch", "p0")
	if err := os.MkdirAll(dir, 0o755); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "B-work-summary-slide4.pptx"), out, 0o644)
	}
}

func TestFillNamedResultSlide(t *testing.T) {
	path := filepath.Join(`D:\PPT模板库`, "工作总结", "76f50305db18c7600ba8a755b66f45e8_20260630269921_1.pptx")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skip(path)
	}
	out, notes, err := FillKept(raw, []int{8}, proofPlan())
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, out)
	slide := files["ppt/slides/slide8.xml"]
	if strings.Contains(slide, "任务一") || strings.Contains(slide, "简述本阶段") || strings.Contains(slide, "关键词一") {
		t.Fatalf("slide 8 prompts remain, notes=%v", notes)
	}
	if !strings.Contains(slide, "营收") {
		t.Fatal("proof copy missing")
	}
	dir := filepath.Join("..", "..", "_scratch", "p0")
	if err := os.MkdirAll(dir, 0o755); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "B-result-slide8.pptx"), out, 0o644)
	}
}

func TestBuildNamedLibrary(t *testing.T) {
	root := DefaultRoot()
	if root == "" {
		t.Skip("template library off")
	}
	out, notes, err := Build(root, proofPlan())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 32<<20 {
		t.Fatalf("filled deck is %d bytes, notes=%v", len(out), notes)
	}
	files := unzip(t, out)
	joined := files["ppt/slides/slide1.xml"] + files["ppt/slides/slide2.xml"]
	if strings.Contains(joined, "添加标题") || strings.Contains(joined, "此处添加") || !strings.Contains(joined, ">毛利<") {
		t.Fatalf("library fill missed title slots, notes=%v", notes)
	}
	dir := filepath.Join("..", "..", "_scratch", "p0")
	if err := os.MkdirAll(dir, 0o755); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "C-library.pptx"), out, 0o644)
	}
	t.Logf("library deck %d bytes notes=%v", len(out), notes)
}

func proofPlan() Plan {
	return Plan{
		DeckTitle: "2026 上半年经营汇报",
		Presenter: "管理层",
		Date:      "2026年7月",
		Points: []Point{
			{Title: "营收", Body: "上半年营收按计划推进，主业订单保持增长。"},
			{Title: "毛利", Body: "毛利率稳定，费用投放集中在已验证的渠道。"},
			{Title: "客户", Body: "重点客户续约完成，新客贡献开始显现。"},
			{Title: "渠道", Body: "渠道结构更集中，低效投放已经收缩。"},
			{Title: "交付", Body: "交付周期缩短，重大项目按节点关闭。"},
			{Title: "下半年", Body: "下半年只加码已跑通的产品，不再铺新战线。"},
		},
	}
}
