package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/deliverable"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/projectattachment"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/projectboard"
	"github.com/lunitide/lunitide/internal/projectgen"
	"github.com/lunitide/lunitide/internal/projectrules"
	"github.com/lunitide/lunitide/internal/projectschema"
	"github.com/lunitide/lunitide/internal/projectsync"
	"github.com/lunitide/lunitide/internal/projecttask"
	"github.com/lunitide/lunitide/internal/projecttestkit"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

func handleProjectFactory(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	switch r.Method {
	case "project.interview.get":
		return handleProjectInterviewGet(e, ctx, r)
	case "project.interview.save":
		return handleProjectInterviewSave(e, ctx, r)
	case "project.deliverable.generate":
		return handleProjectDeliverableGenerate(e, ctx, r)
	case "project.rules.materialize":
		return handleProjectRulesMaterialize(e, ctx, r)
	case "project.rules.get":
		return handleProjectRulesGet(e, ctx, r)
	case "project.schema.get":
		return handleProjectSchemaGet(e, ctx, r)
	case "project.schema.put":
		return handleProjectSchemaPut(e, ctx, r)
	case "project.schema.materialize":
		return handleProjectSchemaMaterialize(e, ctx, r)
	case "project.schema.verify":
		return handleProjectSchemaVerify(e, ctx, r)
	case "project.db.bind":
		return handleProjectDBBind(e, ctx, r)
	case "project.board.get":
		return handleProjectBoardGet(e, ctx, r)
	case "project.board.put":
		return handleProjectBoardPut(e, ctx, r)
	case "project.board.sync":
		return handleProjectBoardSync(e, ctx, r)
	case "project.board.stats":
		return handleProjectBoardStats(e, ctx, r)
	case "project.board.item.open":
		return handleProjectBoardItemOpen(e, ctx, r)
	case "project.test.run":
		return handleProjectTestRun(e, ctx, r)
	case "project.release.sync":
		return handleProjectReleaseSync(e, ctx, r)
	default:
		return r.Fail("BRIDGE_SCHEMA_INVALID", "未知的项目方法", false)
	}
}

func handleProjectInterviewGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.interview.get 参数无效", false)
	}
	proj, err := e.projects.Get(ctx, p.ProjectID)
	if err != nil {
		return projectFailure(r, err)
	}
	if proj.RootPath == "" {
		return projectFailure(r, projectapp.ErrRootRequired)
	}
	doc, err := projectgen.Load(proj.RootPath)
	if err != nil {
		doc = projectgen.InterviewDoc{Version: 1, ProjectID: proj.ID, Phases: map[string]projectgen.PhaseInterview{}}
	}
	return r.Ok(map[string]any{"interview": doc, "phase1": projectgen.Phase1Questions(), "phase2": projectgen.Phase2Questions()})
}

