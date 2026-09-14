package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/agenthub"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/deliverable"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/stage"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/projectboard"
	"github.com/lunitide/lunitide/internal/projectroot"
	"github.com/lunitide/lunitide/internal/projecttask"
	"github.com/lunitide/lunitide/internal/projecttree"
	"github.com/oklog/ulid/v2"
)

func handleProjectSpine(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	switch r.Method {
	case "project.root.pick":
		return handleProjectRootPick(e, ctx, r)
	case "project.root.rebind":
		return handleProjectRootRebind(e, ctx, r)
	case "project.tree.get":
		return handleProjectTreeGet(e, ctx, r)
	case "project.tree.put":
		return handleProjectTreePut(e, ctx, r)
	case "project.tree.materialize":
		return handleProjectTreeMaterialize(e, ctx, r)
	case "project.executor.set":
		return handleProjectExecutorSet(e, ctx, r)
	case "project.task.open":
		return handleProjectTaskOpen(e, ctx, r)
	case "project.task.report":
		return handleProjectTaskReport(e, ctx, r)
	case "project.task.complete":
		return handleProjectTaskComplete(e, ctx, r)
	case "project.task.returnFromTest":
		return handleProjectTaskReturnFromTest(e, ctx, r)
	default:
		return r.Fail("BRIDGE_SCHEMA_INVALID", "未知的项目方法", false)
	}
}

func handleProjectRootPick(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.root.pick 参数无效", false)
	}
	if e.agentHub == nil {
		return r.Fail("FEATURE_DISABLED", "本机选目录尚未初始化", false)
	}
	path, err := e.agentHub.ChooseDir()
	if err != nil {
		if errors.Is(err, agenthub.ErrPickCanceled) {
			return r.Ok(map[string]any{"canceled": true, "path": ""})
		}
		return projectFailure(r, mapPickError(err))
	}
	return r.Ok(map[string]any{"canceled": false, "path": path})
}

func mapPickError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "不受支持") || strings.Contains(msg, "invalid") {
		return projectapp.ErrRootInvalid
	}
	return err
}

