package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/domain/deliverable"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/projecttask"
	"github.com/lunitide/lunitide/internal/projecttree"
	"github.com/lunitide/lunitide/internal/providerapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func factoryEngine(t *testing.T) (*Engine, *projectapp.Service, project.Project, string) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "factory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := t.TempDir()
	svc := projectapp.New(store, store)
	e := NewEngineWithProjects(providerapp.New(store, store), svc, "test", nil)
	files := attachmentapp.NewDirFileStorage(t.TempDir())
	store.SetProjectEvidenceFiles(files, files)
	e.SetDeliverableStorage(store)
	e.SetProjectAttachmentStorage(store, files)
	created, err := svc.Create(ctx, "factory-create", "test", map[string]string{"name": "Mall"}, project.Project{
		Name: "Mall", Type: project.TypeImplementation, Description: "desc", Client: "Acme",
		PlanStart: "2026-01-01", PlanEnd: "2026-12-31", RootPath: root, Status: project.StatusCreated,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e, svc, created, root
}

func TestProjectDeliverableGenerateWritesAttachments(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	resp := handleProjectFactory(e, ctx, validRequest("project.deliverable.generate", `{"projectId":"`+created.ID+`","phase":1}`))
	if !resp.OK {
		t.Fatalf("generate %#v", resp)
	}
	items, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: created.ID, Phase: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 9 {
		t.Fatalf("generated %d", len(items))
	}
	for _, item := range items {
		if item.AttachmentID == "" || item.Status != deliverable.StatusReview {
			t.Fatalf("card %#v", item)
		}
		att, err := e.projectAttachments.GetProjectAttachment(ctx, item.AttachmentID)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := e.projectAttachmentFiles.ReadFile(ctx, att.FilePath)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) < 32 {
			t.Fatalf("%s bytes=%d", item.DocumentType, len(raw))
		}
	}
}

