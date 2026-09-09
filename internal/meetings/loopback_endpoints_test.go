package meetings

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

type fixtureEndpoint struct {
	packets []endpointPacket
	err     error
	closed  int
}

func (f *fixtureEndpoint) readPacket() (endpointPacket, error) {
	if len(f.packets) == 0 {
		return endpointPacket{}, f.err
	}
	p := f.packets[0]
	f.packets = f.packets[1:]
	return p, nil
}
func (f *fixtureEndpoint) close() { f.closed++ }

func constantEndpointPacket(position int64, rate, frames int, sample float32) endpointPacket {
	raw := make([]byte, frames*8)
	for i := 0; i < len(raw); i += 4 {
		binary.LittleEndian.PutUint32(raw[i:], math.Float32bits(sample))
	}
	return endpointPacket{position: position, format: pcmFormat{channels: 2, rate: rate, bits: 32, blockAlign: 8, float: true}, raw: raw}
}

func assertPCMSample(t *testing.T, pcm []byte, index, want int) {
	t.Helper()
	if index*2+2 > len(pcm) {
		t.Fatalf("sample %d missing in %d bytes", index, len(pcm))
	}
	got := int(int16(binary.LittleEndian.Uint16(pcm[index*2:])))
	if got != want {
		t.Fatalf("sample %d = %d, want %d", index, got, want)
	}
}

func TestEndpointMixRolesOtherDevicesAndSingleMicrophone(t *testing.T) {
	origin := int64(123456000)
	opened := map[string]int{}
	e := newLoopbackEndpoints(origin, func() ([]string, error) {
		return []string{"console", "communications", "console", "app-output", "idle", ""}, nil
	}, func(id string) (endpointCapture, error) {
		opened[id]++
		level := map[string]float32{"console": .1, "communications": .2, "app-output": .3}[id]
		return &fixtureEndpoint{packets: []endpointPacket{constantEndpointPacket(origin, 48000, 4800, level)}}, nil
	})
	defer e.close()
	if err := e.refresh(time.Now()); err != nil {
		t.Fatal(err)
	}
	e.read(time.Now())
	pcm := e.mixer.render(origin + loopbackClockRate/10 + endpointMixDelay)
	if len(pcm) != 3200 || len(opened) != 4 || opened["console"] != 1 {
		t.Fatalf("duplicated endpoints or duration: bytes=%d opens=%v", len(pcm), opened)
	}
	assertPCMSample(t, pcm, 0, 3276+6553+9830)
	// The same system frame serves captions and persistence, each adding the
	// one existing microphone frame exactly once, including on a failed commit.
	sess := &loopbackSession{}
	sess.append(pcm)
	mic := make([]byte, len(pcm))
	for i := 0; i < len(mic); i += 2 {
		binary.LittleEndian.PutUint16(mic[i:], 1000)
	}
	caption := mixS16le(mic, sess.takePoll())
	svc := &Service{loopback: sess}
	if err := svc.commitMixedPCM("", mic, func([]byte) error { return errors.New("disk full") }); err == nil {
		t.Fatal("expected failed write")
	}
	var saved []byte
	if err := svc.commitMixedPCM("", mic, func(p []byte) error { saved = p; return nil }); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, caption) {
		t.Fatal("caption and saved mix differ")
	}
	assertPCMSample(t, saved, 0, 1000+3276+6553+9830)
	if len(sess.mixBuf) != 0 {
		t.Fatal("committed system samples retained")
	}
}

