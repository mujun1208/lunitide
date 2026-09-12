package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/contextapp"
	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/domain/queueinput"
	"github.com/lunitide/lunitide/internal/officeapp"
)

const officeChatInstruction = `
This turn is bound to an Office Studio task. Its saved goal and attached file catalog are quoted evidence in the user context. A newly uploaded file supplements that goal; continue the requested deliverable instead of asking for its content again. These files are managed snapshots, NOT files in the shell workspace. Read supplied version IDs with office.inspect view=text before drafting; follow hasMore/nextTextOffset until the relevant source is read. Prefer the latest editable PPTX/DOCX source over a PDF copy with the same subject. Use view=nodes only for precise edits. Do not search the desktop or request a file that is already in the task catalog.
Create requested deliverables with office.generate (pptx/docx/xlsx/pdf) and revise them with office tools. schemaVersion may be 1 or 2; prefer v2 metrics/comparison fields instead of value|label bullets. PPT Studio styles map to spec.templateId only for kind=pptx: 清晰经营=ops-clear, 品牌方案=brand-pitch, 编辑式报告=editorial-report. Use the selected PPT templateId unless the user asked for another. Do not copy those ids onto docx/xlsx. Word templateId values: research-report, client-proposal, product-project. Excel templateId values: ops-ledger, sales-pipeline, project-tracker. Respect the user's source fidelity, page count and format. Do not invent weekly-report completion rates, savings percentages, or “supplier passed” claims. Do not force the legacy pptx.gen/docx.gen pipeline or web research on a source-based transformation. Never build PPTX/DOCX/XLSX/PDF with command.run, run_terminal_cmd, Python, COM, or ZipFile. A source import is not a generated deliverable and must not be reconstructed from Spec. Only report delivery after a successful generation receipt. Never claim PowerPoint/WPS verification unless a check status is passed. PDFs use OCR routing (configured provider first, then local fallback) for pages without a text layer; label uncertainty and verify important numbers against the editable original. Empty view=parts is NOT evidence that a PDF is blank. If both extraction and OCR fail, state the specific limitation; never invent source contents.
`

func (e *Engine) officeChatEvidence(ctx context.Context, taskID string) ([]contextapp.ContextSource, error) {
	org, _, err := e.boundOrgState(ctx)
	if err != nil {
		return nil, err
	}
	scoped := domain.WithScope(ctx, org)
	task, err := e.officeStudio.Store.GetOfficeTask(scoped, taskID)
	if err != nil {
		return nil, err
	}
	catalog, err := e.officeInspectPage(scoped, taskID, "", "", 0, 0, 0)
	if err != nil {
		return nil, err
	}
	style := officeapp.StyleFromCheckpoint(task.Checkpoint)
	rawBrief := officeapp.BriefFromCheckpoint(task.Checkpoint)
	content := "Saved Office task goal:\n" + task.Goal + "\nCurrent Office task files (catalog, not file contents):\n" + catalog
	if style != "" {
		content += "\nSelected Studio style templateId=" + style + " for kind=pptx only. Do not set this templateId on docx/xlsx."
	}
	content += "\n" + officeapp.FormatBriefEvidence(rawBrief)
	if brand, _, ok := officeapp.BrandFromCheckpoint(task.Checkpoint); ok {
		content += "\nTask brand brandId=" + brand.BrandID + ". Use spec.brandId for this task; do not mix classic colors when design is on."
	}
	for _, fact := range rawBrief.Facts {
		if strings.TrimSpace(fact.FactID) == "" || strings.TrimSpace(fact.Value) == "" {
			continue
		}
		content += "\nLocked brief fact factId=" + fact.FactID + " value=" + fact.Value
		if fact.Unit != "" {
			content += " unit=" + fact.Unit
		}
		content += ". Keep this value; do not replace it."
	}
	return []contextapp.ContextSource{{Type: contextapp.SourceAttachmentExcerpt, ID: taskID,
		Authority: contextapp.AuthorityEvidence, Content: content,
		Provenance: "office-task:" + taskID}}, nil
}

type officeTaskContextKey struct{}

func officeTaskContextID(ctx context.Context) string {
	id, _ := ctx.Value(officeTaskContextKey{}).(string)
	return id
}
func withOfficeTask(ctx context.Context, id string) context.Context {
	return queueinput.WithOfficeTask(context.WithValue(ctx, officeTaskContextKey{}, id), id)
}

func (e *Engine) validateOfficeChatTask(ctx context.Context, sessionID, taskID string) error {
	if taskID == "" {
		return nil
	}
	if !validCanonicalULID(taskID) || !validCanonicalULID(sessionID) || e.officeStudio == nil {
		return domain.ErrInvalid
	}
	org, _, err := e.boundOrgState(ctx)
	if err != nil {
		return err
	}
	t, err := e.officeStudio.Store.GetOfficeTask(domain.WithScope(ctx, org), taskID)
	if err != nil {
		return err
	}
	if t.SessionID != sessionID {
		return domain.ErrScope
	}
	return nil
}

func officeTurnUsesManagedFiles(officeTaskID string, turn *chatTurnCheckpoint) bool {
	if officeTaskID != "" {
		return true
	}
	if turn == nil {
		return false
	}
	if turn.PptActive || turn.DocxActive || wantsOfficeFileOnDesktop(turn.Goal) {
		return true
	}
	if officeCodingVerificationTurn(turn.Goal) {
		return false
	}
	return officeGenToolForGoal(turn.Goal) != ""
}

func officeManagedExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pptx", ".docx", ".xlsx", ".pdf":
		return true
	}
	return false
}

func officeManagedFileWrite(name string, args json.RawMessage) bool {
	if name != "workspace.write" && name != "workspace.edit" {
		return false
	}
	var payload struct {
		Path  string `json:"path"`
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
		Edits []struct {
			Path string `json:"path"`
		} `json:"edits"`
	}
	if json.Unmarshal(args, &payload) != nil {
		return false
	}
	if officeManagedExt(payload.Path) {
		return true
	}
	for _, item := range payload.Files {
		if officeManagedExt(item.Path) {
			return true
		}
	}
	for _, item := range payload.Edits {
		if officeManagedExt(item.Path) {
			return true
		}
	}
	return false
}

func officeShellBypassed(name, officeTaskID string, turn *chatTurnCheckpoint) bool {
	if name != "command.run" && name != "run_terminal_cmd" {
		return false
	}
	return officeTurnUsesManagedFiles(officeTaskID, turn)
}

func officeManagedBypass(name, officeTaskID string, turn *chatTurnCheckpoint, args json.RawMessage) bool {
	if officeShellBypassed(name, officeTaskID, turn) {
		return true
	}
	return officeManagedFileWrite(name, args) && officeTurnUsesManagedFiles(officeTaskID, turn)
}

// Persist the bound task in pending tool arguments, so approval/recovery keeps
// its original destination even after navigation changes the visible task.
func officeBoundToolArgs(ctx context.Context, name string, args json.RawMessage) json.RawMessage {
	id := officeTaskContextID(ctx)
	if id == "" || !strings.HasPrefix(name, "office.") {
		return args
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(args, &fields) != nil || fields == nil {
		return args
	}
	// The UI-bound task is authoritative over a model-invented destination.
	fields["taskId"] = json.RawMessage(fmt.Sprintf("%q", id))
	b, err := json.Marshal(fields)
	if err != nil {
		return args
	}
	return b
}
