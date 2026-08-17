package contextapp

import (
	"github.com/lunitide/lunitide/internal/domain/token"
)

// SourceAuthority represents the authority level of a context source.
// Higher authority sources are always included and take precedence over
// lower authority sources when budget is constrained (ADR-005 §3).
type SourceAuthority int

const (
	// AuthorityAuthoritative: system/security/product instructions (priority 1).
	// These are reserved before message selection and never dropped.
	AuthorityAuthoritative SourceAuthority = 100
	// AuthorityWorkspace: current workspace/world state and active task state
	// (priority 2). Reserved before message selection.
	AuthorityWorkspace SourceAuthority = 90
	// AuthorityCheckpoint: latest accepted compaction checkpoint summary
	// (priority 3). Injected as system preamble, participates in budget.
	AuthorityCheckpoint SourceAuthority = 80
	// AuthorityPinned: relevant pinned facts/decisions with provenance
	// (priority 4). Injected as system preamble, participates in budget.
	AuthorityPinned SourceAuthority = 70
	// AuthorityRecent: recent original messages with complete turn boundaries
	// (priority 5). Selected within remaining budget.
	AuthorityRecent SourceAuthority = 50
	// AuthorityLatestUser: latest user turn (priority 6). Protected by a
	// dedicated reserve.
	AuthorityLatestUser SourceAuthority = 60
	// AuthorityEvidence: retrieved older evidence (priority 7). Injected only
	// when relevant and within remaining budget.
	AuthorityEvidence SourceAuthority = 30
)

// SourceType identifies the kind of context source.
type SourceType string

const (
	SourceSystemInstruction SourceType = "system_instruction"
	SourceWorkspaceState    SourceType = "workspace_state"
	SourceTaskState         SourceType = "task_state"
	SourceCompactionSummary SourceType = "compaction_summary"
	SourcePinnedFacts       SourceType = "pinned_facts"
	SourceRecentMessage     SourceType = "recent_message"
	SourceLatestUserTurn    SourceType = "latest_user_turn"
	SourceRetrievedEvidence SourceType = "retrieved_evidence"
	SourceHandoffCapsule    SourceType = "handoff_capsule"
	SourceAttachmentExcerpt SourceType = "attachment_excerpt"
)

// ContextSource is a typed input to context assembly. Each source carries
// enough metadata to make selection decisions, trace provenance, and enforce
// fail-closed readability rules (ADR-005 §3).
type ContextSource struct {
	// Type identifies the kind of source (see SourceType constants).
	Type SourceType
	// ID is a stable identifier for this source (e.g., message ULID,
	// checkpoint ID, capsule ID). May be empty for synthetic sources.
	ID string
	// Revision is the version/revision of the source content (e.g.,
	// checkpoint version, tokenizer revision). May be empty.
	Revision string
	// Provenance describes where this source originated (e.g.,
	// "session:01H...:checkpoint:01H...", "handoff:capsule:01H...").
	// Used for diagnostics and audit; never exposed to the model as
	// authoritative content.
	Provenance string
	// Authority is the priority level (see SourceAuthority constants).
	Authority SourceAuthority
	// Content is the rendered text to include in the assembled prompt.
	Content string
	// TokenCount is the canonical token estimate for Content. If zero,
	// AssembleEnvelope will compute it via token.EstimateTokens.
	TokenCount int64
	// Deleted is true when the underlying source has been logically deleted.
	// AssembleEnvelope must fail-closed: a deleted source is never included.
	Deleted bool
	// CoverageEndSequence is set for SourceCompactionSummary sources: the
	// durable sequence through which the summary covers the session.
	// P2-2 hierarchical context: messages with Sequence <= this value are
	// represented by the summary and are excluded from verbatim projection
	// (the latest user turn stays protected regardless). Zero means
	// coverage unknown — every message projects as before.
	CoverageEndSequence int64
}

// TokenCost returns the token cost of this source, computing it lazily if
// TokenCount was not set.
func (s *ContextSource) TokenCost() int64 {
	if s.TokenCount > 0 {
		return s.TokenCount
	}
	return token.EstimateTokens(s.Content)
}

