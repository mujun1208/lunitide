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
	"github.com/lunitide/lunitide/internal/projectboard"
	"github.com/lunitide/lunitide/internal/projecttask"
	"github.com/lunitide/lunitide/internal/projecttree"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/stageapp"
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
	e.stages = stageapp.New(store, store)
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
		if item.DocumentType == "biz_req_analysis" && !strings.Contains(string(raw), "本题库未答完，按已答 + 骨架生成，请人审。") {
			t.Fatalf("incomplete interview banner missing: %s", raw)
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

func TestPriorDeliverableTextUsesApprovedOnly(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	if err := e.writeDeliverableBody(ctx, created, 1, "biz_req_analysis", "草稿", []byte("DRAFT_ONLY_BODY_must_not_reach_phase_two")); err != nil {
		t.Fatal(err)
	}
	if err := e.writeDeliverableBody(ctx, created, 1, "impl_assessment", "已批", []byte("APPROVED_BODY_phase_one_for_phase_two")); err != nil {
		t.Fatal(err)
	}
	items, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: created.ID, Phase: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.DocumentType != "impl_assessment" {
			continue
		}
		item.Status = deliverable.StatusApproved
		if _, err = e.deliverables.UpsertProjectDeliverable(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	text := e.priorDeliverableText(ctx, created.ID, 1)
	if strings.Contains(text, "DRAFT_ONLY_BODY") {
		t.Fatalf("review draft leaked into phase 2 prior text: %s", text)
	}
	if !strings.Contains(text, "APPROVED_BODY") {
		t.Fatalf("approved body missing: %s", text)
	}
}

func TestProjectReturnFromTestInterfaceDoesNotTouchDev(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	iface := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "I001", Title: "登录接口", Status: "dev_done"}}}
	codeBoard := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "F001", Title: "登录", Status: "dev_done"}}}
	test := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "T-I001", Title: "测接口", Status: "pending", SourceID: "I001", SourceKind: "interface"}}}
	if err := e.saveChecklist(ctx, created, project.InterfacePhase(created.Type), "interface_list", "接口清单", iface, deliverable.StatusApproved); err != nil {
		t.Fatal(err)
	}
	if err := e.saveChecklist(ctx, created, project.DevPhase(created.Type), "dev_checklist", "开发检查清单", codeBoard, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	if err := e.saveChecklist(ctx, created, project.TestPhase(created.Type), "test_checklist", "测试检查清单", test, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	resp := handleProjectSpine(e, ctx, validRequest("project.task.returnFromTest", `{"projectId":"`+created.ID+`","testItemId":"T-I001","reason":"契约失败"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	gotIface, _, err := e.loadChecklist(ctx, created.ID, project.InterfacePhase(created.Type), "interface_list")
	if err != nil {
		t.Fatal(err)
	}
	_, item, ok := projecttask.Find(gotIface, "I001")
	if !ok || item.Status == "dev_done" {
		t.Fatalf("interface not returned: %+v", gotIface.Items)
	}
	gotDev, _, err := e.loadChecklist(ctx, created.ID, project.DevPhase(created.Type), "dev_checklist")
	if err != nil {
		t.Fatal(err)
	}
	_, destItem, ok := projecttask.Find(gotDev, "F001")
	if !ok || destItem.Status != "dev_done" {
		t.Fatalf("dev board changed: %+v", gotDev.Items)
	}
}

func TestProjectIntegrationFailReturnsMembersBySource(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	iface := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "I001", Title: "登录接口", Status: "dev_done"}}}
	codeBoard := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "F001", Title: "登录", Status: "dev_done"}}}
	test := projecttask.Doc{Version: 1, Items: []projecttask.Item{{
		ID: "T-I001", Title: "测接口", Status: "test_pass", SourceID: "I001", SourceKind: "interface",
		RequiredKinds: []string{"unit"}, KindResults: map[string]projecttask.KindResult{"unit": {Status: "pass"}},
	}}}
	scene := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "S001", Title: "登录场景", Status: "pending", MemberIDs: []string{"T-I001"}}}}
	if err := e.saveChecklist(ctx, created, project.InterfacePhase(created.Type), "interface_list", "接口清单", iface, deliverable.StatusApproved); err != nil {
		t.Fatal(err)
	}
	if err := e.saveChecklist(ctx, created, project.DevPhase(created.Type), "dev_checklist", "开发检查清单", codeBoard, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	if err := e.saveChecklist(ctx, created, project.TestPhase(created.Type), "test_checklist", "测试检查清单", test, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	if err := e.saveChecklist(ctx, created, 7, "integration_test_list", "集成测试场景清单", scene, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	resp := handleProjectFactory(e, ctx, validRequest("project.test.run", `{"projectId":"`+created.ID+`","itemId":"S001","kind":"unit","evidence":"fail: 集成失败"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	gotScene, _, err := e.loadChecklist(ctx, created.ID, 7, "integration_test_list")
	if err != nil {
		t.Fatal(err)
	}
	_, sceneItem, ok := projecttask.Find(gotScene, "S001")
	if !ok || sceneItem.Status != "pending" {
		t.Fatalf("scene %#v", gotScene.Items)
	}
	gotIface, _, err := e.loadChecklist(ctx, created.ID, project.InterfacePhase(created.Type), "interface_list")
	if err != nil {
		t.Fatal(err)
	}
	_, item, ok := projecttask.Find(gotIface, "I001")
	if !ok || item.Status == "dev_done" {
		t.Fatalf("interface member not returned: %+v", gotIface.Items)
	}
	gotDev, _, err := e.loadChecklist(ctx, created.ID, project.DevPhase(created.Type), "dev_checklist")
	if err != nil {
		t.Fatal(err)
	}
	_, destItem, ok := projecttask.Find(gotDev, "F001")
	if !ok || destItem.Status != "dev_done" {
		t.Fatalf("dev board changed: %+v", gotDev.Items)
	}
}

func TestDeliverableUpsertChecklistReportsBoardDirty(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	source := projecttask.Doc{Version: 1, Items: []projecttask.Item{
		{ID: "I001", Title: "登录接口", Status: "pending", Method: "POST", Path: "/login"},
	}}
	if err := e.saveChecklist(ctx, created, 2, "api_list", "接口清单", source, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	items, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: created.ID, Phase: 2})
	if err != nil || len(items) != 1 {
		t.Fatalf("items %+v %v", items, err)
	}
	resp := handleDeliverableUpsert(e, ctx, validRequest("deliverable.upsert", `{"projectId":"`+created.ID+`","phase":2,"documentType":"api_list","title":"接口清单","attachmentId":"`+items[0].AttachmentID+`","status":"review"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	raw, _ := json.Marshal(resp.Payload)
	var dto struct {
		BoardDirty bool `json:"boardDirty"`
	}
	if err := json.Unmarshal(raw, &dto); err != nil || !dto.BoardDirty {
		t.Fatalf("want boardDirty=true, got %s", raw)
	}
}

func TestDeliverableUpsertIntegrationTestListOmitsBoardDirty(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	scene := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "S001", Title: "登录场景", Status: "pending", MemberIDs: []string{"T-1"}}}}
	if err := e.saveChecklist(ctx, created, 7, "integration_test_list", "集成测试场景清单", scene, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	items, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: created.ID, Phase: 7})
	if err != nil || len(items) != 1 {
		t.Fatalf("items %+v %v", items, err)
	}
	resp := handleDeliverableUpsert(e, ctx, validRequest("deliverable.upsert", `{"projectId":"`+created.ID+`","phase":7,"documentType":"integration_test_list","title":"集成测试场景清单","attachmentId":"`+items[0].AttachmentID+`","status":"approved"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	raw, _ := json.Marshal(resp.Payload)
	if strings.Contains(string(raw), `"boardDirty":true`) {
		t.Fatalf("want boardDirty omitted or false, got %s", raw)
	}
	board, _, err := e.loadBoard(ctx, created, projectboard.KindIntegration)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Items) == 0 {
		t.Fatalf("scenes wiped: %+v", board)
	}
	for _, item := range board.Items {
		if item.ChangeKind == "removed" {
			t.Fatalf("scene marked removed: %+v", board.Items)
		}
	}
}

func TestDeliverableUpsertInterfaceListOmitsBoardDirtyWhenSelfBoard(t *testing.T) {
	ctx := context.Background()
	e, _, created, _ := factoryEngine(t)
	source := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "I001", Title: "登录", Status: "pending", Method: "POST", Path: "/login"}}}
	if err := e.saveChecklist(ctx, created, project.DesignPhase(created.Type), "api_list", "接口清单", source, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	stale := projecttask.Doc{Version: 1, Items: []projecttask.Item{{
		ID: "I001", Title: "登录", Status: "pending", Method: "POST", Path: "/login",
		NeedsReprocess: true, ChangeKind: "modified",
	}}}
	if err := e.saveChecklist(ctx, created, project.InterfacePhase(created.Type), "interface_list", "接口清单", stale, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	items, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: created.ID, Phase: project.InterfacePhase(created.Type)})
	if err != nil || len(items) != 1 {
		t.Fatalf("items %+v %v", items, err)
	}
	resp := handleDeliverableUpsert(e, ctx, validRequest("deliverable.upsert", `{"projectId":"`+created.ID+`","phase":4,"documentType":"interface_list","title":"接口清单","attachmentId":"`+items[0].AttachmentID+`","status":"approved"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	raw, _ := json.Marshal(resp.Payload)
	if strings.Contains(string(raw), `"boardDirty":true`) {
		t.Fatalf("want boardDirty omitted or false, got %s", raw)
	}
}