func TestDeliverableUpsertRejectsTemplateOnlyApprove(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	resp := handleDeliverableUpsert(e, ctx, validRequest("deliverable.upsert", `{"projectId":"`+created.ID+`","phase":1,"documentType":"biz_req_analysis","title":"业务需求分析报告","templateId":"01ARZ3NDEKTSV4RRFFQ69G5FAT","status":"approved"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROJECT_ATTACHMENT_REQUIRED" {
		t.Fatalf("%#v", resp)
	}
}

func TestProjectTaskOpenRequiresDBReady(t *testing.T) {
	ctx := context.Background()
	e, svc, created, root := factoryEngine(t)
	tree, err := projecttree.Default(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = projecttree.Materialize(root, tree); err != nil {
		t.Fatal(err)
	}
	ready, err := svc.Mutate(ctx, "factory-tree", "test", "project.update", created.ID, created.Version, map[string]string{"t": "1"}, func(p *project.Project) error {
		p.TreeStatus = project.TreeReady
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := handleProjectSpine(e, ctx, validRequest("project.task.open", `{"projectId":"`+ready.ID+`","itemId":"F001"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROJECT_DB_REQUIRED" {
		t.Fatalf("%#v", resp)
	}
}

func TestProjectTaskOpenRequiresInterfaceConfirmed(t *testing.T) {
	ctx := context.Background()
	e, svc, created, root := factoryEngine(t)
	tree, err := projecttree.Default(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = projecttree.Materialize(root, tree); err != nil {
		t.Fatal(err)
	}
	ready, err := svc.Mutate(ctx, "factory-db", "test", "project.update", created.ID, created.Version, map[string]string{"t": "1"}, func(p *project.Project) error {
		p.TreeStatus = project.TreeReady
		p.DBStatus = project.DBReady
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := handleProjectSpine(e, ctx, validRequest("project.task.open", `{"projectId":"`+ready.ID+`","itemId":"F001"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROJECT_INTERFACE_REQUIRED" {
		t.Fatalf("%#v", resp)
	}
}

func TestProjectBoardSyncReportsStats(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	src := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "I001", Title: "登录", Status: "pending", Method: "POST", Path: "/login"}}}
	if err := e.saveChecklist(ctx, created, project.DesignPhase(created.Type), "api_list", "接口清单", src, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	resp := handleProjectFactory(e, ctx, validRequest("project.board.sync", `{"projectId":"`+created.ID+`","boardKind":"interface"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	raw, _ := json.Marshal(resp.Payload)
	if !strings.Contains(string(raw), `"total":1`) || !strings.Contains(string(raw), "I001") {
		t.Fatalf("payload %s", raw)
	}
}

func TestProjectBoardSyncPrefersInterfaceListOverApiList(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	stale := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "I-OLD", Title: "旧方案接口", Status: "pending"}}}
	fresh := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "I-NEW", Title: "注册表接口", Status: "pending", Method: "POST", Path: "/login"}}}
	if err := e.saveChecklist(ctx, created, project.DesignPhase(created.Type), "api_list", "接口清单", stale, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	if err := e.saveChecklist(ctx, created, project.InterfacePhase(created.Type), "interface_list", "接口清单", fresh, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	resp := handleProjectFactory(e, ctx, validRequest("project.board.sync", `{"projectId":"`+created.ID+`","boardKind":"interface"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	raw, _ := json.Marshal(resp.Payload)
	if !strings.Contains(string(raw), "I-NEW") || !strings.Contains(string(raw), "注册表接口") {
		t.Fatalf("wanted registry interface_list, got %s", raw)
	}
	if strings.Contains(string(raw), "I-OLD") {
		t.Fatalf("stale api_list won: %s", raw)
	}
}

func TestProjectBoardSyncParsesOpenAPIInterfaceList(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	stale := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "I-OLD", Title: "旧方案接口", Status: "pending"}}}
	if err := e.saveChecklist(ctx, created, project.DesignPhase(created.Type), "api_list", "接口清单", stale, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	spec := []byte(`{"openapi":"3.0.3","info":{"title":"t","version":"1"},"paths":{"/login":{"post":{"operationId":"login","summary":"注册表接口"}}}}`)
	if err := e.writeDeliverableBody(ctx, created, project.InterfacePhase(created.Type), "interface_list", "接口清单", spec); err != nil {
		t.Fatal(err)
	}
	resp := handleProjectFactory(e, ctx, validRequest("project.board.sync", `{"projectId":"`+created.ID+`","boardKind":"interface"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	raw, _ := json.Marshal(resp.Payload)
	if !strings.Contains(string(raw), `"I001"`) || !strings.Contains(string(raw), "注册表接口") {
		t.Fatalf("wanted parsed OpenAPI rows, got %s", raw)
	}
	if strings.Contains(string(raw), "I-OLD") {
		t.Fatalf("fell back to api_list instead of OpenAPI: %s", raw)
	}
}

func TestProjectBoardPutRejectsDevDoneWithoutSelfTest(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	current := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "I001", Title: "登录", Status: "in_progress", Method: "POST", Path: "/login"}}}
	if err := e.saveChecklist(ctx, created, project.InterfacePhase(created.Type), "interface_list", "接口清单", current, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	current.Items[0].Status = "dev_done"
	body, err := json.Marshal(map[string]any{"projectId": created.ID, "boardKind": "interface", "board": current})
	if err != nil {
		t.Fatal(err)
	}
	resp := handleProjectFactory(e, ctx, validRequest("project.board.put", string(body)))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROJECT_SELFTEST_REQUIRED" {
		t.Fatalf("%#v", resp)
	}
	current.Items[0].SelfTestPass = true
	current.Items[0].LastResultSummary = "对照接口详细设计自测通过"
	body, err = json.Marshal(map[string]any{"projectId": created.ID, "boardKind": "interface", "board": current})
	if err != nil {
		t.Fatal(err)
	}
	resp = handleProjectFactory(e, ctx, validRequest("project.board.put", string(body)))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
}

func TestProjectTestRunUnitRequiresEvidence(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	test := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "T001", Title: "测登录", Status: "pending"}}}
	if err := e.saveChecklist(ctx, created, project.TestPhase(created.Type), "test_checklist", "测试检查清单", test, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	resp := handleProjectFactory(e, ctx, validRequest("project.test.run", `{"projectId":"`+created.ID+`","itemId":"T001","kind":"unit"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROJECT_TEST_KIND_UNSUPPORTED" {
		t.Fatalf("%#v", resp)
	}
	resp = handleProjectFactory(e, ctx, validRequest("project.test.run", `{"projectId":"`+created.ID+`","itemId":"T001","kind":"unit","evidence":"登录单测已记"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
}

func TestProjectBoardPutRejectsTitleChange(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	current := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "I001", Title: "登录", Status: "pending", Method: "POST", Path: "/login"}}}
	if err := e.saveChecklist(ctx, created, project.InterfacePhase(created.Type), "interface_list", "接口清单", current, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	current.Items[0].Title = "改名"
	body, err := json.Marshal(map[string]any{"projectId": created.ID, "boardKind": "interface", "board": current})
	if err != nil {
		t.Fatal(err)
	}
	resp := handleProjectFactory(e, ctx, validRequest("project.board.put", string(body)))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROJECT_INVALID_TRANSITION" {
		t.Fatalf("%#v", resp)
	}
}

func TestProjectBoardItemOpenPropagatesTestBoardLoadError(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	scene := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "S001", Title: "登录场景", Status: "pending", MemberIDs: []string{"T-1"}}}}
	tests := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "T-1", Title: "测登录", Status: "test_pass"}}}
	if err := e.saveChecklist(ctx, created, 7, "integration_test_list", "集成测试场景清单", scene, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	if err := e.saveChecklist(ctx, created, project.TestPhase(created.Type), "test_checklist", "测试检查清单", tests, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	items, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: created.ID, Phase: project.TestPhase(created.Type)})
	if err != nil {
		t.Fatal(err)
	}
	var rec deliverable.ProjectDeliverable
	for _, item := range items {
		if item.DocumentType == "test_checklist" {
			rec = item
			break
		}
	}
	att, err := e.projectAttachments.GetProjectAttachment(ctx, rec.AttachmentID)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.projectAttachmentFiles.DeleteFile(ctx, att.FilePath); err != nil {
		t.Fatal(err)
	}
	resp := handleProjectFactory(e, ctx, validRequest("project.board.item.open", `{"projectId":"`+created.ID+`","boardKind":"integration","itemId":"S001"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code == "PROJECT_INTEGRATION_NOT_READY" {
		t.Fatalf("swallowed load error: %#v", resp)
	}
}

const factorySchemaJSON = `{"version":1,"dialect":"sqlite","tables":[{"name":"items","columns":[{"name":"id","type":"TEXT","primaryKey":true}]}]}`

func TestProjectSchemaPutRequiresRoot(t *testing.T) {
	ctx := context.Background()
	e, svc, created, _ := factoryEngine(t)
	cleared, err := svc.Mutate(ctx, "clear-root", "test", "project.update", created.ID, created.Version, map[string]string{"t": "1"}, func(p *project.Project) error {
		p.RootPath = ""
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := handleProjectFactory(e, ctx, validRequest("project.schema.put", `{"projectId":"`+cleared.ID+`","schema":`+factorySchemaJSON+`}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROJECT_ROOT_REQUIRED" {
		t.Fatalf("%#v", resp)
	}
}

