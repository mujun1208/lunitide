package meetings

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/oklog/ulid/v2"
)

type recoveryMeetingStore struct {
	Store
	meeting Meeting
}

func (s *recoveryMeetingStore) GetMeeting(context.Context, string) (Meeting, error) {
	return s.meeting, nil
}

type endedLoopback struct{ pcm []byte }

func (s *endedLoopback) ReadPCM() ([]byte, error) { return s.pcm, io.EOF }
func (s *endedLoopback) Close() error             { return nil }

func TestLoopbackReconnectKeepsUnconsumedAudio(t *testing.T) {
	prev := openLoopback
	defer func() { openLoopback = prev }()
	store := &recoveryMeetingStore{meeting: Meeting{MeetingID: ulid.Make().String(), Status: StatusRecording, AudioSource: AudioMicrophoneAndSystem}}
	svc := New(store)
	oldPCM := []byte{100, 0, 101, 0}
	openLoopback = func() (loopbackSource, error) { return &endedLoopback{pcm: oldPCM}, nil }
	if err := svc.startLoopback(store.meeting.MeetingID); err != nil {
		t.Fatal(err)
	}
	<-svc.loopback.done
	recovered := &funcLoopback{stop: make(chan struct{})}
	openLoopback = func() (loopbackSource, error) { return recovered, nil }
	defer svc.stopLoopback("")
	pcm, active, err := svc.PollLoopback(context.Background(), store.meeting.MeetingID)
	if err != nil || !active || !bytes.Equal(pcm, oldPCM) {
		t.Fatalf("lost caption audio on recovery: %v %v %v", pcm, active, err)
	}
	var durable []byte
	if err := svc.commitMixedPCM(store.meeting.MeetingID, []byte{1, 0, 2, 0}, func(pcm []byte) error { durable = append([]byte{}, pcm...); return nil }); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(durable, []byte{101, 0, 103, 0}) {
		t.Fatalf("lost durable audio on recovery: %v", durable)
	}
}

func TestLoopbackInitiallyUnavailableRetriesOnlyDuringSystemRecording(t *testing.T) {
	prev := openLoopback
	defer func() { openLoopback = prev }()
	store := &recoveryMeetingStore{meeting: Meeting{MeetingID: ulid.Make().String(), Status: StatusRecording, AudioSource: AudioMicrophoneAndSystem}}
	svc := New(store)
	defer svc.stopLoopback("")
	count := 0
	openLoopback = func() (loopbackSource, error) { count++; return nil, io.ErrClosedPipe }
	for i := 0; i < 3; i++ {
		if _, active, err := svc.PollLoopback(context.Background(), store.meeting.MeetingID); err != nil || active {
			t.Fatalf("missing device reported active: %v %v", active, err)
		}
	}
	if count != 1 {
		t.Fatalf("missing-device retry storm: %d", count)
	}
	svc.loopbackRetryAt = svc.loopbackRetryAt.Add(-10_000_000_000)
	openLoopback = func() (loopbackSource, error) { count++; return &funcLoopback{stop: make(chan struct{})}, nil }
	if _, active, err := svc.PollLoopback(context.Background(), store.meeting.MeetingID); err != nil || !active {
		t.Fatalf("device did not reconnect: %v %v", active, err)
	}
	svc.stopLoopback("")
	store.meeting.Status = StatusReady
	if _, active, err := svc.PollLoopback(context.Background(), store.meeting.MeetingID); err != nil || active {
		t.Fatal("stopped meeting restarted capture")
	}
	store.meeting.Status = StatusRecording
	store.meeting.AudioSource = AudioMicrophone
	if _, active, err := svc.PollLoopback(context.Background(), store.meeting.MeetingID); err != nil || active {
		t.Fatal("microphone-only meeting captured system audio")
	}
}
