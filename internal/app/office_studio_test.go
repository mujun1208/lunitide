package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/config"
	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	projectdomain "github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/m9app"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/officeapp"
	content "github.com/lunitide/lunitide/internal/officestudio"
	"github.com/lunitide/lunitide/internal/org"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

// Hosted Windows runners often spell one TempDir two ways: the 8.3 alias
// (C:\Users\RUNNER~1\...) and the long name. filepath.Clean does not fold them.
func sameExistingPath(t *testing.T, got, want string) bool {
	t.Helper()
	if filepath.Clean(got) == filepath.Clean(want) {
		return true
	}
	ga, err := os.Stat(got)
	if err != nil {
		return false
	}
	wa, err := os.Stat(want)
	return err == nil && os.SameFile(ga, wa)
}

func officeEngineFixture(t *testing.T) (*Engine, *storage.Store) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "office.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	messages, err := messageapp.New(store, store, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithMessages(providerapp.New(store, store), projectapp.New(store, store), sessionapp.New(store, store), messages, "office-test", nil)
	s, err := officeapp.New(store, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.SetOfficeStudio(s)
	runtime, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	e.SetToolRuntime(runtime) // Reverse wiring order must work as well.
	e.attachmentService = attachmentapp.NewService(store, attachmentapp.NewDirFileStorage(t.TempDir()))
	return e, store
}

func officeCall(t *testing.T, e *Engine, method, key string, payload any) bridge.Response {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	r := validRequest(method, string(b))
	r.IdempotencyKey = key
	return e.Handle(context.Background(), r)
}

