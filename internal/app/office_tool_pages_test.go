package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/officeapp"
	content "github.com/lunitide/lunitide/internal/officestudio"
	"github.com/lunitide/lunitide/internal/officetools"
)

func TestOfficeInspectOCRNoticeUsesRoutingNotLocalForProvider(t *testing.T) {
	provider := officeInspectOCRNotice("provider-ocr")
	if provider == "" || strings.Contains(provider, "Local OCR") || !strings.Contains(provider, "misread") {
		t.Fatalf("provider OCR must be routing, not local: %q", provider)
	}
	local := officeInspectOCRNotice("windows-ocr")
	if !strings.Contains(local, "Local OCR") {
		t.Fatalf("windows-ocr must keep the local notice: %q", local)
	}
	if officeInspectOCRNotice("text-layer") != "" || officeInspectOCRNotice("") != "" {
		t.Fatal("text-layer and empty method must keep the structural notice")
	}
}

func TestOfficeInspectReadsUploadedPDFText(t *testing.T) {
	e, _ := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "pdf-source")
	data, err := officetools.GenPDF("Business Plan", "Source revenue 123456. Use this source, not the desktop.")
	if err != nil {
		t.Fatal(err)
	}
	v, err := e.officeStudio.Import(context.Background(), task.ID, "", "source.pdf", data, "", 0, "pdf-source-import")
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Nodes []struct {
			Text     string
			Editable bool
		}
		HasMore bool
	}
	officeModelPage(t, e, task.SessionID, "office.inspect", map[string]any{"taskId": task.ID, "versionId": v.ID}, &page)
	var text strings.Builder
	for _, node := range page.Nodes {
		if node.Editable {
			t.Error("PDF text incorrectly editable")
		}
		text.WriteString(node.Text)
	}
	if !strings.Contains(strings.Join(strings.Fields(text.String()), ""), "123456") {
		t.Fatalf("PDF source unread: %q", text.String())
	}
}

func officeModelPage(t *testing.T, e *Engine, sessionID, name string, args any, target any) string {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	_, _, output, err := e.executeOfficeTool(context.Background(), sessionID, name, raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(output) > officeToolPageLimit || clipToolSummary(output) != output || !json.Valid([]byte(output)) {
		t.Fatalf("tool JSON would be truncated or rewritten (%d): %s", len(output), output)
	}
	if err = json.Unmarshal([]byte(output), target); err != nil {
		t.Fatal(err)
	}
	return output
}

func TestOfficeModelPagesPreserveLongNodeContentAndStableEditMetadata(t *testing.T) {
	e, _ := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "tool-text-pages")
	text := strings.Repeat("<&>中文", 800) + officeGenInternalHint + strings.Repeat("尾文", 120)
	v, err := e.officeStudio.Generate(context.Background(), task.ID, "长正文.docx", content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "长正文", Blocks: []content.Block{{Type: "paragraph", Text: text}, {Type: "paragraph", Text: "最后一段"}}}, "long-node")
	if err != nil {
		t.Fatal(err)
	}
	index, textOffset := 1, 0
	var rebuilt strings.Builder
	stableID, stableDigest := "", ""
	tail := false
	for calls := 0; calls < 100; calls++ {
		var page struct {
			SHA256                                     string
			ExpectedRevision                           int64
			NextNodeOffset, NextTextOffset, TotalNodes int
			HasMore                                    bool
			Nodes                                      []struct {
				ID, Digest, Text           string
				TextOffset, NextTextOffset int
				Editable                   bool
			}
		}
		officeModelPage(t, e, task.SessionID, "office.inspect", map[string]any{"taskId": task.ID, "versionId": v.ID, "nodeOffset": index, "textOffset": textOffset}, &page)
		if page.SHA256 != v.SHA256 || page.ExpectedRevision != 1 || len(page.Nodes) == 0 {
			t.Fatalf("edit metadata missing: %+v", page)
		}
		for _, n := range page.Nodes {
			if n.Text == "最后一段" {
				tail = true
				continue
			}
			if stableID == "" {
				stableID, stableDigest = n.ID, n.Digest
			}
			if n.ID != stableID || n.Digest != stableDigest || len(n.Digest) != 64 || !n.Editable {
				t.Fatal("stable long-node metadata changed", n)
			}
			rebuilt.WriteString(n.Text)
		}
		if !page.HasMore {
			break
		}
		if page.NextNodeOffset == index && page.NextTextOffset <= textOffset {
			t.Fatal("no cursor progress")
		}
		index, textOffset = page.NextNodeOffset, page.NextTextOffset
	}
	if rebuilt.String() != text || !tail {
		t.Fatalf("long node/tail lost: text=%d/%d tail=%v", rebuilt.Len(), len(text), tail)
	}
}

