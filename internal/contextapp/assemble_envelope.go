package contextapp

import (
	"context"
	"errors"
	"fmt"

	"github.com/lunitide/lunitide/internal/domain/token"
)

var (
	// ErrEnvelopeBudgetTooSmall indicates the effective input budget is zero
	// or negative after all reserved components are subtracted.
	ErrEnvelopeBudgetTooSmall = errors.New("context budget too small to assemble any messages")
	// ErrEnvelopeDeletedSource indicates a source marked as deleted was
	// encountered. Assembly fails-closed rather than silently dropping it.
	ErrEnvelopeDeletedSource = errors.New("deleted source encountered in context envelope")
)

// AssembleResult is the output of AssembleEnvelope, containing the assembled
// message sequence and a diagnostic selection trace.
type AssembleResult struct {
	Messages []Message
	Trace    SelectionTrace
}

// AssembleEnvelope assembles a context envelope into a provider-ready message
// sequence following the seven-level priority order defined in ADR-005 §3.
//
// The assembly process:
//
//  1. Compute effective input budget (min of context window and safety
//     ceiling, minus reserved output, system tokens, tool schema tokens,
//     and safety margin).
//  2. Reserve tokens for priority 3 (accepted checkpoint/handoff capsules)
//     and priority 4 (pinned facts). These participate in the budget.
//  3. Fetch recent messages from the Reader and select within remaining
//     budget with turn-boundary and tool-call/result atomicity protection.
//  4. Reserve the latest user turn (priority 6) via a dedicated reserve.
//  5. Inject retrieved evidence (priority 7) only if within remaining budget.
//  6. Build the final message sequence in priority order.
//  7. Validate the provider sequence.
//
// Fail-closed rules:
//   - Any source marked Deleted causes an immediate error.
//   - If the budget is too small to include even the latest user turn, the
//     latest user turn is returned alone (the caller must handle oversized
//     input).
//   - PriorSummary and PinnedFacts are not silently dropped if they exceed
//     budget; they reduce remaining budget and may cause ErrBudgetTooSmall.
func AssembleEnvelope(ctx context.Context, reader Reader, sessionID string, env ContextEnvelope) (*AssembleResult, error) {
	// Validate fail-closed: no deleted sources allowed.
	for i := range env.AuthoritativeInstructions {
		if env.AuthoritativeInstructions[i].Deleted {
			return nil, fmt.Errorf("%w: authoritative instruction %s", ErrEnvelopeDeletedSource, env.AuthoritativeInstructions[i].ID)
		}
	}
	for i := range env.WorkspaceState {
		if env.WorkspaceState[i].Deleted {
			return nil, fmt.Errorf("%w: workspace state %s", ErrEnvelopeDeletedSource, env.WorkspaceState[i].ID)
		}
	}
	for i := range env.TaskState {
		if env.TaskState[i].Deleted {
			return nil, fmt.Errorf("%w: task state %s", ErrEnvelopeDeletedSource, env.TaskState[i].ID)
		}
	}
	if env.AcceptedCheckpoint != nil && env.AcceptedCheckpoint.Deleted {
		return nil, fmt.Errorf("%w: accepted checkpoint %s", ErrEnvelopeDeletedSource, env.AcceptedCheckpoint.ID)
	}
	for i := range env.PinnedFacts {
		if env.PinnedFacts[i].Deleted {
			return nil, fmt.Errorf("%w: pinned facts %s", ErrEnvelopeDeletedSource, env.PinnedFacts[i].ID)
		}
	}
	for i := range env.RelatedEvidence {
		if env.RelatedEvidence[i].Deleted {
			return nil, fmt.Errorf("%w: related evidence %s", ErrEnvelopeDeletedSource, env.RelatedEvidence[i].ID)
		}
	}
	for i := range env.HandoffCapsules {
		if env.HandoffCapsules[i].Deleted {
			return nil, fmt.Errorf("%w: handoff capsule %s", ErrEnvelopeDeletedSource, env.HandoffCapsules[i].ID)
		}
	}

	if env.MaxMessages <= 0 {
		env.MaxMessages = 256
	}

	trace := SelectionTrace{}

	// Compute reserved token costs for preambles (priorities 3, 4, handoff).
	var priorSummaryTokens, pinnedFactsTokens, handoffCapsuleTokens int64
	if env.AcceptedCheckpoint != nil && env.AcceptedCheckpoint.Content != "" {
		priorSummaryTokens = env.AcceptedCheckpoint.TokenCost()
	}
	for i := range env.PinnedFacts {
		pinnedFactsTokens += env.PinnedFacts[i].TokenCost()
	}
	for i := range env.HandoffCapsules {
		handoffCapsuleTokens += env.HandoffCapsules[i].TokenCost()
	}

	trace.ReservedTokens = ReservedTokenBreakdown{
		ReservedOutput:   env.Provider.ReservedOutput,
		SystemTokens:     env.Provider.SystemTokens,
		ToolSchemaTokens: env.Provider.ToolSchemaTokens,
		SafetyMargin:     env.Provider.SafetyMargin,
		PriorSummary:     priorSummaryTokens,
		PinnedFacts:      pinnedFactsTokens,
		HandoffCapsules:  handoffCapsuleTokens,
	}

	// Effective input budget after all fixed reservations.
	budget := env.Provider.EffectiveInputBudget()
	if budget <= 0 {
		return nil, ErrEnvelopeBudgetTooSmall
	}

	// Subtract preamble token costs from the message selection budget.
	// Preambles (priorities 3, 4, handoff) are mandatory once provided;
	// they reduce the budget available for recent messages.
	preambleTokens := priorSummaryTokens + pinnedFactsTokens + handoffCapsuleTokens
	messageBudget := budget - preambleTokens
	if messageBudget < 0 {
		return nil, ErrEnvelopeBudgetTooSmall
	}

	trace.EffectiveBudget = budget

	// Reserve the latest user turn (priority 6).
	recentUserReserve := env.RecentUserReserve
	if recentUserReserve <= 0 {
		recentUserReserve = maxInt64(512, budget/10)
	}

	// Fetch recent messages in reverse order (newest first).
	allMessages, err := reader.ListMessages(ctx, sessionID, "backward", env.MaxMessages)
	if err != nil {
		return nil, err
	}
	if len(allMessages) == 0 {
		return nil, ErrNoMessages
	}

	// Calculate token counts for each message.
	for i := range allMessages {
		if allMessages[i].TokenCount <= 0 {
			allMessages[i].TokenCount = token.EstimateTokens(allMessages[i].Content)
		}
	}

	// Phase 1: Find and reserve the latest user turn (priority 6).
	latestUserIdx := -1
	for i, msg := range allMessages {
		if msg.Role == "user" {
			latestUserIdx = i
			break
		}
	}
	if latestUserIdx < 0 {
		return nil, ErrNoMessages
	}

	latestUser := allMessages[latestUserIdx]
	userReserve := recentUserReserve
	if latestUser.TokenCount > userReserve {
		userReserve = latestUser.TokenCount + env.SafetyMargin
	}

	remaining := messageBudget - userReserve
	if remaining < 0 {
		// Even the latest user message alone exceeds the budget.
		// Return it anyway — the caller must handle the oversized case.
		// Still inject preambles if they fit.
		result := &AssembleResult{
			Messages: []Message{latestUser},
			Trace:    trace,
		}
		result.Trace.UsedTokens = latestUser.TokenCount
		result.Trace.RemainingTokens = 0
		// Trace the latest user turn.
		result.Trace.Entries = append(result.Trace.Entries, SelectionTraceEntry{
			SourceType:   SourceLatestUserTurn,
			SourceID:     latestUser.ID,
			Authority:    AuthorityLatestUser,
			TokenCost:    latestUser.TokenCount,
			Selected:     true,
			Provenance:   fmt.Sprintf("session:%s:message:%s", sessionID, latestUser.ID),
			RejectReason: "",
		})
		result = injectPreambles(result, env)
		return result, nil
	}

	// Phase 2: Fill remaining budget with recent messages (priority 5).
	// Turn boundary and tool-call/result atomicity protection.
	selected := []Message{latestUser}
	selectedSet := map[int]bool{latestUserIdx: true}
	trace.Entries = append(trace.Entries, SelectionTraceEntry{
		SourceType: SourceLatestUserTurn,
		SourceID:   latestUser.ID,
		Authority:  AuthorityLatestUser,
		TokenCost:  latestUser.TokenCount,
		Selected:   true,
		Provenance: fmt.Sprintf("session:%s:message:%s", sessionID, latestUser.ID),
	})

	for i := 0; i < len(allMessages); i++ {
		if selectedSet[i] {
			continue
		}
		if remaining <= 0 {
			// Record remaining candidates as rejected due to budget.
			msg := allMessages[i]
			if !selectedSet[i] {
				trace.Entries = append(trace.Entries, SelectionTraceEntry{
					SourceType:   SourceRecentMessage,
					SourceID:     msg.ID,
					Authority:    AuthorityRecent,
					TokenCost:    msg.TokenCount,
					Selected:     false,
					RejectReason: "budget_exhausted",
					Provenance:   fmt.Sprintf("session:%s:message:%s", sessionID, msg.ID),
				})
			}
			continue
		}

		msg := allMessages[i]

		// Tool result: atomic pairing with preceding assistant tool_call.
		if msg.Role == "tool" {
			pairIdx := -1
			if i+1 < len(allMessages) && allMessages[i+1].Role == "assistant" && !selectedSet[i+1] {
				pairIdx = i + 1
			}
			if pairIdx >= 0 {
				pairMsg := allMessages[pairIdx]
				pairCost := msg.TokenCount + pairMsg.TokenCount
				if pairCost > remaining {
					selectedSet[i] = true
					selectedSet[pairIdx] = true
					trace.Entries = append(trace.Entries,
						SelectionTraceEntry{SourceType: SourceRecentMessage, SourceID: msg.ID, Authority: AuthorityRecent, TokenCost: msg.TokenCount, Selected: false, RejectReason: "atomic_pair_over_budget", Provenance: fmt.Sprintf("session:%s:message:%s", sessionID, msg.ID)},
						SelectionTraceEntry{SourceType: SourceRecentMessage, SourceID: pairMsg.ID, Authority: AuthorityRecent, TokenCost: pairMsg.TokenCount, Selected: false, RejectReason: "atomic_pair_over_budget", Provenance: fmt.Sprintf("session:%s:message:%s", sessionID, pairMsg.ID)},
					)
					continue
				}
				selected = append(selected, msg, pairMsg)
				remaining -= pairCost
				selectedSet[i] = true
				selectedSet[pairIdx] = true
				trace.Entries = append(trace.Entries,
					SelectionTraceEntry{SourceType: SourceRecentMessage, SourceID: msg.ID, Authority: AuthorityRecent, TokenCost: msg.TokenCount, Selected: true, Provenance: fmt.Sprintf("session:%s:message:%s", sessionID, msg.ID)},
					SelectionTraceEntry{SourceType: SourceRecentMessage, SourceID: pairMsg.ID, Authority: AuthorityRecent, TokenCost: pairMsg.TokenCount, Selected: true, Provenance: fmt.Sprintf("session:%s:message:%s", sessionID, pairMsg.ID)},
				)
				continue
			}
			// Orphan tool result.
			if msg.TokenCount > remaining {
				selectedSet[i] = true
				trace.Entries = append(trace.Entries, SelectionTraceEntry{SourceType: SourceRecentMessage, SourceID: msg.ID, Authority: AuthorityRecent, TokenCost: msg.TokenCount, Selected: false, RejectReason: "over_budget_orphan_tool", Provenance: fmt.Sprintf("session:%s:message:%s", sessionID, msg.ID)})
				continue
			}
			selected = append(selected, msg)
			remaining -= msg.TokenCount
			selectedSet[i] = true
			trace.Entries = append(trace.Entries, SelectionTraceEntry{SourceType: SourceRecentMessage, SourceID: msg.ID, Authority: AuthorityRecent, TokenCost: msg.TokenCount, Selected: true, Provenance: fmt.Sprintf("session:%s:message:%s", sessionID, msg.ID)})
			continue
		}

		// Assistant message: turn boundary protection.
		if msg.Role == "assistant" {
			pairIdx := -1
			if i+1 < len(allMessages) && allMessages[i+1].Role == "user" && !selectedSet[i+1] {
				pairIdx = i + 1
			}
			if pairIdx >= 0 {
				pairMsg := allMessages[pairIdx]
				pairCost := msg.TokenCount + pairMsg.TokenCount
				if pairCost > remaining {
					selectedSet[i] = true
					selectedSet[pairIdx] = true
					trace.Entries = append(trace.Entries,
						SelectionTraceEntry{SourceType: SourceRecentMessage, SourceID: msg.ID, Authority: AuthorityRecent, TokenCost: msg.TokenCount, Selected: false, RejectReason: "turn_boundary_over_budget", Provenance: fmt.Sprintf("session:%s:message:%s", sessionID, msg.ID)},
						SelectionTraceEntry{SourceType: SourceRecentMessage, SourceID: pairMsg.ID, Authority: AuthorityRecent, TokenCost: pairMsg.TokenCount, Selected: false, RejectReason: "turn_boundary_over_budget", Provenance: fmt.Sprintf("session:%s:message:%s", sessionID, pairMsg.ID)},
					)
					continue
				}
				selected = append(selected, msg, pairMsg)
				remaining -= pairCost
				selectedSet[i] = true
				selectedSet[pairIdx] = true
				trace.Entries = append(trace.Entries,
					SelectionTraceEntry{SourceType: SourceRecentMessage, SourceID: msg.ID, Authority: AuthorityRecent, TokenCost: msg.TokenCount, Selected: true, Provenance: fmt.Sprintf("session:%s:message:%s", sessionID, msg.ID)},
					SelectionTraceEntry{SourceType: SourceRecentMessage, SourceID: pairMsg.ID, Authority: AuthorityRecent, TokenCost: pairMsg.TokenCount, Selected: true, Provenance: fmt.Sprintf("session:%s:message:%s", sessionID, pairMsg.ID)},
				)
				continue
			}
			// Orphan assistant.
			if msg.TokenCount > remaining {
				selectedSet[i] = true
				trace.Entries = append(trace.Entries, SelectionTraceEntry{SourceType: SourceRecentMessage, SourceID: msg.ID, Authority: AuthorityRecent, TokenCost: msg.TokenCount, Selected: false, RejectReason: "over_budget_orphan_assistant", Provenance: fmt.Sprintf("session:%s:message:%s", sessionID, msg.ID)})
				continue
			}
			selected = append(selected, msg)
			remaining -= msg.TokenCount
			selectedSet[i] = true
			trace.Entries = append(trace.Entries, SelectionTraceEntry{SourceType: SourceRecentMessage, SourceID: msg.ID, Authority: AuthorityRecent, TokenCost: msg.TokenCount, Selected: true, Provenance: fmt.Sprintf("session:%s:message:%s", sessionID, msg.ID)})
			continue
		}

		// User message without a newer assistant reply.
		if msg.TokenCount > remaining {
			selectedSet[i] = true
			trace.Entries = append(trace.Entries, SelectionTraceEntry{SourceType: SourceRecentMessage, SourceID: msg.ID, Authority: AuthorityRecent, TokenCost: msg.TokenCount, Selected: false, RejectReason: "over_budget_user", Provenance: fmt.Sprintf("session:%s:message:%s", sessionID, msg.ID)})
			continue
		}
		selected = append(selected, msg)
		remaining -= msg.TokenCount
		selectedSet[i] = true
		trace.Entries = append(trace.Entries, SelectionTraceEntry{SourceType: SourceRecentMessage, SourceID: msg.ID, Authority: AuthorityRecent, TokenCost: msg.TokenCount, Selected: true, Provenance: fmt.Sprintf("session:%s:message:%s", sessionID, msg.ID)})
	}

	// Reverse selected messages to forward chronological order.
	reverseMessages(selected)

	result := &AssembleResult{
		Messages: selected,
		Trace:    trace,
	}

	// Inject system-level preambles (priorities 3, 4, handoff) in order.
	result = injectPreambles(result, env)

	// Update remaining budget after preamble injection.
	result.Trace.RemainingTokens = remaining

	// Inject retrieved evidence (priority 7) if within remaining budget.
	for i := range env.RelatedEvidence {
		ev := env.RelatedEvidence[i]
		evCost := ev.TokenCost()
		if evCost <= remaining {
			evidenceMsg := Message{
				Role:       "system",
				Content:    "[Retrieved Evidence]\n" + ev.Content,
				TokenCount: evCost,
			}
			result.Messages = append(result.Messages, evidenceMsg)
			remaining -= evCost
			result.Trace.Entries = append(result.Trace.Entries, SelectionTraceEntry{
				SourceType: SourceRetrievedEvidence,
				SourceID:   ev.ID,
				Authority:  AuthorityEvidence,
				TokenCost:  evCost,
				Selected:   true,
				Provenance: ev.Provenance,
			})
		} else {
			result.Trace.Entries = append(result.Trace.Entries, SelectionTraceEntry{
				SourceType:   SourceRetrievedEvidence,
				SourceID:     ev.ID,
				Authority:    AuthorityEvidence,
				TokenCost:    evCost,
				Selected:     false,
				RejectReason: "over_budget",
				Provenance:   ev.Provenance,
			})
		}
	}

	// Compute total used tokens.
	var used int64
	for _, m := range result.Messages {
		used += m.TokenCount
	}
	result.Trace.UsedTokens = used
	result.Trace.RemainingTokens = remaining

	return result, nil
}

