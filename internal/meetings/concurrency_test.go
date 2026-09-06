package meetings_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/meetings"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
)

type delayedHeartbeatStore struct {
	*sqlitestore.Store
	entered chan struct{}
	release chan struct{}
}

func (s *delayedHeartbeatStore) TouchRecording(ctx context.Context, id string, duration int64, updated string) error {
	close(s.entered)
	<-s.release
	return s.Store.TouchRecording(ctx, id, duration, updated)
}

func TestLateHeartbeatDoesNotRestoreStoppedMeeting(t *testing.T) {
	meetings.SilenceLoopbackForTest(t)
	ctx := context.Background()
	store, err := sqlitestore.OpenTemplated(ctx, filepath.Join(t.TempDir(), "meeting.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	delayed := &delayedHeartbeatStore{store, make(chan struct{}), make(chan struct{})}
	svc := meetings.New(delayed)
	m, err := svc.Start(ctx, "保留停止状态", "microphone")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := svc.Heartbeat(ctx, m.MeetingID); done <- err }()
	select {
	case <-delayed.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("heartbeat did not reach store")
	}
	stopped, err := svc.Stop(ctx, m.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	close(delayed.release)
	if err := <-done; !errors.Is(err, meetings.ErrNotRecording) {
		t.Fatalf("late heartbeat error=%v", err)
	}
	got, err := svc.Get(ctx, m.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != meetings.StatusTranscribed || got.EndedAt != stopped.EndedAt || got.Title != stopped.Title {
		t.Fatalf("late heartbeat changed stopped data: %#v", got)
	}
}

func TestSummaryPreservesEditsMadeDuringGeneration(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "provider failure"}[fail], func(t *testing.T) {
			svc := testMeetings(t)
			ctx := context.Background()
			m, err := svc.Start(ctx, "original", "microphone")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Append(ctx, m.MeetingID, "原始逐字稿", 0); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Stop(ctx, m.MeetingID); err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			svc.SetCompleter(func(context.Context, string, string) (meetings.Notes, error) {
				close(entered)
				<-release
				if fail {
					return meetings.Notes{}, errors.New("provider failed")
				}
				return meetings.Notes{Title: "model title", Summary: "model summary"}, nil
			})
			done := make(chan error, 1)
			go func() { _, err := svc.Summarize(ctx, m.MeetingID); done <- err }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("completer not called")
			}
			transcript, title, summary := "人工校正后的逐字稿", "人工标题", "人工摘要"
			if _, err := svc.Update(ctx, m.MeetingID, meetings.MeetingPatch{ExpectedRevision: meetingRevision(t, svc, m.MeetingID), Transcript: &transcript, Title: &title, Summary: &summary}); err != nil {
				t.Fatal(err)
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			got, err := svc.Get(ctx, m.MeetingID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Transcript != transcript || got.Title != title || got.Summary != summary || got.Status != meetings.StatusNeedsSummary {
				t.Fatalf("concurrent edits overwritten: %#v", got)
			}
		})
	}
}