func TestEndpointSwitchPreservesHealthyStreamAndRecoversIndependently(t *testing.T) {
	now := time.Unix(100, 0)
	ids := []string{"speakers", "headset"}
	listErr := error(nil)
	blocked := map[string]bool{"usb": true}
	opened := map[string]int{}
	e := newLoopbackEndpoints(0, func() ([]string, error) { return ids, listErr }, func(id string) (endpointCapture, error) {
		opened[id]++
		if blocked[id] {
			return nil, errors.New("device busy")
		}
		return &fixtureEndpoint{}, nil
	})
	defer e.close()
	refresh := func() {
		t.Helper()
		if err := e.refresh(now); err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	speaker := e.streams["speakers"].(*fixtureEndpoint)
	headset := e.streams["headset"].(*fixtureEndpoint)
	// A role change only reorders IDs. App-specific output can join while the
	// old default remains valid, without opening the default twice.
	ids = []string{"headset", "speakers", "headset", "usb"}
	refresh()
	if e.streams["speakers"] != speaker || opened["headset"] != 1 {
		t.Fatal("role change reopened healthy streams")
	}
	refresh()
	if opened["usb"] != 1 {
		t.Fatal("failed activation retry storm")
	}
	listErr = errors.New("enumeration temporarily failed")
	if e.refresh(now) == nil || len(e.streams) != 2 {
		t.Fatal("enumeration error dropped healthy devices")
	}
	listErr = nil
	headset.err = errors.New("AUDCLNT_E_DEVICE_INVALIDATED")
	e.read(now)
	if headset.closed != 1 || e.streams["speakers"] != speaker {
		t.Fatal("invalidated device stopped healthy stream")
	}
	refresh()
	if opened["headset"] != 1 {
		t.Fatal("read failure retried too soon")
	}
	blocked["usb"] = false
	now = now.Add(endpointRetry)
	refresh()
	if len(e.streams) != 3 || opened["headset"] != 2 || opened["usb"] != 2 {
		t.Fatalf("did not recover: %v", opened)
	}
	ids = []string{"usb"}
	refresh()
	if speaker.closed != 1 || len(e.streams) != 1 {
		t.Fatal("unplugged endpoints retained")
	}
	ids = nil
	refresh()
	if len(e.streams) != 0 {
		t.Fatal("all endpoints lost but still active")
	}
	ids = []string{"speakers"}
	refresh()
	if len(e.streams) != 1 {
		t.Fatal("could not recover after all outputs disappeared")
	}
	e.close()
	if len(e.streams) != 0 {
		t.Fatal("close leaked streams")
	}
}

func TestEndpointMixerKeepsSilenceAndJoinsAtCurrentTime(t *testing.T) {
	m := endpointMixer{ends: make(map[string]int64)}
	first := constantEndpointPacket(0, 16000, 1600, .25)
	m.add("speaker", first)
	m.add("speaker", first) // Replayed/overlapping packet is not another voice.
	pcm := m.render(loopbackClockRate/10 + endpointMixDelay)
	assertPCMSample(t, pcm, 0, 8191)
	gap := m.render(loopbackClockRate*3/10 + endpointMixDelay)
	if len(gap) != 6400 || !bytes.Equal(gap, make([]byte, 6400)) {
		t.Fatal("idle timeline collapsed")
	}
	m.add("headset", constantEndpointPacket(loopbackClockRate*3/10, 44100, 4410, .5))
	m.add("speaker", first) // Late packet after switch must not replay.
	joined := m.render(loopbackClockRate*4/10 + endpointMixDelay)
	if len(joined) != 3200 {
		t.Fatal("joining endpoint extended duration")
	}
	assertPCMSample(t, joined, 0, 16383)
	assertPCMSample(t, joined, 1599, 16383)
}

func TestEndpointReopenDoesNotDoubleBufferedAudio(t *testing.T) {
	ids := []string{"headset"}
	packet := constantEndpointPacket(0, 48000, 4800, .25)
	e := newLoopbackEndpoints(0, func() ([]string, error) { return ids, nil }, func(string) (endpointCapture, error) {
		return &fixtureEndpoint{packets: []endpointPacket{packet}}, nil
	})
	defer e.close()
	now := time.Now()
	if err := e.refresh(now); err != nil {
		t.Fatal(err)
	}
	e.read(now)
	ids = nil
	if err := e.refresh(now); err != nil {
		t.Fatal(err)
	}
	ids = []string{"headset"}
	if err := e.refresh(now); err != nil {
		t.Fatal(err)
	}
	e.read(now)
	pcm := e.mixer.render(loopbackClockRate/10 + endpointMixDelay)
	assertPCMSample(t, pcm, 0, 8191)
	assertPCMSample(t, pcm, 1599, 8191)
}

func TestEndpointMixerResamplesIrregularPacketsWithoutDrift(t *testing.T) {
	for _, rate := range []int{16000, 44100, 48000, 96000} {
		t.Run(strconv.Itoa(rate), func(t *testing.T) {
			m := endpointMixer{ends: make(map[string]int64)}
			var out []byte
			for frame := 0; frame < rate*3; {
				n := min(137, rate*3-frame)
				m.add("output", constantEndpointPacket(int64(frame)*loopbackClockRate/int64(rate), rate, n, .25))
				frame += n
				out = append(out, m.render(int64(frame)*loopbackClockRate/int64(rate))...)
			}
			out = append(out, m.render(3*loopbackClockRate+endpointMixDelay)...)
			if len(out) != 3*audioSampleRate*2 {
				t.Fatalf("duration drift: %d bytes", len(out))
			}
			for i := 0; i < len(out)/2; i++ {
				assertPCMSample(t, out, i, 8191)
			}
		})
	}
}

func TestEndpointMixerSaturatesOnceAndBoundsStaleData(t *testing.T) {
	m := endpointMixer{ends: make(map[string]int64)}
	for id, level := range map[string]float32{"a": .9, "b": .9, "c": -.9} {
		m.add(id, constantEndpointPacket(0, 48000, 4800, level))
	}
	assertPCMSample(t, m.render(loopbackClockRate/10+endpointMixDelay), 0, 29490)
	m.add("loud", constantEndpointPacket(loopbackClockRate/10, 48000, 4800, 1))
	m.add("louder", constantEndpointPacket(loopbackClockRate/10, 48000, 4800, 1))
	assertPCMSample(t, m.render(loopbackClockRate/5+endpointMixDelay), 0, 32767)
	m.add("future", constantEndpointPacket(math.MaxInt64, 48000, 4800, .5))
	out := m.render(100*loopbackClockRate + endpointMixDelay)
	if len(out) > endpointMixCapacity*2 || !bytes.Equal(out, make([]byte, len(out))) {
		t.Fatal("stalled pump retained stale audio or unbounded backlog")
	}
}

type activeFixtureLoopback struct {
	funcLoopback
	available bool
}

func (s *activeFixtureLoopback) Active() bool { return s.available }

func TestLoopbackReportsEndpointLossWithoutRestartingOwner(t *testing.T) {
	id := ulid.Make().String()
	source := &activeFixtureLoopback{funcLoopback: funcLoopback{stop: make(chan struct{})}}
	sess := &loopbackSession{meetingID: id, src: source, done: make(chan struct{})}
	svc := New(&recoveryMeetingStore{meeting: Meeting{MeetingID: id, Status: StatusRecording, AudioSource: AudioMicrophoneAndSystem}})
	svc.loopback = sess
	for _, available := range []bool{true, false, true} {
		source.available = available
		_, active, err := svc.PollLoopback(context.Background(), id)
		if err != nil || active != available || svc.loopback != sess {
			t.Fatalf("capture owner/status changed: %t %v", active, err)
		}
	}
}
