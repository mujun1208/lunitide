package voice

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// What the refiner is for, in one sentence: on this repository's own
// recordings the streaming recognizer hears 频繁 as 平反 and 礼拜二 as 里拜二,
// and a second pass over the finished utterance hears them correctly. So the
// property these tests protect is not "the refiner runs" — it is that its
// text is the one that leaves, and that nothing it can do costs the user a
// turn when it goes wrong.

// fakeRefiner answers without a subprocess.
type fakeRefiner struct {
	text string
	err  error

	mu   sync.Mutex
	sawA []byte
}

func (f *fakeRefiner) Transcribe(_ context.Context, pcm []byte) (string, error) {
	f.mu.Lock()
	f.sawA = append(f.sawA, pcm...)
	f.mu.Unlock()
	return f.text, f.err
}

func (f *fakeRefiner) audio() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.sawA...)
}

// speechFrame is a frame that is not silence, so a test can tell recorded
// audio apart from zero padding.
func speechFrame(value int16) []byte {
	frame := make([]byte, FrameBytes)
	for offset := 0; offset+1 < len(frame); offset += BytesPerSample {
		binary.LittleEndian.PutUint16(frame[offset:], uint16(value))
	}
	return frame
}

func TestFinishReturnsTheRefinedTextRatherThanTheStreamedOne(t *testing.T) {
	fake := &fakeRecognizer{
		emitDuringAudio: []string{`{"text":"这个是平反的","segment":0,"is_final":false}`},
		script:          []string{`{"text":"这个是平反的","segment":0,"is_final":true}`},
	}
	session := dialFake(t, fake.serve(t), nil)
	refiner := &fakeRefiner{text: "这个是频繁的。"}
	session.refiner = refiner

	ctx := context.Background()
	if err := session.Append(ctx, speechFrame(4000)); err != nil {
		t.Fatalf("append: %v", err)
	}
	text, err := session.Finish(ctx)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if text != "这个是频繁的。" {
		t.Errorf("Finish = %q; the streamed transcript was returned instead of the refined one", text)
	}
	// The refiner has to be handed the audio, not the transcript. Sending it
	// text would make it a punctuation model and lose the entire point.
	if got := len(refiner.audio()); got != FrameBytes {
		t.Errorf("refiner saw %d bytes of audio; want the one frame that was appended (%d)", got, FrameBytes)
	}
}

func TestFinishKeepsTheStreamedTextWhenTheRefinerFails(t *testing.T) {
	// Every way this can go wrong ends here: a dead sidecar, a socket that
	// hangs, a model that will not load. The user said a sentence; losing it
	// because the second recognizer had a bad day is not an acceptable
	// outcome, so the streamed text is the floor.
	for _, testCase := range []struct {
		name    string
		refiner *fakeRefiner
	}{
		{"the refiner errors", &fakeRefiner{err: errors.New("sidecar exited")}},
		{"the refiner hears nothing", &fakeRefiner{text: "   "}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fake := &fakeRecognizer{script: []string{`{"text":"明天下午三点","segment":0,"is_final":true}`}}
			session := dialFake(t, fake.serve(t), nil)
			session.refiner = testCase.refiner

			ctx := context.Background()
			if err := session.Append(ctx, speechFrame(4000)); err != nil {
				t.Fatalf("append: %v", err)
			}
			text, err := session.Finish(ctx)
			if err != nil {
				t.Fatalf("finish reported a failure the caller cannot act on: %v", err)
			}
			if text != "明天下午三点" {
				t.Errorf("Finish = %q; want the streamed transcript to survive", text)
			}
		})
	}
}

func TestSessionWithoutARefinerRecordsNothing(t *testing.T) {
	// A minute of audio is two megabytes. Holding it for a refiner that does
	// not exist would be two megabytes per turn of pure waste on exactly the
	// machines that chose not to install the bigger model.
	fake := &fakeRecognizer{script: []string{`{"text":"好","segment":0,"is_final":true}`}}
	session := dialFake(t, fake.serve(t), nil)

	if err := session.Append(context.Background(), speechFrame(4000)); err != nil {
		t.Fatalf("append: %v", err)
	}
	session.mu.Lock()
	held := len(session.utterance)
	session.mu.Unlock()
	if held != 0 {
		t.Errorf("session held %d bytes of audio with no refiner configured", held)
	}
}

func TestRecordingStopsAtTheServersOwnLimit(t *testing.T) {
	// The non-streaming server rejects a connection whose preamble promises
	// more than max-utterance-length. Sending it more than that would turn a
	// long turn into a failed one, so the recording stops instead.
	fake := &fakeRecognizer{script: []string{`{"text":"...","segment":0,"is_final":true}`}}
	session := dialFake(t, fake.serve(t), nil)
	session.refiner = &fakeRefiner{text: "ok"}

	ctx := context.Background()
	// Two frames past the ceiling.
	for sent := 0; sent < utteranceLimitBytes+2*FrameBytes; sent += FrameBytes {
		if err := session.Append(ctx, speechFrame(4000)); err != nil {
			t.Fatalf("append at %d: %v", sent, err)
		}
	}
	session.mu.Lock()
	held := len(session.utterance)
	session.mu.Unlock()
	if held != utteranceLimitBytes {
		t.Errorf("held %d bytes; want it capped at %d", held, utteranceLimitBytes)
	}
}

// The wire format below has to agree with sherpa byte for byte. It is
// described in offline-websocket-server-impl.h and nowhere else, so these
// tests are the only thing standing between a change here and a server that
// silently decodes noise.

