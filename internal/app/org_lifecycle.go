package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/org"
)

// The binding and lifecycle come from persisted organization state on every
// request. A payload and a previously mounted page cannot supply either value.
func (e *Engine) boundOrgState(ctx context.Context) (string, string, error) {
	if e.m9org == nil {
		return "", "", nil
	}
	res, err := e.m9org.Summary(ctx)
	if err != nil {
		return "", "", err
	}
	if res.BoundOrgID == "" {
		return "", "", nil
	}
	if res.Org == nil || res.Org.OrgID != res.BoundOrgID {
		return "", "", errors.New("organization binding is unavailable")
	}
	return res.BoundOrgID, res.Org.State, nil
}

func dataScopedMethod(method string) bool {
	if strings.HasPrefix(method, "org.space.") || strings.HasPrefix(method, "org.member.") {
		return true
	}
	for _, prefix := range []string{"project.", "session.", "message.", "plan.", "node.", "stage.", "deliverable.", "projectAttachment.", "template.", "chat.", "talk.", "agent.run.", "run.", "command.", "evidence.", "attachment.", "workspace.", "review.", "release.", "context.", "changeset.", "barrier.", "subagent.", "delegation.", "merge.", "mro.", "workflow.", "devTask.", "ontology.", "trace.", "automation.", "worker."} {
		if strings.HasPrefix(method, prefix) {
			return true
		}
	}
	// These are project memories. The separate local facts/settings catalog
	// does not acquire organization ownership merely by sharing the prefix.
	switch method {
	case "memory.get", "memory.list", "memory.search", "memory.create", "memory.update", "memory.delete":
		return true
	}
	return false
}

// New organization work fails closed. Explicit read and finalization methods
// remain available: cancellation cannot undo effects already performed, and
// their receipts must survive suspension. Resource ownership is still checked.
func organizationWriteAllowed(state, method string) bool {
	if state != org.OrgSuspended && state != org.OrgClosed {
		return true
	}
	if strings.HasPrefix(method, "mro.") {
		return !mroMutation(method)
	}
	switch method {
	case "org.space.list", "org.member.list", "org.member.revoke":
		return true
	case "project.list", "project.get", "session.list", "session.get", "session.experts.get", "session.metadata.get", "session.folder.get", "session.folder.list", "session.folder.open",
		"message.list", "message.search", "plan.get", "plan.list", "plan.run.tree", "node.list", "stage.list", "deliverable.list", "template.list", "attachment.get", "attachment.list", "projectAttachment.get", "projectAttachment.list",
		"agent.run.get", "command.get", "evidence.list", "review.list", "release.getRevision", "release.getPackage", "release.getPromotion",
		"context.status", "context.handoff.inspect", "context.handoff.list", "context.handoff.list-imports", "chat.turn.get",
		"workspace.list", "workspace.read", "workspace.open", "workspace.root.get", "workspace.artifact.preview", "workspace.artifactReview.list",
		"memory.get", "memory.list", "memory.search", "ontology.node.get", "ontology.node.list", "ontology.node.search", "ontology.edge.list", "trace.query",
		"automation.job.list", "automation.run.list", "automation.status", "run.queueList", "subagent.tree":
		return true
	case "chat.persist", "chat.checkpoint", "talk.persist", "talk.cancel", "agent.run.cancel", "agent.run.reconcile", "command.cancel", "run.cancel", "plan.run.cancel", "plan.pause", "run.queue.withdraw", "run.queueWithdraw", "context.compact.cancel", "context.handoff.revoke", "attachment.upload.abort", "evidence.attachTest", "evidence.attachScan", "node.complete", "node.fail", "plan.complete", "plan.run.join", "subagent.join", "barrier.arrive", "delegation.settle":
		return true
	}
	return false
}

func organizationStopRequest(method string, payload json.RawMessage) bool {
	if method != "workflow.transitionStage" && method != "devTask.transition" {
		return false
	}
	var p struct {
		To string `json:"to"`
	}
	return json.Unmarshal(payload, &p) == nil && (p.To == "cancelled" || p.To == "paused")
}