func officeCreatedTask(t *testing.T, e *Engine, key string) domain.Task {
	t.Helper()
	r := officeCall(t, e, "office.task.create", key, map[string]any{"title": "季度交付", "goal": "生成销售汇报"})
	if !r.OK {
		t.Fatalf("create: %+v", r.Error)
	}
	var v struct {
		Task struct{ ID, SessionID, ProjectID string }
	}
	if err := decodeResponsePayload(r.Payload, &v); err != nil {
		t.Fatal(err)
	}
	if !validCanonicalULID(v.Task.ProjectID) {
		t.Fatalf("missing existing session parent: %+v", v)
	}
	task, err := e.officeStudio.Store.GetOfficeTask(context.Background(), v.Task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestOfficeTaskCreatesItsHiddenSessionInsideTheBoundOrganization(t *testing.T) {
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	admin := m9app.NewOrgAdminService(
		org.NewService(org.NewGate(store.OrgStorage()), nil),
		m9app.NewFileBindingStore(filepath.Join(t.TempDir(), "binding.json")),
	)
	e.SetM9OrgAdminService(admin)
	e.SetDataScopeStore(store)
	if err := m9app.EnsureDefaultOrgBinding(ctx, admin); err != nil {
		t.Fatal(err)
	}
	summary, err := admin.Summary(ctx)
	if err != nil || summary.BoundOrgID == "" {
		t.Fatalf("organization binding: %+v %v", summary, err)
	}
	hiddenID, err := e.ensurePersonalChatProject(ctx)
	if err != nil {
		t.Fatalf("create hidden project: %v", err)
	}
	hidden, err := e.projects.Get(ctx, hiddenID)
	if err != nil || hidden.OrgID != summary.BoundOrgID {
		t.Fatalf("hidden project scope: %+v %v", hidden, err)
	}
	createdResponse := officeCall(t, e, "office.task.create", "scoped-office", map[string]any{"title": "季度交付", "goal": "生成销售汇报"})
	if !createdResponse.OK {
		sessions, _ := e.sessions.List(ctx, session.Filter{ProjectID: hiddenID})
		tasks, _ := store.ListOfficeTasks(domain.WithScope(ctx, summary.BoundOrgID), "", 100)
		t.Fatalf("scoped office create: %+v; sessions=%+v tasks=%+v", createdResponse.Error, sessions, tasks)
	}
	var created struct {
		Task domain.Task `json:"task"`
	}
	if err := decodeResponsePayload(createdResponse.Payload, &created); err != nil {
		t.Fatal(err)
	}
	task := created.Task
	bound, err := e.sessions.(sessionProjectResolver).Get(ctx, task.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := e.projects.Get(ctx, bound.ProjectID)
	if err != nil || parent.OrgID != summary.BoundOrgID {
		t.Fatalf("office parent scope: %+v %v", parent, err)
	}
	personal, err := e.projects.List(ctx, projectdomain.Filter{})
	if err != nil || len(personal) != 0 {
		t.Fatalf("office creation leaked a personal hidden project: %+v %v", personal, err)
	}
}

func TestOfficeGenerateV2MetricsThroughTool(t *testing.T) {
	e, store := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "v2-metrics")
	args, _ := json.Marshal(map[string]any{
		"taskId": task.ID,
		"name":   "经营指标.pptx",
		"spec": content.Spec{
			SchemaVersion: 2, Kind: content.PPTX, Title: "经营指标",
			Slides: []content.Slide{{
				Title:   "指标",
				Layout:  "metrics",
				Metrics: []content.MetricBlock{{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"}},
			}},
		},
	})
	result, err := e.executeUserTool(context.Background(), executionModeFullAccess, task.SessionID, "office.generate", args)
	if err != nil || result.Artifact == nil {
		t.Fatalf("v2 generate: %+v %v", result, err)
	}
	versions, err := store.ListOfficeVersions(context.Background(), task.ID, "")
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions: %v %v", versions, err)
	}
	preview, err := e.officeStudio.Preview(context.Background(), task.ID, versions[0].ID)
	if err != nil || !strings.Contains(preview.Content, "1280") {
		t.Fatalf("v2 preview missing fact: %#v %v", preview, err)
	}
}

func TestOfficeDeliverCaptureByFactID(t *testing.T) {
	e, _ := officeEngineFixture(t)
	ctx := context.Background()
	task := officeCreatedTask(t, e, "fact-capture")
	facts := []content.Fact{{FactID: "orders", Value: "1280", Unit: "单", Locked: true, Locator: "订单数"}}
	wb, err := content.PlanWorkbook("ops-ledger", "经营簿", facts)
	if err != nil {
		t.Fatal(err)
	}
	x, err := e.officeStudio.Generate(ctx, task.ID, "经营簿.xlsx", wb, "wb")
	if err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{
		"taskId": task.ID, "action": "capture", "versionId": x.ID,
		"factId": "orders", "value": "1280", "unit": "单", "name": "订单数",
	})
	_, _, output, err := e.executeOfficeTool(ctx, task.SessionID, "office.deliver", args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "1280") {
		t.Fatalf("fact capture output: %s", output)
	}
	listed, err := e.officeStudio.ListMetrics(ctx, task.ID)
	if err != nil || len(listed) != 1 || listed[0].RawValue != "1280" {
		t.Fatalf("captured: %#v %v", listed, err)
	}
}

func TestOfficeEngineBridgeAndRealRuntimeDelivery(t *testing.T) {
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	task := officeCreatedTask(t, e, "new-office")
	if replay := officeCreatedTask(t, e, "new-office"); replay.ID != task.ID || replay.SessionID != task.SessionID {
		t.Fatal("duplicate task/session on replay")
	}
	args, _ := json.Marshal(map[string]any{"taskId": task.ID, "name": "季度汇报.docx", "spec": content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "季度汇报", Blocks: []content.Block{{Type: "heading", Text: "季度汇报"}, {Type: "paragraph", Text: "收入 123.45 万元"}}}})
	result, err := e.executeUserTool(ctx, executionModeFullAccess, task.SessionID, "office.generate", args)
	if err != nil || result.Artifact == nil {
		t.Fatalf("shared runtime: %+v %v", result, err)
	}
	data, err := e.tools.ReadWorkspaceFile(task.SessionID, result.Artifact.Path, 8<<20)
	if err != nil || len(data) == 0 {
		t.Fatalf("delivery copy: %v", err)
	}
	versions, err := store.ListOfficeVersions(ctx, task.ID, "")
	if err != nil || len(versions) != 1 {
		t.Fatalf("real versions: %v %v", versions, err)
	}
	v := versions[0]
	preview, err := e.officeStudio.Preview(ctx, task.ID, v.ID)
	if err != nil {
		t.Fatal(err)
	}
	var node content.Node
	for _, n := range preview.Nodes {
		if n.Text == "收入 123.45 万元" {
			node = n
		}
	}
	if !node.Editable {
		t.Fatalf("editable node not indexed: %+v", preview)
	}
	request := map[string]any{"taskId": task.ID, "artifactId": v.ArtifactID, "baseVersionId": v.ID, "expectedRevision": 1, "nodeId": node.ID, "nodeDigest": node.Digest, "text": "收入 234.56 万元"}
	patched := officeCall(t, e, "office.artifact.patch", "patch-v1", request)
	if !patched.OK {
		t.Fatalf("patch bridge: %+v", patched.Error)
	}
	versions, _ = store.ListOfficeVersions(ctx, task.ID, v.ArtifactID)
	if len(versions) != 2 {
		t.Fatalf("version count %d", len(versions))
	}
	diff := officeCall(t, e, "office.artifact.diff", "", map[string]any{"taskId": task.ID, "baseVersionId": v.ID, "versionId": versions[0].ID})
	if !diff.OK {
		t.Fatalf("version diff bridge: %+v", diff.Error)
	}
	var comparison officeapp.DiffResult
	if err := decodeResponsePayload(diff.Payload, &comparison); err != nil {
		t.Fatal(err)
	}
	if comparison.Summary.ModifiedNodes != 1 || comparison.Summary.UnchangedParts == 0 || comparison.BytesIdentical {
		t.Fatalf("diff did not compare actual files: %+v", comparison.Summary)
	}
	if repeat := officeCall(t, e, "office.artifact.patch", "patch-v1", request); !repeat.OK {
		t.Fatalf("replay must return published version: %+v", repeat.Error)
	}
	if stale := officeCall(t, e, "office.artifact.patch", "competing-edit", request); stale.OK || stale.Error.Code != "OFFICE_VERSION_CONFLICT" {
		t.Fatalf("stale head: %+v", stale)
	}
	heads, _ := store.ListOfficeHeads(ctx, task.ID)
	accepted := officeCall(t, e, "office.artifact.accept", "accept-v1", map[string]any{"taskId": task.ID, "artifactId": v.ArtifactID, "versionId": v.ID, "expectedRevision": heads[0].Revision})
	if !accepted.OK {
		t.Fatalf("accept: %+v", accepted.Error)
	}
	old, _, _ := e.officeStudio.ReadVersion(ctx, task.ID, v.ID)
	if old.Quality == "passed" {
		t.Fatal("acceptance invented target validation")
	}
	refused := officeCall(t, e, "office.artifact.export", "export-final", map[string]any{"taskId": task.ID, "versionId": v.ID, "draft": false})
	if refused.OK {
		t.Fatal("unverified final export accepted")
	}
	exported := officeCall(t, e, "office.artifact.export", "export-draft", map[string]any{"taskId": task.ID, "versionId": v.ID, "draft": true})
	if !exported.OK {
		t.Fatalf("export: %+v", exported.Error)
	}
	var path struct{ Path string }
	_ = decodeResponsePayload(exported.Payload, &path)
	if _, err = os.Stat(path.Path); err != nil {
		t.Fatal(err)
	}
	opened := openArtifactTarget
	t.Cleanup(func() { openArtifactTarget = opened })
	openArtifactTarget = func(p string, file, reveal bool) error {
		if !file || !sameExistingPath(t, p, path.Path) {
			t.Fatalf("wrong open target got=%s want=%s file=%v", p, path.Path, file)
		}
		return nil
	}
	if got := officeCall(t, e, "office.artifact.open", "open", map[string]any{"taskId": task.ID, "path": path.Path}); !got.OK {
		t.Fatalf("open: %+v", got.Error)
	}
	if got := officeCall(t, e, "office.artifact.open", "open-escape", map[string]any{"taskId": task.ID, "path": filepath.Join(t.TempDir(), "other.docx")}); got.OK {
		t.Fatal("open escaped export root")
	}
}

func TestOfficeEngineImportReadableAttachmentAndTaskIsolation(t *testing.T) {
	e, _ := officeEngineFixture(t)
	ctx := context.Background()
	task := officeCreatedTask(t, e, "import-a")
	sess, err := e.sessions.(sessionProjectResolver).Get(ctx, task.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := content.Generate(content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "原始报告", Blocks: []content.Block{{Type: "paragraph", Text: "附件中可检索的销售金额"}}})
	if err != nil {
		t.Fatal(err)
	}
	a, err := e.attachmentService.IngestFile(ctx, attachmentapp.IngestFileRequest{ProjectID: sess.ProjectID, SessionID: task.SessionID, OriginalName: "原始报告.docx", MIME: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Content: b})
	if err != nil || !strings.Contains(a.ParsedText, "销售金额") {
		t.Fatalf("shared chat must read imported Office content: %+v %v", a, err)
	}
	if r := officeCall(t, e, "office.artifact.import", "import-file", map[string]any{"taskId": task.ID, "attachmentId": a.ID}); !r.OK {
		t.Fatalf("import: %+v", r.Error)
	}
	other := officeCreatedTask(t, e, "import-b")
	if r := officeCall(t, e, "office.artifact.import", "wrong-session", map[string]any{"taskId": other.ID, "attachmentId": a.ID}); r.OK {
		t.Fatal("attachment leaked to different task session")
	}
	vs, _ := e.officeStudio.Store.ListOfficeVersions(ctx, task.ID, "")
	if r := officeCall(t, e, "office.artifact.preview", "", map[string]any{"taskId": other.ID, "versionId": vs[0].ID}); r.OK {
		t.Fatal("version leaked across task")
	}
}

func TestOfficeMetricCaptureBridgeAcceptsFactID(t *testing.T) {
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	task := officeCreatedTask(t, e, "bridge-fact")
	facts := []content.Fact{{FactID: "orders", Value: "1280", Unit: "单", Locked: true, Locator: "订单数"}}
	wb, err := content.PlanWorkbook("ops-ledger", "经营簿", facts)
	if err != nil {
		t.Fatal(err)
	}
	x, err := e.officeStudio.Generate(ctx, task.ID, "经营簿.xlsx", wb, "wb")
	if err != nil {
		t.Fatal(err)
	}
	r := officeCall(t, e, "office.metric.capture", "bridge-fact", map[string]any{
		"taskId": task.ID, "versionId": x.ID, "nodeId": "unused-node",
		"nodeDigest": strings.Repeat("0", 64), "name": "订单数",
		"factId": "orders", "value": "1280", "unit": "单",
	})
	if !r.OK {
		t.Fatal(r.Error)
	}
	listed, err := store.ListOfficeMetrics(ctx, task.ID)
	if err != nil || len(listed) != 1 || listed[0].RawValue != "1280" {
		t.Fatalf("bridge fact capture: %#v %v", listed, err)
	}
}

func TestOfficeEngineMetricsRangeBundlesAndRollbackReads(t *testing.T) {
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	task := officeCreatedTask(t, e, "metrics")
	x, err := e.officeStudio.Generate(ctx, task.ID, "源数据.xlsx", content.Spec{SchemaVersion: 1, Kind: content.XLSX, Title: "源数据", Sheets: []content.Sheet{{Name: "销售", Rows: [][]content.Cell{{{Type: "text", Value: "001234567890123456"}, {Type: "number", Value: "123.456"}}}}}}, "source")
	if err != nil {
		t.Fatal(err)
	}
	w, err := e.officeStudio.Generate(ctx, task.ID, "报告.docx", content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "报告", Document: &content.DocumentOptions{PageNumbers: true}, Blocks: []content.Block{{Type: "toc"}, {Type: "heading3", Text: "统计"}, {Type: "paragraph", Text: "收入待补"}}}, "word")
	if err != nil {
		t.Fatal(err)
	}
	px, err := e.officeStudio.Preview(ctx, task.ID, x.ID)
	if err != nil {
		t.Fatal(err)
	}
	pw, err := e.officeStudio.Preview(ctx, task.ID, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	var source, target content.Node
	for _, n := range px.Nodes {
		if n.Text == "123.456" {
			source = n
		}
	}
	for _, n := range pw.Nodes {
		if n.Text == "收入待补" {
			target = n
		}
	}
	if source.ID == "" || target.ID == "" {
		t.Fatal("missing real source/target")
	}
	for method, payload := range map[string]any{
		"office.metric.capture": map[string]any{"taskId": task.ID, "versionId": x.ID, "nodeId": source.ID, "nodeDigest": source.Digest, "name": "季度收入", "unit": "万元", "currency": "CNY", "period": "2026Q3", "roundingDigits": 2},
	} {
		if r := officeCall(t, e, method, "capture", payload); !r.OK {
			t.Fatalf("%s: %+v", method, r.Error)
		}
	}
	listed := officeCall(t, e, "office.metric.list", "", map[string]any{"taskId": task.ID})
	if !listed.OK {
		t.Fatal(listed.Error)
	}
	var ms struct{ Items []domain.Metric }
	_ = decodeResponsePayload(listed.Payload, &ms)
	if len(ms.Items) != 1 || ms.Items[0].RawValue != "123.456" || ms.Items[0].DisplayValue != "123.46" {
		t.Fatalf("metric: %+v", ms)
	}
	if r := officeCall(t, e, "office.metric.apply", "apply", map[string]any{"taskId": task.ID, "metricId": ms.Items[0].ID, "targetVersionId": w.ID, "targetNodeId": target.ID, "targetNodeDigest": target.Digest, "template": "季度收入 {{value}} {{unit}}", "expectedRevision": 1}); !r.OK {
		t.Fatalf("apply: %+v", r.Error)
	}
	versions, _ := store.ListOfficeVersions(ctx, task.ID, w.ArtifactID)
	var derived domain.Version
	for _, v := range versions {
		if v.ID != w.ID {
			derived = v
		}
	}
	preview, _ := e.officeStudio.Preview(ctx, task.ID, derived.ID)
	if !strings.Contains(preview.Content, "123.46 万元") {
		t.Fatal("metric did not enter actual report")
	}
	if r := officeCall(t, e, "office.artifact.accept", "accept-derived", map[string]any{"taskId": task.ID, "artifactId": w.ArtifactID, "versionId": derived.ID, "expectedRevision": 2}); !r.OK {
		t.Fatal(r.Error)
	}
	if len(px.Parts) == 0 {
		t.Fatal("worksheet digest missing")
	}
	if r := officeCall(t, e, "office.artifact.patchRange", "range-source", map[string]any{"taskId": task.ID, "artifactId": x.ArtifactID, "baseVersionId": x.ID, "expectedRevision": 1, "ranges": []content.RangePatch{{Part: source.Part, Range: "B1:B1", ExpectedDigest: px.Parts[0].SHA256, Rows: [][]content.Cell{{{Type: "number", Value: "456.789"}}}}}}); !r.OK {
		t.Fatalf("range: %+v", r.Error)
	}
	derived, err = store.GetOfficeVersion(ctx, derived.ID)
	if err != nil || derived.Quality != "stale" {
		t.Fatalf("upstream dependency not stale: %+v %v", derived, err)
	}
	heads, _ := store.ListOfficeHeads(ctx, task.ID)
	for _, h := range heads {
		if h.ArtifactID == w.ArtifactID && h.AcceptedVersionID != derived.ID {
			t.Fatal("changed accepted report")
		}
	}
	bundleResponse := officeCall(t, e, "office.bundle.create", "bundle", map[string]any{"taskId": task.ID, "title": "固定交付", "versionIds": []string{x.ID, derived.ID}})
	if !bundleResponse.OK {
		t.Fatalf("bundle: %+v", bundleResponse.Error)
	}
	var bundle domain.Bundle
	_ = decodeResponsePayload(bundleResponse.Payload, &bundle)
	if len(bundle.Files) != 2 || bundle.Files[0].SHA256 != x.SHA256 {
		t.Fatal("bundle changed selected version")
	}
	for i := 0; i < 2; i++ {
		r := officeCall(t, e, "office.bundle.export", "bundle-export", map[string]any{"taskId": task.ID, "bundleId": bundle.ID})
		if !r.OK {
			t.Fatalf("export %d: %+v", i, r.Error)
		}
		var out officeapp.BundleExport
		_ = decodeResponsePayload(r.Payload, &out)
		if !out.Complete || len(out.Files) != 2 {
			t.Fatalf("incomplete receipt %+v", out)
		}
		if _, err = os.Stat(out.ManifestPath); err != nil {
			t.Fatal(err)
		}
	}
	f := config.DefaultOfficeFlags()
	f.Studio = false
	e.SetOfficeFlags(f)
	if r := officeCall(t, e, "office.task.create", "closed", map[string]any{"title": "不应创建"}); r.OK || r.Error.Code != "FEATURE_DISABLED" {
		t.Fatal("rollback flag ineffective")
	}
	if r := officeCall(t, e, "office.artifact.preview", "", map[string]any{"taskId": task.ID, "versionId": x.ID}); !r.OK {
		t.Fatal("rollback blocked existing reads")
	}
	if err = store.DeleteSession(ctx, task.SessionID); err != nil {
		t.Fatal(err)
	}
	if r := officeCall(t, e, "office.artifact.export", "retained", map[string]any{"taskId": task.ID, "versionId": derived.ID, "draft": true}); !r.OK {
		t.Fatalf("retained file lost after session deletion: %+v", r.Error)
	}
}

func TestOfficeFailureMapsBusySeparatelyFromVersionConflict(t *testing.T) {
	r := bridge.Request{ID: "req-busy", Method: "office.task.sync"}
	busy := officeFailure(r, domain.ErrBusy)
	if busy.OK || busy.Error == nil || busy.Error.Code != "OFFICE_BUSY" || !busy.Error.Retryable {
		t.Fatalf("busy: %+v", busy)
	}
	if busy.Error.Message != "办公任务正在同步或写入，请稍后重试" {
		t.Fatalf("busy message leaked raw sentinel: %q", busy.Error.Message)
	}
	conflict := officeFailure(r, fmt.Errorf("同步文件 汇报.pptx：%w", domain.ErrConflict))
	if conflict.OK || conflict.Error == nil || conflict.Error.Code != "OFFICE_VERSION_CONFLICT" || conflict.Error.Retryable {
		t.Fatalf("conflict: %+v", conflict)
	}
	if strings.Contains(conflict.Error.Message, "OFFICE_") || !officeUserMessageHasHan(conflict.Error.Message) {
		t.Fatalf("conflict message leaked sentinel: %q", conflict.Error.Message)
	}
}

func TestOfficeFailureUserMessagesAreChinese(t *testing.T) {
	r := bridge.Request{ID: "req-office-zh", Method: "office.task.get"}
	cases := []struct {
		name string
		err  error
		code string
	}{
		{"not_found", domain.ErrNotFound, "OFFICE_NOT_FOUND"},
		{"quota", domain.ErrStorageQuota, "OFFICE_STORAGE_QUOTA"},
		{"lease", domain.ErrBlobLease, "OFFICE_BLOB_LEASE_EXPIRED"},
		{"scope", domain.ErrScope, "OFFICE_SCOPE_MISMATCH"},
		{"invalid", domain.ErrInvalid, "BRIDGE_SCHEMA_INVALID"},
		{"cancelled", context.Canceled, "OFFICE_CANCELLED"},
		{"timeout", context.DeadlineExceeded, "OFFICE_TIMEOUT"},
	}
	for _, tc := range cases {
		got := officeFailure(r, tc.err)
		if got.OK || got.Error == nil || got.Error.Code != tc.code {
			t.Fatalf("%s: %+v", tc.name, got)
		}
		if strings.Contains(got.Error.Message, "OFFICE_") || !officeUserMessageHasHan(got.Error.Message) {
			t.Fatalf("%s leaked %q", tc.name, got.Error.Message)
		}
	}
}

func TestOfficeFailureKeepsChineseOfficeAppMessage(t *testing.T) {
	r := bridge.Request{ID: "req-keep", Method: "office.task.sync"}
	got := officeFailure(r, errors.New("办公受管目录已改变，停止文件写入与清理"))
	if got.Error == nil || got.Error.Message != "办公受管目录已改变，停止文件写入与清理" {
		t.Fatalf("lost officeapp Chinese: %+v", got)
	}
}

func TestOfficeFailureEnglishFallbackIsChinese(t *testing.T) {
	r := bridge.Request{ID: "req-en", Method: "office.task.sync"}
	got := officeFailure(r, errors.New("office store does not support durable recovery"))
	if got.Error == nil || strings.Contains(got.Error.Message, "office store") || !officeUserMessageHasHan(got.Error.Message) {
		t.Fatalf("english leaked: %+v", got)
	}
}

func officeUserMessageHasHan(s string) bool {
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}

func TestOfficeTaskSyncBusyIsRetryableNotVersionConflict(t *testing.T) {
	e, _ := officeEngineFixture(t)
	ctx := context.Background()
	a := officeCreatedTask(t, e, "sync-busy-bridge")
	m := officeArchiveMessageForTest(t, e, a.SessionID, "sync-busy-message")
	officeArchiveFileForTest(t, e, a.SessionID, "sync-busy.docx", "桥接同步碰到写入锁")
	officeArchiveCardForTest(t, e, a.SessionID, m, "sync-busy.docx", a.ID)
	started, release, ended := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		ended <- e.officeStudio.Execute(ctx, a.ID, "running", func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("hold not started")
	}
	got := officeCall(t, e, "office.task.sync", "sync-busy", map[string]any{"taskId": a.ID})
	if got.OK || got.Error == nil || got.Error.Code != "OFFICE_BUSY" || !got.Error.Retryable {
		t.Fatalf("sync overlap: %+v", got)
	}
	if errors.Is(fmt.Errorf("%s", got.Error.Code), domain.ErrConflict) || got.Error.Code == "OFFICE_VERSION_CONFLICT" {
		t.Fatal("sync overlap reported as version conflict")
	}
	close(release)
	if err := <-ended; err != nil {
		t.Fatal(err)
	}
}

func TestOfficeEngineToolCallPublicationIsIdempotent(t *testing.T) {
	e, store := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "tool-replay")
	ctx := toolruntime.WithExecutionKey(context.Background(), task.SessionID, "model-call-1")
	args, _ := json.Marshal(map[string]any{"name": "stable.docx", "spec": content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "Stable", Blocks: []content.Block{{Type: "paragraph", Text: "exactly one file"}}}})
	for i := 0; i < 2; i++ {
		if _, err := e.executeUserTool(ctx, executionModeFullAccess, task.SessionID, "office.generate", args); err != nil {
			t.Fatal(err)
		}
	}
	versions, err := store.ListOfficeVersions(ctx, task.ID, "")
	if err != nil || len(versions) != 1 {
		t.Fatalf("duplicate tool publication: %d %v", len(versions), err)
	}
}

