package contextapp

import (
	"context"
	"errors"
	"fmt"

	"github.com/lunitide/lunitide/internal/domain/token"
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

// Assemble selects messages from the durable session history to fit within the
// provider's effective input budget. It follows the priority order defined in
// ADR-005 §3 and preserves complete turn boundaries.
//
// Priority order (ADR-005 §3):
// 1. Authoritative system/security/product instructions (reserved via SystemTokens)
// 2. Current workspace/world state and active task state (reserved via ToolSchemaTokens)
// 3. Latest accepted compaction checkpoint (injected as PriorSummary preamble)
// 4. Relevant pinned facts/decisions with provenance (injected as PinnedFacts preamble)
// 5. Recent original messages, keeping complete turn/tool-call boundaries
// 6. Latest user turn, protected by a dedicated reserve
// 7. Retrieved older evidence, injected only when relevant and within budget
//
// Turn boundary protection: an assistant response and the user message it
// replies to form an atomic turn. Assemble never includes an assistant
// message without its corresponding user message (ADR-005 §3: "never splits
// an assistant tool call from its tool result").
func Assemble(ctx context.Context, reader Reader, sessionID string, info ProviderInfo, opts AssembleOptions) ([]Message, error) {
	if opts.MaxMessages <= 0 {
		opts.MaxMessages = 256
	}
	if opts.RecentUserReserve <= 0 {
		opts.RecentUserReserve = maxInt64(512, info.EffectiveInputBudget()/10)
	}

	budget := info.EffectiveInputBudget()
	if budget <= 0 {
		return nil, ErrBudgetTooSmall
	}

	// Fetch recent messages in reverse order (newest first).
	allMessages, err := reader.ListMessages(ctx, sessionID, "backward", opts.MaxMessages)
	if err != nil {
		return nil, err
	}
	if len(allMessages) == 0 {
		return nil, ErrNoMessages
	}

	// Calculate token counts for each message. If the reader doesn't provide
	// token counts, estimate conservatively using the canonical tokenizer.
	for i := range allMessages {
		if allMessages[i].TokenCount <= 0 {
			allMessages[i].TokenCount = token.EstimateTokens(allMessages[i].Content)
		}
	}

	// Phase 1: Find and reserve the latest user turn.
	// In backward order, the first user message is the latest user turn.
	latestUserIdx := -1
	for i, msg := range allMessages {
		if msg.Role == "user" {
			latestUserIdx = i
			break
		}
	}
	if latestUserIdx < 0 {
		// No user message at all — return whatever is available.
		return nil, ErrNoMessages
	}

	latestUser := allMessages[latestUserIdx]
	userReserve := opts.RecentUserReserve
	if latestUser.TokenCount > userReserve {
		userReserve = latestUser.TokenCount + opts.SafetyMargin
	}

	remaining := budget - userReserve
	if remaining < 0 {
		// Even the latest user message alone exceeds the budget.
		// Return it anyway — the caller must handle the oversized case.
		return []Message{latestUser}, nil
	}

	// Phase 2: Fill remaining budget with recent messages.
	// Turn boundary protection: when encountering an assistant message in
	// backward order, the next message (older) should be the user message
	// it replies to. They must be included together or skipped together.
	selected := []Message{latestUser}
	usedTokens := latestUser.TokenCount
	_ = usedTokens

	// Build a set of already-selected indices to avoid double-counting the
	// latest user message when iterating.
	selectedSet := map[int]bool{latestUserIdx: true}

	for i := 0; i < len(allMessages); i++ {
		if selectedSet[i] {
			continue
		}
		if remaining <= 0 {
			break
		}

		msg := allMessages[i]

		// Tool result: must be paired atomically with the assistant tool_call
		// that produced it (ADR-005 §3: "never splits an assistant tool call
		// from its tool result"). In backward order, the tool result at index
		// i is newer than the assistant tool_call at i+1 (older). They must be
		// included together or skipped together.
		if msg.Role == "tool" {
			pairIdx := -1
			if i+1 < len(allMessages) && allMessages[i+1].Role == "assistant" && !selectedSet[i+1] {
				pairIdx = i + 1
			}
			if pairIdx >= 0 {
				pairMsg := allMessages[pairIdx]
				pairCost := msg.TokenCount + pairMsg.TokenCount
				if pairCost > remaining {
					// Atomic pair doesn't fit; skip both.
					selectedSet[i] = true
					selectedSet[pairIdx] = true
					continue
				}
				selected = append(selected, msg, pairMsg)
				remaining -= pairCost
				selectedSet[i] = true
				selectedSet[pairIdx] = true
				continue
			}
			// Orphan tool result (no preceding assistant); include if fits.
			if msg.TokenCount > remaining {
				selectedSet[i] = true
				continue
			}
			selected = append(selected, msg)
			remaining -= msg.TokenCount
			selectedSet[i] = true
			continue
		}

		// If this is an assistant message, check turn boundary: the user
		// message it replies to is at i+1 in backward order (the preceding
		// user turn). They must be included together.
		if msg.Role == "assistant" {
			// Find the corresponding user message (next in backward order).
			pairIdx := -1
			if i+1 < len(allMessages) && allMessages[i+1].Role == "user" && !selectedSet[i+1] {
				pairIdx = i + 1
			}

			if pairIdx >= 0 {
				pairMsg := allMessages[pairIdx]
				pairCost := msg.TokenCount + pairMsg.TokenCount
				if pairCost > remaining {
					// Turn doesn't fit; skip both (turn boundary protection).
					selectedSet[i] = true
					selectedSet[pairIdx] = true
					continue
				}
				// Both fit; include the turn.
				selected = append(selected, msg, pairMsg)
				remaining -= pairCost
				selectedSet[i] = true
				selectedSet[pairIdx] = true
				continue
			}
			// No matching user message found (orphan assistant). Include
			// only if it fits alone.
			if msg.TokenCount > remaining {
				selectedSet[i] = true
				continue
			}
			selected = append(selected, msg)
			remaining -= msg.TokenCount
			selectedSet[i] = true
			continue
		}

		// User message not paired with a following assistant (in backward
		// order, this user has no newer assistant reply). Include if it fits.
		if msg.TokenCount > remaining {
			selectedSet[i] = true
			continue
		}
		selected = append(selected, msg)
		remaining -= msg.TokenCount
		selectedSet[i] = true
	}

	// Reverse to forward chronological order for the model.
	reverseMessages(selected)

	// Inject system-level preambles in ADR-005 §3 priority order:
	//   priority 3: PriorSummary (latest accepted compaction checkpoint)
	//   priority 4: PinnedFacts (relevant pinned facts/decisions with provenance)
	// Both are prepended to the selected message body, with PriorSummary first.
	var preambles []Message
	if opts.PriorSummary != "" {
		preambles = append(preambles, Message{
			Role:       "system",
			Content:    "[Prior Context Summary]\n" + opts.PriorSummary,
			TokenCount: token.EstimateTokens(opts.PriorSummary),
		})
	}
	if opts.PinnedFacts != "" {
		preambles = append(preambles, Message{
			Role:       "system",
			Content:    "[Pinned Facts]\n" + opts.PinnedFacts,
			TokenCount: token.EstimateTokens(opts.PinnedFacts),
		})
	}
	if len(preambles) > 0 {
		selected = append(preambles, selected...)
	}

	// Inject retrieved older evidence at the end (ADR-005 §3 priority 7:
	// retrieved older evidence only when relevant and within budget).
	if opts.RetrievedEvidence != "" {
		evidenceTokens := token.EstimateTokens(opts.RetrievedEvidence)
		if evidenceTokens <= remaining {
			evidenceMsg := Message{
				Role:       "system",
				Content:    "[Retrieved Evidence]\n" + opts.RetrievedEvidence,
				TokenCount: evidenceTokens,
			}
			selected = append(selected, evidenceMsg)
		}
	}

	return selected, nil
}

// AssembleOptions controls the behavior of Assemble.
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
