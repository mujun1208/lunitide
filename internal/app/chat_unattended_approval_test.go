package app

import (
	"context"
	"strings"
	"testing"
)

// An unattended turn (IM inbound auto-run, colleague auto-reply) drives
// chat.start with a noop emitter. If a tool reports ErrApprovalRequired, the
// loop must not emit an approval request and pause — nothing carries the event
// and no one can grant it, so the turn would be abandoned mid-flight. These
// tests pin the decision seam that makes such a turn deny in place instead.

func TestDecideApprovalOutcomeUnattendedDeniesInPlace(t *testing.T) {
	if got := decideApprovalOutcome(false, true); got != approvalDenyUnattended {
		t.Fatalf("unattended turn = %v, want approvalDenyUnattended", got)
	}
}

func TestDecideApprovalOutcomeAttendedEmits(t *testing.T) {
	if got := decideApprovalOutcome(false, false); got != approvalEmit {
		t.Fatalf("attended turn = %v, want approvalEmit", got)
	}
}

func TestDecideApprovalOutcomeCompanionPreapprovalWins(t *testing.T) {
	// Companion pre-approval is a real grant; it must win even if the turn is
	// otherwise unattended, so voice tools still run.
	if got := decideApprovalOutcome(true, false); got != approvalPreapproved {
		t.Fatalf("companion preapproved = %v, want approvalPreapproved", got)
	}
	if got := decideApprovalOutcome(true, true); got != approvalPreapproved {
		t.Fatalf("companion preapproved + unattended = %v, want approvalPreapproved", got)
	}
}

func TestUnattendedContextFlag(t *testing.T) {
	if unattended(context.Background()) {
		t.Fatal("a plain context must not read as unattended")
	}
	if !unattended(withUnattended(context.Background())) {
		t.Fatal("withUnattended must mark the context")
	}
}

func TestUnattendedApprovalDenialShape(t *testing.T) {
	got := unattendedApprovalDenial("command.run")
	if !strings.HasPrefix(got, "ok:false\n") {
		t.Fatalf("denial must be an ok:false tool result, got %q", got)
	}
	if !strings.Contains(got, "command.run") {
		t.Fatalf("denial must name the refused tool, got %q", got)
	}
}