func TestOfficeRendererProbeListsOptionalTools(t *testing.T) {
	t.Setenv("LUNITIDE_PDFA_VALIDATOR", "")
	t.Setenv("LUNITIDE_OFFICE_VISION", "")
	t.Setenv("LUNITIDE_TYPST", "")
	t.Setenv("LUNITIDE_PRESENTON", "")
	t.Setenv("LUNITIDE_PPTXGENJS", "")
	e, _ := officeEngineFixture(t)
	r := officeCall(t, e, "office.renderer.probe", "probe-optional", map[string]any{})
	if !r.OK {
		t.Fatalf("probe: %+v", r.Error)
	}
	var out struct {
		Components []struct{ ID, Label, Status, Detail string }
	}
	if err := decodeResponsePayload(r.Payload, &out); err != nil {
		t.Fatal(err)
	}
	seen := map[string]struct{ Status, Detail string }{}
	for _, c := range out.Components {
		seen[c.ID] = struct{ Status, Detail string }{c.Status, c.Detail}
	}
	for _, id := range []string{"go", "desktop-office", "libreoffice", "pdfa", "vision", "typst", "presenton", "pptxgenjs"} {
		if _, ok := seen[id]; !ok {
			t.Fatalf("missing component %s: %#v", id, out.Components)
		}
	}
	if seen["pdfa"].Status != "unavailable" || !strings.Contains(seen["pdfa"].Detail, "草稿") {
		t.Fatalf("pdfa=%#v", seen["pdfa"])
	}
	if seen["presenton"].Status != "unavailable" || !strings.Contains(seen["presenton"].Detail, "未进生产主链") {
		t.Fatalf("presenton=%#v", seen["presenton"])
	}
	if !strings.Contains(seen["desktop-office"].Detail, "不等于") {
		t.Fatalf("desktop=%#v", seen["desktop-office"])
	}
	if !strings.Contains(seen["typst"].Detail, "gofpdf") {
		t.Fatalf("typst=%#v", seen["typst"])
	}
}