// injectPreambles injects system-level preambles (priorities 3, 4, handoff)
// in strict priority order before the selected message body. It also records
// trace entries for each preamble source.
func injectPreambles(result *AssembleResult, env ContextEnvelope) *AssembleResult {
	var preambles []Message

	// Priority 3: Accepted compaction checkpoint.
	if env.AcceptedCheckpoint != nil && env.AcceptedCheckpoint.Content != "" {
		src := env.AcceptedCheckpoint
		preambles = append(preambles, Message{
			Role:       "system",
			Content:    "[Prior Context Summary]\n" + src.Content,
			TokenCount: src.TokenCost(),
		})
		result.Trace.Entries = append(result.Trace.Entries, SelectionTraceEntry{
			SourceType: SourceCompactionSummary,
			SourceID:   src.ID,
			Authority:  src.Authority,
			TokenCost:  src.TokenCost(),
			Selected:   true,
			Provenance: src.Provenance,
		})
	}

	// Handoff capsules: treated as untrusted context at checkpoint authority.
	// Tagged with provenance, never elevated above system preamble level.
	for i := range env.HandoffCapsules {
		capsule := env.HandoffCapsules[i]
		preambles = append(preambles, Message{
			Role:       "system",
			Content:    "[Handoff Context]\n" + capsule.Content,
			TokenCount: capsule.TokenCost(),
		})
		result.Trace.Entries = append(result.Trace.Entries, SelectionTraceEntry{
			SourceType: SourceHandoffCapsule,
			SourceID:   capsule.ID,
			Authority:  capsule.Authority,
			TokenCost:  capsule.TokenCost(),
			Selected:   true,
			Provenance: capsule.Provenance,
		})
	}

	// Priority 4: Pinned facts.
	for i := range env.PinnedFacts {
		facts := env.PinnedFacts[i]
		preambles = append(preambles, Message{
			Role:       "system",
			Content:    "[Pinned Facts]\n" + facts.Content,
			TokenCount: facts.TokenCost(),
		})
		result.Trace.Entries = append(result.Trace.Entries, SelectionTraceEntry{
			SourceType: SourcePinnedFacts,
			SourceID:   facts.ID,
			Authority:  facts.Authority,
			TokenCost:  facts.TokenCost(),
			Selected:   true,
			Provenance: facts.Provenance,
		})
	}

	if len(preambles) > 0 {
		result.Messages = append(preambles, result.Messages...)
	}

	return result
}