func TestOfflineRequestCarriesTheRateAndSizeSherpaExpects(t *testing.T) {
	// Written through a slice rather than as constants: uint16(-16384) is a
	// compile error, and the conversion has to go through an int16 value.
	pcm := make([]byte, 4*BytesPerSample)
	for index, sample := range []int16{16384, -16384, -32768, 0} {
		binary.LittleEndian.PutUint16(pcm[index*BytesPerSample:], uint16(sample))
	}

	request, err := offlineRequest(pcm)
	if err != nil {
		t.Fatalf("frame request: %v", err)
	}
	if rate := binary.LittleEndian.Uint32(request[0:4]); rate != SampleRate {
		t.Errorf("preamble rate = %d; want %d", rate, SampleRate)
	}
	// The size is in bytes of float32 samples, not bytes of PCM. Getting
	// this wrong makes the server wait forever for audio that never comes.
	if size := binary.LittleEndian.Uint32(request[4:8]); size != 4*4 {
		t.Errorf("preamble size = %d; want %d", size, 4*4)
	}
	body := request[offlineHeaderBytes:]
	for index, want := range []float32{0.5, -0.5, -1, 0} {
		got := math.Float32frombits(binary.LittleEndian.Uint32(body[index*4:]))
		if math.Abs(float64(got-want)) > 1e-6 {
			t.Errorf("sample %d = %v; want %v", index, got, want)
		}
	}
}

func TestOfflineRequestRefusesAnEmptyUtterance(t *testing.T) {
	// A turn where the user pressed the button and said nothing. Sending a
	// zero-length promise makes the server wait for a decode that will never
	// be asked for, holding a connection until the budget expires.
	if _, err := offlineRequest(nil); !errors.Is(err, ErrNoAudio) {
		t.Errorf("offlineRequest(nil) error = %v; want ErrNoAudio", err)
	}
}

func TestOfflineChunksStayWithinOneFrameAndLoseNothing(t *testing.T) {
	request, err := offlineRequest(make([]byte, 30*1024))
	if err != nil {
		t.Fatalf("frame request: %v", err)
	}
	var rejoined []byte
	for _, chunk := range offlineChunks(request) {
		if len(chunk) > offlineChunkBytes {
			t.Fatalf("chunk of %d bytes exceeds %d", len(chunk), offlineChunkBytes)
		}
		rejoined = append(rejoined, chunk...)
	}
	if len(rejoined) != len(request) {
		t.Errorf("rejoined %d bytes; want %d", len(rejoined), len(request))
	}
}

func TestOfflineResultReportsUnreadableRepliesRatherThanReturningNothing(t *testing.T) {
	// The server closes with a plain-text reason when it refuses an
	// utterance. Parsing that as an empty transcript would be
	// indistinguishable from the user having said nothing, and the failure
	// would never reach the log that explains it.
	if _, err := parseOfflineResult([]byte("Max utterance length is configured to 60 seconds")); err == nil {
		t.Error("a non-JSON reply parsed as a transcript")
	}
	text, err := parseOfflineResult([]byte(`{"text":"礼拜二"}`))
	if err != nil || text != "礼拜二" {
		t.Errorf("parseOfflineResult = %q, %v", text, err)
	}
}

func TestAColdRefinerGivesUpImmediatelyInsteadOfLoadingAModel(t *testing.T) {
	// The first turn after the app starts. Waiting here would put the whole
	// model load — seconds of it — into the pause after the user stops
	// talking, which is the worst possible place to spend it.
	refiner := &Refiner{Root: t.TempDir()}

	started := time.Now()
	_, err := refiner.Transcribe(context.Background(), speechFrame(1000))
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("a refiner with no model installed reported a successful decode")
	}
	if elapsed > time.Second {
		t.Errorf("cold Transcribe took %s; it must not wait for a model to load", elapsed)
	}
}

// fakeOfflineServer speaks the non-streaming protocol: it collects binary
// frames until it has what the preamble promised, then answers once.
func fakeOfflineServer(t *testing.T, reply string) (endpoint string, received func() []byte) {
	t.Helper()
	var mu sync.Mutex
	var body []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var expected int
		var got []byte
		for {
			kind, frame, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if kind != websocket.BinaryMessage {
				continue
			}
			if expected == 0 {
				if len(frame) < offlineHeaderBytes {
					return
				}
				expected = int(binary.LittleEndian.Uint32(frame[4:8]))
				frame = frame[offlineHeaderBytes:]
			}
			got = append(got, frame...)
			if len(got) >= expected {
				mu.Lock()
				body = got
				mu.Unlock()
				_ = conn.WriteMessage(websocket.TextMessage, []byte(reply))
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http"), func() []byte {
		mu.Lock()
		defer mu.Unlock()
		return append([]byte(nil), body...)
	}
}

func TestTranscribeOnSendsTheWholeUtteranceAndReturnsTheResult(t *testing.T) {
	endpoint, received := fakeOfflineServer(t, `{"text":" 昨天是 monday "}`)

	pcm := speechFrame(1000)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	text, err := transcribeOn(ctx, endpoint, pcm)
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if text != "昨天是 monday" {
		t.Errorf("text = %q; want it trimmed", text)
	}
	if got, want := len(received()), len(pcm)/BytesPerSample*4; got != want {
		t.Errorf("server received %d bytes of samples; want %d", got, want)
	}
}

func TestTranscribeOnGivesUpWhenTheServerNeverAnswers(t *testing.T) {
	// A decode that wedges must cost a bounded wait, not the turn. Without
	// the read deadline this blocks until the process is killed.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := transcribeOn(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), speechFrame(1000)); err == nil {
		t.Fatal("a server that never answers was treated as a successful decode")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Errorf("gave up after %s; the deadline was 300ms", elapsed)
	}
}
