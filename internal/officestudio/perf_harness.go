package officestudio

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

type PerfSample struct {
	Fixture, Kind, Reason string
	ElapsedMS             int64
	Skipped               bool
}

type PerfSummary struct {
	Ready bool
	P50MS int64
	P95MS int64
	N     int
}

func MeasureOfficeFixtures(n int) []PerfSample {
	if n < 1 {
		return []PerfSample{{Skipped: true, Reason: "need ≥1 timed run before claiming a P50"}}
	}
	fixtures := []struct {
		kind Kind
		name string
		spec Spec
	}{
		{PPTX, "12-page-ppt", Spec{SchemaVersion: 2, Kind: PPTX, Title: "计时PPT", Slides: twelveTimingSlides()}},
		{DOCX, "20-block-word", Spec{SchemaVersion: 2, Kind: DOCX, Title: "计时Word", Blocks: twentyTimingBlocks()}},
		{XLSX, "3-sheet-excel", Spec{SchemaVersion: 2, Kind: XLSX, Title: "计时Excel", Sheets: []Sheet{
			{Name: "原始数据", Rows: [][]Cell{{{Type: "text", Value: "订单"}, {Type: "number", Value: "1280"}}}},
			{Name: "汇总", Rows: [][]Cell{{{Type: "formula", Value: "=COUNTA('原始数据'!A2:A1048576)"}}}},
			{Name: "说明", Rows: [][]Cell{{{Type: "text", Value: "密级未填写"}}}},
		}}},
		{PDF, "short-pdf", Spec{SchemaVersion: 2, Kind: PDF, Title: "计时PDF", Body: "订单 1280单"}},
	}
	var out []PerfSample
	for _, fx := range fixtures {
		for i := 0; i < n; i++ {
			start := time.Now()
			data, err := Generate(fx.spec)
			elapsed := time.Since(start).Milliseconds()
			if err != nil {
				out = append(out, PerfSample{Fixture: fx.name + "/generate", Kind: string(fx.kind), ElapsedMS: elapsed, Skipped: true, Reason: err.Error()})
				continue
			}
			checkStart := time.Now()
			_, checkErr := Validate(fx.kind, data)
			checkMS := time.Since(checkStart).Milliseconds()
			reason := ""
			if checkErr != nil {
				reason = checkErr.Error()
			}
			out = append(out, PerfSample{Fixture: fx.name + "/generate", Kind: string(fx.kind), ElapsedMS: elapsed})
			out = append(out, PerfSample{Fixture: fx.name + "/check", Kind: string(fx.kind), ElapsedMS: checkMS, Reason: reason})
			if patch := measureTextPatch(fx.name, fx.kind, data); patch.Fixture != "" {
				out = append(out, patch)
			}
		}
	}
	return out
}

func measureTextPatch(name string, kind Kind, data []byte) PerfSample {
	if kind != PPTX && kind != DOCX {
		return PerfSample{}
	}
	insp, err := Inspect(kind, data)
	if err != nil {
		return PerfSample{Fixture: name + "/patch", Kind: string(kind), Skipped: true, Reason: err.Error()}
	}
	var node Node
	for _, n := range insp.Nodes {
		if n.Editable && strings.TrimSpace(n.Text) != "" {
			node = n
			break
		}
	}
	if node.ID == "" {
		return PerfSample{Fixture: name + "/patch", Kind: string(kind), Skipped: true, Reason: "no editable text node"}
	}
	start := time.Now()
	patched, err := Patch(data, PatchRequest{Kind: kind, BaseSHA256: digest(data), Operations: []TextPatch{{NodeID: node.ID, ExpectedDigest: node.Digest, Text: node.Text + "修订"}}})
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		return PerfSample{Fixture: name + "/patch", Kind: string(kind), ElapsedMS: elapsed, Skipped: true, Reason: err.Error()}
	}
	checkStart := time.Now()
	_, checkErr := Validate(kind, patched.Data)
	checkMS := time.Since(checkStart).Milliseconds()
	reason := ""
	if checkErr != nil {
		reason = checkErr.Error()
	}
	return PerfSample{Fixture: name + "/patch", Kind: string(kind), ElapsedMS: elapsed + checkMS, Reason: reason}
}

func twelveTimingSlides() []Slide {
	out := make([]Slide, 0, 12)
	for i := 0; i < 12; i++ {
		out = append(out, Slide{Title: "页" + strconv.Itoa(i+1), Layout: "section", Bullets: []string{"订单 1280单"}})
	}
	return out
}

func twentyTimingBlocks() []Block {
	out := make([]Block, 0, 20)
	for i := 0; i < 20; i++ {
		out = append(out, Block{Type: "paragraph", Text: "正文段落订单 1280单"})
	}
	return out
}

func SummarizePerf(samples []PerfSample) PerfSummary {
	var times []int64
	for _, s := range samples {
		if s.Skipped {
			continue
		}
		times = append(times, s.ElapsedMS)
	}
	if len(times) == 0 {
		return PerfSummary{Ready: false}
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	return PerfSummary{
		Ready: len(times) >= 30,
		P50MS: percentileMS(times, 50),
		P95MS: percentileMS(times, 95),
		N:     len(times),
	}
}

func percentileMS(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := (len(sorted)*p + 99) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