func handleProjectInterviewSave(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID   string              `json:"projectId"`
		Phase       int                 `json:"phase"`
		Mode        string              `json:"mode"`
		CompletedAt string              `json:"completedAt"`
		Answers     []projectgen.Answer `json:"answers"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || (p.Phase != 1 && p.Phase != 2) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.interview.save 参数无效", false)
	}
	proj, fail := e.factoryMutateRoot(ctx, r, p.ProjectID)
	if fail != nil {
		return *fail
	}
	doc, err := projectgen.SavePhase(proj.RootPath, proj.ID, p.Phase, p.Mode, p.CompletedAt, p.Answers)
	if err != nil {
		return projectFailure(r, err)
	}
	return r.Ok(map[string]any{"interview": doc})
}

func handleProjectDeliverableGenerate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID        string   `json:"projectId"`
		Phase            int      `json:"phase"`
		DocumentTypes    []string `json:"documentTypes"`
		OverwriteDrafts  bool     `json:"overwriteDrafts"`
		CouncilSynthesis string   `json:"councilSynthesis"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || (p.Phase != 1 && p.Phase != 2) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.deliverable.generate 参数无效", false)
	}
	proj, fail := e.factoryMutateRoot(ctx, r, p.ProjectID)
	if fail != nil {
		return *fail
	}
	types := p.DocumentTypes
	if len(types) == 0 {
		types = projectgen.PhaseDocumentTypes(p.Phase)
	}
	answers := map[string]string{}
	if interview, ierr := projectgen.Load(proj.RootPath); ierr == nil {
		if phase, ok := interview.Phases[strconv.Itoa(p.Phase)]; ok {
			answers = projectgen.AnswerMap(phase)
		}
	}
	prior := e.priorDeliverableText(ctx, proj.ID, 1)
	created := 0
	for _, key := range types {
		items, _ := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: proj.ID, Phase: p.Phase})
		var existing deliverable.ProjectDeliverable
		for _, item := range items {
			if item.DocumentType == key {
				existing = item
				break
			}
		}
		approved := existing.Status == deliverable.StatusApproved || existing.Status == deliverable.StatusImmutable
		if approved {
			continue
		}
		if existing.AttachmentID != "" && !p.OverwriteDrafts && !approved {
			continue
		}
		title := key
		for _, def := range projectgen.PhaseDocumentTypes(p.Phase) {
			if def == key {
				title = key
			}
		}
		body, gerr := projectgen.Render(projectgen.Input{
			ProjectName: proj.Name, ProjectCode: proj.ProjectCode, ProjectType: string(proj.Type),
			RootPath: proj.RootPath, PhaseLabel: phaseLabel(proj.Type, p.Phase), DocumentType: key, Title: title,
			Answers: answers, Prior: prior, Council: p.CouncilSynthesis,
			AlreadyApproved: approved, OverwriteOK: p.OverwriteDrafts,
			TemplateBody:        e.templateBody(ctx, existing.TemplateID),
			IncompleteInterview: !projectgen.PhaseAnswersComplete(p.Phase, answers),
		})
		if gerr != nil {
			if gerr == projectgen.ErrGenerateSkipApproved {
				continue
			}
			return projectFailure(r, gerr)
		}
		if err := e.writeDeliverableBody(ctx, proj, p.Phase, key, title, []byte(body)); err != nil {
			return projectFailure(r, err)
		}
		created++
	}
	return r.Ok(map[string]any{"generated": created})
}

func handleProjectRulesMaterialize(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		Version   int64  `json:"version"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.rules.materialize 参数无效", false)
	}
	proj, fail := e.factoryMutateRoot(ctx, r, p.ProjectID)
	if fail != nil {
		return *fail
	}
	man, err := e.materializeProjectRules(ctx, proj)
	if err != nil {
		return projectFailure(r, err)
	}
	updated, err := e.spineUpdate(ctx, r, p.ProjectID, p.Version, p, func(cur *project.Project) error {
		cur.RulesDigest = man.Digest
		cur.RulesMaterializedAt = man.MaterializedAt
		return nil
	})
	if err != nil {
		return projectFailure(r, err)
	}
	return r.Ok(map[string]any{"project": newProjectDTO(updated), "manifest": man})
}

func handleProjectRulesGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.rules.get 参数无效", false)
	}
	proj, err := e.projects.Get(ctx, p.ProjectID)
	if err != nil {
		return projectFailure(r, err)
	}
	if proj.RootPath == "" {
		return projectFailure(r, projectapp.ErrRootRequired)
	}
	man, err := projectrules.LoadManifest(proj.RootPath)
	if err != nil {
		if os.IsNotExist(err) {
			return r.Ok(map[string]any{"manifest": projectrules.Manifest{Version: 1}, "guidance": projectrules.Guidance(proj.RootPath)})
		}
		return projectFailure(r, err)
	}
	return r.Ok(map[string]any{"manifest": man, "guidance": projectrules.Guidance(proj.RootPath)})
}

func handleProjectSchemaGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	proj, err := e.factoryProject(ctx, r)
	if err != nil {
		return projectFailure(r, err)
	}
	schema, err := projectschema.ReadFile(proj.RootPath)
	if err != nil {
		schema = projectschema.Schema{Version: 1, Dialect: "sqlite"}
	}
	return r.Ok(map[string]any{"schema": schema, "dbPath": proj.DBPath, "dbStatus": proj.DBStatus})
}

func handleProjectSchemaPut(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string               `json:"projectId"`
		Schema    projectschema.Schema `json:"schema"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.schema.put 参数无效", false)
	}
	proj, fail := e.factoryMutateRoot(ctx, r, p.ProjectID)
	if fail != nil {
		return *fail
	}
	if err := projectschema.WriteFile(proj.RootPath, p.Schema); err != nil {
		return projectFailure(r, err)
	}
	return r.Ok(map[string]any{"schema": p.Schema})
}

