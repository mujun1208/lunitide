package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/compactionapp"
	"github.com/lunitide/lunitide/internal/domain/compaction"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/sessionapp"
)

type archiveSummary struct{}

func (archiveSummary) Summarize(_ context.Context, _, _, _ string, _, _ int64, messages []compactionapp.SummaryMessage, _ string) (string, string, error) {
	var parts []string
	for _, m := range messages {
		parts = append(parts, m.Content)
	}
	body := strings.Join(parts, "；")
	raw, _ := json.Marshal(map[string]any{"summary": body, "keyPoints": parts, "actionItems": []string{}})
	return string(raw), body, nil
}

func TestWeeklyArchivesCatchUpAndRecallOlderWeekAfterDatabaseReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "weekly.db")
	store, err := OpenTemplated(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if store != nil {
			store.Close()
		}
	}()
	projectID := createSessionProject(t, store, "weekly-project", "Weekly")
	sess, err := sessionapp.New(store, store).Create(ctx, "weekly-session", "test", struct{ Title string }{"月伴对话"}, session.Session{ProjectID: projectID, Title: "月伴对话"})
	if err != nil {
		t.Fatal(err)
	}
	zone := time.FixedZone("this-PC", 8*60*60)
	start := time.Date(2026, 8, 17, 12, 0, 0, 0, zone)
	for week, topic := range []string{"图书馆和科幻小说", "河边散步", "茶叶与陶器", "本周安排"} {
		for index := 0; index < 2; index++ {
			msg, err := newMessageApp(t, store).Append(ctx, fmt.Sprintf("week-%d-%d", week, index), "test", struct{ Text string }{topic}, message.Message{SessionID: sess.ID, Text: topic})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.ExecContext(ctx, `UPDATE messages SET created_at=? WHERE id=?`, formatTime(start.AddDate(0, 0, week*7).UTC()), msg.ID); err != nil {
				t.Fatal(err)
			}
			// Move the fixture's durable receipt with its timestamp as well;
			// startup correctly rejects an edited row with an inconsistent receipt.
			msg.CreatedAt = start.AddDate(0, 0, week*7).UTC()
			receipt, err := json.Marshal(msg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.ExecContext(ctx, `UPDATE idempotency_records SET response_json=? WHERE operation='message.append' AND idempotency_key=?`, string(receipt), fmt.Sprintf("week-%d-%d", week, index)); err != nil {
				t.Fatal(err)
			}
		}
	}
	now := time.Date(2026, 9, 7, 0, 0, 0, 0, zone)
	trigger := compactionapp.NewTrigger(compactionapp.DefaultWatermarkConfig(), store, store, store.CompactionMessageReader())
	executor := compactionapp.NewExecutor(store, store, archiveSummary{})
	for week := 0; week < 3; week++ {
		due, err := store.ListDueCompanionWeeks(ctx, now)
		if err != nil || len(due) != 1 {
			t.Fatalf("due week %d: %+v %v", week, due, err)
		}
		if due[0].StartSeq != int64(week*2+1) || due[0].EndSeq != int64(week*2+2) {
			t.Fatalf("wrong weekly source range: %+v", due[0])
		}
		pending, err := trigger.TriggerCompanionWeek(ctx, due[0], "local-test", "model")
		if err != nil || !pending.Triggered {
			t.Fatalf("weekly trigger: %+v %v", pending, err)
		}
		done, err := executor.Execute(ctx, pending.CheckpointID)
		if err != nil || done.Status != compaction.StatusSucceeded {
			t.Fatalf("weekly summary: %+v %v", done, err)
		}
		if cp, err := store.GetCheckpoint(ctx, pending.CheckpointID); err != nil || cp.PrevCheckpointID != nil {
			t.Fatalf("weekly summaries must cover their own week only: %+v %v", cp, err)
		}
	}
	store.Close()
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if due, err := store.ListDueCompanionWeeks(ctx, now); err != nil || len(due) != 0 {
		t.Fatalf("replayed old/current week: %+v %v", due, err)
	}
	got, err := store.SearchCompanionArchives(ctx, sess.ID, "我以前说的图书馆", 1)
	if err != nil || len(got) != 1 || got[0].Period != "2026-08-17" || !strings.Contains(got[0].Summary, "图书馆") {
		t.Fatalf("old week not recalled after restart: %+v %v", got, err)
	}
	var messages, sessions int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE session_id=?`, sess.ID).Scan(&messages); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE project_id=?`, projectID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if messages != 8 || sessions != 1 {
		t.Fatalf("weekly archiving changed original conversation: messages=%d sessions=%d", messages, sessions)
	}
}
