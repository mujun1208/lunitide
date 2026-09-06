package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/domain/memory"
	"github.com/lunitide/lunitide/internal/domain/ontology"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/m9app"
	"github.com/lunitide/lunitide/internal/memoryapp"
	"github.com/lunitide/lunitide/internal/ontologyapp"
	"github.com/lunitide/lunitide/internal/org"
)

func TestDataScopeLegacyWorkflowMemoryOntologyAndTraceActualOwners(t *testing.T) {
	ctx := context.Background()
	e, sessionID, store := agentRunEngine(t)
	e.SetDataScopeStore(store)
	e.memories = memoryapp.New(store, store)
	e.ontology = ontologyapp.New(store, store, store, store)
	wf := m7app.NewWorkflowService(store.AgentRuntimeRepository())
	trace := m7app.NewTraceService(store.AgentRuntimeRepository())
	e.SetM7WorkflowServices(wf)
	e.SetM7EvidenceServices(trace, m7app.NewGateService(store.AgentRuntimeRepository()), m7app.NewReviewService(store.AgentRuntimeRepository(), trace))
	admin := m9app.NewOrgAdminService(org.NewService(org.NewGate(store.OrgStorage()), nil), m9app.NewFileBindingStore(filepath.Join(t.TempDir(), "binding.json")))
	e.SetM9OrgAdminService(admin)
	a, err := admin.CreateOrg(ctx, "Org A")
	if err != nil {
		t.Fatal(err)
	}
	personalID, ok, err := projectIDForSession(e, ctx, sessionID)
	if err != nil || !ok {
		t.Fatalf("project: %v", err)
	}
	owned, err := e.projects.Create(ctx, "scope-owned", "test", map[string]string{"name": "A"}, project.Project{Name: "A", OrgID: a.OrgID})
	if err != nil {
		t.Fatal(err)
	}
	m, err := e.memories.Create(ctx, memory.Memory{ProjectID: personalID, Layer: memory.LayerSemantic, Scope: memory.ScopeProject, Key: "secret", Content: "foreign content", Confidence: 1})
	if err != nil {
		t.Fatal(err)
	}
	personalNode, err := store.CreateOntologyNode(ctx, ontology.Node{ProjectID: personalID, Type: ontology.NodeType("module"), Name: "foreign"})
	if err != nil {
		t.Fatal(err)
	}
	ownedNode, err := store.CreateOntologyNode(ctx, ontology.Node{ProjectID: owned.ID, Type: ontology.NodeType("module"), Name: "own"})
	if err != nil {
		t.Fatal(err)
	}
	// Model an old database which predates the engine ownership guard.
	mixed, err := store.CreateOntologyEdge(ctx, ontology.Edge{SourceNodeID: ownedNode.ID, TargetNodeID: personalNode.ID, Type: ontology.EdgeType("references"), Label: "legacy mixed", Weight: 1})
	if err != nil {
		t.Fatal(err)
	}
	wv, err := wf.CreateVersion(ctx, personalID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wf.Publish(ctx, wv.ID); err != nil {
		t.Fatal(err)
	}
	sr, err := wf.StartStage(ctx, personalID, "INITIATION_BOUNDARY")
	if err != nil {
		t.Fatal(err)
	}
	task, err := trace.CreateDevTask(ctx, m7flow.DevTask{StageRunID: sr.Run.ID, Title: "private", Priority: "P1", Risk: "low", AcceptanceDigest: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = trace.AddEdge(ctx, m7flow.TraceEdge{FromType: "project", FromID: owned.ID, FromDigest: strings.Repeat("a", 64), Relation: "traces_to", ToType: "project", ToID: personalID, ToDigest: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if r := e.Handle(ctx, validRequest("memory.get", fmt.Sprintf(`{"id":%q}`, m.ID))); !r.OK {
		t.Fatal(r.Error)
	}
	if _, err = admin.Switch(ctx, a.OrgID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, payload string }{
		{"memory.get", fmt.Sprintf(`{"id":%q}`, m.ID)},
		{"memory.update", fmt.Sprintf(`{"id":%q,"content":"tampered"}`, m.ID)},
		{"memory.delete", fmt.Sprintf(`{"id":%q}`, m.ID)},
		{"ontology.node.get", fmt.Sprintf(`{"id":%q}`, personalNode.ID)},
		{"ontology.node.delete", fmt.Sprintf(`{"id":%q}`, personalNode.ID)},
		{"ontology.edge.update", fmt.Sprintf(`{"id":%q,"weight":0.1}`, mixed.ID)},
		{"ontology.edge.list", fmt.Sprintf(`{"nodeId":%q}`, ownedNode.ID)},
		{"ontology.edge.create", fmt.Sprintf(`{"sourceNodeId":%q,"targetNodeId":%q,"type":"references","label":"new"}`, ownedNode.ID, personalNode.ID)},
		{"workflow.publish", fmt.Sprintf(`{"workflowVersionId":%q,"requestId":"r"}`, wv.ID)},
		{"workflow.transitionStage", fmt.Sprintf(`{"stageRunId":%q,"expectedVersion":1,"to":"running"}`, sr.Run.ID)},
		{"workflow.captureInput", fmt.Sprintf(`{"stageRunId":%q,"requestId":"r","inputs":{}}`, sr.Run.ID)},
		{"devTask.transition", fmt.Sprintf(`{"taskId":%q,"expectedVersion":1,"to":"ready"}`, task.ID)},
		{"evidence.attachTest", fmt.Sprintf(`{"taskRef":%q,"result":"pass","reportDigest":%q}`, task.ID, strings.Repeat("a", 64))},
		{"evidence.attachTest", fmt.Sprintf(`{"taskRef":"opaque-unowned","result":"pass","reportDigest":%q}`, strings.Repeat("a", 64))},
		{"review.submit", fmt.Sprintf(`{"subjectType":"dev_task","subjectId":%q,"subjectVersion":1,"verdict":"approve","reviewerId":"B","authorId":"A","reason":"done"}`, task.ID)},
		{"trace.query", fmt.Sprintf(`{"rootType":"project","rootId":%q,"direction":"down","depth":2}`, personalID)},
		// The root is owned; traversing a legacy edge still cannot expose its peer or cursor.
		{"trace.query", fmt.Sprintf(`{"rootType":"project","rootId":%q,"direction":"down","depth":2}`, owned.ID)},
	} {
		r := validRequest(tc.method, tc.payload)
		r.IdempotencyKey = "legacy-" + tc.method
		if out := e.Handle(ctx, r); out.OK || out.Error.Code != "DATA_SCOPE_DENIED" {
			t.Errorf("%s: %+v", tc.method, out)
		}
	}
	if out := e.Handle(ctx, validRequest("ontology.node.get", fmt.Sprintf(`{"id":%q}`, ownedNode.ID))); !out.OK {
		t.Fatal(out.Error)
	}
	ownRequest := validRequest("workflow.createVersion", fmt.Sprintf(`{"projectId":%q,"requestId":"own"}`, owned.ID))
	ownRequest.IdempotencyKey = "own-workflow"
	if out := e.Handle(ctx, ownRequest); !out.OK {
		t.Fatal(out.Error)
	}
	unchanged, err := e.memories.Get(ctx, m.ID)
	if err != nil || unchanged.Content != "foreign content" {
		t.Fatalf("foreign memory changed: %+v %v", unchanged, err)
	}
	for _, method := range []string{"chat.persist", "chat.checkpoint", "talk.persist"} {
		payload, _ := json.Marshal(map[string]string{"sessionId": sessionID})
		release, err := e.authorizeDataRequest(ctx, method, payload)
		release()
		if !dataScopeAccessError(err) {
			t.Errorf("late %s migrated old session: %v", method, err)
		}
	}
}