func handleProjectSchemaMaterialize(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		Version   int64  `json:"version"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || p.Version < 1 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.schema.materialize 参数无效", false)
	}
	proj, fail := e.factoryMutateRoot(ctx, r, p.ProjectID)
	if fail != nil {
		return *fail
	}
	schema, err := projectschema.ReadFile(proj.RootPath)
	if err != nil {
		return projectFailure(r, projectapp.ErrSchemaInvalid)
	}
	path := proj.DBPath
	if path == "" {
		path = projectschema.DefaultDBPath(proj.RootPath)
	}
	if err = projectschema.Materialize(path, schema); err != nil {
		_, _ = e.spineUpdate(ctx, r, p.ProjectID, p.Version, p, func(cur *project.Project) error {
			cur.DBStatus = project.DBFailed
			cur.DBPath = path
			return nil
		})
		return projectFailure(r, err)
	}
	raw, _ := json.Marshal(schema)
	sum := sha256.Sum256(raw)
	updated, err := e.spineUpdate(ctx, r, p.ProjectID, p.Version, p, func(cur *project.Project) error {
		cur.DBStatus = project.DBPending
		cur.DBPath = path
		cur.DBDigest = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		return projectFailure(r, err)
	}
	return r.Ok(map[string]any{"project": newProjectDTO(updated)})
}

func handleProjectSchemaVerify(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		Version   int64  `json:"version"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || p.Version < 1 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.schema.verify 参数无效", false)
	}
	proj, fail := e.factoryMutateRoot(ctx, r, p.ProjectID)
	if fail != nil {
		return *fail
	}
	schema, err := projectschema.ReadFile(proj.RootPath)
	if err != nil {
		return projectFailure(r, projectapp.ErrSchemaInvalid)
	}
	path := proj.DBPath
	if path == "" {
		path = projectschema.DefaultDBPath(proj.RootPath)
	}
	if err = projectschema.Verify(path, schema); err != nil {
		_, _ = e.spineUpdate(ctx, r, p.ProjectID, p.Version, p, func(cur *project.Project) error {
			cur.DBStatus = project.DBFailed
			cur.DBPath = path
			return nil
		})
		return projectFailure(r, projectapp.ErrDBIncomplete)
	}
	updated, err := e.spineUpdate(ctx, r, p.ProjectID, p.Version, p, func(cur *project.Project) error {
		cur.DBStatus = project.DBReady
		cur.DBPath = path
		cur.DBVerifiedAt = time.Now().UTC().Format(time.RFC3339)
		return nil
	})
	if err != nil {
		return projectFailure(r, err)
	}
	return r.Ok(map[string]any{"project": newProjectDTO(updated)})
}

func handleProjectDBBind(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		Version   int64  `json:"version"`
		DBPath    string `json:"dbPath"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || p.Version < 1 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.db.bind 参数无效", false)
	}
	proj, fail := e.factoryMutateRoot(ctx, r, p.ProjectID)
	if fail != nil {
		return *fail
	}
	path, err := projectschema.BindPath(proj.RootPath, p.DBPath)
	if err != nil {
		return projectFailure(r, err)
	}
	updated, err := e.spineUpdate(ctx, r, p.ProjectID, p.Version, p, func(cur *project.Project) error {
		cur.DBPath = path
		cur.DBStatus = project.DBPending
		return nil
	})
	if err != nil {
		return projectFailure(r, err)
	}
	return r.Ok(newProjectDTO(updated))
}

func handleProjectBoardGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	kind, proj, err := e.decodeBoard(ctx, r)
	if err != nil {
		return projectFailure(r, err)
	}
	doc, _, err := e.loadBoard(ctx, proj, kind)
	if err != nil {
		return projectFailure(r, err)
	}
	return r.Ok(map[string]any{"board": doc, "stats": projectboard.Summarize(doc), "statsText": projectboard.FormatStats(projectboard.Summarize(doc))})
}

func handleProjectBoardPut(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string          `json:"projectId"`
		BoardKind string          `json:"boardKind"`
		Board     projecttask.Doc `json:"board"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.board.put 参数无效", false)
	}
	proj, fail := e.factoryMutateProject(ctx, r, p.ProjectID)
	if fail != nil {
		return *fail
	}
	kind := projectboard.Kind(p.BoardKind)
	current, _, err := e.loadBoard(ctx, proj, kind)
	if err != nil {
		return projectFailure(r, err)
	}
	if !sameBoardIdentity(current, p.Board) {
		return projectFailure(r, projectapp.ErrInvalidTransition)
	}
	if err = validateBoardProgress(current, p.Board); err != nil {
		return projectFailure(r, err)
	}
	phase, docType, title := boardMeta(proj.Type, kind)
	if err = e.saveChecklist(ctx, proj, phase, docType, title, p.Board, deliverable.StatusReview); err != nil {
		return projectFailure(r, err)
	}
	return r.Ok(map[string]any{"board": p.Board, "stats": projectboard.Summarize(p.Board)})
}

