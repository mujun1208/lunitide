package contextapp

import (
	"context"
	"errors"

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
	// SystemTokens is the estimated token cost of system/developer instructions.
	SystemTokens int64
	// SafetyMargin is additional headroom to avoid edge cases.
	SafetyMargin int64
}

// EffectiveInputBudget returns the number of tokens available for message input.
func (p ProviderInfo) EffectiveInputBudget() int64 {
	ceiling := p.ContextWindow
	if p.SafetyCeiling > 0 && p.SafetyCeiling < ceiling {
		ceiling = p.SafetyCeiling
	}
	budget := ceiling - p.ReservedOutput - p.SystemTokens - p.SafetyMargin
	if budget < 0 {
		return 0
	}
	return budget
}

// Message represents a message to be included in the assembled context.
type Message struct {
	ID          string
	Role        string
	Content     string
	Sequence    int64
	TokenCount  int64
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
// ADR-005 and preserves message ordering.
//
// Priority order:
// 1. Latest user turn (always included, reserved budget)
// 2. Recent messages in reverse chronological order (most recent first)
// 3. Older messages only if budget allows
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
	// token counts, estimate conservatively.
	for i := range allMessages {
		if allMessages[i].TokenCount <= 0 {
			allMessages[i].TokenCount = token.EstimateTokens(allMessages[i].Content)
		}
	}

	// Phase 1: Reserve the latest user turn.
	// The latest message is at index 0 (backward order).
	latestUser := allMessages[0]
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

	// Phase 2: Fill remaining budget with recent messages (excluding the latest user).
	selected := []Message{latestUser}
	usedTokens := latestUser.TokenCount

	for i := 1; i < len(allMessages) && remaining > 0; i++ {
		msg := allMessages[i]
		if msg.TokenCount > remaining {
			// This message doesn't fit; skip it but continue checking older ones.
			continue
		}
		selected = append(selected, msg)
		usedTokens += msg.TokenCount
		remaining -= msg.TokenCount
	}

	// Reverse to forward chronological order for the model.
	reverseMessages(selected)

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