func TestOfficeFormalAcceptRequiresPassedQuality(t *testing.T) {
	e, store := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "formal-gate")
	ctx := context.Background()
	data, err := content.Generate(content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "草稿", Blocks: []content.Block{{Type: "paragraph", Text: "正文"}}})
	if err != nil {
		t.Fatal(err)
	}
	v, err := e.officeStudio.Import(ctx, task.ID, "", "draft.docx", data, "", 0, "formal-import")
	if err != nil {
		t.Fatal(err)
	}
	if v.Quality == "passed" {
		t.Fatal("imported docx must not start passed")
	}
	formal := officeCall(t, e, "office.artifact.accept", "accept-formal", map[string]any{
		"taskId": task.ID, "artifactId": v.ArtifactID, "versionId": v.ID, "expectedRevision": 1, "formal": true,
	})
	if formal.OK || formal.Error.Code != "OFFICE_DRAFT_REQUIRED" {
		t.Fatalf("formal accept of draft: %+v", formal)
	}
	draft := officeCall(t, e, "office.artifact.accept", "accept-draft", map[string]any{
		"taskId": task.ID, "artifactId": v.ArtifactID, "versionId": v.ID, "expectedRevision": 1,
	})
	if !draft.OK {
		t.Fatalf("draft accept: %+v", draft.Error)
	}
	heads, err := store.ListOfficeHeads(ctx, task.ID)
	if err != nil || len(heads) == 0 || heads[0].AcceptedVersionID != v.ID {
		t.Fatalf("accepted pointer: %#v %v", heads, err)
	}
}