func TestProjectSchemaPutRejectsClosedProject(t *testing.T) {
	ctx := context.Background()
	e, svc, created, _ := factoryEngine(t)
	closed, err := svc.Mutate(ctx, "close-factory", "test", "project.update", created.ID, created.Version, map[string]string{"t": "1"}, func(p *project.Project) error {
		p.Status = project.StatusClosed
		p.CloseReason = "验收完成"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := handleProjectFactory(e, ctx, validRequest("project.schema.put", `{"projectId":"`+closed.ID+`","schema":`+factorySchemaJSON+`}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROJECT_INVALID_TRANSITION" {
		t.Fatalf("%#v", resp)
	}
}

func TestDeliverableUpsertRejectsNonJSONChecklist(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	body := []byte("# 接口清单\n这是一段超过三十二字节的 Markdown，不能当作清单批准。")
	if err := e.writeDeliverableBody(ctx, created, 2, "api_list", "接口清单", body); err != nil {
		t.Fatal(err)
	}
	items, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: created.ID, Phase: 2})
	if err != nil || len(items) != 1 {
		t.Fatalf("items %+v %v", items, err)
	}
	resp := handleDeliverableUpsert(e, ctx, validRequest("deliverable.upsert", `{"projectId":"`+created.ID+`","phase":2,"documentType":"api_list","title":"接口清单","attachmentId":"`+items[0].AttachmentID+`","status":"approved"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROJECT_BOARD_SOURCE_INVALID" {
		t.Fatalf("%#v", resp)
	}
}

func TestProjectTestRunRequiresRoot(t *testing.T) {
	ctx := context.Background()
	e, svc, created, _ := factoryEngine(t)
	cleared, err := svc.Mutate(ctx, "clear-root-test", "test", "project.update", created.ID, created.Version, map[string]string{"t": "1"}, func(p *project.Project) error {
		p.RootPath = ""
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := handleProjectFactory(e, ctx, validRequest("project.test.run", `{"projectId":"`+cleared.ID+`","itemId":"T001","kind":"unit"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROJECT_ROOT_REQUIRED" {
		t.Fatalf("%#v", resp)
	}
}

func TestProjectReleaseSyncRejectsNestedDest(t *testing.T) {
	ctx := context.Background()
	e, _, created, root := factoryEngine(t)
	dest := filepath.Join(root, "out", "pkg")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	escaped := strings.ReplaceAll(dest, `\`, `\\`)
	resp := handleProjectFactory(e, ctx, validRequest("project.release.sync", `{"projectId":"`+created.ID+`","destPath":"`+escaped+`"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROJECT_SYNC_INVALID" {
		t.Fatalf("%#v", resp)
	}
}

func TestProjectReleaseSyncRejectsRootDest(t *testing.T) {
	ctx := context.Background()
	e, _, created, root := factoryEngine(t)
	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("hello factory sync file"), 0o644); err != nil {
		t.Fatal(err)
	}
	escaped := strings.ReplaceAll(root, `\`, `\\`)
	resp := handleProjectFactory(e, ctx, validRequest("project.release.sync", `{"projectId":"`+created.ID+`","destPath":"`+escaped+`"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROJECT_SYNC_INVALID" {
		t.Fatalf("%#v", resp)
	}
}

func TestProjectInterviewSaveRejectsInvalidMode(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	resp := handleProjectFactory(e, ctx, validRequest("project.interview.save", `{"projectId":"`+created.ID+`","phase":1,"mode":"nope","answers":[{"id":"core_problem","value":"内部提效"}]}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROJECT_INTERVIEW_INVALID" {
		t.Fatalf("%#v", resp)
	}
}

func TestProjectRulesGetMissingManifestOK(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	resp := handleProjectFactory(e, ctx, validRequest("project.rules.get", `{"projectId":"`+created.ID+`"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
}

func TestProjectRulesGetCorruptManifestFails(t *testing.T) {
	ctx := context.Background()
	e, _, created, root := factoryEngine(t)
	dir := filepath.Join(root, ".lunitide", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"version":9}`), 0o644); err != nil {
		t.Fatal(err)
	}
	resp := handleProjectFactory(e, ctx, validRequest("project.rules.get", `{"projectId":"`+created.ID+`"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROJECT_RULES_FAILED" {
		t.Fatalf("%#v", resp)
	}
}