func TestOfficeModelPagesReachEveryVersionAndBundleFile(t *testing.T) {
	e, _ := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "tool-version-pages")
	ids := []string{}
	for n := 0; n < 32; n++ {
		v, err := e.officeStudio.Generate(context.Background(), task.ID, fmt.Sprintf("版本%02d.docx", n), content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: fmt.Sprintf("版本%d", n), Blocks: []content.Block{{Type: "paragraph", Text: strings.Repeat("<&>内容", 20)}}}, fmt.Sprintf("many-file-%d", n))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, v.ID)
	}
	offset := 0
	seen := map[string]bool{}
	for calls := 0; calls < 40; calls++ {
		var page struct {
			NextOffset, Total int
			HasMore           bool
			Versions          []struct {
				VersionID, SHA256, ArtifactID string
				HeadRevision                  int64
			}
		}
		officeModelPage(t, e, task.SessionID, "office.inspect", map[string]any{"taskId": task.ID, "offset": offset}, &page)
		for _, v := range page.Versions {
			if seen[v.VersionID] || len(v.SHA256) != 64 || v.HeadRevision != 1 {
				t.Fatal("version metadata lost", v)
			}
			seen[v.VersionID] = true
		}
		if !page.HasMore {
			break
		}
		if page.NextOffset <= offset {
			t.Fatal("version cursor stalled")
		}
		offset = page.NextOffset
	}
	if len(seen) != 32 {
		t.Fatal("not every version reached", len(seen))
	}
	var created struct {
		Bundle struct {
			ID        string
			FileCount int
		}
	}
	officeModelPage(t, e, task.SessionID, "office.deliver", map[string]any{"taskId": task.ID, "action": "bundle", "title": "固定版本交付", "versionIds": ids}, &created)
	if created.Bundle.ID == "" || created.Bundle.FileCount != 32 {
		t.Fatal("bundle summary lost", created)
	}
	offset = 0
	seen = map[string]bool{}
	for calls := 0; calls < 40; calls++ {
		var page struct {
			NextFileOffset, TotalFiles int
			HasMore                    bool
			Files                      []struct{ VersionID, SHA256 string }
		}
		officeModelPage(t, e, task.SessionID, "office.deliver", map[string]any{"taskId": task.ID, "action": "bundles", "bundleId": created.Bundle.ID, "fileOffset": offset}, &page)
		for _, f := range page.Files {
			if seen[f.VersionID] || len(f.SHA256) != 64 {
				t.Fatal("bundle file metadata lost", f)
			}
			seen[f.VersionID] = true
		}
		if !page.HasMore {
			break
		}
		if page.NextFileOffset <= offset {
			t.Fatal("bundle cursor stalled")
		}
		offset = page.NextFileOffset
	}
	if len(seen) != 32 {
		t.Fatal("not every bundle file reached", len(seen))
	}
}

func TestOfficeMetricModelPagesKeepFullLongSourceValue(t *testing.T) {
	e, _ := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "tool-metric-pages")
	text := strings.Repeat("<&>指标", 400)
	v, err := e.officeStudio.Generate(context.Background(), task.ID, "来源.docx", content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "来源", Blocks: []content.Block{{Type: "paragraph", Text: text}}}, "metric-source")
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.officeStudio.Preview(context.Background(), task.ID, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	n := p.Nodes[1]
	m, err := e.officeStudio.CaptureMetric(context.Background(), task.ID, officeapp.MetricCapture{SourceVersionID: v.ID, SourceNodeID: n.ID, SourceNodeDigest: n.Digest, Name: strings.Repeat("<", 80), Unit: strings.Repeat("<", 64), Period: strings.Repeat("<", 128)}, "long-metric")
	if err != nil {
		t.Fatal(err)
	}
	offset, textOffset := 0, 0
	var rebuilt strings.Builder
	for calls := 0; calls < 100; calls++ {
		var page struct {
			NextOffset, NextTextOffset int
			HasMore                    bool
			Metrics                    []struct{ ID, RawValue, SourceVersionID, SourceSHA256, SourceNodeDigest string }
		}
		officeModelPage(t, e, task.SessionID, "office.deliver", map[string]any{"taskId": task.ID, "action": "metrics", "metricId": m.ID, "offset": offset, "textOffset": textOffset}, &page)
		if len(page.Metrics) != 1 || page.Metrics[0].SourceNodeDigest != n.Digest || page.Metrics[0].SourceSHA256 != v.SHA256 {
			t.Fatal("metric source identity missing", page)
		}
		rebuilt.WriteString(page.Metrics[0].RawValue)
		if !page.HasMore {
			break
		}
		if page.NextOffset == offset && page.NextTextOffset <= textOffset {
			t.Fatal("metric cursor stalled")
		}
		offset, textOffset = page.NextOffset, page.NextTextOffset
	}
	if rebuilt.String() != text {
		t.Fatalf("metric source value clipped: %d/%d", rebuilt.Len(), len(text))
	}
}
