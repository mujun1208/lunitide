package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
