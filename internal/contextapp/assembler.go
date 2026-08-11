package contextapp

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrBudgetTooSmall = errors.New("context budget too small to assemble any messages")
	ErrNoMessages     = errors.New("no messages available for assembly")
)

// ProviderInfo describes the target model's context capabilities.
type ProviderInfo struct {
	Provider          string
	Model             string
	TokenizerRevision string
	// ContextWindow is the model's advertised context window in tokens.
	ContextWindow int64
	// SafetyCeiling is the configured maximum input tokens (must be <= ContextWindow).
	SafetyCeiling int64
	// ReservedOutput is the token budget reserved for model output.
	ReservedOutput int64
	// SystemTokens is the estimated token cost of system/developer instructions
	// (ADR-005 §3 priority 1: authoritative system/security/product instructions).
	SystemTokens int64
	// ToolSchemaTokens is the estimated token cost of tool/function schemas
	// injected into the prompt. These are reserved before message selection.
	ToolSchemaTokens int64
	// SafetyMargin is additional headroom to avoid edge cases.
	SafetyMargin int64
}

// EffectiveInputBudget returns the number of tokens available for message input.
// It subtracts reserved output, system instructions, tool schemas, and safety
// margin from the effective ceiling (min of context window and safety ceiling).
func (p ProviderInfo) EffectiveInputBudget() int64 {
	ceiling := p.ContextWindow
	if p.SafetyCeiling > 0 && p.SafetyCeiling < ceiling {
		ceiling = p.SafetyCeiling
	}
	budget := ceiling - p.ReservedOutput - p.SystemTokens - p.ToolSchemaTokens - p.SafetyMargin
	if budget < 0 {
		return 0
	}
	return budget
}

// Message represents a message to be included in the assembled context.
type Message struct {
	ID           string
	Role         string
	Content      string
	Sequence     int64
	TokenCount   int64
	IsCheckpoint bool
}

// Reader provides access to durable messages and token estimates.
type Reader interface {
	// ListMessages returns messages in a session ordered by sequence.
	// direction: "forward" or "backward"; limit: max messages to return.
	ListMessages(ctx context.Context, sessionID string, direction string, limit int) ([]Message, error)

	// SumTokens returns the total token count for all messages in a session
	// matching the given provider/model/tokenizer tuple.
	SumTokens(ctx context.Context, sessionID, provider, model, tokenizerRevision string) (int64, error)
}

// Assemble is the legacy entry point that delegates to AssembleEnvelope.
// New callers should use AssembleEnvelope directly with a typed
// ContextEnvelope. This wrapper exists for backward compatibility.
//
// See AssembleEnvelope for the full seven-level priority assembly.
func Assemble(ctx context.Context, reader Reader, sessionID string, info ProviderInfo, opts AssembleOptions) ([]Message, error) {
	env := ContextEnvelope{
		Provider:          info,
		MaxMessages:       opts.MaxMessages,
		RecentUserReserve: opts.RecentUserReserve,
		SafetyMargin:      opts.SafetyMargin,
	}
	if opts.PriorSummary != "" {
		env.AcceptedCheckpoint = &ContextSource{
			Type:      SourceCompactionSummary,
			Authority: AuthorityCheckpoint,
			Content:   opts.PriorSummary,
			Provenance: fmt.Sprintf("session:%s:checkpoint:latest", sessionID),
		}
	}
	if opts.PinnedFacts != "" {
		env.PinnedFacts = []ContextSource{{
			Type:      SourcePinnedFacts,
			Authority: AuthorityPinned,
			Content:   opts.PinnedFacts,
			Provenance: fmt.Sprintf("session:%s:pinned_facts", sessionID),
		}}
	}
	if opts.RetrievedEvidence != "" {
		env.RelatedEvidence = []ContextSource{{
			Type:      SourceRetrievedEvidence,
			Authority: AuthorityEvidence,
			Content:   opts.RetrievedEvidence,
			Provenance: fmt.Sprintf("session:%s:retrieved_evidence", sessionID),
		}}
	}
	result, err := AssembleEnvelope(ctx, reader, sessionID, env)
	if err != nil {
		// Map envelope errors to legacy error variables for backward compat.
		if err == ErrEnvelopeBudgetTooSmall {
			return nil, ErrBudgetTooSmall
		}
		return nil, err
	}
	return result.Messages, nil
}

// assembleLegacy is the original implementation retained for reference. The
// public Assemble function now delegates to AssembleEnvelope.

// AssembleOptions controls the behavior of the legacy Assemble function.
// New callers should use ContextEnvelope instead.
type AssembleOptions struct {
	// MaxMessages is the maximum number of messages to fetch from storage.
	MaxMessages int
	// RecentUserReserve is the token budget reserved for the latest user turn.
	// If zero, defaults to max(512, 10% of effective budget).
	RecentUserReserve int64
	// SafetyMargin is additional headroom for token estimation inaccuracies.
	SafetyMargin int64
	// PriorSummary is the latest succeeded compaction checkpoint summary.
	// When non-empty, it is injected as a system-level preamble at the
	// beginning of the assembled context (ADR-005 §3 priority 3).
	PriorSummary string
	// PinnedFacts contains pinned facts/decisions with provenance to inject
	// as a system-level preamble (ADR-005 §3 priority 4).
	PinnedFacts string
	// RetrievedEvidence contains older evidence retrieved from memory to
	// inject at the end of the assembled context, only when within budget
	// (ADR-005 §3 priority 7).
	RetrievedEvidence string
}

func reverseMessages(msgs []Message) {
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// ValidateProviderSequence checks that the assembled message sequence conforms
// to provider-neutral validity rules (ADR-005 §3: "never emits an invalid
// provider message sequence"). It enforces:
//   - The sequence is non-empty.
//   - System messages may only appear at the start (before any non-system message).
//   - No two consecutive assistant messages without an intervening user or tool message.
//   - Tool messages must follow an assistant message or another tool message
//     (a single assistant turn may emit multiple tool calls, each with its own
//     tool result).
//   - At least one user message is present.
func ValidateProviderSequence(messages []Message) error {
	if len(messages) == 0 {
		return errors.New("empty message sequence")
	}
	seenNonSystem := false
	hasUser := false
	var prevNonSystemRole string
	for i, m := range messages {
		if m.Role == "system" {
			if seenNonSystem {
				return fmt.Errorf("system message at position %d must precede all non-system messages", i)
			}
			continue
		}
		seenNonSystem = true
		if m.Role == "user" {
			hasUser = true
		}
		if m.Role == "assistant" && prevNonSystemRole == "assistant" {
			return fmt.Errorf("consecutive assistant messages at position %d without intervening user or tool message", i)
		}
		if m.Role == "tool" && prevNonSystemRole != "assistant" && prevNonSystemRole != "tool" {
			return fmt.Errorf("tool message at position %d must follow an assistant or tool message", i)
		}
		prevNonSystemRole = m.Role
	}
	if !hasUser {
		return errors.New("message sequence must contain at least one user message")
	}
	return nil
}