func handleProjectRootRebind(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID       string `json:"id"`
		Version  int64  `json:"version"`
		RootPath string `json:"rootPath"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) || p.Version < 1 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.root.rebind 参数无效", false)
	}
	if !projectServiceAvailable(e.projects) {
		return r.Fail("STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
	normalized, err := projectroot.Normalize(p.RootPath)
	if err != nil {
		return projectFailure(r, projectapp.ErrRootInvalid)
	}
	if err = projectroot.Probe(normalized); err != nil {
		return projectFailure(r, mapRootBindError(err))
	}
	cur, err := e.projects.Get(ctx, p.ID)
	if err != nil {
		return projectFailure(r, err)
	}
	if !cur.CanEditMutableFields() {
		return projectFailure(r, projectapp.ErrInvalidTransition)
	}
	updated, err := e.spineUpdate(ctx, r, p.ID, p.Version, p, func(proj *project.Project) error {
		if err := projectroot.Bind(normalized, projectroot.Lock{ProjectID: proj.ID, ProjectCode: proj.ProjectCode, Name: proj.Name}); err != nil {
			return mapRootBindError(err)
		}
		if proj.RootPath != "" && proj.RootPath != normalized {
			_ = projectroot.RemoveLock(proj.RootPath)
		}
		proj.RootPath = normalized
		proj.TreeStatus = project.TreePending
		return nil
	})
	if err != nil {
		return projectFailure(r, err)
	}
	_ = cur
	return r.Ok(newProjectDTO(updated))
}

func mapRootBindError(err error) error {
	switch {
	case projectroot.IsBusy(err):
		return projectapp.ErrRootBusy
	case projectroot.IsReadonly(err):
		return projectapp.ErrRootReadonly
	case projectroot.IsInvalid(err):
		return projectapp.ErrRootInvalid
	default:
		return err
	}
}

func (e *Engine) spineUpdate(ctx context.Context, r bridge.Request, id string, version int64, request any, apply func(*project.Project) error) (project.Project, error) {
	key := strings.TrimSpace(r.IdempotencyKey)
	if key == "" {
		key = ulid.Make().String()
	}
	return e.projects.Mutate(ctx, key, projectMutationActor, "project.update", id, version, request, apply)
}

func handleProjectTreeGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.tree.get 参数无效", false)
	}
	if !projectServiceAvailable(e.projects) {
		return r.Fail("STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
	proj, err := e.projects.Get(ctx, p.ProjectID)
	if err != nil {
		return projectFailure(r, err)
	}
	if proj.RootPath != "" {
		_ = projectroot.Bind(proj.RootPath, projectroot.Lock{ProjectID: proj.ID, ProjectCode: proj.ProjectCode, Name: proj.Name})
	}
	tree, err := e.resolveProjectTree(proj)
	if err != nil {
		return projectFailure(r, projectapp.ErrTreeInvalid)
	}
	var receipt any
	if proj.RootPath != "" {
		if raw, rerr := os.ReadFile(filepath.Join(proj.RootPath, ".lunitide", "tree-receipt.json")); rerr == nil {
			_ = json.Unmarshal(raw, &receipt)
		}
	}
	return r.Ok(map[string]any{"tree": tree, "receipt": receipt, "treeStatus": proj.TreeStatus, "rootPath": proj.RootPath})
}

func handleProjectTreePut(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string          `json:"projectId"`
		Version   int64           `json:"version"`
		Tree      json.RawMessage `json:"tree"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || p.Version < 1 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.tree.put 参数无效", false)
	}
	tree, err := projecttree.Parse(p.Tree)
	if err != nil {
		return projectFailure(r, projectapp.ErrTreeInvalid)
	}
	if !projectServiceAvailable(e.projects) {
		return r.Fail("STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
	proj, err := e.projects.Get(ctx, p.ProjectID)
	if err != nil {
		return projectFailure(r, err)
	}
	if proj.RootPath == "" {
		return projectFailure(r, projectapp.ErrRootRequired)
	}
	if !proj.CanEditMutableFields() {
		return projectFailure(r, projectapp.ErrInvalidTransition)
	}
	body, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return projectFailure(r, err)
	}
	meta := filepath.Join(proj.RootPath, ".lunitide")
	if err = os.MkdirAll(meta, 0o755); err != nil {
		return projectFailure(r, projectapp.ErrRootReadonly)
	}
	if err = os.WriteFile(filepath.Join(meta, "project-tree.json"), body, 0o644); err != nil {
		return projectFailure(r, projectapp.ErrRootReadonly)
	}
	digest, _ := projecttree.Digest(tree)
	updated, err := e.spineUpdate(ctx, r, p.ProjectID, p.Version, p, func(cur *project.Project) error {
		cur.TreeDigest = digest
		if cur.TreeStatus == project.TreeNone {
			cur.TreeStatus = project.TreePending
		}
		return nil
	})
	if err != nil {
		return projectFailure(r, err)
	}
	return r.Ok(map[string]any{"tree": tree, "project": newProjectDTO(updated)})
}