func TestOfficeGenerateXLSXFallsIntoCurrentTask(t *testing.T) {
	e, store := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "xlsx-generate")
	ctx := toolruntime.WithExecutionKey(context.Background(), task.SessionID, "xlsx-gen-1")
	args, _ := json.Marshal(map[string]any{
		"name": "stable.xlsx",
		"spec": content.Spec{SchemaVersion: 2, Kind: content.XLSX, Title: "经营簿", Sheets: []content.Sheet{
			{Name: "原始数据", Rows: [][]content.Cell{{{Type: "text", Value: "订单"}, {Type: "number", Value: "1280"}}}},
		}},
	})
	if _, err := e.executeUserTool(ctx, executionModeFullAccess, task.SessionID, "office.generate", args); err != nil {
		t.Fatal(err)
	}
	versions, err := store.ListOfficeVersions(ctx, task.ID, "")
	if err != nil || len(versions) != 1 || versions[0].Kind != "xlsx" {
		t.Fatalf("xlsx generate versions: %#v %v", versions, err)
	}
}

func TestOfficeGeneratePDFFallsIntoCurrentTask(t *testing.T) {
	e, store := officeEngineFixture(t)
	task := officeCreatedTask(t, e, "pdf-generate")
	ctx := toolruntime.WithExecutionKey(context.Background(), task.SessionID, "pdf-gen-1")
	args, _ := json.Marshal(map[string]any{
		"name": "stable.pdf",
		"spec": content.Spec{SchemaVersion: 2, Kind: content.PDF, Title: "月报", Body: "订单 1280单"},
	})
	if _, err := e.executeUserTool(ctx, executionModeFullAccess, task.SessionID, "office.generate", args); err != nil {
		t.Fatal(err)
	}
	versions, err := store.ListOfficeVersions(ctx, task.ID, "")
	if err != nil || len(versions) != 1 || versions[0].Kind != "pdf" {
		t.Fatalf("pdf generate versions: %#v %v", versions, err)
	}
}

