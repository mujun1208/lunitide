package app

import (
	"context"
	"slices"

	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

// An explicit turn still goes through the normal evidence and budget assembly.
// This is used before the first durable message exists; dropping to raw messages
// would silently discard selected attachments and authoritative instructions.
type explicitChatReader struct{ messages []llmadapter.Message }

func (r explicitChatReader) ListMessages(_ context.Context, _ string, direction string, limit int) ([]contextapp.Message, error) {
	var rows []contextapp.Message
	for _, m := range r.messages {
		if m.Role != llmadapter.RoleSystem {
			rows = append(rows, contextapp.Message{Role: string(m.Role), Content: m.Content, Sequence: int64(len(rows) + 1)})
		}
	}
	if direction == "backward" {
		slices.Reverse(rows)
	}
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (explicitChatReader) SumTokens(context.Context, string, string, string, string) (int64, error) {
	return 0, nil
}

func assembleExplicitChat(ctx context.Context, session string, env contextapp.ContextEnvelope, trusted []llmadapter.Message) ([]llmadapter.Message, error) {
	// These sequences belong to the explicit request, not the durable journal.
	// A checkpoint cannot claim coverage of newly supplied messages.
	if env.AcceptedCheckpoint != nil {
		checkpoint := *env.AcceptedCheckpoint
		checkpoint.CoverageEndSequence = 0
		env.AcceptedCheckpoint = &checkpoint
	}
	result, err := contextapp.AssembleEnvelope(ctx, explicitChatReader{messages: trusted}, session, env)
	if err != nil {
		return nil, err
	}
	var system []llmadapter.Message
	for _, m := range trusted {
		if m.Role == llmadapter.RoleSystem {
			system = append(system, m)
		}
	}
	return combineDurableProviderMessages(result.Messages, system, env.Provider)
}