func handleProjectTreeMaterialize(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		Version   int64  `json:"version"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || p.Version < 1 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.tree.materialize 参数无效", false)
	}
	if !projectServiceAvailable(e.projects) {
		return r.Fail("STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
	proj, err := e.projects.Get(ctx, p.ProjectID)
	if err != nil {
		return projectFailure(r, err)
	}
	if proj.RootPath == "" {
		return projectFailure(r, projectapp.ErrRootRequired)
	}
	if !proj.CanEditMutableFields() {
		return projectFailure(r, projectapp.ErrInvalidTransition)
	}
	tree, err := e.resolveProjectTree(proj)
	if err != nil {
		return projectFailure(r, projectapp.ErrTreeInvalid)
	}
	receipt, err := projecttree.Materialize(proj.RootPath, tree)
	if err != nil {
		_, _ = e.spineUpdate(ctx, r, p.ProjectID, p.Version, p, func(cur *project.Project) error {
			cur.TreeStatus = project.TreeFailed
			cur.TreeDigest = receipt.Digest
			return nil
		})
		return projectFailure(r, projectapp.ErrTreeFailed)
	}
	updated, err := e.spineUpdate(ctx, r, p.ProjectID, p.Version, p, func(cur *project.Project) error {
		cur.TreeStatus = project.TreeReady
		cur.TreeDigest = receipt.Digest
		cur.TreeGeneratedAt = nowRFC3339()
		return nil
	})
	if err != nil {
		return projectFailure(r, err)
	}
	return r.Ok(map[string]any{"receipt": receipt, "project": newProjectDTO(updated)})
}

func handleProjectExecutorSet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID       string `json:"id"`
		Version  int64  `json:"version"`
		Executor string `json:"executor"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) || p.Version < 1 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.executor.set 参数无效", false)
	}
	exec := project.Executor(p.Executor)
	if project.NormalizeExecutor(exec) != exec && exec != "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.executor.set 参数无效", false)
	}
	exec = project.NormalizeExecutor(exec)
	if !e.executorAvailable(exec) {
		return projectFailure(r, projectapp.ErrExecutorUnavailable)
	}
	if !projectServiceAvailable(e.projects) {
		return r.Fail("STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
	cur, err := e.projects.Get(ctx, p.ID)
	if err != nil {
		return projectFailure(r, err)
	}
	if !cur.CanEditMutableFields() {
		return projectFailure(r, projectapp.ErrInvalidTransition)
	}
	updated, err := e.spineUpdate(ctx, r, p.ID, p.Version, p, func(proj *project.Project) error {
		proj.DefaultExecutor = exec
		return nil
	})
	if err != nil {
		return projectFailure(r, err)
	}
	return r.Ok(newProjectDTO(updated))
}

func handleProjectTaskOpen(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID     string `json:"projectId"`
		ItemID        string `json:"itemId"`
		Executor      string `json:"executor"`
		WorkSessionID string `json:"workSessionId"`
		HubThreadID   string `json:"hubThreadId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || strings.TrimSpace(p.ItemID) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.task.open 参数无效", false)
	}
	if !projectServiceAvailable(e.projects) {
		return r.Fail("STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
	if failure := rejectIfProjectReadOnly(e, ctx, r, p.ProjectID); failure != nil {
		return *failure
	}
	proj, err := e.projects.Get(ctx, p.ProjectID)
	if err != nil {
		return projectFailure(r, err)
	}
	if proj.RootPath == "" || proj.TreeStatus != project.TreeReady {
		return projectFailure(r, projectapp.ErrTreeRequired)
	}
	if proj.DBStatus != project.DBReady {
		return projectFailure(r, projectapp.ErrDBRequired)
	}
	if !e.interfacePhaseConfirmed(ctx, proj) {
		return projectFailure(r, projectapp.ErrInterfaceRequired)
	}
	devPhase := project.DevPhase(proj.Type)
	dev, rec, err := e.loadChecklist(ctx, proj.ID, devPhase, "dev_checklist")
	if err != nil {
		return projectFailure(r, err)
	}
	if len(dev.Items) == 0 && proj.Type != project.TypeOperations {
		feat, _, ferr := e.loadChecklist(ctx, proj.ID, project.DesignPhase(proj.Type), "feature_dev_list")
		if ferr == nil {
			dev = projecttask.ImportMissing(dev, feat)
		}
	}
	exec := project.NormalizeExecutor(project.Executor(p.Executor))
	if p.Executor == "" {
		if _, item, ok := projecttask.Find(dev, p.ItemID); ok && item.Executor != "" {
			exec = project.NormalizeExecutor(item.Executor)
		} else {
			exec = project.NormalizeExecutor(proj.DefaultExecutor)
		}
	}
	if !e.executorAvailable(exec) {
		return projectFailure(r, projectapp.ErrExecutorUnavailable)
	}
	tree, _ := e.resolveProjectTree(proj)
	brief, err := projecttask.Open(&dev, p.ItemID, proj.RootPath, tree.CodeRoot, exec)
	if err != nil {
		return projectFailure(r, err)
	}
	if _, item, ok := projecttask.Find(dev, p.ItemID); ok {
		brief.Text = strings.TrimSpace(brief.Text + "\n" + e.designExcerpt(ctx, proj, item))
	}
	hubThreadID := ""
	if i, item, ok := projecttask.Find(dev, p.ItemID); ok {
		if p.WorkSessionID != "" {
			item.WorkSessionID = p.WorkSessionID
		}
		if p.HubThreadID != "" {
			item.HubThreadID = p.HubThreadID
		}
		dev.Items[i] = item
		hubThreadID = item.HubThreadID
	}
	status := deliverable.StatusReview
	if rec.Status == deliverable.StatusApproved || rec.Status == deliverable.StatusImmutable {
		status = deliverable.StatusReview
	}
	if err = e.saveChecklist(ctx, proj, devPhase, "dev_checklist", "开发检查清单", dev, status); err != nil {
		return projectFailure(r, err)
	}
	return r.Ok(map[string]any{"brief": brief, "executor": exec, "itemId": p.ItemID, "hubThreadId": hubThreadID})
}

func handleProjectTaskReport(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID   string `json:"projectId"`
		ItemID      string `json:"itemId"`
		Executor    string `json:"executor"`
		Summary     string `json:"summary"`
		HubThreadID string `json:"hubThreadId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || strings.TrimSpace(p.ItemID) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.task.report 参数无效", false)
	}
	if !projectServiceAvailable(e.projects) {
		return r.Fail("STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
	if failure := rejectIfProjectReadOnly(e, ctx, r, p.ProjectID); failure != nil {
		return *failure
	}
	proj, err := e.projects.Get(ctx, p.ProjectID)
	if err != nil {
		return projectFailure(r, err)
	}
	exec := project.NormalizeExecutor(project.Executor(p.Executor))
	if p.Executor == "" {
		exec = project.NormalizeExecutor(proj.DefaultExecutor)
	}
	devPhase := project.DevPhase(proj.Type)
	dev, rec, err := e.loadChecklist(ctx, proj.ID, devPhase, "dev_checklist")
	if err != nil {
		return projectFailure(r, err)
	}
	if err = projecttask.Report(&dev, p.ItemID, p.Summary, exec, p.HubThreadID); err != nil {
		return projectFailure(r, err)
	}
	if err = e.saveChecklist(ctx, proj, devPhase, "dev_checklist", "开发检查清单", dev, checklistWriteStatus(rec.Status)); err != nil {
		return projectFailure(r, err)
	}
	return r.Ok(map[string]any{"itemId": p.ItemID, "reported": true})
}

func handleProjectTaskComplete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID    string `json:"projectId"`
		ItemID       string `json:"itemId"`
		SelfTestPass bool   `json:"selfTestPass"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || strings.TrimSpace(p.ItemID) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.task.complete 参数无效", false)
	}
	if !projectServiceAvailable(e.projects) {
		return r.Fail("STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
	if failure := rejectIfProjectReadOnly(e, ctx, r, p.ProjectID); failure != nil {
		return *failure
	}
	proj, err := e.projects.Get(ctx, p.ProjectID)
	if err != nil {
		return projectFailure(r, err)
	}
	devPhase := project.DevPhase(proj.Type)
	testPhase := project.TestPhase(proj.Type)
	dev, rec, err := e.loadChecklist(ctx, proj.ID, devPhase, "dev_checklist")
	if err != nil {
		return projectFailure(r, err)
	}
	test, testRec, err := e.loadChecklist(ctx, proj.ID, testPhase, "test_checklist")
	if err != nil {
		return projectFailure(r, err)
	}
	if i, item, ok := projecttask.Find(dev, p.ItemID); ok {
		item.SelfTestPass = p.SelfTestPass
		dev.Items[i] = item
	}
	if err = projecttask.Complete(&dev, &test, p.ItemID); err != nil {
		return projectFailure(r, err)
	}
	if err = e.saveChecklist(ctx, proj, devPhase, "dev_checklist", "开发检查清单", dev, checklistWriteStatus(rec.Status)); err != nil {
		return projectFailure(r, err)
	}
	if len(test.Items) > 0 {
		if err = e.saveChecklist(ctx, proj, testPhase, "test_checklist", "测试检查清单", test, checklistWriteStatus(testRec.Status)); err != nil {
			return projectFailure(r, err)
		}
	}
	return r.Ok(map[string]any{"itemId": p.ItemID, "completed": true})
}

func handleProjectTaskReturnFromTest(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID  string `json:"projectId"`
		TestItemID string `json:"testItemId"`
		Reason     string `json:"reason"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || strings.TrimSpace(p.TestItemID) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.task.returnFromTest 参数无效", false)
	}
	if !projectServiceAvailable(e.projects) {
		return r.Fail("STORAGE_UNAVAILABLE", "项目数据暂时不可用", true)
	}
	if failure := rejectIfProjectReadOnly(e, ctx, r, p.ProjectID); failure != nil {
		return *failure
	}
	proj, err := e.projects.Get(ctx, p.ProjectID)
	if err != nil {
		return projectFailure(r, err)
	}
	testItem, err := e.returnTestToSource(ctx, proj, p.TestItemID, p.Reason)
	if err != nil {
		return projectFailure(r, err)
	}
	scenes, sceneRec, serr := e.loadChecklist(ctx, proj.ID, 7, "integration_test_list")
	if serr != nil {
		return projectFailure(r, serr)
	}
	if len(scenes.Items) > 0 {
		projectboard.ResetScenesForTest(&scenes, p.TestItemID)
		if err = e.saveChecklist(ctx, proj, 7, "integration_test_list", "集成测试场景清单", scenes, checklistWriteStatus(sceneRec.Status)); err != nil {
			return projectFailure(r, err)
		}
	}
	return r.Ok(map[string]any{"testItemId": p.TestItemID, "devItemId": testItem.SourceID, "returned": true, "targetKind": testItem.SourceKind})
}

