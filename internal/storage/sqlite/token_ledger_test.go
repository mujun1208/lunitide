package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/token"
)

func TestTokenLedgerPersistsIdentityAndSupportsRevisions(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	projectID, sessionID, messageID := "01ARZ3NDEKTSV4RRFFQ69G5FA0", "01ARZ3NDEKTSV4RRFFQ69G5FA1", "01ARZ3NDEKTSV4RRFFQ69G5FA2"
	stamp := now.Format(time.RFC3339Nano)
	if _, err = store.db.ExecContext(ctx, `INSERT INTO projects(id,name,created_at,updated_at) VALUES(?,?,?,?)`, projectID, "p", stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.ExecContext(ctx, `INSERT INTO sessions(id,project_id,title,created_at,updated_at) VALUES(?,?,?,?,?)`, sessionID, projectID, "s", stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.ExecContext(ctx, `INSERT INTO messages(id,session_id,role,sequence,created_at) VALUES(?,?,'assistant',1,?)`, messageID, sessionID, stamp); err != nil {
		t.Fatal(err)
	}

	entries := []token.LedgerEntry{
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FA3", MessageID: messageID, TokenizerID: token.CanonicalTokenizerID, TokenizerRevision: token.CanonicalTokenizerRevision, TokenCount: 7, EstimationMethod: token.CharRatio, UTF8Bytes: 20, ComputedAt: now},
		{ID: "01ARZ3NDEKTSV4RRFFQ69G5FA4", MessageID: messageID, Provider: "openai_compatible", Model: "gpt-4", TokenizerID: token.ProviderReportTokenizerID, TokenizerRevision: token.ProviderReportTokenizerRevision, TokenCount: 5, EstimationMethod: token.ProviderReport, UTF8Bytes: 20, ComputedAt: now},
	}
	for _, entry := range entries {
		if err = store.UpsertTokenLedger(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.db.ExecContext(ctx, `INSERT INTO token_ledger(id,message_id,provider,model,tokenizer_revision,token_count,estimation_method,utf8_bytes,computed_at,subject_type,subject_id,tokenizer_id) VALUES(?,?, '', '', 'v2.0.0',9,'manual',20,?,'message',?,?)`, "01ARZ3NDEKTSV4RRFFQ69G5FA5", messageID, stamp, messageID, token.CanonicalTokenizerID); err != nil {
		t.Fatalf("second revision rejected: %v", err)
	}
	got, err := store.ListTokenLedgerByMessage(ctx, messageID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("entries = %d, want 3", len(got))
	}
	if got[0].TokenizerID == "" || got[1].TokenizerID == "" {
		t.Fatal("tokenizer identity was not read")
	}
	total, err := store.SumTokenLedgerBySession(ctx, sessionID, "ignored", "ignored", token.CanonicalTokenizerRevision)
	if err != nil || total != 7 {
		t.Fatalf("canonical total = %d, err=%v", total, err)
	}
}
