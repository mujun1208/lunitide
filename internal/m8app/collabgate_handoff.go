// collabgate_handoff.go is the S2 write-collaboration shared-session
// handoff, gate-guarded and default-off.
//
// FREEZE RELATIONSHIP: the collab-gate freeze is "the write-collaboration
// capability stays DISABLED unless an operator confirms an enable decision
// (single-use token, binding-pinned)". S2 does NOT change that: preparing a
// handoff runs the exact same capabilityLocked preflight CheckGate uses, so
// with no confirmed decision (the default) every handoff request is refused
// with M8-028 and zero side effects beyond an audited refusal. Only when the
// gate has been explicitly enabled does a handoff ticket get minted, and
// that too is audited. The engine layer additionally requires the default-
// off CollabHandoff governance switch before it ever calls this.
package m8app

import (
	"context"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// WriteCollabHandoffTTL bounds a prepared handoff ticket's validity window.
const WriteCollabHandoffTTL = 10 * time.Minute

// WriteCollabHandoffInput is the S2 prepare command.
type WriteCollabHandoffInput struct {
	SubjectID string
	SessionID string
	Summary   string // human-readable description of the work being handed off
}

// WriteCollabHandoffTicket authorizes one shared-session write-collaboration
// handoff. It is minted only while the gate capability is enabled.
type WriteCollabHandoffTicket struct {
	TicketID      string `json:"ticketId"`
	SubjectID     string `json:"subjectId"`
	SessionID     string `json:"sessionId"`
	SummaryDigest string `json:"summaryDigest"`
	Token         string `json:"token"`
	ExpiresAt     string `json:"expiresAt"`
	CreatedAt     string `json:"createdAt"`
}

// PrepareWriteCollabHandoff mints a handoff ticket IFF the collab-gate
// capability is currently enabled for the subject; otherwise it audits a
// refusal and answers ErrGateDisabled (M8-028). It reuses the same
// capabilityLocked preflight as CheckGate, so binding drift also refuses.
func (s *CollabGateService) PrepareWriteCollabHandoff(ctx context.Context, in WriteCollabHandoffInput) (WriteCollabHandoffTicket, error) {
	if s == nil || s.uow == nil {
		return WriteCollabHandoffTicket{}, ErrServiceUnavailable
	}
	if len(in.SubjectID) < 1 || len(in.SubjectID) > 128 {
		return WriteCollabHandoffTicket{}, ErrPayloadInvalid
	}
	if len(in.SessionID) < 1 || len(in.SessionID) > 128 {
		return WriteCollabHandoffTicket{}, ErrPayloadInvalid
	}
	var out WriteCollabHandoffTicket
	err := s.uow.TransactCollabGate(ctx, func(tx CollabGateTx) error {
		capability, decision, cerr := s.capabilityLocked(tx, in.SubjectID)
		if cerr != nil {
			return cerr
		}
		now := s.clock.Now().UTC()
		if capability != m8core.CapabilityEnabled {
			_, _ = tx.AppendAuditEvent(audit.Event{
				ID: ulid.Make().String(), Action: "collabGate.handoff.refused",
				ResourceType: "collab_gate_handoff", ResourceID: in.SubjectID,
				Actor: in.SubjectID, CreatedAt: now.Format(time.RFC3339),
			})
			return ErrGateDisabled
		}
		token, terr := newDecisionToken()
		if terr != nil {
			return terr
		}
		digest := m8core.DigestOf(in.Summary)
		ticket := WriteCollabHandoffTicket{
			TicketID:      ulid.Make().String(),
			SubjectID:     in.SubjectID,
			SessionID:     in.SessionID,
			SummaryDigest: digest,
			Token:         token,
			ExpiresAt:     now.Add(WriteCollabHandoffTTL).Format(time.RFC3339),
			CreatedAt:     now.Format(time.RFC3339),
		}
		if _, aerr := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "collabGate.handoff.prepared",
			ResourceType: "collab_gate_handoff", ResourceID: ticket.TicketID,
			Actor: in.SubjectID, AfterDigest: digest,
			CorrelationID: decision.DecisionID, CreatedAt: now.Format(time.RFC3339),
		}); aerr != nil {
			return aerr
		}
		out = ticket
		return nil
	})
	if err != nil {
		return WriteCollabHandoffTicket{}, fmt.Errorf("prepare write-collab handoff: %w", err)
	}
	return out, nil
}
