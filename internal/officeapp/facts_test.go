package officeapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

func metricSourceSpec(value string) content.Spec {
	return content.Spec{SchemaVersion: 1, Kind: content.XLSX, Title: "指标来源", Sheets: []content.Sheet{{Name: "数据", Rows: [][]content.Cell{{{Type: "number", Value: value}, {Type: "text", Value: "000012340001234567"}, {Type: "formula", Value: "=SUM(A1:A1)"}}}}}}
}

func metricNode(t *testing.T, s *Service, taskID string, v domain.Version, text string) content.Node {
	t.Helper()
	_, b, err := s.ReadVersion(context.Background(), taskID, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	i, err := content.Inspect(content.Kind(v.Kind), b)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range i.Nodes {
		if n.Text == text {
			return n
		}
	}
	t.Fatalf("missing node %q: %#v", text, i.Nodes)
	return content.Node{}
}

func TestOfficeMetricExactNumericAndTextCapture(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	source, err := svc.Generate(ctx, task.ID, "来源.xlsx", metricSourceSpec("12.3456"), "source")
	if err != nil {
		t.Fatal(err)
	}
	n := metricNode(t, svc, task.ID, source, "12.3456")
	if n.Kind != "cell:number" {
		t.Fatalf("numeric source: %#v", n)
	}
	digits := 2
	r := MetricCapture{SourceVersionID: source.ID, SourceNodeID: n.ID, SourceNodeDigest: n.Digest, Name: "季度收入", Unit: "万元", Currency: "CNY", Period: "2026Q3", RoundingDigits: &digits}
	m, err := svc.CaptureMetric(ctx, task.ID, r, "metric")
	if err != nil {
		t.Fatal(err)
	}
	if m.RawValue != "12.3456" || m.DisplayValue != "12.35" || m.RoundingPolicy != "half_away_from_zero" || m.SourceSHA256 != source.SHA256 {
		t.Fatalf("metric corrupted: %#v", m)
	}
	retry, err := svc.CaptureMetric(ctx, task.ID, r, "metric")
	if err != nil || retry.ID != m.ID {
		t.Fatalf("retry: %#v %v", retry, err)
	}
	r.SourceNodeDigest = strings.Repeat("0", 64)
	if _, err = svc.CaptureMetric(ctx, task.ID, r, "bad-digest"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("wrong node accepted: %v", err)
	}
	text := metricNode(t, svc, task.ID, source, "000012340001234567")
	r.SourceNodeID, r.SourceNodeDigest, r.Name, r.RoundingDigits = text.ID, text.Digest, "编码", nil
	m2, err := svc.CaptureMetric(ctx, task.ID, r, "identifier")
	if err != nil || m2.DisplayValue != "000012340001234567" || m2.RawValue != m2.DisplayValue || m2.ValueType != "text" {
		t.Fatalf("identifier: %#v %v", m2, err)
	}
	r.RoundingDigits = &digits
	if _, err = svc.CaptureMetric(ctx, task.ID, r, "bad-rounding"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("identifier rounding accepted: %v", err)
	}
	var sourceIndex content.Inspection
	if err = json.Unmarshal(source.Index, &sourceIndex); err != nil {
		t.Fatal(err)
	}
	var formula content.Node
	for _, node := range sourceIndex.Nodes {
		if node.Kind == "cell:formula" {
			formula = node
			break
		}
	}
	if formula.ID == "" {
		t.Fatal("formula node missing")
	}
	r.SourceNodeID, r.SourceNodeDigest, r.RoundingDigits = formula.ID, formula.Digest, nil
	if _, err = svc.CaptureMetric(ctx, task.ID, r, "bad-formula"); err == nil {
		t.Fatal("unrecalculated formula accepted")
	}
	all, err := store.ListOfficeMetrics(ctx, task.ID)
	if err != nil || len(all) != 2 {
		t.Fatalf("metric count: %#v %v", all, err)
	}
}

func TestOfficeMetricCrossFileApplyAndRestoredSourceStalesActualHead(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	source1, err := svc.Generate(ctx, task.ID, "来源.xlsx", metricSourceSpec("12.3456"), "source")
	if err != nil {
		t.Fatal(err)
	}
	newSourceBytes, err := content.Generate(metricSourceSpec("23.4567"))
	if err != nil {
		t.Fatal(err)
	}
	source2, err := svc.Import(ctx, task.ID, source1.ArtifactID, source1.Name, newSourceBytes, source1.ID, 1, "new-source")
	if err != nil {
		t.Fatal(err)
	}
	n := metricNode(t, svc, task.ID, source2, "23.4567")
	digits := 2
	m, err := svc.CaptureMetric(ctx, task.ID, MetricCapture{SourceVersionID: source2.ID, SourceNodeID: n.ID, SourceNodeDigest: n.Digest, Name: "收入", Unit: "万元", Currency: "CNY", Period: "2026Q3", RoundingDigits: &digits}, "metric")
	if err != nil {
		t.Fatal(err)
	}
	word := generatedWord(t, svc, task, "word")
	accepted, err := store.AcceptOfficeVersion(ctx, task.ID, word.ArtifactID, word.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	ppt, err := svc.Generate(ctx, task.ID, "汇报.pptx", content.Spec{SchemaVersion: 1, Kind: content.PPTX, Title: "收入汇报", Slides: []content.Slide{{Title: "收入", Bullets: []string{"初始状态"}}}}, "slides")
	if err != nil {
		t.Fatal(err)
	}
	var updated []domain.Version
	var wordRequest MetricApply
	for _, target := range []domain.Version{word, ppt} {
		node := metricNode(t, svc, task.ID, target, "初始状态")
		revision := int64(1)
		if target.ID == word.ID {
			revision = accepted.Revision
		}
		r := MetricApply{MetricID: m.ID, TargetVersionID: target.ID, TargetNodeID: node.ID, TargetNodeDigest: node.Digest, Template: "{{period}} 收入 {{value}}{{unit}}（{{currency}}）", ExpectedRevision: revision}
		if target.ID == word.ID {
			wordRequest = r
		}
		next, e := svc.ApplyMetric(ctx, task.ID, r, "apply-"+target.Kind)
		if e != nil {
			t.Fatal(e)
		}
		preview, e := svc.Preview(ctx, task.ID, next.ID)
		if e != nil || !strings.Contains(preview.Content, "2026Q3 收入 23.46万元（CNY）") {
			t.Fatalf("preview: %#v %v", preview, e)
		}
		updated = append(updated, next)
	}
	retry, err := svc.ApplyMetric(ctx, task.ID, wordRequest, "apply-docx")
	if err != nil || retry.ID != updated[0].ID {
		t.Fatalf("apply retry: %#v %v", retry, err)
	}
	edges, err := store.ListOfficeEvidenceEdges(ctx, task.ID)
	if err != nil || len(edges) != 2 {
		t.Fatalf("evidence: %#v %v", edges, err)
	}
	for _, e := range edges {
		var saved domain.Metric
		if err = json.Unmarshal(e.Metric, &saved); err != nil || e.SourceVersionID != source2.ID || saved.ID != m.ID || saved.RawValue != "23.4567" {
			t.Fatalf("unbound evidence: %#v %v", e, err)
		}
	}
	// Restoring source1 replaces source2, not source1. Both downstream files
	// rely on source2 and must become stale in the publication transaction.
	if _, err = svc.Restore(ctx, task.ID, source1.ID, 2, "restore-old-source"); err != nil {
		t.Fatal(err)
	}
	for _, v := range updated {
		actual, e := store.GetOfficeVersion(ctx, v.ID)
		if e != nil || actual.Quality != "stale" {
			t.Fatalf("restored source left current derivative valid: %#v %v", actual, e)
		}
	}
	heads, err := store.ListOfficeHeads(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range heads {
		if h.ArtifactID == word.ArtifactID && (h.AcceptedVersionID != word.ID || h.LatestVersionID != updated[0].ID) {
			t.Fatalf("acceptance moved: %#v", h)
		}
	}
	wordRequest.ExpectedRevision++
	before, _ := store.ListOfficeVersions(ctx, task.ID, word.ArtifactID)
	if _, err = svc.ApplyMetric(ctx, task.ID, wordRequest, "apply-old-source"); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale source allowed: %v", err)
	}
	after, _ := store.ListOfficeVersions(ctx, task.ID, word.ArtifactID)
	if len(before) != len(after) {
		t.Fatal("failed metric apply published half a version")
	}
}

func TestOfficeMetricScopeAndTemplateGuards(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	source := generatedWord(t, svc, task, "source")
	n := metricNode(t, svc, task.ID, source, "初始状态")
	m, err := svc.CaptureMetric(ctx, task.ID, MetricCapture{SourceVersionID: source.ID, SourceNodeID: n.ID, SourceNodeDigest: n.Digest, Name: "状态"}, "metric")
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateOfficeTask(ctx, domain.Task{SessionID: task.SessionID, Title: "另一任务"}, "other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ApplyMetric(ctx, other.ID, MetricApply{MetricID: m.ID}, "bad-scope"); !errors.Is(err, domain.ErrScope) {
		t.Fatalf("cross-task metric: %v", err)
	}
	for i, template := range []string{"no value", "{{value}} {{value}}", "{{value}} {{unknown}}"} {
		_, err = svc.ApplyMetric(ctx, task.ID, MetricApply{MetricID: m.ID, TargetVersionID: source.ID, Template: template}, "bad-template-"+string(rune('a'+i)))
		if err == nil {
			t.Fatalf("invalid template accepted %q", template)
		}
	}
}

func TestOfficeMetricProvenanceSurvivesOrdinaryEdits(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	source := generatedWord(t, svc, task, "source")
	sn := metricNode(t, svc, task.ID, source, "初始状态")
	m, err := svc.CaptureMetric(ctx, task.ID, MetricCapture{SourceVersionID: source.ID, SourceNodeID: sn.ID, SourceNodeDigest: sn.Digest, Name: "状态"}, "metric")
	if err != nil {
		t.Fatal(err)
	}
	target := generatedWord(t, svc, task, "target")
	tn := metricNode(t, svc, task.ID, target, "初始状态")
	applied, err := svc.ApplyMetric(ctx, task.ID, MetricApply{MetricID: m.ID, TargetVersionID: target.ID, TargetNodeID: tn.ID, TargetNodeDigest: tn.Digest, Template: "来源状态：{{value}}", ExpectedRevision: 1}, "apply")
	if err != nil {
		t.Fatal(err)
	}
	heading := metricNode(t, svc, task.ID, applied, "进度")
	edited, err := svc.Patch(ctx, task.ID, applied.ID, 2, content.PatchRequest{Kind: content.DOCX, BaseSHA256: applied.SHA256, Operations: []content.TextPatch{{NodeID: heading.ID, ExpectedDigest: heading.Digest, Text: "新标题"}}}, "edit-heading")
	if err != nil || edited.Quality == "stale" {
		t.Fatalf("unrelated edit invalidated metric: %#v %v", edited, err)
	}
	edges, err := store.ListOfficeEvidenceEdges(ctx, task.ID)
	if err != nil || len(edges) != 2 || edges[1].TargetVersionID != edited.ID {
		t.Fatalf("lost inherited source: %#v %v", edges, err)
	}
	n := metricNode(t, svc, task.ID, edited, "来源状态：初始状态")
	changed, err := svc.Patch(ctx, task.ID, edited.ID, 3, content.PatchRequest{Kind: content.DOCX, BaseSHA256: edited.SHA256, Operations: []content.TextPatch{{NodeID: n.ID, ExpectedDigest: n.Digest, Text: "改写后的值"}}}, "edit-metric")
	if err != nil || changed.Quality != "stale" {
		t.Fatalf("edited metric falsely valid: %#v %v", changed, err)
	}
	heading = metricNode(t, svc, task.ID, changed, "新标题")
	afterUnrelated, err := svc.Patch(ctx, task.ID, changed.ID, 4, content.PatchRequest{Kind: content.DOCX, BaseSHA256: changed.SHA256, Operations: []content.TextPatch{{NodeID: heading.ID, ExpectedDigest: heading.Digest, Text: "又一个标题"}}}, "edit-heading-after-wrong-metric")
	if err != nil || afterUnrelated.Quality != "stale" {
		t.Fatalf("unrelated edit washed away wrong metric: %#v %v", afterUnrelated, err)
	}
	if _, err = svc.Check(ctx, task.ID, afterUnrelated.ID, false); err != nil {
		t.Fatal(err)
	}
	checked, err := store.GetOfficeVersion(ctx, afterUnrelated.ID)
	if err != nil || checked.Quality != "stale" {
		t.Fatalf("recheck washed away wrong metric: %#v %v", checked, err)
	}
	n = metricNode(t, svc, task.ID, afterUnrelated, "改写后的值")
	refreshed, err := svc.ApplyMetric(ctx, task.ID, MetricApply{MetricID: m.ID, TargetVersionID: afterUnrelated.ID, TargetNodeID: n.ID, TargetNodeDigest: n.Digest, Template: "更新：{{value}}", ExpectedRevision: 5}, "refresh-metric")
	if err != nil || refreshed.Quality == "stale" {
		t.Fatalf("explicit metric could not replace old edge: %#v %v", refreshed, err)
	}
	if _, err = svc.Patch(ctx, task.ID, source.ID, 1, textPatchFor(t, source, "来源已更新"), "update-source"); err != nil {
		t.Fatal(err)
	}
	latest, err := store.GetOfficeVersion(ctx, refreshed.ID)
	if err != nil || latest.Quality != "stale" {
		t.Fatalf("source update skipped descendant: %#v %v", latest, err)
	}
}
