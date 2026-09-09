package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	content "github.com/lunitide/lunitide/internal/officestudio"
)

func TestOfficePreviewPagesReachLastCellWithinHostFrame(t *testing.T) {
	e, _ := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "many-cells")
	rows := make([][]content.Cell, 750)
	for i := range rows {
		rows[i] = []content.Cell{{Type: "text", Value: fmt.Sprintf("编号%04d", i)}, {Type: "text", Value: strings.Repeat("中文材料<&>", 30)}}
	}
	v, err := e.officeStudio.Generate(context.Background(), task.ID, "长数据.xlsx", content.Spec{SchemaVersion: 1, Kind: content.XLSX, Title: "长数据", Sheets: []content.Sheet{{Name: "数据", Rows: rows}}}, "many-cells-file")
	if err != nil {
		t.Fatal(err)
	}
	offset, pages := 0, 0
	seen := map[string]bool{}
	last := false
	for {
		r := officeCall(t, e, "office.artifact.preview", "", map[string]any{"taskId": task.ID, "versionId": v.ID, "nodeOffset": offset})
		if !r.OK {
			t.Fatalf("preview: %+v", r.Error)
		}
		wire, _ := json.Marshal(r)
		if len(wire) >= 256<<10 {
			t.Fatalf("host frame overflow: %d bytes", len(wire))
		}
		var page struct {
			Nodes []struct {
				ID, Text string
				Editable bool
			}
			NodeOffset, NextNodeOffset, TotalNodes int
		}
		if err := decodeResponsePayload(r.Payload, &page); err != nil {
			t.Fatal(err)
		}
		if page.NodeOffset != offset || page.NextNodeOffset <= offset {
			t.Fatalf("nonprogressing page: %+v", page)
		}
		for _, n := range page.Nodes {
			if seen[n.ID] {
				t.Fatalf("repeated node %s", n.ID)
			}
			seen[n.ID] = true
			if n.Text == "编号0749" {
				last = true
			}
		}
		pages++
		if page.NextNodeOffset == page.TotalNodes {
			break
		}
		offset = page.NextNodeOffset
		if pages > 30 {
			t.Fatal("pagination failed to terminate")
		}
	}
	if !last || len(seen) != 1500 || pages < 2 {
		t.Fatalf("missing tail: last=%v nodes=%d pages=%d", last, len(seen), pages)
	}
	toolLast, textOffset := false, 0
	for calls := 0; calls < 200; calls++ {
		args, _ := json.Marshal(map[string]any{"taskId": task.ID, "versionId": v.ID, "nodeOffset": offset, "textOffset": textOffset})
		_, _, output, err := e.executeOfficeTool(context.Background(), task.SessionID, "office.inspect", args)
		if err != nil || len(output) > officeToolPageLimit || !json.Valid([]byte(clipToolSummary(output))) {
			t.Fatalf("model tool page incomplete: %v %s", err, output)
		}
		var next struct {
			NextNodeOffset, NextTextOffset int
			HasMore                        bool
		}
		if err = json.Unmarshal([]byte(output), &next); err != nil {
			t.Fatal(err)
		}
		toolLast = toolLast || strings.Contains(output, "编号0749")
		if !next.HasMore {
			break
		}
		if next.NextNodeOffset == offset && next.NextTextOffset <= textOffset {
			t.Fatal("tool page did not advance")
		}
		offset, textOffset = next.NextNodeOffset, next.NextTextOffset
	}
	if !toolLast {
		t.Fatal("model cannot inspect final cell through bounded pages")
	}
}

func TestOfficeOversizedParagraphIsReadOnlyWithoutBlockingFollowingNodes(t *testing.T) {
	e, _ := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "long-paragraph")
	v, err := e.officeStudio.Generate(context.Background(), task.ID, "长段落.docx", content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "长段落", Blocks: []content.Block{{Type: "paragraph", Text: strings.Repeat("中文段落", 10000)}, {Type: "paragraph", Text: "最后一段"}}}, "long-paragraph-file")
	if err != nil {
		t.Fatal(err)
	}
	// The generated document has its own title before the oversized paragraph.
	p, err := e.officeStudio.PreviewPage(context.Background(), task.ID, v.ID, 1)
	if err != nil || len(p.Nodes) != 1 || p.Nodes[0].Editable || !p.Truncated {
		t.Fatalf("abbreviated content became editable: nodes=%d offset=%d err=%v", len(p.Nodes), p.NextNodeOffset, err)
	}
	next, err := e.officeStudio.PreviewPage(context.Background(), task.ID, v.ID, p.NextNodeOffset)
	if err != nil || len(next.Nodes) != 1 || next.Nodes[0].Text != "最后一段" {
		t.Fatalf("tail hidden behind long paragraph: %+v %v", next, err)
	}
}
