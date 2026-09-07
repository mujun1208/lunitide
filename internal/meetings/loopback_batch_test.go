package meetings_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/meetings"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func TestLoopbackBatchRetainsSystemAudioAcrossFailedWriteAndReplay(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := storage.OpenTemplated(ctx, filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	frames := make(chan []byte, 1)
	system := []byte{100, 0, 200, 0, 44, 1, 144, 1}
	frames <- system
	meetings.InstallLoopbackForTest(t, func() []byte {
		select {
		case pcm := <-frames:
			return pcm
		default:
			return nil
		}
	})
	svc := meetings.New(store)
	svc.SetAudioRoot(filepath.Join(root, "audio"))
	m, err := svc.Start(ctx, "all computer audio", meetings.AudioMicrophoneAndSystem)
	if err != nil || m.AudioSource != meetings.AudioMicrophoneAndSystem {
		t.Fatalf("start: %+v %v", m, err)
	}
	defer svc.Stop(ctx, m.MeetingID)
	until := time.Now().Add(2 * time.Second)
	polled := 0
	for time.Now().Before(until) && polled < len(system) {
		pcm, _, err := svc.PollLoopback(ctx, m.MeetingID)
		if err != nil {
			t.Fatal(err)
		}
		polled += len(pcm)
		time.Sleep(time.Millisecond)
	}
	if polled != len(system) {
		t.Fatal("native capture fixture did not arrive")
	}
	mic := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	id := batchIdentity(mic, 0, 0)
	// Sequence rejection must leave both sources available.
	if _, err := svc.AppendAudioBatch(ctx, m.MeetingID, mic, batchIdentity(mic, 1, 0)); err == nil {
		t.Fatal("sequence gap accepted")
	}
	path := filepath.Join(root, "audio", m.MeetingID, fmt.Sprintf("batch_%s_%012d.json", id.CaptureSessionID, id.ChunkSeq))
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AppendAudioBatch(ctx, m.MeetingID, mic, id); err == nil {
		t.Fatal("fixture must reject disk commit")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	ack, err := svc.AppendAudioBatch(ctx, m.MeetingID, mic, id)
	if err != nil {
		t.Fatal(err)
	}
	var batch struct {
		PCM []byte `json:"pcm"`
	}
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &batch) != nil {
		t.Fatalf("read durable batch: %v", err)
	}
	want := []uint16{101, 202, 303, 404}
	got := []uint16{}
	for i := 0; i < len(batch.PCM); i += 2 {
		got = append(got, binary.LittleEndian.Uint16(batch.PCM[i:]))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("system audio lost or overlapped: got %v want %v", got, want)
	}
	replay, err := svc.AppendAudioBatch(ctx, m.MeetingID, mic, id)
	if err != nil || replay != ack {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || string(unchanged) != string(raw) {
		t.Fatal("lost ACK replay remixed committed audio")
	}
}
