package m8app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/m8app"
)

func enableGate(t *testing.T, svc *m8app.CollabGateService, subject string) {
	t.Helper()
	ctx := context.Background()
	ws, we := gateWin()
	if _, err := svc.Evaluate(ctx, m8app.EvaluateInput{
		SubjectID: subject, WindowStart: ws, WindowEnd: we, CriteriaVersion: "write-collab-v1",
	}); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	d, has, err := svc.PendingDecision(ctx, subject)
	if err != nil || !has {
		t.Fatalf("pending decision err=%v has=%v", err, has)
	}
	confirmed, err := svc.Confirm(ctx, m8app.GateConfirmInput{DecisionID: d.DecisionID, DecisionToken: d.DecisionToken})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if err := svc.CheckGate(ctx, subject); err != nil {
		t.Fatalf("gate not enabled after confirm (%+v): %v", confirmed, err)
	}
}

func TestWriteCollabHandoffRefusedWhenGateDisabled(t *testing.T) {
	svc, _, _ := openGateService(t, passingEvidence())
	// The default state has no confirmed decision: the gate is disabled, so
	// preparing a handoff must be refused with M8-028 and no ticket.
	_, err := svc.PrepareWriteCollabHandoff(context.Background(), m8app.WriteCollabHandoffInput{
		SubjectID: "subject-1", SessionID: "sess-1", Summary: "hand off the build task",
	})
	if !errors.Is(err, m8app.ErrGateDisabled) {
		t.Fatalf("disabled handoff err = %v, want ErrGateDisabled", err)
	}
}

func TestWriteCollabHandoffPreparedWhenEnabled(t *testing.T) {
	svc, _, _ := openGateService(t, passingEvidence())
	enableGate(t, svc, "subject-3")

	ticket, err := svc.PrepareWriteCollabHandoff(context.Background(), m8app.WriteCollabHandoffInput{
		SubjectID: "subject-3", SessionID: "sess-9", Summary: "hand off the release build",
	})
	if err != nil {
		t.Fatalf("prepare handoff: %v", err)
	}
	if ticket.TicketID == "" || len(ticket.Token) != 64 || ticket.SummaryDigest == "" || ticket.ExpiresAt == "" {
		t.Fatalf("ticket incomplete: %+v", ticket)
	}
	if ticket.SessionID != "sess-9" || ticket.SubjectID != "subject-3" {
		t.Fatalf("ticket identity mismatch: %+v", ticket)
	}
}

func TestWriteCollabHandoffValidation(t *testing.T) {
	svc, _, _ := openGateService(t, passingEvidence())
	ctx := context.Background()
	if _, err := svc.PrepareWriteCollabHandoff(ctx, m8app.WriteCollabHandoffInput{SubjectID: "", SessionID: "s", Summary: "x"}); !errors.Is(err, m8app.ErrPayloadInvalid) {
		t.Fatalf("empty subject err = %v, want ErrPayloadInvalid", err)
	}
	if _, err := svc.PrepareWriteCollabHandoff(ctx, m8app.WriteCollabHandoffInput{SubjectID: "s", SessionID: "", Summary: "x"}); !errors.Is(err, m8app.ErrPayloadInvalid) {
		t.Fatalf("empty session err = %v, want ErrPayloadInvalid", err)
	}
}
