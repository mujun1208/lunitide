package app

import (
	"context"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/bridge"
)

type stubAuditVerifier struct{ err error }

func (s stubAuditVerifier) VerifyAuditChain(context.Context) error { return s.err }

// TestM7AuditGuardBlocksOnBrokenGeneralChain proves W3's chain now has teeth:
// a broken audit_events chain freezes production promotion with M7-DR-001,
// exactly like the m7 ledger.
func TestM7AuditGuardBlocksOnBrokenGeneralChain(t *testing.T) {
	e := &Engine{auditVerifier: stubAuditVerifier{err: audit.ErrChainBroken}}
	resp := m7AuditGuard(e, context.Background(), bridge.Request{ID: "1"})
	if resp == nil || resp.Error == nil || resp.Error.Code != "M7-DR-001" {
		t.Fatalf("broken chain: got %+v", resp)
	}
}

// TestM7AuditGuardUnreadableChainIsRetryable maps a non-tamper read error onto
// the retryable storage code rather than the freeze code.
func TestM7AuditGuardUnreadableChainIsRetryable(t *testing.T) {
	e := &Engine{auditVerifier: stubAuditVerifier{err: errors.New("db closed")}}
	resp := m7AuditGuard(e, context.Background(), bridge.Request{ID: "1"})
	if resp == nil || resp.Error == nil || resp.Error.Code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("unreadable chain: got %+v", resp)
	}
}

// TestM7AuditGuardPassesOnIntactChain confirms an intact chain does not block.
func TestM7AuditGuardPassesOnIntactChain(t *testing.T) {
	e := &Engine{auditVerifier: stubAuditVerifier{err: nil}}
	if resp := m7AuditGuard(e, context.Background(), bridge.Request{ID: "1"}); resp != nil {
		t.Fatalf("intact chain should pass, got %+v", resp)
	}
}

// TestM7AuditGuardNoVerifiersPasses preserves the original behaviour: with no
// verifier wired the guard is a no-op.
func TestM7AuditGuardNoVerifiersPasses(t *testing.T) {
	if resp := m7AuditGuard(&Engine{}, context.Background(), bridge.Request{ID: "1"}); resp != nil {
		t.Fatalf("no verifiers should pass, got %+v", resp)
	}
}
