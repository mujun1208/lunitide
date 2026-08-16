package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/sessionapp"
)

func TestGetLatestCompactionSummaryNoActiveSummary(t *testing.T) {
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "summary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	summary, err := store.GetLatestCompactionSummary(context.Background(), "missing-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary != "" {
		t.Fatalf("summary = %q, want empty", summary)
	}
}

func TestGetLatestCompactionSummaryPropagatesDatabaseError(t *testing.T) {
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "summary-error.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.db.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = store.GetLatestCompactionSummary(context.Background(), "missing-session")
	if err == nil {
		t.Fatal("expected database error")
	}
	if !strings.Contains(err.Error(), "get latest compaction summary") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSumTokenLedgerAfterSeqUsesCanonicalIdentityAndRevision(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "low-watermark-tokenizer.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	projectID := "01ARZ3NDEKTSV4RRFFQ69G5FA0"
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FA1"
	now := "2025-01-01T00:00:00Z"
	if _, err := store.db.ExecContext(ctx, `INSERT INTO projects(id,name,project_code,created_at,updated_at) VALUES(?,?, 'ITM00001', ?,?)`, projectID, "p", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO sessions(id,project_id,title,created_at,updated_at) VALUES(?,?,?,?,?)`, sessionID, projectID, "s", now, now); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		id, messageID, tokenizerID, revision, invalidatedAt string
		sequence, tokens                                    int
	}{
		{"01ARZ3NDEKTSV4RRFFQ69G5FA3", "01ARZ3NDEKTSV4RRFFQ69G5FA2", token.CanonicalTokenizerID, token.CanonicalTokenizerRevision, "", 2, 11},
		{"01ARZ3NDEKTSV4RRFFQ69G5FA5", "01ARZ3NDEKTSV4RRFFQ69G5FA4", "other-tokenizer", token.CanonicalTokenizerRevision, "", 3, 100},
		{"01ARZ3NDEKTSV4RRFFQ69G5FA7", "01ARZ3NDEKTSV4RRFFQ69G5FA6", token.CanonicalTokenizerID, "other-revision", "", 4, 1000},
		{"01ARZ3NDEKTSV4RRFFQ69G5FA9", "01ARZ3NDEKTSV4RRFFQ69G5FA8", token.CanonicalTokenizerID, token.CanonicalTokenizerRevision, now, 5, 10000},
	} {
		if _, err := store.db.ExecContext(ctx, `INSERT INTO messages(id,session_id,role,sequence,created_at) VALUES(?,?,'user',?,?)`, entry.messageID, sessionID, entry.sequence, now); err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.ExecContext(ctx,
			`INSERT INTO token_ledger(id,message_id,provider,model,tokenizer_revision,token_count,estimation_method,utf8_bytes,computed_at,subject_type,subject_id,tokenizer_id,invalidated_at)
			 VALUES(?,?, '', '', ?,?,'char-ratio',1,?,'message',?,?,NULLIF(?,''))`,
			entry.id, entry.messageID, entry.revision, entry.tokens, now, entry.messageID, entry.tokenizerID, entry.invalidatedAt); err != nil {
			t.Fatal(err)
		}
	}

	total, err := store.SumTokenLedgerAfterSeq(ctx, sessionID, "ignored-provider", "ignored-model", token.CanonicalTokenizerRevision, 1)
	if err != nil {
		t.Fatal(err)
	}
	if total != 11 {
		t.Fatalf("total = %d, want canonical identity/revision total 11", total)
	}
}

func TestMessageSchemaRejectsUnsupportedMultipartMessage(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "multipart-compaction.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projectID := createSessionProject(t, store, "multipart-project", "Multipart")
	created, err := sessionapp.New(store, store).Create(ctx, "multipart-session", "test", struct{ ProjectID, Title string }{projectID, "Multipart"}, session.Session{ProjectID: projectID, Title: "Multipart"})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := newMessageApp(t, store).Append(ctx, "multipart-message", "test", struct{ SessionID, Text string }{created.ID, "first"}, message.Message{SessionID: created.ID, Text: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.ExecContext(ctx, `INSERT INTO message_parts(message_id,ordinal,type,text) VALUES(?,2,'text','second')`, msg.ID); err == nil {
		t.Fatal("schema accepted unsupported second message part")
	}
	items, err := store.ListMessagesByRange(ctx, created.ID, 1, 1)
	if err != nil || len(items) != 1 || items[0].Content != "first" {
		t.Fatalf("valid single-part message unreadable: items=%#v err=%v", items, err)
	}
}
