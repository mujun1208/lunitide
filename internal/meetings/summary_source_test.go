package meetings_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/lunitide/lunitide/internal/meetings"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

func summarySourceDigest(t *testing.T, title, transcript string) string {
	t.Helper()
	raw, err := json.Marshal(struct {
		Title      string `json:"title"`
		Transcript string `json:"transcript"`
	}{title, transcript})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func TestSummarySourceSurvivesEditFailureAndDatabaseReopen(t *testing.T) {
	meetings.SilenceLoopbackForTest(t)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "meeting.db")
	store, err := sqlitestore.OpenTemplated(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := meetings.New(store)
	m, err := svc.Start(ctx, "输入标题", meetings.AudioMicrophone)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Append(ctx, m.MeetingID, "先确认交付范围，再确认截止时间。", 0); err != nil {
		t.Fatal(err)
	}
	m, err = svc.Stop(ctx, m.MeetingID, m.Revision)
	if err != nil {
		t.Fatal(err)
	}
	inputRevision := m.TranscriptRevision
	source := meetings.CleanTranscript(m.Transcript)
	svc.SetCompleter(func(_ context.Context, title, text string) (meetings.Notes, error) {
		if title != "输入标题" || text != source {
			t.Errorf("different provider input: %q %q", title, text)
		}
		return meetings.Notes{Title: "模型输出标题", Summary: "原摘要", Actions: "原待办"}, nil
	})
	ready, err := svc.Summarize(ctx, m.MeetingID, m.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if ready.TranscriptRevision != inputRevision || ready.SummarySourceRevision != inputRevision || ready.SummarySourceTitle != "输入标题" || ready.SummarySourceTranscript != source || ready.SummarySourceDigest != summarySourceDigest(t, "输入标题", source) || ready.SummaryEdited {
		t.Fatalf("incorrect committed source: %#v", ready)
	}
	// Normalized no-op editor values must not invalidate the same source.
	same := "  " + ready.Transcript + "  "
	unchanged, err := svc.Update(ctx, m.MeetingID, meetings.MeetingPatch{ExpectedRevision: ready.Revision, Transcript: &same})
	if err != nil || unchanged.TranscriptRevision != inputRevision || unchanged.Status != meetings.StatusReady {
		t.Fatalf("no-op source changed: %#v %v", unchanged, err)
	}
	editedText, editedSummary := "修订后的交付范围，不使用旧日期。", "人工补充后的摘要"
	edited, err := svc.Update(ctx, m.MeetingID, meetings.MeetingPatch{ExpectedRevision: unchanged.Revision, Transcript: &editedText, Summary: &editedSummary})
	if err != nil {
		t.Fatal(err)
	}
	if edited.TranscriptRevision != inputRevision+1 || edited.SummarySourceRevision != inputRevision || edited.Status != meetings.StatusNeedsSummary || edited.Summary != editedSummary || edited.Actions != ready.Actions || !edited.SummaryEdited || edited.SummarySourceTranscript != source || edited.SummarySourceDigest != ready.SummarySourceDigest {
		t.Fatalf("edit lost old source: %#v", edited)
	}
	if _, err = svc.Update(ctx, m.MeetingID, meetings.MeetingPatch{ExpectedRevision: unchanged.Revision, Transcript: &same}); !errors.Is(err, meetings.ErrConflict) {
		t.Fatalf("stale source edit: %v", err)
	}
	svc.SetCompleter(func(context.Context, string, string) (meetings.Notes, error) {
		return meetings.Notes{}, errors.New("provider unavailable")
	})
	failed, err := svc.Summarize(ctx, m.MeetingID, edited.Revision)
	if err != nil || failed.SummarySourceDigest != ready.SummarySourceDigest || failed.SummarySourceTranscript != source || failed.Summary != editedSummary {
		t.Fatalf("failed generation replaced source: %#v %v", failed, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlitestore.OpenTemplated(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	svc = meetings.New(store)
	reopened, err := svc.Get(ctx, m.MeetingID)
	if err != nil || reopened.TranscriptRevision != inputRevision+1 || reopened.SummarySourceDigest != ready.SummarySourceDigest {
		t.Fatalf("reopened source: %#v %v", reopened, err)
	}
	page, err := svc.SummarySource(ctx, m.MeetingID, reopened.SummarySourceDigest, 0)
	if err != nil || page.Transcript != source || page.SourceRevision != inputRevision {
		t.Fatalf("retained snapshot: %#v %v", page, err)
	}
	if !strings.Contains(meetings.RenderMarkdown(reopened), "原稿已变更") {
		t.Fatal("export falsely presents stale summary as current")
	}
	svc.SetCompleter(func(context.Context, string, string) (meetings.Notes, error) {
		return meetings.Notes{Summary: "基于修订原稿的新摘要"}, nil
	})
	regenerated, err := svc.Summarize(ctx, m.MeetingID, reopened.Revision)
	if err != nil || regenerated.Status != meetings.StatusReady || regenerated.SummarySourceRevision != regenerated.TranscriptRevision || regenerated.SummarySourceTranscript != meetings.CleanTranscript(editedText) || regenerated.SummarySourceDigest == ready.SummarySourceDigest || regenerated.SummaryEdited {
		t.Fatalf("regenerated source: %#v %v", regenerated, err)
	}
	if _, err = svc.SummarySource(ctx, m.MeetingID, ready.SummarySourceDigest, 0); !errors.Is(err, meetings.ErrConflict) {
		t.Fatalf("old snapshot cursor accepted after regeneration: %v", err)
	}
}

func TestSummarySourceConcurrentEditRejectsObsoleteGeneration(t *testing.T) {
	svc := testMeetings(t)
	ctx := context.Background()
	m, err := svc.Start(ctx, "来源一致性", meetings.AudioMicrophone)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Append(ctx, m.MeetingID, "原始输入。", 0); err != nil {
		t.Fatal(err)
	}
	m, err = svc.Stop(ctx, m.MeetingID, m.Revision)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetCompleter(func(context.Context, string, string) (meetings.Notes, error) {
		return meetings.Notes{Summary: "保留的已完成摘要"}, nil
	})
	before, err := svc.Summarize(ctx, m.MeetingID, m.Revision)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	svc.SetCompleter(func(context.Context, string, string) (meetings.Notes, error) {
		close(entered)
		<-release
		return meetings.Notes{Summary: "晚到的过时摘要"}, nil
	})
	done := make(chan error, 1)
	go func() { _, err := svc.Summarize(ctx, m.MeetingID, before.Revision); done <- err }()
	acceptanceWait(t, entered, "source provider")
	changed := "模型工作期间已修订的输入。"
	if _, err = svc.Update(ctx, m.MeetingID, meetings.MeetingPatch{ExpectedRevision: meetingRevision(t, svc, m.MeetingID), Transcript: &changed}); err != nil {
		close(release)
		t.Fatal(err)
	}
	close(release)
	if err = acceptanceResult(t, done, "source completion"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, m.MeetingID)
	if err != nil || got.Status != meetings.StatusNeedsSummary || got.Summary != before.Summary || got.SummarySourceDigest != before.SummarySourceDigest || got.SummarySourceTranscript != before.SummarySourceTranscript || got.Transcript != changed || got.TranscriptRevision != before.TranscriptRevision+1 {
		t.Fatalf("late result/source replaced current data: %#v %v", got, err)
	}
}

type summarySourceCommitFailure struct {
	*sqlitestore.Store
	fail bool
}

func (s *summarySourceCommitFailure) CompareAndSwapMeeting(ctx context.Context, previous string, m meetings.Meeting) (bool, error) {
	if s.fail && m.Status == meetings.StatusReady {
		return false, errors.New("injected SQLite commit failure")
	}
	return s.Store.CompareAndSwapMeeting(ctx, previous, m)
}

func TestSummarySourceListRecoveryPreservesSnapshotAfterFinalCommitFailure(t *testing.T) {
	meetings.SilenceLoopbackForTest(t)
	ctx := context.Background()
	store, err := sqlitestore.OpenTemplated(ctx, filepath.Join(t.TempDir(), "meeting.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	failing := &summarySourceCommitFailure{Store: store}
	svc := meetings.New(failing)
	m, err := svc.Start(ctx, "来源保留", meetings.AudioMicrophone)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Append(ctx, m.MeetingID, "不会丢失的原稿。", 0); err != nil {
		t.Fatal(err)
	}
	m, err = svc.Stop(ctx, m.MeetingID, m.Revision)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetCompleter(func(context.Context, string, string) (meetings.Notes, error) {
		return meetings.Notes{Summary: "已经提交的摘要"}, nil
	})
	before, err := svc.Summarize(ctx, m.MeetingID, m.Revision)
	if err != nil {
		t.Fatal(err)
	}
	failing.fail = true
	if _, err = svc.Summarize(ctx, m.MeetingID, before.Revision); err == nil {
		t.Fatal("failed final commit acknowledged")
	}
	// Simulate reopening: List's small projection does not contain the source
	// body, yet its interruption recovery must preserve the full stored row.
	svc = meetings.New(store)
	items, err := svc.List(ctx)
	if err != nil || len(items) != 1 || items[0].Status != meetings.StatusNeedsSummary {
		t.Fatalf("recovery list: %#v %v", items, err)
	}
	got, err := svc.Get(ctx, m.MeetingID)
	if err != nil || got.Summary != before.Summary || got.SummarySourceTranscript != before.SummarySourceTranscript || got.SummarySourceDigest != before.SummarySourceDigest {
		t.Fatalf("recovery destroyed source: %#v %v", got, err)
	}
}

func TestSummarySourceMaximumInputPagesStayWithinIPCFrame(t *testing.T) {
	ctx := context.Background()
	store, err := sqlitestore.OpenTemplated(ctx, filepath.Join(t.TempDir(), "meeting.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// Non-BMP UTF-8 and HTML escaping exercise the real JSON byte budget.
	text := strings.Repeat("😀<&", (1<<20)/3)
	m := meetings.Meeting{MeetingID: ulid.Make().String(), Revision: 1, TranscriptRevision: 1, SummarySourceRevision: 1, SummarySourceTitle: "最大输入", SummarySourceTranscript: text, Transcript: text,
		Title: "最大输入", Summary: "已生成", Status: meetings.StatusReady, AudioSource: meetings.AudioMicrophone, StartedAt: "2026-09-06T00:00:00Z", CreatedAt: "2026-09-06T00:00:00Z", UpdatedAt: "2026-09-06T00:00:00Z"}
	m.SummarySourceDigest = summarySourceDigest(t, m.SummarySourceTitle, text)
	if err = store.InsertMeeting(ctx, m); err != nil {
		t.Fatal(err)
	}
	svc := meetings.New(store)
	var joined strings.Builder
	offset, pages := 0, 0
	for {
		page, err := svc.SummarySource(ctx, m.MeetingID, m.SummarySourceDigest, offset)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(page)
		if err != nil || len(raw) >= 128<<10 || len(raw) >= ipc.MaxFrameSize {
			t.Fatalf("oversized source frame: bytes=%d err=%v", len(raw), err)
		}
		joined.WriteString(page.Transcript)
		pages++
		if page.NextOffset == 0 {
			break
		}
		if page.NextOffset <= offset {
			t.Fatal("nonadvancing source cursor")
		}
		offset = page.NextOffset
	}
	if joined.String() != text || pages != 64 {
		t.Fatalf("source lost at page boundary: pages=%d bytes=%d want=%d", pages, joined.Len(), len(text))
	}
	if _, err = svc.SummarySource(ctx, m.MeetingID, m.SummarySourceDigest, 1<<20); !errors.Is(err, meetings.ErrInvalid) {
		t.Fatalf("invalid cursor accepted: %v", err)
	}
	t.Logf("source_runes=%d pages=%d maximum_json_frame<128KiB IPC_limit=%d full_input_hash_preserved=true", len([]rune(text)), pages, ipc.MaxFrameSize)
}
