package meetings_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/meetings"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestCatchupReconcilesInterruptedFinalJournalCommit(t *testing.T) {
	meetings.SilenceLoopbackForTest(t)
	ctx := context.Background()
	root := t.TempDir()
	store, err := sqlitestore.OpenTemplated(ctx, filepath.Join(root, "meeting.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := meetings.New(store)
	svc.SetAudioRoot(root)
	m, err := svc.Start(ctx, "恢复补转写", "microphone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.AppendAudio(ctx, m.MeetingID, silencePCM(5000)); err != nil {
		t.Fatal(err)
	}
	before, err := svc.Stop(ctx, m.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAudioTranscriber(func(context.Context, []byte) (string, error) { return "补转写已提交数据库", nil })
	completed, err := svc.CatchUp(ctx, m.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, m.MeetingID, "catchup.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var journal map[string]any
	if err = json.Unmarshal(raw, &journal); err != nil {
		t.Fatal(err)
	}
	// Recreate the durable precommit file left when SQLite succeeded but the
	// final journal replacement did not happen.
	journal["lastTranscript"] = before.Transcript
	journal["preparedTranscript"] = completed.Transcript
	journal["preparedUpdatedAt"] = completed.UpdatedAt
	raw, err = json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	svc = meetings.New(store)
	svc.SetAudioRoot(root)
	svc.SetAudioTranscriber(func(context.Context, []byte) (string, error) { t.Fatal("committed span decoded twice"); return "", nil })
	recovered, err := svc.CatchUp(ctx, m.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Transcript != completed.Transcript || recovered.UpdatedAt != completed.UpdatedAt {
		t.Fatalf("committed output changed: %#v", recovered)
	}
}

func TestCatchupFailurePreservesOriginalTimedSegments(t *testing.T) {
	svc := testMeetings(t)
	svc.SetAudioRoot(t.TempDir())
	ctx := context.Background()
	m, err := svc.Start(ctx, "短会", "microphone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Append(ctx, m.MeetingID, "客户确认周五交付", 0); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.AppendAudio(ctx, m.MeetingID, silencePCM(5000)); err != nil {
		t.Fatal(err)
	}
	before, err := svc.Stop(ctx, m.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAudioTranscriber(func(context.Context, []byte) (string, error) { return "", errors.New("decoder unavailable") })
	got, err := svc.CatchUp(ctx, m.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Segments, before.Segments) || got.Transcript != before.Transcript {
		t.Fatalf("original evidence changed: %#v", got)
	}
	if got.Status != meetings.StatusNeedsSummary || !strings.Contains(got.SummaryError, "缺口") {
		t.Fatalf("missing gap status: %#v", got)
	}
}

func TestCatchupRestartRetriesOnlyFailedSpanAndKeepsSource(t *testing.T) {
	meetings.SilenceLoopbackForTest(t)
	ctx := context.Background()
	dir := t.TempDir()
	store, err := sqlitestore.OpenTemplated(ctx, filepath.Join(dir, "meeting.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := meetings.New(store)
	svc.SetAudioRoot(filepath.Join(dir, "audio"))
	m, err := svc.Start(ctx, "长会", "microphone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Append(ctx, m.MeetingID, "不可修改的实时记录", 0); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.AppendAudio(ctx, m.MeetingID, silencePCM(45000)); err != nil {
		t.Fatal(err)
	}
	before, err := svc.Stop(ctx, m.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	svc.SetAudioTranscriber(func(context.Context, []byte) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("first span failed")
		}
		return strings.Repeat("这一段转写成功。", 12), nil
	})
	first, err := svc.CatchUp(ctx, m.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || !strings.Contains(first.SummaryError, "0–20秒") {
		t.Fatalf("unreported gap: calls=%d %#v", calls, first)
	}
	svc.SetCompleter(func(context.Context, string, string) (meetings.Notes, error) {
		t.Error("must not summarize incomplete recording")
		return meetings.Notes{}, nil
	})
	if _, err := svc.Summarize(ctx, m.MeetingID); err != nil {
		t.Fatal(err)
	}
	// A new service has no in-memory transcription cache.
	restarted := meetings.New(store)
	restarted.SetAudioRoot(filepath.Join(dir, "audio"))
	retries := 0
	restarted.SetAudioTranscriber(func(context.Context, []byte) (string, error) { retries++; return "补齐最早二十秒。", nil })
	complete, err := restarted.CatchUp(ctx, m.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if retries != 1 || complete.SummaryError != "" || !strings.Contains(complete.Transcript, "补齐最早二十秒") {
		t.Fatalf("retry=%d %#v", retries, complete)
	}
	if !reflect.DeepEqual(complete.Segments, before.Segments) {
		t.Fatalf("source segments changed: %#v", complete.Segments)
	}
	if _, err := restarted.CatchUp(ctx, m.MeetingID); err != nil {
		t.Fatal(err)
	}
	if retries != 1 {
		t.Fatalf("successful spans were decoded again: %d", retries)
	}
}