func (e *Engine) returnTestToSource(ctx context.Context, proj project.Project, testItemID, reason string) (projecttask.Item, error) {
	var empty projecttask.Item
	testPhase := project.TestPhase(proj.Type)
	test, _, err := e.loadChecklist(ctx, proj.ID, testPhase, "test_checklist")
	if err != nil {
		return empty, err
	}
	_, testItem, ok := projecttask.Find(test, testItemID)
	if !ok {
		return empty, projectapp.ErrTaskNotFound
	}
	targetPhase, targetType, targetTitle := project.DevPhase(proj.Type), "dev_checklist", "开发检查清单"
	if testItem.SourceKind == "interface" {
		targetPhase, targetType, targetTitle = project.InterfacePhase(proj.Type), "interface_list", "接口清单"
	}
	target, _, err := e.loadChecklist(ctx, proj.ID, targetPhase, targetType)
	if err != nil {
		return empty, err
	}
	if err = projecttask.ReturnFromTest(&target, &test, testItemID, reason); err != nil {
		return empty, err
	}
	if err = e.saveChecklist(ctx, proj, testPhase, "test_checklist", "测试检查清单", test, deliverable.StatusReview); err != nil {
		return empty, err
	}
	if err = e.saveChecklist(ctx, proj, targetPhase, targetType, targetTitle, target, deliverable.StatusReview); err != nil {
		return empty, err
	}
	return testItem, nil
}

func checklistWriteStatus(current deliverable.Status) deliverable.Status {
	if current == deliverable.StatusApproved || current == deliverable.StatusImmutable {
		return deliverable.StatusReview
	}
	if current == "" {
		return deliverable.StatusReview
	}
	return current
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func (e *Engine) interfacePhaseConfirmed(ctx context.Context, proj project.Project) bool {
	if e.stages != nil {
		items, err := e.stages.List(ctx, stage.Filter{ProjectID: proj.ID})
		if err == nil {
			found := false
			for _, item := range items {
				if item.Phase != project.InterfacePhase(proj.Type) {
					continue
				}
				found = true
				if item.Status == "completed" || item.Status == "approved" {
					return true
				}
			}
			if found {
				return false
			}
		}
	}
	_, rec, err := e.loadChecklist(ctx, proj.ID, project.InterfacePhase(proj.Type), "interface_list")
	return err == nil && (rec.Status == deliverable.StatusApproved || rec.Status == deliverable.StatusImmutable)
}
