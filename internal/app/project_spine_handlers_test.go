package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/agenthub"
	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/domain/deliverable"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/projecttask"
	"github.com/lunitide/lunitide/internal/projecttree"
	"github.com/lunitide/lunitide/internal/providerapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestProjectRootPickCanceled(t *testing.T) {
	e := NewEngineWithProjects(nil, nil, "test", nil)
	e.SetAgentHub(&agenthub.Service{Pick: func() (string, error) { return "", agenthub.ErrPickCanceled }})
	resp := handleProjectSpine(e, context.Background(), validRequest("project.root.pick", `{}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	raw, _ := json.Marshal(resp.Payload)
	if !strings.Contains(string(raw), `"canceled":true`) {
		t.Fatalf("payload %s", raw)
	}
}

func TestProjectTaskOpenRequiresReadyTree(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "spine-notree.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := projectapp.New(store, store)
	e := NewEngineWithProjects(providerapp.New(store, store), svc, "test", nil)
	created, err := svc.Create(ctx, "no-tree", "test", map[string]string{"n": "x"}, project.Project{
		Name: "Bare", Type: project.TypeImplementation, Description: "d", Client: "c",
		PlanStart: "2026-01-01", PlanEnd: "2026-12-31", RootPath: t.TempDir(), Status: project.StatusCreated,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := handleProjectSpine(e, ctx, validRequest("project.task.open", `{"projectId":"`+created.ID+`","itemId":"F001"}`))
	if resp.OK || resp.Error.Code != "PROJECT_DB_REQUIRED" {
		t.Fatalf("a selected root is the project directory; tree generation is not another gate: %#v", resp)
	}
}

func TestProjectTaskOpenReportCompleteAndReturn(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "spine-loop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := t.TempDir()
	svc := projectapp.New(store, store)
	e := NewEngineWithProjects(providerapp.New(store, store), svc, "test", nil)
	files := attachmentapp.NewDirFileStorage(t.TempDir())
	store.SetProjectEvidenceFiles(files, files)
	e.SetDeliverableStorage(store)
	e.SetProjectAttachmentStorage(store, files)
	created, err := svc.Create(ctx, "loop-create", "test", map[string]string{"name": "Mall"}, project.Project{
		Name: "Mall", Type: project.TypeImplementation, Description: "desc", Client: "Acme",
		PlanStart: "2026-01-01", PlanEnd: "2026-12-31", RootPath: root, Status: project.StatusCreated,
	})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := projecttree.Default(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = projecttree.Materialize(root, tree); err != nil {
		t.Fatal(err)
	}
	ready, err := svc.Mutate(ctx, "loop-ready", "test", "project.update", created.ID, created.Version, map[string]string{"t": "1"}, func(p *project.Project) error {
		p.TreeStatus = project.TreeReady
		p.DBStatus = project.DBReady
		p.Status = project.StatusInProgress
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.saveChecklist(ctx, ready, project.InterfacePhase(ready.Type), "interface_list", "接口清单", projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "I001", Title: "登录接口", Status: "dev_done"}}}, deliverable.StatusApproved); err != nil {
		t.Fatal(err)
	}
	dev := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "F001", Title: "登录", Status: "pending", Acceptance: "能登录"}}}
	if err = e.saveChecklist(ctx, ready, project.DevPhase(ready.Type), "dev_checklist", "开发检查清单", dev, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	open := handleProjectSpine(e, ctx, validRequest("project.task.open", `{"projectId":"`+ready.ID+`","itemId":"F001"}`))
	if !open.OK {
		t.Fatalf("open %#v", open)
	}
	raw, _ := json.Marshal(open.Payload)
	if !strings.Contains(string(raw), "F001") || !strings.Contains(string(raw), "不要 git") {
		t.Fatalf("brief missing id/constraint: %s", raw)
	}
	if !strings.Contains(string(raw), filepath.Base(root)) {
		t.Fatalf("brief missing root: %s", raw)
	}
	report := handleProjectSpine(e, ctx, validRequest("project.task.report", `{"projectId":"`+ready.ID+`","itemId":"F001","summary":"写了登录"}`))
	if !report.OK {
		t.Fatalf("report %#v", report)
	}
	complete := handleProjectSpine(e, ctx, validRequest("project.task.complete", `{"projectId":"`+ready.ID+`","itemId":"F001","selfTestPass":true}`))
	if !complete.OK {
		t.Fatalf("complete %#v", complete)
	}
	test := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "T-F001", Title: "测试：登录", Status: "pending", SourceID: "F001"}}}
	if err = e.saveChecklist(ctx, ready, project.TestPhase(ready.Type), "test_checklist", "测试检查清单", test, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	empty := handleProjectSpine(e, ctx, validRequest("project.task.returnFromTest", `{"projectId":"`+ready.ID+`","testItemId":"T-F001","reason":""}`))
	if empty.OK || empty.Error.Code != "PROJECT_TEST_REASON_REQUIRED" {
		t.Fatalf("empty reason %#v", empty)
	}
	ret := handleProjectSpine(e, ctx, validRequest("project.task.returnFromTest", `{"projectId":"`+ready.ID+`","testItemId":"T-F001","reason":"登录失败"}`))
	if !ret.OK {
		t.Fatalf("return %#v", ret)
	}
	reopen := handleProjectSpine(e, ctx, validRequest("project.task.open", `{"projectId":"`+ready.ID+`","itemId":"F001"}`))
	if !reopen.OK {
		t.Fatalf("reopen %#v", reopen)
	}
	raw, _ = json.Marshal(reopen.Payload)
	if !strings.Contains(string(raw), "登录失败") {
		t.Fatalf("reopen brief missing reason: %s", raw)
	}
	if _, err = os.Stat(filepath.Join(root, "src")); err != nil {
		t.Fatal(err)
	}
}

func TestProjectExecutorSetRejectsUnknown(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "exec.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := projectapp.New(store, store)
	e := NewEngineWithProjects(providerapp.New(store, store), svc, "test", nil)
	created, err := svc.Create(ctx, "exec-create", "test", map[string]string{"n": "x"}, project.Project{
		Name: "Mall", Type: project.TypeImplementation, Description: "d", Client: "c",
		PlanStart: "2026-01-01", PlanEnd: "2026-12-31", RootPath: t.TempDir(), Status: project.StatusCreated,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := handleProjectSpine(e, ctx, validRequest("project.executor.set", `{"id":"`+created.ID+`","version":1,"executor":"kimi"}`))
	if resp.OK || resp.Error.Code != "PROJECT_EXECUTOR_UNAVAILABLE" {
		t.Fatalf("%#v", resp)
	}
}

func TestProjectTreeGetRebuildsMissingLock(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "lock-rebuild.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := t.TempDir()
	svc := projectapp.New(store, store)
	e := NewEngineWithProjects(providerapp.New(store, store), svc, "test", nil)
	created, err := svc.Create(ctx, "lock-create", "test", map[string]string{"n": "x"}, project.Project{
		Name: "Mall", Type: project.TypeImplementation, Description: "d", Client: "c",
		PlanStart: "2026-01-01", PlanEnd: "2026-12-31", RootPath: root, Status: project.StatusCreated,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(root, ".lunitide", "project.json")); err != nil {
		t.Fatal(err)
	}
	resp := handleProjectSpine(e, ctx, validRequest("project.tree.get", `{"projectId":"`+created.ID+`"}`))
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	lock, err := os.ReadFile(filepath.Join(root, ".lunitide", "project.json"))
	if err != nil || !strings.Contains(string(lock), created.ID) {
		t.Fatalf("lock not rebuilt: %s %v", lock, err)
	}
}

func TestProjectPublishRequiresRoot(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "pub-root.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := projectapp.New(store, store)
	e := NewEngineWithProjects(providerapp.New(store, store), svc, "test", nil)
	created, err := svc.Create(ctx, "pub-create", "test", map[string]string{"n": "x"}, project.Project{
		Name: "Bare", Type: project.TypeImplementation, Description: "d", Client: "c",
		PlanStart: "2026-01-01", PlanEnd: "2026-12-31", Status: project.StatusCreated,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := validRequest("project.publish", fmt.Sprintf(`{"id":%q,"version":%d}`, created.ID, created.Version))
	req.IdempotencyKey = "pub-root"
	resp := e.Handle(ctx, req)
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROJECT_ROOT_REQUIRED" {
		code := ""
		if resp.Error != nil {
			code = resp.Error.Code
		}
		t.Fatalf("code=%q %#v", code, resp)
	}
}

func TestProjectUpdateCreatedMovesRootLock(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "move-root.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	oldRoot := t.TempDir()
	newRoot := t.TempDir()
	svc := projectapp.New(store, store)
	e := NewEngineWithProjects(providerapp.New(store, store), svc, "test", nil)
	created, err := svc.Create(ctx, "move-create", "test", map[string]string{"n": "x"}, project.Project{
		Name: "Mall", Type: project.TypeImplementation, Description: "d", Client: "c",
		PlanStart: "2026-01-01", PlanEnd: "2026-12-31", RootPath: oldRoot, Status: project.StatusCreated,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := validRequest("project.update", fmt.Sprintf(`{"id":%q,"version":%d,"name":"Mall","type":"implementation","description":"d","client":"c","planStart":"2026-01-01","planEnd":"2026-12-31","rootPath":%q}`, created.ID, created.Version, newRoot))
	req.IdempotencyKey = "move-root"
	resp := e.Handle(ctx, req)
	if !resp.OK {
		t.Fatalf("%#v", resp)
	}
	lock, err := os.ReadFile(filepath.Join(newRoot, ".lunitide", "project.json"))
	if err != nil || !strings.Contains(string(lock), created.ID) {
		t.Fatalf("new lock: %s %v", lock, err)
	}
	if _, err = os.Stat(filepath.Join(oldRoot, ".lunitide", "project.json")); !os.IsNotExist(err) {
		t.Fatalf("old lock still present: %v", err)
	}
}

func TestProjectTaskOpenPersistsHubThread(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "hub-open.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := t.TempDir()
	svc := projectapp.New(store, store)
	e := NewEngineWithProjects(providerapp.New(store, store), svc, "test", nil)
	files := attachmentapp.NewDirFileStorage(t.TempDir())
	store.SetProjectEvidenceFiles(files, files)
	e.SetDeliverableStorage(store)
	e.SetProjectAttachmentStorage(store, files)
	created, err := svc.Create(ctx, "hub-create", "test", map[string]string{"name": "Mall"}, project.Project{
		Name: "Mall", Type: project.TypeImplementation, Description: "desc", Client: "Acme",
		PlanStart: "2026-01-01", PlanEnd: "2026-12-31", RootPath: root, Status: project.StatusCreated,
	})
	if err != nil {
		t.Fatal(err)
	}
	tree, err := projecttree.Default(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = projecttree.Materialize(root, tree); err != nil {
		t.Fatal(err)
	}
	ready, err := svc.Mutate(ctx, "hub-ready", "test", "project.update", created.ID, created.Version, map[string]string{"t": "1"}, func(p *project.Project) error {
		p.TreeStatus = project.TreeReady
		p.DBStatus = project.DBReady
		p.Status = project.StatusInProgress
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.saveChecklist(ctx, ready, project.InterfacePhase(ready.Type), "interface_list", "接口清单", projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "I001", Title: "登录接口", Status: "dev_done"}}}, deliverable.StatusApproved); err != nil {
		t.Fatal(err)
	}
	dev := projecttask.Doc{Version: 1, Items: []projecttask.Item{{ID: "F001", Title: "登录", Status: "pending"}}}
	if err = e.saveChecklist(ctx, ready, project.DevPhase(ready.Type), "dev_checklist", "开发检查清单", dev, deliverable.StatusReview); err != nil {
		t.Fatal(err)
	}
	hubID := "01ARZ3NDEKTSV4RRFFQ69G5FAT"
	first := handleProjectSpine(e, ctx, validRequest("project.task.open", `{"projectId":"`+ready.ID+`","itemId":"F001","hubThreadId":"`+hubID+`"}`))
	if !first.OK {
		t.Fatalf("open %#v", first)
	}
	raw, _ := json.Marshal(first.Payload)
	if !strings.Contains(string(raw), hubID) {
		t.Fatalf("missing hubThreadId: %s", raw)
	}
	again := handleProjectSpine(e, ctx, validRequest("project.task.open", `{"projectId":"`+ready.ID+`","itemId":"F001"}`))
	if !again.OK {
		t.Fatalf("reopen %#v", again)
	}
	raw, _ = json.Marshal(again.Payload)
	if !strings.Contains(string(raw), hubID) {
		t.Fatalf("did not retain hubThreadId: %s", raw)
	}
}
