package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/officetools"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestOfficeSessionOutputDoesNotRequestOrGrantFullDisk(t *testing.T) {
	tools, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetToolRuntime(tools)
	ctx := context.Background()
	resp := e.Handle(ctx, observedPolicyRequest(t, e, "tools.commandPolicy.set", `{"commands":[],"fullAccess":true}`))
	if !resp.OK {
		t.Fatalf("policy: %+v", resp.Error)
	}
	const session = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	args, _ := json.Marshal(map[string]any{"path": "weekly.docx", "title": "Weekly report", "blocks": officetools.SampleStyledDocxBlocks()})
	result, err := e.executeUserTool(ctx, executionModeFullAccess, session, "docx.gen", args)
	if err != nil {
		t.Fatal(err)
	}
	if result.Artifact == nil {
		t.Fatalf("missing artifact: %+v", result)
	}
	dir, err := tools.SessionFolder(session)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(dir, "weekly.docx")); err != nil || info.Size() == 0 {
		t.Fatalf("missing generated document: %v", err)
	}
	if tools.FullDiskSessionConfirmed(session) {
		t.Fatal("scoped generation granted full disk")
	}
	args, _ = json.Marshal(map[string]any{"path": "weekly.docx", "desktop": true, "title": "Weekly report", "blocks": officetools.SampleStyledDocxBlocks()})
	if _, err := e.executeUserTool(ctx, executionModeFullAccess, session, "docx.gen", args); err != nil {
		t.Fatalf("full-access desktop output must not ask for a second approval: %v", err)
	}
}

func TestApprovalModeDesktopOfficeStillGates(t *testing.T) {
	tools, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetToolRuntime(tools)
	ctx := context.Background()
	resp := e.Handle(ctx, observedPolicyRequest(t, e, "tools.commandPolicy.set", `{"commands":[],"fullAccess":true}`))
	if !resp.OK {
		t.Fatalf("policy: %+v", resp.Error)
	}
	const session = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	args, _ := json.Marshal(map[string]any{"path": "weekly.docx", "desktop": true, "title": "Weekly report", "blocks": officetools.SampleStyledDocxBlocks()})
	if _, err := e.executeUserTool(ctx, executionModeApproval, session, "docx.gen", args); !errors.Is(err, toolruntime.ErrApprovalRequired) {
		t.Fatalf("approval mode must still gate desktop output: %v", err)
	}
}

func TestOfficeOutputScopeRejectsExternalPathsAndOtherTools(t *testing.T) {
	for _, args := range []string{`{"path":"../weekly.docx"}`, `{"path":"C:/weekly.docx"}`, `{"path":"/weekly.docx"}`, `{"path":"weekly.docx","desktop":true}`, `{"path":"weekly.docx:stream"}`, `{}`} {
		if sessionOfficeOutput("docx.gen", json.RawMessage(args)) {
			t.Fatalf("external output accepted: %s", args)
		}
	}
	if sessionOfficeOutput("command.run", json.RawMessage(`{"path":"weekly.docx"}`)) {
		t.Fatal("non-office tool accepted")
	}
}
