package meetings_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/lunitide/lunitide/internal/meetings"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func batchIdentity(pcm []byte, seq, start int64) meetings.AudioBatchIdentity {
	sum := sha256.Sum256(pcm)
	return meetings.AudioBatchIdentity{CaptureSessionID: "capture_session_12345", ChunkSeq: seq, SampleStart: start, SampleCount: int64(len(pcm) / 2), Digest: hex.EncodeToString(sum[:])}
}

func TestAudioBatchReopenReplayConflictAndLateAck(t *testing.T) {
	meetings.SilenceLoopbackForTest(t)
	ctx := context.Background()
	root := t.TempDir()
	db := filepath.Join(root, "meetings.db")
	store, err := sqlitestore.OpenTemplated(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	svc := meetings.New(store)
	svc.SetAudioRoot(filepath.Join(root, "audio"))
	m, err := svc.Start(ctx, "可靠音频", "microphone")
	if err != nil {
		t.Fatal(err)
	}
	pcm := silencePCM(1200)
	id := batchIdentity(pcm, 0, 0)
	ack, err := svc.AppendAudioBatch(ctx, m.MeetingID, pcm, id)
	if err != nil {
		t.Fatal(err)
	}
	if ack.AudioMS != 1200 {
		t.Fatal(ack)
	}
	if _, err = svc.AppendAudioBatch(ctx, m.MeetingID, pcm, batchIdentity(pcm, 2, id.SampleCount)); !errors.Is(err, meetings.ErrConflict) {
		t.Fatalf("gap accepted: %v", err)
	}
	changed := append([]byte{}, pcm...)
	changed[0] = 1
	if _, err = svc.AppendAudioBatch(ctx, m.MeetingID, changed, batchIdentity(changed, 0, 0)); !errors.Is(err, meetings.ErrConflict) {
		t.Fatalf("identity collision accepted: %v", err)
	}
	if _, err = svc.AppendAudioBatch(ctx, m.MeetingID, changed, id); !errors.Is(err, meetings.ErrInvalid) {
		t.Fatalf("digest mismatch accepted: %v", err)
	}
	// A staged but uncommitted file is ignored on restart.
	if err = os.WriteFile(filepath.Join(root, "audio", m.MeetingID, ".meeting-interrupted.tmp"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlitestore.OpenTemplated(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc = meetings.New(store)
	svc.SetAudioRoot(filepath.Join(root, "audio"))
	replay, err := svc.AppendAudioBatch(ctx, m.MeetingID, pcm, id)
	if err != nil || !reflect.DeepEqual(replay, ack) {
		t.Fatalf("restart replay: %#v %v", replay, err)
	}
	for seq := int64(1); seq < 3; seq++ {
		if _, err = svc.AppendAudioBatch(ctx, m.MeetingID, pcm, batchIdentity(pcm, seq, seq*id.SampleCount)); err != nil {
			t.Fatal(err)
		}
	}
	stopped, err := svc.Stop(ctx, m.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	replay, err = svc.AppendAudioBatch(ctx, m.MeetingID, pcm, id)
	if err != nil || !reflect.DeepEqual(replay, ack) {
		t.Fatalf("late ack: %#v %v", replay, err)
	}
	if _, err = svc.AppendAudioBatch(ctx, m.MeetingID, pcm, batchIdentity(pcm, 3, 3*id.SampleCount)); !errors.Is(err, meetings.ErrNotRecording) {
		t.Fatalf("late new audio accepted: %v", err)
	}
	latest, err := svc.Get(ctx, m.MeetingID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.UpdatedAt != stopped.UpdatedAt || latest.Status != stopped.Status {
		t.Fatalf("replay changed stopped meeting: %#v", latest)
	}
	calls := 0
	svc.SetAudioTranscriber(func(_ context.Context, got []byte) (string, error) {
		calls++
		if !reflect.DeepEqual(got, silencePCM(3600)) {
			t.Fatalf("audio duplicated or changed: %d", len(got))
		}
		return "这是完整的录音原文", nil
	})
	if _, err = svc.CatchUp(ctx, m.MeetingID); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("decoder calls: %d", calls)
	}
}

func TestAudioBatchReadViewAggregatesAndPreservesLegacyOrder(t *testing.T) {
	svc := testMeetings(t)
	svc.SetAudioRoot(t.TempDir())
	ctx := context.Background()
	m, err := svc.Start(ctx, "兼容录音", "microphone")
	if err != nil {
		t.Fatal(err)
	}
	legacy := silencePCM(1000)
	legacy[0] = 9
	if _, err = svc.AppendAudio(ctx, m.MeetingID, legacy); err != nil {
		t.Fatal(err)
	}
	pcm := silencePCM(1200)
	for seq := int64(0); seq < 20; seq++ {
		if _, err = svc.AppendAudioBatch(ctx, m.MeetingID, pcm, batchIdentity(pcm, seq, seq*19200)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = svc.Stop(ctx, m.MeetingID); err != nil {
		t.Fatal(err)
	}
	sizes := []int{}
	total := 0
	svc.SetAudioTranscriber(func(_ context.Context, got []byte) (string, error) {
		sizes = append(sizes, len(got))
		total += len(got)
		if len(sizes) == 1 && got[0] != 9 {
			t.Fatal("legacy order changed")
		}
		return "音频已经按顺序读取", nil
	})
	if _, err = svc.CatchUp(ctx, m.MeetingID); err != nil {
		t.Fatal(err)
	}
	if total != 25*32000 || !reflect.DeepEqual(sizes, []int{32000, 640000, 128000}) {
		t.Fatalf("unbounded/fragmented view sizes=%v total=%d", sizes, total)
	}
}
