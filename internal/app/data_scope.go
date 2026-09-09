package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/lunitide/lunitide/internal/org"
	"strings"
	"sync"
)

type DataScopeStore interface {
	AuthorizeDataResource(context.Context, string, string, string) error
}
type engineDataScope struct {
	mu    sync.RWMutex
	store DataScopeStore
}

func (e *Engine) SetDataScopeStore(store DataScopeStore) { e.dataScope.store = store }

// authorizeDataRequest pins the verified binding through the synchronous
// request. Payload ids select resources; they never declare ownership.
func (e *Engine) authorizeDataRequest(ctx context.Context, method string, payload json.RawMessage) (func(), error) {
	noop := func() {}
	if !dataScopedMethod(method) {
		return noop, nil
	}
	if e.dataScope.store == nil {
		if e.m9org != nil && !strings.HasPrefix(method, "org.") {
			return noop, errors.New("data scope storage unavailable")
		}
		if e.m9org == nil {
			return noop, nil
		}
	}
	// Background finalization can run under an existing request lease. Do not
	// recursively block behind a waiting scope switch; retain its checkpoint
	// and let the caller retry under the next verified binding.
	if !e.dataScope.mu.TryRLock() {
		return noop, errors.New("organization switch in progress")
	}
	release := e.dataScope.mu.RUnlock
	orgID, state, err := e.boundOrgState(ctx)
	if err != nil {
		release()
		return noop, err
	}
	if !organizationWriteAllowed(state, method) && !organizationStopRequest(method, payload) {
		release()
		return noop, org.ErrOrgSuspended
	}
	if strings.HasPrefix(method, "org.") {
		// Org-admin resolves principals/spaces with its verified-org repository.
		// It still shares this lease with binding and lifecycle changes.
		return release, nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(payload, &fields) != nil {
		release()
		return noop, nil
	}
	ids := map[string]string{}
	add := func(field, kind string) {
		var id string
		if json.Unmarshal(fields[field], &id) == nil && id != "" {
			ids[kind+"/"+id] = kind
		}
	}
	addTyped := func(typeField, idField string) {
		var kind string
		if json.Unmarshal(fields[typeField], &kind) == nil && m7TraceNodeType(kind) {
			add(idField, "trace:"+kind)
		}
	}
	add("projectId", "project")
	add("sessionId", "session")
	add("rootSessionId", "session")
	add("sourceSessionId", "session")
	add("destSessionId", "session")
	add("checkpointId", "checkpoint")
	add("capsuleId", "capsule")
	add("planId", "plan")
	add("messageId", "message")
	if method == "chat.start" || strings.HasPrefix(method, "run.queue") {
		add("officeTaskId", "office-task")
	}
	if strings.HasPrefix(method, "office.") {
		add("taskId", "office-task")
		add("versionId", "office-version")
		add("baseVersionId", "office-version")
		add("targetVersionId", "office-version")
		add("attachmentId", "attachment")
	}
	for _, container := range []string{"manifest", "target"} {
		var nested struct {
			ProjectID string `json:"projectId"`
		}
		if json.Unmarshal(fields[container], &nested) == nil && nested.ProjectID != "" {
			ids["project/"+nested.ProjectID] = "project"
		}
	}
	switch {
	case strings.HasPrefix(method, "project."):
		add("id", "project")
	case strings.HasPrefix(method, "session."):
		add("id", "session")
	case strings.HasPrefix(method, "template."):
		add("id", "template")
	case strings.HasPrefix(method, "plan.run."):
		add("runId", "plan-run")
		add("parentRunId", "plan-run")
		add("nodeId", "node")
	case strings.HasPrefix(method, "plan.todo."):
		add("nodeId", "node")
	case strings.HasPrefix(method, "plan."):
		add("id", "plan")
	case strings.HasPrefix(method, "node."):
		add("nodeId", "node")
		add("parentNodeId", "node")
	case strings.HasPrefix(method, "stage."):
		add("id", "stage")
	case strings.HasPrefix(method, "deliverable."):
		add("id", "deliverable")
		add("templateId", "template")
		add("attachmentId", "project-attachment")
	case strings.HasPrefix(method, "projectAttachment."):
		add("attachmentId", "project-attachment")
	case strings.HasPrefix(method, "attachment."):
		add("attachmentId", "attachment")
	case strings.HasPrefix(method, "memory."):
		add("id", "memory")
	case strings.HasPrefix(method, "ontology.node."):
		add("id", "ontology-node")
	case strings.HasPrefix(method, "ontology.edge."):
		add("id", "ontology-edge")
		add("nodeId", "ontology-node")
		add("sourceNodeId", "ontology-node")
		add("targetNodeId", "ontology-node")
	case strings.HasPrefix(method, "workflow."):
		add("workflowVersionId", "trace:workflow_version")
		add("stageRunId", "trace:stage_run")
	case strings.HasPrefix(method, "devTask."):
		add("stageRunId", "trace:stage_run")
		add("taskId", "trace:dev_task")
	case strings.HasPrefix(method, "subagent."):
		add("subagentId", "subagent")
		add("stageRunId", "trace:stage_run")
	case strings.HasPrefix(method, "delegation."):
		add("rootId", "agent-run")
		add("delegationId", "delegation")
	case strings.HasPrefix(method, "barrier."):
		add("barrierId", "barrier")
	case strings.HasPrefix(method, "merge."):
		add("rootId", "agent-run")
	case strings.HasPrefix(method, "trace."):
		addTyped("fromType", "fromId")
		addTyped("toType", "toId")
		addTyped("rootType", "rootId")
		addTyped("subjectType", "subjectId")
		add("causeEdge", "trace:trace_edge")
		add("staleMarkId", "stale-mark")
		add("reevaluationId", "gate-evaluation")
	case strings.HasPrefix(method, "evidence."):
		add("taskRef", "trace:dev_task")
	case strings.HasPrefix(method, "review."):
		add("reviewId", "review")
		addTyped("subjectType", "subjectId")
	case strings.HasPrefix(method, "release."):
		add("crRevisionId", "release-revision")
		add("packageId", "release-package")
		add("promotionId", "release-promotion")
		if method == "release.getRevision" {
			add("crId", "release-cr")
		}
	}
	// Scheduler run ids belong to the durable automation journal, not the
	// agent_runs table. Automation handlers authorize their owning job/session.
	if !strings.HasPrefix(method, "plan.") && !strings.HasPrefix(method, "automation.") {
		add("runId", "agent-run")
		add("rootRunId", "agent-run")
		add("parentRunId", "agent-run")
	}
	if strings.HasPrefix(method, "command.") {
		add("jobId", "command")
	}
	for key, kind := range ids {
		id := strings.TrimPrefix(key, kind+"/")
		if err = e.dataScope.store.AuthorizeDataResource(ctx, kind, id, orgID); err != nil {
			release()
			return noop, err
		}
	}
	return release, nil
}

func (e *Engine) beginScopeSwitch() func() {
	e.CancelAllStreams()
	e.cancelAllPlanExecutionWorkers()
	e.dataScope.mu.Lock()
	// A start request may register a worker while the writer is waiting.
	e.CancelAllStreams()
	e.cancelAllPlanExecutionWorkers()
	return e.dataScope.mu.Unlock
}
func (e *Engine) cancelAllPlanExecutionWorkers() {
	e.planExecutions.mu.Lock()
	defer e.planExecutions.mu.Unlock()
	for _, worker := range e.planExecutions.workers {
		worker.cancel()
	}
}
func dataScopeAccessError(err error) bool { return errors.Is(err, org.ErrCrossOrgAccess) }
