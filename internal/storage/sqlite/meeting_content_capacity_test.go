package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/meetings"
	"github.com/oklog/ulid/v2"
)

func seedCapacitySegments(t *testing.T, store *Store, id string, total int) string {
	t.Helper()
	var texts []string
	remaining := total
	for i := 0; remaining > 0; i++ {
		if i > 0 {
			remaining--
			if remaining <= 0 {
				t.Fatal("fixture newline remainder")
			}
		}
		n := min(16384, remaining)
		prefix := fmt.Sprintf("%04d:", i)
		body := prefix + strings.Repeat("中", n-len(prefix))
		if err := store.InsertSegment(context.Background(), meetings.Segment{SegmentID: ulid.Make().String(), MeetingID: id, Seq: i + 1, Text: body, StartedMS: int64(i), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
			t.Fatal(err)
		}
		texts = append(texts, body)
		remaining -= n
	}
	return strings.Join(texts, "\n")
}

func TestMeetingAggregationExactLimitAndConcurrentAppendRefusesOverflow(t *testing.T) {
	meetings.SilenceLoopbackForTest(t)
	ctx := context.Background()
	store := openRuntimeStore(t)
	svc := meetings.New(store)
	m, err := svc.Start(ctx, "exact limit", meetings.AudioMicrophone)
	if err != nil {
		t.Fatal(err)
	}
	want := seedCapacitySegments(t, store, m.MeetingID, meetings.MaxTranscriptRunes)
	stopped, err := svc.Stop(ctx, m.MeetingID)
	if err != nil || stopped.Transcript != want || utf8.RuneCountInString(stopped.Transcript) != meetings.MaxTranscriptRunes {
		t.Fatalf("exact limit changed: runes=%d %v", utf8.RuneCountInString(stopped.Transcript), err)
	}
	m, err = svc.Start(ctx, "atomic budget", meetings.AudioMicrophone)
	if err != nil {
		t.Fatal(err)
	}
	seedCapacitySegments(t, store, m.MeetingID, meetings.MaxTranscriptRunes-2)
	// JSON permits NUL. SQLite length(text) alone would count this legal
	// segment as one rune and admit both concurrent appends.
	if _, err := store.db.ExecContext(ctx, `UPDATE meeting_segments SET body=substr(body,1,1)||char(0)||substr(body,3) WHERE meeting_id=? AND seq=1`, m.MeetingID); err != nil {
		t.Fatal(err)
	}
	before, err := store.CountSegments(ctx, m.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	second := meetings.New(store)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, s := range []*meetings.Service{svc, second} {
		wg.Add(1)
		go func(i int, s *meetings.Service) {
			defer wg.Done()
			_, err := s.Append(ctx, m.MeetingID, []string{"甲", "乙"}[i], 100)
			results <- err
		}(i, s)
	}
	wg.Wait()
	close(results)
	accepted, refused := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, meetings.ErrCapacity) {
			refused++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || refused != 1 {
		t.Fatalf("accepted=%d refused=%d", accepted, refused)
	}
	count, err := store.CountSegments(ctx, m.MeetingID)
	if err != nil || count != before+1 {
		t.Fatalf("rows=%d %v", count, err)
	}
	if _, err = svc.Summarize(ctx, m.MeetingID); !errors.Is(err, meetings.ErrNotRecording) {
		t.Fatalf("summarize must not end recording on capacity: %v", err)
	}
	if current, err := store.GetMeeting(ctx, m.MeetingID); err != nil || current.Status != meetings.StatusRecording {
		t.Fatalf("summarize changed recording: %s %v", current.Status, err)
	}
	stopped, err = svc.Stop(ctx, m.MeetingID)
	if !errors.Is(err, meetings.ErrCapacity) || stopped.Status != meetings.StatusNeedsSummary || stopped.Transcript != "" {
		t.Fatalf("Stop claimed partial capture complete: %+v %v", stopped, err)
	}
	if _, err = svc.Start(ctx, "next recording", meetings.AudioMicrophone); err != nil {
		t.Fatalf("capacity stop left recording locked: %v", err)
	}
}

func TestMeetingLegacyOverflowPreservesRawDataAndCanRecoverFromAudio(t *testing.T) {
	meetings.SilenceLoopbackForTest(t)
	ctx := context.Background()
	store := openRuntimeStore(t)
	svc := meetings.New(store)
	root := t.TempDir()
	svc.SetAudioRoot(root)
	m, err := svc.Start(ctx, "legacy overflow", meetings.AudioMicrophone)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.AppendAudio(ctx, m.MeetingID, make([]byte, 16000*2*5)); err != nil {
		t.Fatal(err)
	}
	seedCapacitySegments(t, store, m.MeetingID, meetings.MaxTranscriptRunes+10)
	count, _ := store.CountSegments(ctx, m.MeetingID)
	stopped, err := svc.Stop(ctx, m.MeetingID)
	if !errors.Is(err, meetings.ErrCapacity) || stopped.Status != meetings.StatusNeedsSummary || stopped.Transcript != "" {
		t.Fatalf("legacy Stop: status=%s transcript bytes=%d err=%v", stopped.Status, len(stopped.Transcript), err)
	}
	if after, _ := store.CountSegments(ctx, m.MeetingID); after != count {
		t.Fatal("raw segments were changed")
	}
	files, err := filepath.Glob(filepath.Join(root, m.MeetingID, "*.wav"))
	if err != nil || len(files) == 0 {
		t.Fatalf("captured audio lost: %v %v", files, err)
	}
	modelCalls := 0
	svc.SetCompleter(func(context.Context, string, string) (meetings.Notes, error) {
		modelCalls++
		return meetings.Notes{Summary: "full"}, nil
	})
	if _, err = svc.Summarize(ctx, m.MeetingID); !errors.Is(err, meetings.ErrCapacity) || modelCalls != 0 {
		t.Fatalf("incomplete source summarized: calls=%d err=%v", modelCalls, err)
	}
	if _, err = svc.Get(ctx, m.MeetingID); !errors.Is(err, meetings.ErrCapacity) {
		t.Fatalf("legacy full read: %v", err)
	}
	if page, err := svc.Segments(ctx, m.MeetingID, stopped.Revision, 0, 0); err != nil || len(page.Items) != 10 {
		t.Fatalf("raw pagination unavailable: %d %v", len(page.Items), err)
	}
	// A fresh service reads the durable notice; recovery starts at the audio
	// beginning instead of skipping the raw rows that exceeded the text budget.
	svc = meetings.New(store)
	svc.SetAudioRoot(root)
	svc.SetAudioTranscriber(func(context.Context, []byte) (string, error) { return "完整音频恢复后的原稿", nil })
	recovered, err := svc.CatchUp(ctx, m.MeetingID)
	if err != nil || recovered.Transcript != "完整音频恢复后的原稿" || recovered.SummaryError != "" {
		t.Fatalf("audio recovery: %+v %v", recovered, err)
	}
	if after, _ := store.CountSegments(ctx, m.MeetingID); after != count {
		t.Fatal("recovery deleted original segments")
	}
}

func TestMeetingCatchupDoesNotClipLongRecognizerResult(t *testing.T) {
	meetings.SilenceLoopbackForTest(t)
	ctx := context.Background()
	store := openRuntimeStore(t)
	svc := meetings.New(store)
	root := t.TempDir()
	svc.SetAudioRoot(root)
	m, err := svc.Start(ctx, "recognizer budget", meetings.AudioMicrophone)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.AppendAudio(ctx, m.MeetingID, make([]byte, 16000*2*25)); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Stop(ctx, m.MeetingID); err != nil {
		t.Fatal(err)
	}
	firstCalls := 0
	svc.SetAudioTranscriber(func(context.Context, []byte) (string, error) {
		firstCalls++
		if firstCalls == 1 {
			return "已成功的第一段", nil
		}
		return strings.Repeat("中", meetings.MaxTranscriptRunes+1), nil
	})
	if _, err = svc.CatchUp(ctx, m.MeetingID); !errors.Is(err, meetings.ErrCapacity) {
		t.Fatalf("oversized ASR accepted: %v", err)
	}
	stored, err := store.GetMeeting(ctx, m.MeetingID)
	if err != nil || stored.Transcript != "" {
		t.Fatalf("partial ASR prefix committed: %d %v", len(stored.Transcript), err)
	}
	oversize := strings.Repeat("中", meetings.MaxTranscriptRunes+1)
	if _, err = svc.Update(ctx, m.MeetingID, meetings.MeetingPatch{ExpectedRevision: stored.Revision, Transcript: &oversize}); !errors.Is(err, meetings.ErrCapacity) {
		t.Fatalf("oversized manual source silently clipped: %v", err)
	}
	svc = meetings.New(store)
	svc.SetAudioRoot(root)
	tail := strings.Repeat("中", 20000) + "完整末尾"
	want := "已成功的第一段\n" + tail
	retries := 0
	svc.SetAudioTranscriber(func(context.Context, []byte) (string, error) { retries++; return tail, nil })
	recovered, err := svc.CatchUp(ctx, m.MeetingID)
	if err != nil || recovered.Transcript != want || retries != 1 || firstCalls != 2 {
		t.Fatalf("retry lost cached success or clipped ASR at old 16k cap: runes=%d retries=%d first=%d err=%v", utf8.RuneCountInString(recovered.Transcript), retries, firstCalls, err)
	}
}
