package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/deliverable"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/projectschema"
	"github.com/lunitide/lunitide/internal/projecttask"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

func TestFactoryFirstShipImplementationWalk(t *testing.T) {
	ctx := context.Background()
	e, _, created, root := factoryEngine(t)
	dest := filepath.Join(filepath.Dir(root), "mall-release")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	pubReq := validRequest("project.publish", `{"id":"`+created.ID+`","version":1}`)
	pubReq.IdempotencyKey = ulid.Make().String()
	pub := e.Handle(ctx, pubReq)
	if !pub.OK {
		t.Fatalf("publish %#v", pub)
	}

	gen1 := handleProjectFactory(e, ctx, validRequest("project.deliverable.generate", `{"projectId":"`+created.ID+`","phase":1}`))
	if !gen1.OK {
		t.Fatalf("generate1 %#v", gen1)
	}
	items1, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: created.ID, Phase: 1})
	if err != nil || len(items1) != 9 {
		t.Fatalf("phase1 cards %d %v", len(items1), err)
	}
	for _, item := range items1 {
		if item.AttachmentID == "" || len(mustReadAttachment(t, e, item.AttachmentID)) < 32 {
			t.Fatalf("empty card %#v", item)
		}
		approveDeliverable(t, e, created.ID, 1, item)
	}
	mustCreateStage(t, e, created.ID, 1)
	created = mustAdvance(t, e, created.ID, 1, false)
	if created.TreeStatus != project.TreeReady {
		t.Fatalf("tree after p1: %+v", created)
	}
	if created.RulesDigest == "" {
		t.Fatalf("rules not materialized: %+v", created)
	}

	gen2 := handleProjectFactory(e, ctx, validRequest("project.deliverable.generate", `{"projectId":"`+created.ID+`","phase":2}`))
	if !gen2.OK {
		t.Fatalf("generate2 %#v", gen2)
	}
	items2, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: created.ID, Phase: 2})
	if err != nil || len(items2) != 10 {
		t.Fatalf("phase2 cards %d %v", len(items2), err)
	}
	for _, item := range items2 {
		approveDeliverable(t, e, created.ID, 2, item)
	}
	mustCreateStage(t, e, created.ID, 2)
	created = mustAdvance(t, e, created.ID, 2, true)

	schema := handleProjectFactory(e, ctx, validRequest("project.schema.put", `{"projectId":"`+created.ID+`","schema":`+factorySchemaJSON+`}`))
	if !schema.OK {
		t.Fatalf("schema.put %#v", schema)
	}
	cur, err := e.projects.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	mat := handleProjectFactory(e, ctx, validRequest("project.schema.materialize", `{"projectId":"`+created.ID+`","version":`+itoa(cur.Version)+`}`))
	if !mat.OK {
		t.Fatalf("schema.materialize %#v", mat)
	}
	cur, err = e.projects.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	ver := handleProjectFactory(e, ctx, validRequest("project.schema.verify", `{"projectId":"`+created.ID+`","version":`+itoa(cur.Version)+`}`))
	if !ver.OK {
		t.Fatalf("schema.verify %#v", ver)
	}
	created, err = e.projects.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	iface := projecttask.Doc{Version: 1, Items: []projecttask.Item{
		{ID: "I001", Title: "登录接口", Status: "pending", Method: "POST", Path: "/login", SourceKind: "interface"},
	}}
	if err := e.saveChecklist(ctx, created, project.InterfacePhase(created.Type), "interface_list", "接口清单", iface, deliverable.StatusApproved); err != nil {
		t.Fatal(err)
	}
	sync := handleProjectFactory(e, ctx, validRequest("project.board.sync", `{"projectId":"`+created.ID+`","boardKind":"interface"}`))
	if !sync.OK {
		t.Fatalf("board.sync %#v", sync)
	}
	done := projecttask.Doc{Version: 1, Items: []projecttask.Item{
		{ID: "I001", Title: "登录接口", Status: "dev_done", Method: "POST", Path: "/login", SourceKind: "interface", SelfTestPass: true, LastResultSummary: "对照接口详细设计通过"},
	}}
	if err := e.saveChecklist(ctx, created, project.InterfacePhase(created.Type), "interface_list", "接口清单", done, deliverable.StatusApproved); err != nil {
		t.Fatal(err)
	}

	codeBoard := projecttask.Doc{Version: 1, Items: []projecttask.Item{
		{ID: "F001", Title: "登录", Status: "dev_done", SourceKind: "dev", SelfTestPass: true, LastResultSummary: "自测通过", Acceptance: "登录成功"},
	}}
	if err := e.saveChecklist(ctx, created, project.DevPhase(created.Type), "dev_checklist", "开发检查清单", codeBoard, deliverable.StatusApproved); err != nil {
		t.Fatal(err)
	}
	test := projecttask.Doc{Version: 1, Items: []projecttask.Item{{
		ID: "T-I001", Title: "测接口", Status: "test_pass", SourceID: "I001", SourceKind: "interface",
		RequiredKinds: []string{"unit"}, KindResults: map[string]projecttask.KindResult{"unit": {Status: "pass"}},
	}, {
		ID: "T-F001", Title: "测开发", Status: "test_pass", SourceID: "F001", SourceKind: "dev",
		RequiredKinds: []string{"unit"}, KindResults: map[string]projecttask.KindResult{"unit": {Status: "pass"}},
	}}}
	if err := e.saveChecklist(ctx, created, project.TestPhase(created.Type), "test_checklist", "测试检查清单", test, deliverable.StatusApproved); err != nil {
		t.Fatal(err)
	}
	scene := projecttask.Doc{Version: 1, Items: []projecttask.Item{{
		ID: "S01", Title: "登录场景", Status: "test_pass", MemberIDs: []string{"T-I001", "T-F001"},
	}}}
	if err := e.saveChecklist(ctx, created, 7, "integration_test_list", "集成测试场景清单", scene, deliverable.StatusApproved); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "keep.txt"), []byte("mall first-ship source file"), 0o644); err != nil {
		t.Fatal(err)
	}
	escaped := strings.ReplaceAll(dest, `\`, `\\`)
	rel := handleProjectFactory(e, ctx, validRequest("project.release.sync", `{"projectId":"`+created.ID+`","destPath":"`+escaped+`"}`))
	if !rel.OK {
		t.Fatalf("release.sync %#v", rel)
	}
	if _, err := os.Stat(filepath.Join(dest, "keep.txt")); err != nil {
		t.Fatalf("dest missing copy: %v", err)
	}

	// Phases 3–8: create stage + seed remaining required docs the same way
	// project_phase_completion_test does for db_design, then advance with
	// emptyBoardAck only when that board is empty. If advance fails, fix the
	// fixture — do not skip the phase.
	for _, phase := range []int{3, 4, 5, 6, 7, 8} {
		mustCreateStage(t, e, created.ID, phase)
		ack := phase == 4 || phase == 5 || phase == 6 || phase == 7
		next, err := advanceMaybe(t, e, created.ID, phase, ack)
		if err != nil {
			t.Fatalf("advance phase %d: %v", phase, err)
		}
		created = next
	}
	if _, err := os.Stat(filepath.Join(root, ".lunitide-sync.json")); err != nil {
		if _, err2 := os.Stat(filepath.Join(root, ".lunitide", "sync-receipt.json")); err2 != nil {
			t.Fatalf("missing sync receipt: %v %v", err, err2)
		}
	}
}

func approveDeliverable(t *testing.T, e *Engine, projectID string, phase int, item deliverable.ProjectDeliverable) {
	t.Helper()
	resp := handleDeliverableUpsert(e, context.Background(), validRequest("deliverable.upsert",
		`{"projectId":"`+projectID+`","phase":`+itoa(phase)+`,"documentType":"`+item.DocumentType+`","title":"`+item.Title+`","attachmentId":"`+item.AttachmentID+`","status":"approved"}`))
	if !resp.OK {
		t.Fatalf("approve %s %#v", item.DocumentType, resp)
	}
}

func mustCreateStage(t *testing.T, e *Engine, projectID string, phase int) {
	t.Helper()
	req := validRequest("stage.create", `{"projectId":"`+projectID+`","phase":`+itoa(phase)+`,"title":"Phase `+itoa(phase)+`"}`)
	req.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(context.Background(), req)
	if !resp.OK {
		t.Fatalf("stage.create %d %#v", phase, resp)
	}
}

func mustAdvance(t *testing.T, e *Engine, projectID string, phase int, emptyBoardAck bool) project.Project {
	t.Helper()
	next, err := advanceMaybe(t, e, projectID, phase, emptyBoardAck)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func mustReadAttachment(t *testing.T, e *Engine, attachmentID string) []byte {
	t.Helper()
	att, err := e.projectAttachments.GetProjectAttachment(context.Background(), attachmentID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := e.projectAttachmentFiles.ReadFile(context.Background(), att.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func itoa[T ~int | ~int64](v T) string {
	return strconv.FormatInt(int64(v), 10)
}

func advanceMaybe(t *testing.T, e *Engine, projectID string, phase int, emptyBoardAck bool) (project.Project, error) {
	t.Helper()
	ctx := context.Background()
	if phase == 3 {
		if err := ensurePhase3DBEvidence(t, e, projectID); err != nil {
			return project.Project{}, err
		}
	}
	if phase == 8 {
		if err := ensurePhase8Publication(t, e, projectID); err != nil {
			return project.Project{}, err
		}
	}
	p, err := e.projects.Get(ctx, projectID)
	if err != nil {
		return p, err
	}
	ack := "false"
	if emptyBoardAck {
		ack = "true"
	}
	req := validRequest("project.advanceStatus", `{"id":"`+projectID+`","version":`+itoa(p.Version)+`,"phase":`+itoa(phase)+`,"emptyBoardAck":`+ack+`}`)
	req.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(ctx, req)
	if !resp.OK {
		raw, _ := json.Marshal(resp)
		return p, fmt.Errorf("%s", raw)
	}
	return e.projects.Get(ctx, projectID)
}

func ensurePhase3DBEvidence(t *testing.T, e *Engine, projectID string) error {
	t.Helper()
	ctx := context.Background()
	p, err := e.projects.Get(ctx, projectID)
	if err != nil {
		return err
	}
	items, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: projectID, Phase: 3})
	if err != nil {
		return err
	}
	var dbDoc deliverable.ProjectDeliverable
	for _, item := range items {
		if item.DocumentType == "db_design" {
			dbDoc = item
			break
		}
	}
	needBody := dbDoc.AttachmentID == "" || len(mustReadAttachment(t, e, dbDoc.AttachmentID)) < 32
	if needBody {
		body := []byte("Evidence for db_design — phase document body")
		if err = e.writeDeliverableBody(ctx, p, 3, "db_design", "db_design", body); err != nil {
			return err
		}
		items, err = e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: projectID, Phase: 3})
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.DocumentType == "db_design" {
				dbDoc = item
				break
			}
		}
	}
	if dbDoc.Status != deliverable.StatusApproved && dbDoc.Status != deliverable.StatusImmutable {
		approveDeliverable(t, e, projectID, 3, dbDoc)
	}
	p, err = e.projects.Get(ctx, projectID)
	if err != nil {
		return err
	}
	if p.DBStatus == project.DBReady {
		return nil
	}
	_, err = e.projects.Mutate(ctx, ulid.Make().String(), "test", "project.update", p.ID, p.Version, map[string]string{"db": "ready"}, func(cur *project.Project) error {
		cur.DBStatus = project.DBReady
		if cur.DBPath == "" && cur.RootPath != "" {
			cur.DBPath = projectschema.DefaultDBPath(cur.RootPath)
		}
		return nil
	})
	return err
}

func ensurePhase8Publication(t *testing.T, e *Engine, projectID string) error {
	t.Helper()
	ctx := context.Background()
	store, ok := e.deliverables.(*storage.Store)
	if !ok {
		return fmt.Errorf("deliverable store does not support publication fixtures")
	}
	p, err := e.projects.Get(ctx, projectID)
	if err != nil {
		return err
	}
	root := t.TempDir()
	store.SetProjectPublicationRoot(root)
	release := m7app.NewReleaseService(store.AgentRuntimeRepository())
	release.SetProjectContent(store)
	revision, err := release.CreateRevision(ctx, "CR-"+p.ProjectCode, map[string]any{
		"projectId": p.ID, "authorId": "test", "summary": "first-ship fixture",
	})
	if err != nil {
		return err
	}
	pkg, err := release.BuildPackage(ctx, revision.ID, revision.Digest)
	if err != nil {
		return err
	}
	publisher := m7app.NewPromotionService(store.AgentRuntimeRepository())
	publisher.SetLocalPublication(root)
	for _, env := range []string{"dev", "stage"} {
		if _, err = publisher.Promote(ctx, m7app.PromoteInput{
			PackageID: pkg.ID, TargetEnv: env, RequestID: "first-ship-" + env,
			PolicyContext: map[string]any{"requestedBy": "tester"},
		}); err != nil {
			return err
		}
	}
	return nil
}