func TestOfficeCacheRefreshWithoutRendererFailsInChinese(t *testing.T) {
	e, store := officeEngineFixture(t)
	flags := config.DefaultOfficeFlags()
	flags.Render = false
	e.SetOfficeFlags(flags)
	task := officeCreatedTask(t, e, "refresh-flag-off")
	ctx := toolruntime.WithExecutionKey(context.Background(), task.SessionID, "refresh-docx")
	args, _ := json.Marshal(map[string]any{
		"name": "目录.docx",
		"spec": content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "目录", Blocks: []content.Block{{Type: "paragraph", Text: "正文"}}},
	})
	if _, err := e.executeUserTool(ctx, executionModeFullAccess, task.SessionID, "office.generate", args); err != nil {
		t.Fatal(err)
	}
	versions, err := store.ListOfficeVersions(ctx, task.ID, "")
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions: %#v %v", versions, err)
	}
	refreshArgs, _ := json.Marshal(map[string]any{"taskId": task.ID, "versionId": versions[0].ID, "expectedRevision": 1})
	_, err = e.executeUserTool(toolruntime.WithExecutionKey(context.Background(), task.SessionID, "refresh-1"), executionModeFullAccess, task.SessionID, "office.cache.refresh", refreshArgs)
	if err == nil || !strings.Contains(err.Error(), "LibreOffice") || !strings.Contains(err.Error(), "不能刷新") {
		t.Fatalf("cache.refresh without renderer: %v", err)
	}
	bridgeRefresh := officeCall(t, e, "office.artifact.refresh", "refresh-bridge", map[string]any{
		"taskId": task.ID, "artifactId": versions[0].ArtifactID, "versionId": versions[0].ID, "expectedRevision": 1,
	})
	if bridgeRefresh.OK || bridgeRefresh.Error.Code != "FEATURE_DISABLED" || !strings.Contains(bridgeRefresh.Error.Message, "本机排版更新已关闭") {
		t.Fatalf("artifact.refresh flag off: %+v", bridgeRefresh)
	}
}