// ContextEnvelope is the structured input to context assembly. It replaces
// the string-based AssembleOptions with typed sources that carry provenance,
// authority, and readability state (ADR-005 §3).
//
// The envelope is assembled in strict priority order:
//
//  1. AuthoritativeInstructions (reserved, never dropped)
//  2. WorkspaceState + TaskState (reserved, never dropped)
//  3. AcceptedCheckpoint (injected as system preamble, participates in budget)
//  4. PinnedFacts (injected as system preamble, participates in budget)
//  5. RecentTurns (selected within remaining budget, turn-boundary protected)
//  6. LatestUserTurn (protected by dedicated reserve)
//  7. RelatedEvidence (injected only when within remaining budget)
type ContextEnvelope struct {
	// Provider describes the target model's context capabilities and budget.
	Provider ProviderInfo

	// Priority 1: Authoritative system/security/product instructions.
	// These are reserved via Provider.SystemTokens and never appear as
	// message bodies in the assembled output (they are prepended by the
	// caller as gateway.RoleSystem messages).
	AuthoritativeInstructions []ContextSource

	// Priority 2: Workspace state and active task state.
	// Reserved via Provider.ToolSchemaTokens.
	WorkspaceState []ContextSource
	TaskState      []ContextSource

	// Priority 3: Latest accepted compaction checkpoint summary.
	// Injected as a system preamble. Participates in the assembly budget.
	AcceptedCheckpoint *ContextSource

	// Priority 4: Pinned facts/decisions with provenance.
	// Injected as system preambles. Participate in the assembly budget.
	PinnedFacts []ContextSource

	// Priority 5: Recent original messages from durable storage.
	// These are fetched via the Reader and selected within remaining budget
	// with turn-boundary and tool-call/result atomicity protection.
	// This field is populated internally by AssembleEnvelope from the Reader.

	// Priority 6: Latest user turn.
	// Protected by a dedicated token reserve (RecentUserReserve).

	// Priority 7: Retrieved older evidence. This is untrusted derived data and is
	// JSON-quoted and appended to a user message only when its exact rendered
	// form fits within the remaining budget; it never becomes a system message.
	RelatedEvidence []ContextSource

	// HandoffCapsules are provenance-linked summaries from other sessions.
	// They are untrusted user-context data: assembly safely quotes and appends
	// them to a user message, never a provider/system message, regardless of the
	// Authority metadata supplied by a caller.
	HandoffCapsules []ContextSource

	// AttachmentExcerpts are parsed text from user-supplied files attached to
	// the session. They are untrusted evidence and must never become system
	// instructions. Assembly appends them as quoted data to the latest user turn.
	// Only readable (non-deleted, succeeded) attachments are injected; the caller
	// must filter fail-closed.
	AttachmentExcerpts []ContextSource

	// MaxMessages limits the number of messages fetched from the Reader.
	// If zero, defaults to 256.
	MaxMessages int

	// RecentUserReserve is the token budget reserved for the latest user
	// turn. If zero, defaults to max(512, 10% of effective input budget).
	RecentUserReserve int64

	// SafetyMargin is additional headroom for token estimation drift.
	SafetyMargin int64
}

// SelectionTraceEntry records the selection decision for a single candidate
// source during assembly. It is used for diagnostics without leaking
// sensitive content (only metadata is recorded, not source content).
type SelectionTraceEntry struct {
	SourceType   SourceType
	SourceID     string
	Authority    SourceAuthority
	TokenCost    int64
	Selected     bool
	RejectReason string
	Provenance   string
}

// SelectionTrace is the diagnostic output of an assembly operation. It
// records every candidate source and the budget state after assembly.
type SelectionTrace struct {
	// Entries lists every candidate source evaluated, in evaluation order.
	Entries []SelectionTraceEntry

	// EffectiveBudget is the token budget available for message selection
	// after all reserved components are subtracted.
	EffectiveBudget int64

	// UsedTokens is exactly the sum of canonical estimates for final Messages.
	// Non-message provider reservations are reported only in ReservedTokens.
	UsedTokens int64

	// RemainingTokens is the budget left after assembly.
	RemainingTokens int64

	// ReservedTokens breaks down the reserved (non-message) token costs.
	ReservedTokens ReservedTokenBreakdown
}

// ReservedTokenBreakdown details the token costs reserved before message
// selection (priorities 1-2 and fixed overheads).
type ReservedTokenBreakdown struct {
	ReservedOutput     int64
	SystemTokens       int64
	ToolSchemaTokens   int64
	SafetyMargin       int64
	PriorSummary       int64
	PinnedFacts        int64
	HandoffCapsules    int64
	AttachmentExcerpts int64
}

// Total returns the sum of all reserved token costs.
func (r ReservedTokenBreakdown) Total() int64 {
	return r.ReservedOutput + r.SystemTokens + r.ToolSchemaTokens +
		r.SafetyMargin + r.PriorSummary + r.PinnedFacts + r.HandoffCapsules +
		r.AttachmentExcerpts
}
