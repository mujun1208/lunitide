package sqlite

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
)

func TestM3ExitMillionTokenPersistencePaginationAndCleanCloseCopyRecovery(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.db")
	store, err := Open(ctx, sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	projectID := createSessionProject(t, store, "m3-exit-project", "M3 Exit Project")
	created, err := sessionapp.New(store, store).Create(ctx, "m3-exit-session", "m3-exit", struct{ ProjectID, Title string }{projectID, "M3 Exit"}, session.Session{ProjectID: projectID, Title: "M3 Exit"})
	if err != nil {
		t.Fatal(err)
	}
	messages := newMessageApp(t, store)
	text := strings.Repeat("context ", 256)
	perMessage := token.EstimateTokens(text)
	if perMessage <= 0 {
		t.Fatal("canonical token estimate is zero")
	}
	messageCount := int((1_000_000+perMessage-1)/perMessage) + 1
	for i := 0; i < messageCount; i++ {
		_, err = messages.Append(ctx, fmt.Sprintf("m3-exit-%06d", i), "m3-exit", struct{ SessionID, Text string }{created.ID, text}, message.Message{SessionID: created.ID, Text: text})
		if err != nil {
			t.Fatalf("append message %d: %v", i+1, err)
		}
	}
	total, err := store.SumTokenLedgerBySession(ctx, created.ID, "", "", token.CanonicalTokenizerRevision)
	if err != nil {
		t.Fatal(err)
	}
	if total < 1_000_000 {
		t.Fatalf("logical tokens=%d, want at least 1000000", total)
	}
	assertM3CompletePagination(t, store, created.ID, messageCount)
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}

	restoreDir := filepath.Join(root, "restored")
	if err = os.Mkdir(restoreDir, 0o700); err != nil {
		t.Fatal(err)
	}
	restoredPath := filepath.Join(restoreDir, "lunitide.db")
	copyFileForM3Drill(t, sourcePath, restoredPath)
	restored, err := Open(ctx, restoredPath)
	if err != nil {
		t.Fatalf("open isolated restored database: %v", err)
	}
	defer restored.Close()
	var integrity string
	if err = restored.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("restored full integrity_check=%q err=%v", integrity, err)
	}
	restoredTotal, err := restored.SumTokenLedgerBySession(ctx, created.ID, "", "", token.CanonicalTokenizerRevision)
	if err != nil {
		t.Fatal(err)
	}
	if restoredTotal != total {
		t.Fatalf("restored logical tokens=%d, source=%d", restoredTotal, total)
	}
	assertM3CompletePagination(t, restored, created.ID, messageCount)
}

func assertM3CompletePagination(t *testing.T, store *Store, sessionID string, want int) {
	t.Helper()
	var boundary, snapshot int64
	count := 0
	const pageLimit = 37
	maxPages := (want+pageLimit-1)/pageLimit + 1
	for page := 0; page < maxPages; page++ {
		previousBoundary := boundary
		items, stableSnapshot, more, err := store.ListMessages(context.Background(), messageapp.PageQuery{SessionID: sessionID, Direction: messageapp.Forward, Snapshot: snapshot, Boundary: boundary, Limit: 37})
		if err != nil {
			t.Fatal(err)
		}
		if snapshot == 0 {
			snapshot = stableSnapshot
		} else if stableSnapshot != snapshot {
			t.Fatalf("snapshot changed from %d to %d", snapshot, stableSnapshot)
		}
		for _, item := range items {
			count++
			if item.Sequence != int64(count) {
				t.Fatalf("sequence[%d]=%d", count, item.Sequence)
			}
			boundary = item.Sequence
		}
		if !more {
			if count != want || snapshot != int64(want) {
				t.Fatalf("paged count=%d snapshot=%d want=%d", count, snapshot, want)
			}
			return
		}
		if len(items) == 0 || boundary <= previousBoundary {
			t.Fatalf("nonterminal page did not advance: boundary=%d previous=%d items=%d", boundary, previousBoundary, len(items))
		}
	}
	t.Fatalf("pagination exceeded %d pages", maxPages)
}

func copyFileForM3Drill(t *testing.T, source, target string) {
	t.Helper()
	in, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err = out.Sync(); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err = out.Close(); err != nil {
		t.Fatal(err)
	}
}