func TestOfficeArtifactRefreshMissingSofficeFailsInChinese(t *testing.T) {
	e, store := officeEngineFixture(t)
	e.officeStudio.Renderer.Executable = filepath.Join(t.TempDir(), "missing-soffice.exe")
	task := officeCreatedTask(t, e, "refresh-missing")
	ctx := toolruntime.WithExecutionKey(context.Background(), task.SessionID, "refresh-word")
	args, _ := json.Marshal(map[string]any{
		"name": "目录.docx",
		"spec": content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "目录", Blocks: []content.Block{{Type: "toc"}, {Type: "paragraph", Text: "正文"}}},
	})
	if _, err := e.executeUserTool(ctx, executionModeFullAccess, task.SessionID, "office.generate", args); err != nil {
		t.Fatal(err)
	}
	versions, err := store.ListOfficeVersions(ctx, task.ID, "")
	if err != nil || len(versions) != 1 {
		t.Fatalf("versions: %#v %v", versions, err)
	}
	got := officeCall(t, e, "office.artifact.refresh", "refresh-missing", map[string]any{
		"taskId": task.ID, "artifactId": versions[0].ArtifactID, "versionId": versions[0].ID, "expectedRevision": 1,
	})
	if got.OK || !strings.Contains(got.Error.Message, "LibreOffice") {
		t.Fatalf("missing soffice refresh: %+v", got)
	}
}

