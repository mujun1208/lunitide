package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/sessionapp"
)

func TestGetLatestCompactionSummaryNoActiveSummary(t *testing.T) {
	store, err := OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "summary.db"))
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
	store, err := OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "summary-error.db"))
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
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "low-watermark-tokenizer.db"))
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

// TestGetLatestCompactionCheckpointAnswersCoverage pins the P2-2
// hierarchical-context storage contract: the summary comes back together
// with source_end_seq so the assembler knows which messages the summary
// already represents.
func TestGetLatestCompactionCheckpointAnswersCoverage(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "coverage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	summary, endSeq, err := store.GetLatestCompactionCheckpoint(ctx, "missing-session")
	if err != nil || summary != "" || endSeq != 0 {
		t.Fatalf("missing session = (%q,%d,%v), want empty/0/nil", summary, endSeq, err)
	}

	projectID := "01ARZ3NDEKTSV4RRFFQ69G5FA0"
	sessionID := "01ARZ3NDEKTSV4RRFFQ69G5FA1"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.db.ExecContext(ctx, `INSERT INTO projects(id,name,project_code,created_at,updated_at) VALUES(?,?, 'ITM00001', ?,?)`, projectID, "p", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO sessions(id,project_id,title,created_at,updated_at) VALUES(?,?,?,?,?)`, sessionID, projectID, "s", now, now); err != nil {
		t.Fatal(err)
	}
	msgID := ulid.Make().String()
	if _, err := store.db.ExecContext(ctx, `INSERT INTO messages(id,session_id,role,sequence,created_at) VALUES(?,?,'user',1,?)`, msgID, sessionID, now); err != nil {
		t.Fatal(err)
	}
	cpID := ulid.Make().String()
	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO compaction_checkpoints(id,session_id,version,source_start_id,source_end_id,source_start_seq,source_end_seq,source_digest,summary_schema_version,trigger,status,provider,model,summary_json,human_summary,created_at,completed_at)
		 VALUES(?,?,1,?,?,1,42,?,'1.0','automatic','succeeded','test','test-model','{}','covered turns 1..42',?,?)`,
		cpID, sessionID, msgID, msgID, "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO compaction_activations(session_id,checkpoint_id,revision,updated_at) VALUES(?,?,1,?)`, sessionID, cpID, now); err != nil {
		t.Fatal(err)
	}

	summary, endSeq, err = store.GetLatestCompactionCheckpoint(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if summary != "covered turns 1..42" || endSeq != 42 {
		t.Fatalf("checkpoint = (%q,%d), want (\"covered turns 1..42\",42)", summary, endSeq)
	}
}

func TestMessageSchemaRejectsUnsupportedMultipartMessage(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "multipart-compaction.db"))
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
