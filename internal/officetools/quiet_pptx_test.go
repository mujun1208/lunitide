package officetools

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestQuietLayoutsAreDifferentCompositions(t *testing.T) {
	data, err := GenQuietPptx("上半年经营", []SlideSpec{
		{Title: "上半年经营汇报", Subtitle: "给管理层的一页结论"},
		{Title: "目录", Layout: "agenda", Bullets: []string{"营收", "毛利", "客户", "下一步"}},
		{Title: "毛利抬升", Layout: "metrics", Metrics: []SlideMetric{{Value: "18.6%", Label: "毛利率"}}, Source: "财务月报"},
		{Title: "客户原话", Layout: "quote", Bullets: []string{"交付比价格更决定续约"}, Source: "客户访谈"},
		{Title: "下一步"},
	})
	if err != nil {
		t.Fatal(err)
	}
	slides := readSlides(t, data)
	if len(slides) != 5 {
		t.Fatalf("slides = %d", len(slides))
	}
	if strings.Contains(slides[0], `cy="1180000"`) || strings.Contains(slides[0], "0B1F3A") {
		t.Fatal("cover still uses the navy header skeleton")
	}
	if !strings.Contains(slides[0], "上半年经营汇报") || !strings.Contains(slides[0], "F6F4F1") {
		t.Fatal("cover missing title or paper")
	}
	if !strings.Contains(slides[1], "name=\"Number1\"") || strings.Contains(slides[1], "name=\"Value1\"") {
		t.Fatal("agenda did not use its own composition")
	}
	if !strings.Contains(slides[2], "18.6%") || !strings.Contains(slides[2], "来源：财务月报") {
		t.Fatal("metrics did not keep the number and source")
	}
	if !strings.Contains(slides[3], "交付比价格") || strings.Contains(slides[3], "●") {
		t.Fatal("quote fell back to a bullet list")
	}
	if !strings.Contains(slides[4], "下一步") {
		t.Fatal("closing title missing")
	}
	if slides[0] == slides[2] {
		t.Fatal("cover and metrics rendered the same slide")
	}
}

func TestQuietKeepsCopyAndAReadableSection(t *testing.T) {
	points := []string{"订单", "毛利", "回款", "客户", "交付", "库存", "费用", "现金流"}
	data, err := GenQuietPptx("经营", []SlideSpec{
		{Title: "封面", Bullets: []string{"营收", "毛利", "客户", "现金流"}},
		{Title: "市场", Layout: "section"},
		{Title: "要点", Layout: "content", Bullets: points},
		{Title: "原话", Layout: "quote", Bullets: []string{"交付决定续约"}, Source: "客户访谈"},
		{Title: "下一步", Bullets: []string{"复盘毛利", "拜访头部客户"}, Notes: "先讲动作"},
	})
	if err != nil {
		t.Fatal(err)
	}
	slides := readSlides(t, data)
	for _, word := range []string{"营收", "毛利", "客户", "现金流"} {
		if !strings.Contains(slides[0], word) {
			t.Fatalf("cover dropped %s", word)
		}
	}
	if strings.Contains(slides[0], "营收  ·  毛利") {
		t.Fatal("cover joined every point into one line")
	}
	if !strings.Contains(slides[1], `val="8A8178"`) || strings.Contains(slides[1], "E6E1DA") {
		t.Fatal("section index is the paper color")
	}
	for _, word := range points {
		if !strings.Contains(slides[2], word) {
			t.Fatalf("content dropped %s", word)
		}
	}
	if strings.Count(slides[3], "客户访谈") != 1 {
		t.Fatalf("quote source repeated: %d", strings.Count(slides[3], "客户访谈"))
	}
	if !strings.Contains(slides[4], "复盘毛利") || !strings.Contains(slides[4], "拜访头部客户") {
		t.Fatal("closing dropped the action list")
	}
	master := zipPartBody(t, data, "ppt/slideMasters/slideMaster1.xml")
	if strings.Contains(master, "0B1F3A") || !strings.Contains(master, "F6F4F1") {
		t.Fatal("quiet master still uses the navy background")
	}
	notes := zipPartBody(t, data, "ppt/notesSlides/notesSlide5.xml")
	if !strings.Contains(notes, "先讲动作") {
		t.Fatal("speaker notes were dropped")
	}
}

func TestQuietKeepsCopyAndSeparatesLayouts(t *testing.T) {
	points := []string{"第一条", "第二条", "第三条", "第四条", "第五条", "第六条", "第七条", "第八条"}
	data, err := GenQuietPptx("经营", []SlideSpec{
		{Title: "封面", Subtitle: "一句结论", Bullets: []string{"不要并成一行"}},
		{Title: "这一章", Layout: "section"},
		{Title: "要点都在", Layout: "content", Bullets: append(append([]string{}, points...), "", "  ")},
		{Title: "下一步", Bullets: []string{"先改报价", "再约客户"}, Source: "周会"},
		{Title: "原话", Layout: "quote", Bullets: []string{"交付决定续约"}, Source: "访谈记录"},
		{Title: "有备注", Notes: "讲这页时先给结论", Bullets: []string{"正文还在"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	slides := readSlides(t, data)
	if strings.Contains(slides[0], "不要并成一行") == false || strings.Contains(slides[0], "一句结论") == false {
		t.Fatal("cover dropped the subtitle or the line")
	}
	if strings.Contains(slides[0], "不要并成一行  ·  ") {
		t.Fatal("cover joined lines into one sentence")
	}
	if !strings.Contains(slides[1], `name="Index"`) || !strings.Contains(slides[1], "8A8178") {
		t.Fatal("section number is missing or still the paper-colored watermark")
	}
	for _, point := range points {
		if !strings.Contains(slides[2], point) {
			t.Fatalf("content dropped %s", point)
		}
	}
	if strings.Count(slides[2], `name="Mark`) != len(points) {
		t.Fatal("blank bullets still drew empty marks")
	}
	if !strings.Contains(slides[3], "先改报价") || !strings.Contains(slides[3], "再约客户") || !strings.Contains(slides[3], "来源：周会") {
		t.Fatal("closing dropped the actions or the source")
	}
	if strings.Count(slides[4], "访谈记录") != 1 || !strings.Contains(slides[4], "交付决定续约") {
		t.Fatal("quote repeated the source or dropped the sentence")
	}
	notes := zipPartBody(t, data, "ppt/notesSlides/notesSlide6.xml")
	if !strings.Contains(notes, "讲这页时先给结论") {
		t.Fatal("speaker notes were dropped")
	}
	if !strings.Contains(slides[5], "正文还在") {
		t.Fatal("notes slide lost its visible text")
	}
	master := zipPartBody(t, data, "ppt/slideMasters/slideMaster1.xml")
	if strings.Contains(master, "0B1F3A") || !strings.Contains(master, "F6F4F1") {
		t.Fatal("new slides would inherit the navy master")
	}
}

func readSlides(t *testing.T, data []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, 8)
	for i := 1; i < 20; i++ {
		name := "ppt/slides/slide" + itoa(i) + ".xml"
		var found *zip.File
		for _, f := range zr.File {
			if f.Name == name {
				found = f
				break
			}
		}
		if found == nil {
			break
		}
		rc, err := found.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(body))
	}
	return out
}