func TestOfficeClosedLoopProtocol(t *testing.T) {
	t.Setenv("LUNITIDE_TYPST", "")
	t.Setenv("LUNITIDE_PRESENTON", "")
	t.Setenv("LUNITIDE_PDFA_VALIDATOR", "")
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	task := officeCreatedTask(t, e, "closed-loop-s1")
	specs := []struct {
		kind content.Kind
		name string
		spec content.Spec
	}{
		{content.PPTX, "闭环.pptx", content.Spec{SchemaVersion: 2, Kind: content.PPTX, Title: "闭环PPT", Slides: []content.Slide{{Title: "封面", Layout: "section", Bullets: []string{"订单 1280单"}}}}},
		{content.DOCX, "闭环.docx", content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "闭环Word", Blocks: []content.Block{{Type: "paragraph", Text: "订单 1280单"}}}},
		{content.XLSX, "闭环.xlsx", content.Spec{SchemaVersion: 2, Kind: content.XLSX, Title: "闭环Excel", Sheets: []content.Sheet{{Name: "原始数据", Rows: [][]content.Cell{{{Type: "text", Value: "订单"}, {Type: "number", Value: "1280"}}}}}}},
		{content.PDF, "闭环.pdf", content.Spec{SchemaVersion: 2, Kind: content.PDF, Title: "闭环PDF", Body: "订单 1280单"}},
	}
	for i, fx := range specs {
		ctx := toolruntime.WithExecutionKey(context.Background(), task.SessionID, fmt.Sprintf("cl-gen-%d", i))
		args, _ := json.Marshal(map[string]any{"name": fx.name, "spec": fx.spec})
		if _, err := e.executeUserTool(ctx, executionModeFullAccess, task.SessionID, "office.generate", args); err != nil {
			t.Fatalf("generate %s: %v", fx.kind, err)
		}
	}
	versions, err := store.ListOfficeVersions(ctx, task.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]domain.Version{}
	for _, v := range versions {
		seen[v.Kind] = v
	}
	for _, kind := range []string{"pptx", "docx", "xlsx", "pdf"} {
		if seen[kind].ID == "" {
			t.Fatalf("missing generated %s in current task: %#v", kind, versions)
		}
	}
	got := officeCall(t, e, "office.task.get", "cl-get", map[string]any{"taskId": task.ID})
	if !got.OK {
		t.Fatalf("task.get: %+v", got.Error)
	}
	var page struct {
		Artifacts []struct {
			Kind     string
			Versions []struct {
				ID, Quality string
			}
		}
	}
	if err := decodeResponsePayload(got.Payload, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Artifacts) < 4 {
		t.Fatalf("task.get artifacts: %#v", page.Artifacts)
	}
	probe := officeCall(t, e, "office.renderer.probe", "cl-probe", map[string]any{})
	if !probe.OK {
		t.Fatalf("probe: %+v", probe.Error)
	}
	var components struct {
		Components []struct{ ID, Detail string }
	}
	if err := decodeResponsePayload(probe.Payload, &components); err != nil {
		t.Fatal(err)
	}
	typst := ""
	presenton := ""
	for _, c := range components.Components {
		if c.ID == "typst" {
			typst = c.Detail
		}
		if c.ID == "presenton" {
			presenton = c.Detail
		}
	}
	if !strings.Contains(typst, "gofpdf") {
		t.Fatalf("typst probe: %q", typst)
	}
	if !strings.Contains(presenton, "未进生产主链") {
		t.Fatalf("presenton probe: %q", presenton)
	}
	pdf := seen["pdf"]
	if pdf.Quality != "passed" {
		t.Fatalf("independent PDF without Typst must be formally check-passed: %#v", pdf)
	}
	docx := seen["docx"]
	formalDoc := officeCall(t, e, "office.artifact.accept", "cl-formal-docx", map[string]any{
		"taskId": task.ID, "artifactId": docx.ArtifactID, "versionId": docx.ID, "expectedRevision": 1, "formal": true,
	})
	if formalDoc.OK || formalDoc.Error.Code != "OFFICE_DRAFT_REQUIRED" {
		t.Fatalf("formal accept non-passed docx: %+v", formalDoc)
	}
	draftDoc := officeCall(t, e, "office.artifact.accept", "cl-draft-docx", map[string]any{
		"taskId": task.ID, "artifactId": docx.ArtifactID, "versionId": docx.ID, "expectedRevision": 1,
	})
	if !draftDoc.OK {
		t.Fatalf("draft accept: %+v", draftDoc.Error)
	}
	exportFormal := officeCall(t, e, "office.artifact.export", "cl-export-formal", map[string]any{"taskId": task.ID, "versionId": docx.ID, "draft": false})
	if exportFormal.OK || exportFormal.Error.Code != "OFFICE_DRAFT_REQUIRED" {
		t.Fatalf("formal export of draft: %+v", exportFormal)
	}
	exportDraft := officeCall(t, e, "office.artifact.export", "cl-export-draft", map[string]any{"taskId": task.ID, "versionId": docx.ID, "draft": true})
	if !exportDraft.OK {
		t.Fatalf("draft export: %+v", exportDraft.Error)
	}
	formalPDF := officeCall(t, e, "office.artifact.accept", "cl-formal-pdf", map[string]any{
		"taskId": task.ID, "artifactId": pdf.ArtifactID, "versionId": pdf.ID, "expectedRevision": 1, "formal": true,
	})
	if !formalPDF.OK {
		t.Fatalf("formal accept passed PDF: %+v", formalPDF.Error)
	}
	exportPDF := officeCall(t, e, "office.artifact.export", "cl-export-pdf", map[string]any{"taskId": task.ID, "versionId": pdf.ID, "draft": false})
	if !exportPDF.OK {
		t.Fatalf("formal export passed PDF: %+v", exportPDF.Error)
	}
	var exported struct{ Notice string }
	if err := decodeResponsePayload(exportPDF.Payload, &exported); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(exported.Notice, "检查通过不是已接受为正式版") {
		t.Fatalf("export notice: %q", exported.Notice)
	}
	if !strings.Contains(exported.Notice, "不保证分页") || !strings.Contains(exported.Notice, "PDF/A") {
		t.Fatalf("independent PDF export must not claim Word-pixel or PDF/A: %q", exported.Notice)
	}
	if e.officeStudio.SameSourceExport(ctx, docx) {
		t.Fatal("docx without a bound render PDF must not export as same-source")
	}
	preview := officeCall(t, e, "office.artifact.preview", "cl-preview", map[string]any{"taskId": task.ID, "versionId": docx.ID})
	if !preview.OK {
		t.Fatalf("preview: %+v", preview.Error)
	}
	var nodes struct {
		Nodes []struct{ ID, Digest, Text string }
	}
	if err := decodeResponsePayload(preview.Payload, &nodes); err != nil {
		t.Fatal(err)
	}
	var node struct{ ID, Digest, Text string }
	for _, n := range nodes.Nodes {
		if strings.Contains(n.Text, "1280") {
			node = n
			break
		}
	}
	if node.ID == "" {
		t.Fatalf("docx preview missing fact text: %#v", nodes.Nodes)
	}
	heads, err := store.ListOfficeHeads(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	var docHead domain.Head
	for _, h := range heads {
		if h.ArtifactID == docx.ArtifactID {
			docHead = h
		}
	}
	patched := officeCall(t, e, "office.artifact.patch", "cl-patch", map[string]any{
		"taskId": task.ID, "artifactId": docx.ArtifactID, "baseVersionId": docx.ID, "expectedRevision": docHead.Revision,
		"nodeId": node.ID, "nodeDigest": node.Digest, "text": "订单 1280单 已修订",
	})
	if !patched.OK {
		t.Fatalf("patch: %+v", patched.Error)
	}
}