func handleProjectBoardSync(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	kind, proj, err := e.decodeBoard(ctx, r)
	if err != nil {
		return projectFailure(r, err)
	}
	if failure := rejectIfProjectReadOnly(e, ctx, r, proj.ID); failure != nil {
		return *failure
	}
	board, _, err := e.loadBoard(ctx, proj, kind)
	if err != nil {
		return projectFailure(r, err)
	}
	source, err := e.boardSource(ctx, proj, kind)
	if err != nil {
		return projectFailure(r, err)
	}
	next := projectboard.Sync(board, source)
	phase, docType, title := boardMeta(proj.Type, kind)
	if err = e.saveChecklist(ctx, proj, phase, docType, title, next, deliverable.StatusReview); err != nil {
		return projectFailure(r, err)
	}
	st := projectboard.Summarize(next)
	return r.Ok(map[string]any{"board": next, "stats": st, "statsText": projectboard.FormatStats(st), "dirty": projectboard.Dirty(next)})
}

func checklistBoardKind(documentType string) projectboard.Kind {
	switch documentType {
	case "api_list", "interface_list":
		return projectboard.KindInterface
	case "feature_dev_list", "dev_checklist":
		return projectboard.KindDev
	case "test_checklist":
		return projectboard.KindTest
	case "integration_test_list":
		return projectboard.KindIntegration
	default:
		return ""
	}
}

func (e *Engine) checklistBoardDirty(ctx context.Context, proj project.Project, documentType string) bool {
	switch documentType {
	case "interface_list", "dev_checklist", "test_checklist", "integration_test_list":
		return false
	}
	kind := checklistBoardKind(documentType)
	if kind == "" || kind == projectboard.KindIntegration {
		return false
	}
	board, _, err := e.loadBoard(ctx, proj, kind)
	if err != nil {
		return false
	}
	source, err := e.boardSource(ctx, proj, kind)
	if err != nil {
		return false
	}
	return projectboard.Dirty(projectboard.Sync(board, source))
}

func handleProjectBoardStats(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	kind, proj, err := e.decodeBoard(ctx, r)
	if err != nil {
		return projectFailure(r, err)
	}
	doc, _, err := e.loadBoard(ctx, proj, kind)
	if err != nil {
		return projectFailure(r, err)
	}
	st := projectboard.Summarize(doc)
	return r.Ok(map[string]any{"stats": st, "statsText": projectboard.FormatStats(st)})
}

