package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lunitide/lunitide/internal/contextapp"
	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/domain/queueinput"
)

const officeChatInstruction = `
This turn is bound to an Office Studio task. Its saved goal and attached file catalog are quoted evidence in the user context. A newly uploaded file supplements that goal; continue the requested deliverable instead of asking for its content again. These files are managed snapshots, NOT files in the shell workspace. Read supplied version IDs with office.inspect view=text before drafting; follow hasMore/nextTextOffset until the relevant source is read. Prefer the latest editable PPTX/DOCX source over a PDF copy with the same subject. Use view=nodes only for precise edits. Do not search the desktop or request a file that is already in the task catalog.
Create requested deliverables with office.generate (pptx/docx/xlsx/pdf) and revise them with office tools. Respect the user's source fidelity, page count and format. Do not force the legacy pptx.gen/docx.gen pipeline or web research on a source-based transformation. A source import is not a generated deliverable. Only report delivery after a successful generation receipt. PDFs without a text layer use local OCR; label its uncertainty and verify important numbers against the editable original. Empty view=parts is NOT evidence that a PDF is blank. If both extraction and OCR fail, state the specific limitation; never invent source contents.
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
	return []contextapp.ContextSource{{Type: contextapp.SourceAttachmentExcerpt, ID: taskID,
		Authority: contextapp.AuthorityEvidence, Content: "Saved Office task goal:\n" + task.Goal + "\nCurrent Office task files (catalog, not file contents):\n" + catalog,
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
