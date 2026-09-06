package meetings_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/meetings"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
)

type meetingAcceptanceFence struct {
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newMeetingAcceptanceFence() *meetingAcceptanceFence {
	return &meetingAcceptanceFence{entered: make(chan struct{}), release: make(chan struct{})}
}
func (f *meetingAcceptanceFence) open() { f.once.Do(func() { close(f.release) }) }
func (f *meetingAcceptanceFence) wait(ctx context.Context) error {
	if !f.armed.CompareAndSwap(true, false) {
		return nil
	}
	close(f.entered)
	select {
	case <-f.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type meetingAcceptanceStore struct {
	*sqlitestore.Store
	heartbeat, stop *meetingAcceptanceFence
}

func (s *meetingAcceptanceStore) TouchRecording(ctx context.Context, id string, duration int64, at string) error {
	if err := s.heartbeat.wait(ctx); err != nil {
		return err
	}
	return s.Store.TouchRecording(ctx, id, duration, at)
}
func (s *meetingAcceptanceStore) CompareAndSwapMeeting(ctx context.Context, previous string, m meetings.Meeting) (bool, error) {
	if m.Status == meetings.StatusTranscribed {
		if err := s.stop.wait(ctx); err != nil {
			return false, err
		}
	}
	return s.Store.CompareAndSwapMeeting(ctx, previous, m)
}

func acceptanceWait(t *testing.T, ready <-chan struct{}, stage string) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not reach its controlled barrier", stage)
	}
}
func acceptanceResult(t *testing.T, result <-chan error, stage string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not finish", stage)
		return nil
	}
}