func handleProjectBoardItemOpen(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		BoardKind string `json:"boardKind"`
		ItemID    string `json:"itemId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || strings.TrimSpace(p.ItemID) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.board.item.open 参数无效", false)
	}
	if p.BoardKind == "dev" {
		return handleProjectTaskOpen(e, ctx, r)
	}
	proj, fail := e.factoryMutateProject(ctx, r, p.ProjectID)
	if fail != nil {
		return *fail
	}
	kind := projectboard.Kind(p.BoardKind)
	doc, rec, err := e.loadBoard(ctx, proj, kind)
	if err != nil {
		return projectFailure(r, err)
	}
	i, item, ok := projecttask.Find(doc, p.ItemID)
	if !ok {
		return projectFailure(r, projectapp.ErrTaskNotFound)
	}
	if kind == projectboard.KindIntegration {
		tests, _, terr := e.loadBoard(ctx, proj, projectboard.KindTest)
		if terr != nil {
			return projectFailure(r, terr)
		}
		if err = projectboard.IntegrationReady(item, tests); err != nil {
			return projectFailure(r, err)
		}
	}
	item.Status = "in_progress"
	item.OpenedAt = time.Now().UTC().Format(time.RFC3339)
	doc.Items[i] = item
	phase, docType, title := boardMeta(proj.Type, kind)
	if err = e.saveChecklist(ctx, proj, phase, docType, title, doc, checklistWriteStatus(rec.Status)); err != nil {
		return projectFailure(r, err)
	}
	design := e.designExcerpt(ctx, proj, item)
	brief := "任务 " + item.ID + "：" + item.Title + "\n" + design + "\n约束：对照详细设计处理；完成后必须自测。"
	return r.Ok(map[string]any{"brief": map[string]any{"itemId": item.ID, "title": item.Title, "text": brief, "rootPath": proj.RootPath}, "itemId": item.ID})
}

func handleProjectTestRun(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		ItemID    string `json:"itemId"`
		Kind      string `json:"kind"`
		Command   string `json:"command"`
		Evidence  string `json:"evidence"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || p.ItemID == "" || p.Kind == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.test.run 参数无效", false)
	}
	proj, fail := e.factoryMutateRoot(ctx, r, p.ProjectID)
	if fail != nil {
		return *fail
	}
	test, rec, err := e.loadChecklist(ctx, proj.ID, project.TestPhase(proj.Type), "test_checklist")
	if err != nil {
		return projectFailure(r, err)
	}
	i, item, ok := projecttask.Find(test, p.ItemID)
	if !ok {
		integ, irec, ierr := e.loadChecklist(ctx, proj.ID, 7, "integration_test_list")
		if ierr == nil {
			if ii, it, iok := projecttask.Find(integ, p.ItemID); iok {
				test, rec, i, item, ok = integ, irec, ii, it, true
			}
		}
		if !ok {
			return projectFailure(r, projectapp.ErrTaskNotFound)
		}
	}
	schema, _ := projectschema.ReadFile(proj.RootPath)
	if strings.HasPrefix(item.ID, "S") || len(item.MemberIDs) > 0 {
		tests, _, terr := e.loadBoard(ctx, proj, projectboard.KindTest)
		if terr != nil {
			return projectFailure(r, terr)
		}
		if err = projectboard.IntegrationReady(item, tests); err != nil {
			return projectFailure(r, err)
		}
	}
	res, err := projecttestkit.Run(ctx, projecttestkit.RunInput{
		Kind: p.Kind, RootPath: proj.RootPath, DBPath: proj.DBPath, Schema: schema,
		Board: test, Item: item, Command: p.Command, Evidence: p.Evidence,
	})
	if err != nil {
		return projectFailure(r, err)
	}
	if item.KindResults == nil {
		item.KindResults = map[string]projecttask.KindResult{}
	}
	item.KindResults[p.Kind] = res
	scene := strings.HasPrefix(item.ID, "S") || len(item.MemberIDs) > 0
	if projecttestkit.RequiredPassed(item) {
		item.Status = "test_pass"
	} else if res.Status == "fail" {
		if scene {
			item.Status = "pending"
		} else {
			item.Status = "test_fail"
		}
	}
	test.Items[i] = item
	phase, docType, title := project.TestPhase(proj.Type), "test_checklist", "测试检查清单"
	if scene {
		phase, docType, title = 7, "integration_test_list", "集成测试场景清单"
	}
	if err = e.saveChecklist(ctx, proj, phase, docType, title, test, checklistWriteStatus(rec.Status)); err != nil {
		return projectFailure(r, err)
	}
	if scene && res.Status == "fail" {
		for _, mid := range item.MemberIDs {
			if _, err = e.returnTestToSource(ctx, proj, mid, "集成场景失败，按来源退回"); err != nil {
				return projectFailure(r, err)
			}
		}
	}
	return r.Ok(map[string]any{"result": res, "item": item})
}

func handleProjectReleaseSync(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		DestPath  string `json:"destPath"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "project.release.sync 参数无效", false)
	}
	proj, fail := e.factoryMutateRoot(ctx, r, p.ProjectID)
	if fail != nil {
		return *fail
	}
	rec, err := projectsync.SyncTree(proj.RootPath, p.DestPath, proj.ID)
	if err != nil {
		return projectFailure(r, err)
	}
	return r.Ok(map[string]any{"receipt": rec})
}

func (e *Engine) factoryProject(ctx context.Context, r bridge.Request) (project.Project, error) {
	var p struct {
		ProjectID string `json:"projectId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return project.Project{}, projectapp.ErrInvalidTransition
	}
	proj, err := e.projects.Get(ctx, p.ProjectID)
	if err != nil {
		return proj, err
	}
	if proj.RootPath == "" {
		return proj, projectapp.ErrRootRequired
	}
	return proj, nil
}

func (e *Engine) factoryMutateProject(ctx context.Context, r bridge.Request, projectID string) (project.Project, *bridge.Response) {
	if failure := rejectIfProjectReadOnly(e, ctx, r, projectID); failure != nil {
		return project.Project{}, failure
	}
	proj, err := e.projects.Get(ctx, projectID)
	if err != nil {
		resp := projectFailure(r, err)
		return project.Project{}, &resp
	}
	return proj, nil
}

