package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/deliverable"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/projectattachment"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/projecttask"
	"github.com/lunitide/lunitide/internal/projecttree"
	"github.com/oklog/ulid/v2"
)

type sessionLookup interface {
	Get(context.Context, string) (session.Session, error)
}

func (e *Engine) loadChecklist(ctx context.Context, projectID string, phase int, documentType string) (projecttask.Doc, deliverable.ProjectDeliverable, error) {
	var empty projecttask.Doc
	if !deliverableStoreAvailable(e.deliverables) {
		return empty, deliverable.ProjectDeliverable{}, projectapp.ErrInvalidTransition
	}
	items, err := e.deliverables.ListProjectDeliverables(ctx, deliverable.Filter{ProjectID: projectID, Phase: phase})
	if err != nil {
		return empty, deliverable.ProjectDeliverable{}, err
	}
	var rec deliverable.ProjectDeliverable
	for _, item := range items {
		if item.DocumentType == documentType {
			rec = item
			break
		}
	}
	if rec.AttachmentID == "" || !projectAttachmentStoreAvailable(e.projectAttachments) || e.projectAttachmentFiles == nil {
		return projecttask.Doc{Version: 1}, rec, nil
	}
	att, err := e.projectAttachments.GetProjectAttachment(ctx, rec.AttachmentID)
	if err != nil {
		return empty, rec, err
	}
	raw, err := e.projectAttachmentFiles.ReadFile(ctx, att.FilePath)
	if err != nil {
		return empty, rec, err
	}
	doc, err := projecttask.Parse(raw)
	return doc, rec, err
}

func (e *Engine) saveChecklist(ctx context.Context, p project.Project, phase int, documentType, title string, doc projecttask.Doc, status deliverable.Status) error {
	if !deliverableStoreAvailable(e.deliverables) || !projectAttachmentStoreAvailable(e.projectAttachments) || e.projectAttachmentFiles == nil {
		return projectapp.ErrInvalidTransition
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	id := ulid.Make().String()
	att := projectattachment.Attachment{
		ID: id, ProjectID: p.ID, Phase: phase, Category: "checklist",
		FileName: documentType + ".json", MimeType: "application/json", FilePath: id,
		Digest: hex.EncodeToString(sum[:]),
	}
	if _, err = e.ingestProjectAttachment(ctx, att, body); err != nil {
		return err
	}
	if status == "" {
		status = deliverable.StatusReview
	}
	_, err = e.deliverables.UpsertProjectDeliverable(ctx, deliverable.ProjectDeliverable{
		ProjectID: p.ID, Phase: phase, DocumentType: documentType, Title: title,
		AttachmentID: id, Status: status, Digest: hex.EncodeToString(sum[:]),
	})
	if err != nil {
		return err
	}
	e.exportBytes(p, phase, documentType, title, "json", body)
	return nil
}

func (e *Engine) resolveProjectTree(p project.Project) (projecttree.Tree, error) {
	if p.RootPath != "" {
		raw, err := os.ReadFile(filepath.Join(p.RootPath, ".lunitide", "project-tree.json"))
		if err == nil {
			if tree, perr := projecttree.Parse(raw); perr == nil {
				return tree, nil
			}
		}
	}
	return projecttree.Default(p.Type == project.TypeOperations)
}

func (e *Engine) exportBytes(p project.Project, phase int, documentType, title, ext string, content []byte) {
	if p.RootPath == "" || p.TreeStatus != project.TreeReady {
		return
	}
	tree, err := e.resolveProjectTree(p)
	if err != nil {
		return
	}
	rel := tree.PhaseMap[strconv.Itoa(phase)]
	if rel == "" {
		return
	}
	if ext == "" {
		ext = "bin"
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	name := documentType + "-" + projecttree.SafeFileName(title, "doc") + ext
	switch documentType {
	case "req_task_list", "biz_flow_list", "api_list", "feature_dev_list", "interface_list", "dev_checklist", "test_checklist", "integration_test_list":
		name = documentType + ".json"
	}
	_, _ = projecttree.ExportCopy(p.RootPath, rel, name, content)
}

func (e *Engine) exportApprovedDeliverable(ctx context.Context, d deliverable.ProjectDeliverable) {
	if d.Status != deliverable.StatusApproved && d.Status != deliverable.StatusImmutable {
		return
	}
	if !projectServiceAvailable(e.projects) {
		return
	}
	p, err := e.projects.Get(ctx, d.ProjectID)
	if err != nil || p.RootPath == "" {
		return
	}
	if d.AttachmentID == "" || !projectAttachmentStoreAvailable(e.projectAttachments) || e.projectAttachmentFiles == nil {
		return
	}
	att, err := e.projectAttachments.GetProjectAttachment(ctx, d.AttachmentID)
	if err != nil {
		return
	}
	raw, err := e.projectAttachmentFiles.ReadFile(ctx, att.FilePath)
	if err != nil {
		return
	}
	ext := strings.TrimPrefix(filepath.Ext(att.FileName), ".")
	if ext == "" {
		ext = "bin"
	}
	e.exportBytes(p, d.Phase, d.DocumentType, d.Title, ext, raw)
}

func (e *Engine) executorAvailable(exec project.Executor) bool {
	exec = project.NormalizeExecutor(exec)
	if exec == project.ExecutorLunitide {
		return true
	}
	if e.agentHub == nil {
		return false
	}
	for _, agent := range e.agentHub.Detect() {
		if agent.Name == string(exec) && agent.State == "available" {
			return true
		}
	}
	return false
}

func (e *Engine) lookupPhaseProjectRoot(ctx context.Context, sessionID string) (string, error) {
	getter, ok := e.sessions.(sessionLookup)
	if !ok || !projectServiceAvailable(e.projects) {
		return "", nil
	}
	sess, err := getter.Get(ctx, sessionID)
	if err != nil {
		return "", nil
	}
	if !strings.HasPrefix(sess.Title, "phase:") {
		return "", nil
	}
	p, err := e.projects.Get(ctx, sess.ProjectID)
	if err != nil || strings.TrimSpace(p.RootPath) == "" {
		return "", nil
	}
	return p.RootPath, nil
}