// Ten mutation/provider schedules times 100 different transcript fixtures.
// Each group also rotates three actual heartbeat/stop commit orderings. These
// are controlled concurrent calls through the real Service and SQLite store,
// not a thousand repetitions of a mock returning the expected result.
func TestMeetingAcceptance1000Interleavings(t *testing.T) {
	meetings.SilenceLoopbackForTest(t)
	ctx := context.Background()
	store, err := sqlitestore.OpenTemplated(ctx, filepath.Join(t.TempDir(), "matrix.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	schedules := []string{"title-during-success", "transcript-during-failure", "summary-during-success", "actions-during-success", "all-during-success", "all-during-failure", "stale-edit-during-success", "provider-failure", "transcript-after-success", "scope-cancel-during-generation"}
	executed, passed := 0, 0
	for scenario, name := range schedules {
		for sample := range 100 {
			t.Run(fmt.Sprintf("%s/%03d", name, sample), func(t *testing.T) {
				executed++
				checkMeetingInterleaving(t, store, scenario, sample)
				passed++
			})
		}
	}
	t.Logf("%d groups executed, %d passed: 10 summary/edit schedules x 100 distinct fixtures; three controlled heartbeat/stop orderings, real SQLite, source hash and revision checks", executed, passed)
}

func checkMeetingInterleaving(t *testing.T, store *sqlitestore.Store, scenario, sample int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	heartbeat, stop := newMeetingAcceptanceFence(), newMeetingAcceptanceFence()
	defer heartbeat.open()
	defer stop.open()
	svc := meetings.New(&meetingAcceptanceStore{Store: store, heartbeat: heartbeat, stop: stop})
	original := fmt.Sprintf("会议%d-%d：保留原始决定。%s", scenario, sample, strings.Repeat("交付范围🙂；", sample%19+1))
	m, err := svc.Start(ctx, fmt.Sprintf("回归会议%d-%d", scenario, sample), "microphone")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = svc.Stop(cleanup, m.MeetingID)
	})
	if _, err := svc.Append(ctx, m.MeetingID, original, 0); err != nil {
		t.Fatal(err)
	}
	heartbeat.armed.Store(true)
	hbDone := make(chan error, 1)
	go func() { _, err := svc.Heartbeat(ctx, m.MeetingID); hbDone <- err }()
	acceptanceWait(t, heartbeat.entered, "heartbeat")
	caption := fmt.Sprintf("最后补充：批次%d，责任人已明确。", sample)
	if _, err := svc.Append(ctx, m.MeetingID, caption, int64(sample+1)); err != nil {
		t.Fatal(err)
	}
	if current := meetingRevision(t, svc, m.MeetingID); current != m.Revision {
		t.Fatalf("heartbeat/caption changed edit revision: %d -> %d", m.Revision, current)
	}
	var stopped meetings.Meeting
	if sample%3 == 2 {
		stop.armed.Store(true)
		stopDone := make(chan error, 1)
		go func() { var err error; stopped, err = svc.Stop(ctx, m.MeetingID, m.Revision); stopDone <- err }()
		acceptanceWait(t, stop.entered, "stop CAS")
		heartbeat.open()
		if err := acceptanceResult(t, hbDone, "heartbeat before stop commit"); err != nil {
			t.Fatal(err)
		}
		stop.open()
		if err := acceptanceResult(t, stopDone, "stop after heartbeat commit"); err != nil {
			t.Fatalf("heartbeat must not create a false content conflict: %v", err)
		}
	} else {
		if sample%3 == 1 {
			heartbeat.open()
			if err := acceptanceResult(t, hbDone, "heartbeat before stop"); err != nil {
				t.Fatal(err)
			}
		}
		stopped, err = svc.Stop(ctx, m.MeetingID, m.Revision)
		if err != nil {
			t.Fatal(err)
		}
		if sample%3 == 0 {
			heartbeat.open()
			if err := acceptanceResult(t, hbDone, "late heartbeat"); !errors.Is(err, meetings.ErrNotRecording) {
				t.Fatalf("late heartbeat = %v", err)
			}
		}
	}
	if stopped.Status != meetings.StatusTranscribed || stopped.Revision != m.Revision+1 || !strings.Contains(stopped.Transcript, original) || !strings.Contains(stopped.Transcript, caption) {
		t.Fatalf("stop lost committed source/status/revision: %+v", stopped)
	}
	sourceHash := sha256.Sum256([]byte(stopped.Transcript))
	if _, err := svc.Append(ctx, m.MeetingID, "停止后不应追加的新片段", 9999); !errors.Is(err, meetings.ErrNotRecording) {
		t.Fatalf("late caption accepted: %v", err)
	}
	provider := newMeetingAcceptanceFence()
	provider.armed.Store(true)
	defer provider.open()
	lifetime, revoke := context.WithCancel(ctx)
	defer revoke()
	svc.SetExecutionScope(func(context.Context, string) (context.Context, func(), error) { return lifetime, func() {}, nil })
	svc.SetCompleter(func(work context.Context, title, transcript string) (meetings.Notes, error) {
		if !strings.Contains(transcript, original) || !strings.Contains(transcript, caption) {
			return meetings.Notes{}, errors.New("provider source did not match committed transcript")
		}
		if err := provider.wait(work); err != nil {
			return meetings.Notes{}, err
		}
		if scenario == 1 || scenario == 5 || scenario == 7 {
			return meetings.Notes{}, errors.New("isolated provider failure")
		}
		return meetings.Notes{Title: "模型标题", Summary: "模型摘要", Actions: "模型待办"}, nil
	})
	summaryDone := make(chan error, 1)
	go func() { _, err := svc.Summarize(ctx, m.MeetingID, stopped.Revision); summaryDone <- err }()
	acceptanceWait(t, provider.entered, "summary provider")
	generating, err := svc.Get(ctx, m.MeetingID)
	if err != nil || generating.Revision != stopped.Revision+1 || generating.Status != meetings.StatusSummarizing {
		t.Fatalf("generation did not persist its revision: %+v %v", generating, err)
	}
	title, transcript, summary, actions := "人工标题", original+"\n人工校正后的新事实", "人工摘要", "人工待办"
	patch := meetings.MeetingPatch{ExpectedRevision: generating.Revision}
	switch scenario {
	case 0:
		patch.Title = &title
	case 1:
		patch.Transcript = &transcript
	case 2:
		patch.Summary = &summary
	case 3:
		patch.Actions = &actions
	case 4, 5:
		patch.Title, patch.Transcript, patch.Summary, patch.Actions = &title, &transcript, &summary, &actions
	case 6:
		patch.Title, patch.ExpectedRevision = &title, stopped.Revision
	case 9:
		revoke()
	}
	if scenario <= 6 {
		_, err := svc.Update(ctx, m.MeetingID, patch)
		if scenario == 6 {
			if !errors.Is(err, meetings.ErrConflict) {
				t.Fatalf("stale editor = %v", err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
	}
	provider.open()
	if err := acceptanceResult(t, summaryDone, "summary result"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, m.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if scenario == 8 {
		if got.Status != meetings.StatusReady {
			t.Fatalf("uncontended model result not ready: %+v", got)
		}
		got, err = svc.Update(ctx, m.MeetingID, meetings.MeetingPatch{ExpectedRevision: got.Revision, Transcript: &transcript})
		if err != nil {
			t.Fatal(err)
		}
		if got.Summary != "模型摘要" || got.Status != meetings.StatusNeedsSummary || got.SummaryError == "" {
			t.Fatalf("changed source still presented as current summary: %+v", got)
		}
	} else if scenario == 6 {
		if got.Status != meetings.StatusReady || got.Summary != "模型摘要" || got.Title != "模型标题" {
			t.Fatalf("rejected stale edit displaced valid generation: %+v", got)
		}
	} else if got.Status != meetings.StatusNeedsSummary {
		t.Fatalf("conflicted/failed/revoked generation reported ready: %+v", got)
	}
	if scenario <= 5 {
		if patch.Title != nil && got.Title != title || patch.Transcript != nil && got.Transcript != transcript || patch.Summary != nil && got.Summary != summary || patch.Actions != nil && got.Actions != actions {
			t.Fatalf("late generation overwrote manual edits: %+v", got)
		}
	}
	if scenario != 1 && scenario != 4 && scenario != 5 && scenario != 8 && sha256.Sum256([]byte(got.Transcript)) != sourceHash {
		t.Fatal("unmodified source hash changed")
	}
	beforeReplay := got
	replayed, err := svc.Stop(ctx, m.MeetingID, got.Revision)
	if err != nil || replayed.Revision != beforeReplay.Revision || replayed.Transcript != beforeReplay.Transcript || replayed.Summary != beforeReplay.Summary {
		t.Fatalf("repeated stop changed settled content: %+v %v", replayed, err)
	}
}
