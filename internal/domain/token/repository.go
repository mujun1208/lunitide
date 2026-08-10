package token

import "context"

// Repository defines the storage contract for token ledger entries.
type Repository interface {
	// UpsertTokenLedger inserts or replaces a token ledger entry for the given
	// (message_id, provider, model, tokenizer_revision) tuple.
	UpsertTokenLedger(ctx context.Context, entry LedgerEntry) error

	// GetTokenLedger returns the ledger entry for the given tuple, or nil if not found.
	GetTokenLedger(ctx context.Context, messageID, provider, model, tokenizerRevision string) (*LedgerEntry, error)

	// ListTokenLedgerByMessage returns all ledger entries for a message.
	ListTokenLedgerByMessage(ctx context.Context, messageID string) ([]LedgerEntry, error)

	// SumTokenLedgerBySession returns the total token count for all messages in a session
	// matching the given provider, model, and tokenizer revision.
	SumTokenLedgerBySession(ctx context.Context, sessionID, provider, model, tokenizerRevision string) (int64, error)

	// DeleteTokenLedgerByMessage removes all ledger entries for a message.
	DeleteTokenLedgerByMessage(ctx context.Context, messageID string) error
}