func (e *Engine) factoryMutateRoot(ctx context.Context, r bridge.Request, projectID string) (project.Project, *bridge.Response) {
	proj, fail := e.factoryMutateProject(ctx, r, projectID)
	if fail != nil {
		return proj, fail
	}
	if proj.RootPath == "" {
		resp := projectFailure(r, projectapp.ErrRootRequired)
		return proj, &resp
	}
	return proj, nil
}

func (e *Engine) decodeBoard(ctx context.Context, r bridge.Request) (projectboard.Kind, project.Project, error) {
	var p struct {
		ProjectID string `json:"projectId"`
		BoardKind string `json:"boardKind"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return "", project.Project{}, projectapp.ErrInvalidTransition
	}
	proj, err := e.projects.Get(ctx, p.ProjectID)
	return projectboard.Kind(p.BoardKind), proj, err
}

func (e *Engine) loadBoard(ctx context.Context, proj project.Project, kind projectboard.Kind) (projecttask.Doc, deliverable.ProjectDeliverable, error) {
	phase, docType, _ := boardMeta(proj.Type, kind)
	return e.loadChecklist(ctx, proj.ID, phase, docType)
}

func (e *Engine) boardSource(ctx context.Context, proj project.Project, kind projectboard.Kind) (projecttask.Doc, error) {
	switch kind {
	case projectboard.KindInterface:
		iface, ifaceRec, ierr := e.loadChecklist(ctx, proj.ID, project.InterfacePhase(proj.Type), "interface_list")
		if ierr != nil {
			return projecttask.Doc{}, ierr
		}
		if len(iface.Items) > 0 {
			return iface, nil
		}
		if raw := e.attachmentBytes(ctx, ifaceRec.AttachmentID); len(raw) > 0 {
			open, oerr := projectboard.ParseOpenAPI(raw)
			if oerr == nil && len(open.Items) > 0 {
				return open, nil
			}
		}
		doc, _, err := e.loadChecklist(ctx, proj.ID, project.DesignPhase(proj.Type), "api_list")
		if err != nil {
			return projecttask.Doc{}, err
		}
		if len(doc.Items) > 0 {
			return doc, nil
		}
		if ifaceRec.AttachmentID != "" {
			return projecttask.Doc{}, projectapp.ErrBoardSourceInvalid
		}
		return doc, nil
	case projectboard.KindDev:
		if proj.Type == project.TypeOperations {
			doc, _, err := e.loadChecklist(ctx, proj.ID, 1, "req_task_list")
			return doc, err
		}
		doc, _, err := e.loadChecklist(ctx, proj.ID, project.DesignPhase(proj.Type), "feature_dev_list")
		return doc, err
	case projectboard.KindTest:
		iface, _, ierr := e.loadBoard(ctx, proj, projectboard.KindInterface)
		if ierr != nil {
			return projecttask.Doc{}, ierr
		}
		dev, _, derr := e.loadBoard(ctx, proj, projectboard.KindDev)
		if derr != nil {
			return projecttask.Doc{}, derr
		}
		return projectboard.BuildTestBoard(iface, dev), nil
	default:
		return projecttask.Doc{Version: 1}, nil
	}
}

func boardMeta(t project.Type, kind projectboard.Kind) (int, string, string) {
	switch kind {
	case projectboard.KindInterface:
		return project.InterfacePhase(t), "interface_list", "接口清单"
	case projectboard.KindDev:
		return project.DevPhase(t), "dev_checklist", "开发检查清单"
	case projectboard.KindTest:
		return project.TestPhase(t), "test_checklist", "测试检查清单"
	case projectboard.KindIntegration:
		return 7, "integration_test_list", "集成测试场景清单"
	default:
		return project.DevPhase(t), "dev_checklist", "开发检查清单"
	}
}

func (e *Engine) writeDeliverableBody(ctx context.Context, proj project.Project, phase int, documentType, title string, body []byte) error {
	sum := sha256.Sum256(body)
	id := ulid.Make().String()
	att := projectattachment.Attachment{
		ID: id, ProjectID: proj.ID, Phase: phase, Category: "phase_doc",
		FileName: documentType + ".md", MimeType: "text/markdown", FilePath: id,
		Digest: hex.EncodeToString(sum[:]),
	}
	if projectgen.IsChecklistType(documentType) {
		att.FileName = documentType + ".json"
		att.MimeType = "application/json"
	}
	if _, err := e.ingestProjectAttachment(ctx, att, body); err != nil {
		return err
	}
	_, err := e.deliverables.UpsertProjectDeliverable(ctx, deliverable.ProjectDeliverable{
		ProjectID: proj.ID, Phase: phase, DocumentType: documentType, Title: title,
		AttachmentID: id, Status: deliverable.StatusReview, Digest: hex.EncodeToString(sum[:]),
	})
	return err
}

func (e *Engine) priorDeliverableText(ctx context.Context, projectID string, phase int) string {
	if e.projectAttachmentFiles == nil {
		return ""
	}
	items, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: projectID, Phase: phase})
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, item := range items {
		if item.AttachmentID == "" || (item.Status != deliverable.StatusApproved && item.Status != deliverable.StatusImmutable) {
			continue
		}
		att, err := e.projectAttachments.GetProjectAttachment(ctx, item.AttachmentID)
		if err != nil {
			continue
		}
		raw, err := e.projectAttachmentFiles.ReadFile(ctx, att.FilePath)
		if err != nil {
			continue
		}
		text := string(raw)
		if len(text) > 12*1024 {
			text = text[:12*1024]
		}
		b.WriteString("## ")
		b.WriteString(item.Title)
		b.WriteString("\n")
		b.WriteString(text)
		b.WriteString("\n")
		if b.Len() > 64*1024 {
			break
		}
	}
	return b.String()
}

func (e *Engine) templateBody(ctx context.Context, templateID string) string {
	if templateID == "" || e.assets == nil || e.templateFiles == nil {
		return ""
	}
	tpl, err := e.assets.GetAssetTemplate(ctx, templateID)
	if err != nil || tpl.FilePath == "" {
		return ""
	}
	raw, err := e.templateFiles.ReadFile(ctx, tpl.FilePath)
	if err != nil {
		return ""
	}
	return string(raw)
}

func (e *Engine) designExcerpt(ctx context.Context, proj project.Project, item projecttask.Item) string {
	docType := "feature_detail"
	if item.SourceKind == "interface" || item.Method != "" {
		docType = "api_detail"
	}
	items, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: proj.ID, Phase: project.DesignPhase(proj.Type)})
	if err != nil {
		return "未精确匹配章节，请对照全文"
	}
	for _, rec := range items {
		if rec.DocumentType != docType || rec.AttachmentID == "" || e.projectAttachmentFiles == nil {
			continue
		}
		att, err := e.projectAttachments.GetProjectAttachment(ctx, rec.AttachmentID)
		if err != nil {
			continue
		}
		raw, err := e.projectAttachmentFiles.ReadFile(ctx, att.FilePath)
		if err != nil {
			continue
		}
		text := string(raw)
		if idx := strings.Index(text, item.ID); idx >= 0 {
			end := idx + 2000
			if end > len(text) {
				end = len(text)
			}
			return text[idx:end]
		}
		if len(text) > 2000 {
			return "未精确匹配章节，请对照全文\n" + text[:2000]
		}
		return "未精确匹配章节，请对照全文\n" + text
	}
	return "未精确匹配章节，请对照全文"
}

func (e *Engine) materializeProjectRules(ctx context.Context, proj project.Project) (projectrules.Manifest, error) {
	if proj.RootPath == "" {
		return projectrules.Manifest{}, projectapp.ErrRootRequired
	}
	dev := e.deliverableText(ctx, proj.ID, 1, "dev_standard")
	tech := e.deliverableText(ctx, proj.ID, 1, "tech_standard")
	biz := e.deliverableText(ctx, proj.ID, 1, "biz_standard")
	return projectrules.Materialize(proj.RootPath, projectrules.Input{
		ProjectID: proj.ID, DevStandard: dev, TechStandard: tech, BizStandard: biz,
		At: time.Now().UTC().Format(time.RFC3339),
	})
}

func (e *Engine) attachmentBytes(ctx context.Context, attachmentID string) []byte {
	if attachmentID == "" || !projectAttachmentStoreAvailable(e.projectAttachments) || e.projectAttachmentFiles == nil {
		return nil
	}
	att, err := e.projectAttachments.GetProjectAttachment(ctx, attachmentID)
	if err != nil {
		return nil
	}
	raw, err := e.projectAttachmentFiles.ReadFile(ctx, att.FilePath)
	if err != nil {
		return nil
	}
	return raw
}

func (e *Engine) deliverableText(ctx context.Context, projectID string, phase int, documentType string) string {
	items, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: projectID, Phase: phase})
	if err != nil || e.projectAttachmentFiles == nil {
		return ""
	}
	for _, rec := range items {
		if rec.DocumentType != documentType || rec.AttachmentID == "" {
			continue
		}
		att, err := e.projectAttachments.GetProjectAttachment(ctx, rec.AttachmentID)
		if err != nil {
			return ""
		}
		raw, err := e.projectAttachmentFiles.ReadFile(ctx, att.FilePath)
		if err != nil {
			return ""
		}
		return string(raw)
	}
	return ""
}

func phaseLabel(t project.Type, phase int) string {
	if t == project.TypeOperations {
		switch phase {
		case 1:
			return "需求架构规范"
		case 2:
			return "数据库"
		}
		return "阶段"
	}
	if phase == 1 {
		return "需求架构规范"
	}
	if phase == 2 {
		return "方案和UI设计"
	}
	return "阶段"
}

func (e *Engine) executeDeliverableDraft(ctx context.Context, sessionID string, args json.RawMessage) (toolruntime.Result, error) {
	var p struct {
		DocumentType string `json:"documentType"`
		Title        string `json:"title"`
		Markdown     string `json:"markdown"`
	}
	if json.Unmarshal(args, &p) != nil || len([]byte(p.Markdown)) < 32 || strings.TrimSpace(p.DocumentType) == "" {
		return toolruntime.Result{Output: "ok:false\n交付物草稿参数无效"}, nil
	}
	getter, ok := e.sessions.(sessionLookup)
	if !ok {
		return toolruntime.Result{Output: "ok:false\n当前不是项目阶段会话"}, nil
	}
	sess, err := getter.Get(ctx, sessionID)
	if err != nil || !strings.HasPrefix(sess.Title, "phase:") {
		return toolruntime.Result{Output: "ok:false\n当前不是项目阶段会话"}, nil
	}
	rest := strings.TrimPrefix(sess.Title, "phase:")
	phase := 0
	if i := strings.Index(rest, ":"); i > 0 {
		phase, _ = strconv.Atoi(rest[:i])
	}
	if phase < 1 || phase > 9 || !projectServiceAvailable(e.projects) {
		return toolruntime.Result{Output: "ok:false\n当前不是项目阶段会话"}, nil
	}
	proj, err := e.projects.Get(ctx, sess.ProjectID)
	if err != nil {
		return toolruntime.Result{Output: "ok:false\n项目不存在"}, nil
	}
	if !proj.CanEditMutableFields() {
		return toolruntime.Result{Output: "ok:false\n项目已只读，不能写入交付物"}, nil
	}
	title := strings.TrimSpace(p.Title)
	if title == "" {
		title = p.DocumentType
	}
	if err = e.writeDeliverableBody(ctx, proj, phase, p.DocumentType, title, []byte(p.Markdown)); err != nil {
		return toolruntime.Result{Output: "ok:false\n写入失败"}, nil
	}
	return toolruntime.Result{Output: "ok:true\n已写入审核中的交付物附件，不会自动确认。"}, nil
}

func validateBoardProgress(current, next projecttask.Doc) error {
	index := map[string]projecttask.Item{}
	for _, item := range current.Items {
		index[item.ID] = item
	}
	for _, item := range next.Items {
		prev := index[item.ID]
		if item.Status == "dev_done" && prev.Status != "dev_done" {
			if !item.SelfTestPass || strings.TrimSpace(item.LastResultSummary) == "" {
				return projectapp.ErrSelfTestRequired
			}
		}
		if item.Status == "test_pass" && prev.Status != "test_pass" {
			if !projecttestkit.RequiredPassed(item) {
				return projectapp.ErrTestOpen
			}
		}
	}
	return nil
}

func sameBoardIDs(a, b projecttask.Doc) bool {
	if len(a.Items) != len(b.Items) {
		return false
	}
	seen := map[string]bool{}
	for _, item := range a.Items {
		seen[item.ID] = true
	}
	for _, item := range b.Items {
		if !seen[item.ID] {
			return false
		}
	}
	return true
}

func sameBoardIdentity(a, b projecttask.Doc) bool {
	if !sameBoardIDs(a, b) {
		return false
	}
	index := map[string]projecttask.Item{}
	for _, item := range a.Items {
		index[item.ID] = item
	}
	for _, item := range b.Items {
		cur := index[item.ID]
		if cur.Title != item.Title || cur.Method != item.Method || cur.Path != item.Path || cur.OperationID != item.OperationID {
			return false
		}
	}
	return true
}
