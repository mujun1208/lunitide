package voice

import (
	"context"
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The session is tested against a stand-in speaking sherpa's protocol rather
// than against sherpa itself. The real server needs a 226 MB model and some
// seconds to load it, which would make this untestable in CI and slow enough
// locally that nobody would run it. What is being checked here is the wire
// contract — what we send, what we do with what comes back — and that is
// exactly what a stand-in can hold still.

type fakeRecognizer struct {
	// script is sent as text frames once the client says it is done.
	script []string
	// emitDuringAudio is sent as soon as the first audio frame arrives, to
	// exercise partial transcripts.
	emitDuringAudio []string

	mu       sync.Mutex
	received []byte
	sawDone  bool
}

func (f *fakeRecognizer) serve(t *testing.T) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		var emitted bool
		for {
			kind, frame, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if kind == websocket.BinaryMessage {
				f.mu.Lock()
				f.received = append(f.received, frame...)
				f.mu.Unlock()
				if !emitted {
					emitted = true
					for _, message := range f.emitDuringAudio {
						_ = conn.WriteMessage(websocket.TextMessage, []byte(message))
					}
				}
				continue
			}
			if string(frame) == doneRequest {
				f.mu.Lock()
				f.sawDone = true
				f.mu.Unlock()
				for _, message := range f.script {
					_ = conn.WriteMessage(websocket.TextMessage, []byte(message))
				}
				_ = conn.WriteMessage(websocket.TextMessage, []byte(doneMarker))
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func (f *fakeRecognizer) audio() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.received...)
}

// dialFake opens a session against the stand-in, bypassing the process
// management that a real backend would do.
func dialFake(t *testing.T, server *httptest.Server, onTranscript func(Transcript)) *sherpaSession {
	t.Helper()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("dial stand-in: %v", err)
	}
	session := &sherpaSession{conn: conn, onTranscript: onTranscript, closed: make(chan struct{})}
	go session.readLoop()
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func silentFrame() []byte { return make([]byte, FrameBytes) }

func TestSessionStreamsAudioAndReturnsTheFinalTranscript(t *testing.T) {
	fake := &fakeRecognizer{
		emitDuringAudio: []string{`{"text":"今天","segment":0,"is_final":false}`},
		script:          []string{`{"text":"今天天气怎么样","segment":0,"is_final":true}`},
	}
	server := fake.serve(t)

	var mu sync.Mutex
	var seen []Transcript
	session := dialFake(t, server, func(tr Transcript) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, tr)
	})

	for range 3 {
		if err := session.Append(context.Background(), silentFrame()); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	text, err := session.Finish(ctx)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if text != "今天天气怎么样" {
		t.Errorf("transcript = %q; want the final result", text)
	}

	// Three 100ms frames of 16-bit audio become three frames of float32.
	if got, want := len(fake.audio()), 3*FrameBytes*2; got != want {
		t.Errorf("server received %d bytes; want %d as float32", got, want)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) < 2 {
		t.Fatalf("saw %d transcripts; want the partial and the final", len(seen))
	}
	if seen[0].Final {
		t.Error("the first transcript should have been a partial")
	}
	if !seen[len(seen)-1].Final {
		t.Error("the last transcript should have been final")
	}
}

func TestSessionSendsNormalizedFloats(t *testing.T) {
	fake := &fakeRecognizer{script: []string{`{"text":"x","is_final":true}`}}
	server := fake.serve(t)
	session := dialFake(t, server, nil)

	// Full-scale positive and negative, which is where a scaling mistake
	// shows up as clipping the recognizer cannot see past.
	frame := make([]byte, FrameBytes)
	positiveFullScale, negativeFullScale := int16(32767), int16(-32768)
	binary.LittleEndian.PutUint16(frame[0:], uint16(positiveFullScale))
	binary.LittleEndian.PutUint16(frame[2:], uint16(negativeFullScale))
	if err := session.Append(context.Background(), frame); err != nil {
		t.Fatalf("append: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := session.Finish(ctx); err != nil {
		t.Fatalf("finish: %v", err)
	}

	audio := fake.audio()
	if len(audio) < 8 {
		t.Fatalf("server received %d bytes; want at least two samples", len(audio))
	}
	first := math.Float32frombits(binary.LittleEndian.Uint32(audio[0:]))
	second := math.Float32frombits(binary.LittleEndian.Uint32(audio[4:]))
	if first <= 0.99 || first > 1 {
		t.Errorf("full-scale positive arrived as %v; want just under 1", first)
	}
	if second != -1 {
		t.Errorf("full-scale negative arrived as %v; want -1", second)
	}
}

func TestSessionRejectsAFrameThatIsNotWholeSamples(t *testing.T) {
	fake := &fakeRecognizer{}
	session := dialFake(t, fake.serve(t), nil)
	if err := session.Append(context.Background(), []byte{0x01}); err == nil {
		t.Error("an odd-length frame should be refused rather than sent")
	}
}

func TestSessionReturnsWhatItHeardWhenTheContextExpires(t *testing.T) {
	// A recognizer that takes the audio and then says nothing. Half a
	// sentence the user really said beats an empty box and an apology.
	fake := &fakeRecognizer{
		emitDuringAudio: []string{`{"text":"今天天气","segment":0,"is_final":false}`},
		script:          nil,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(fake.emitDuringAudio[0]))
		// Then hang, which is the failure being modelled.
		select {}
	}))
	t.Cleanup(server.Close)

	session := dialFake(t, server, nil)
	if err := session.Append(context.Background(), silentFrame()); err != nil {
		t.Fatalf("append: %v", err)
	}

	// Give the partial time to arrive before the deadline that follows.
	time.Sleep(200 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	text, err := session.Finish(ctx)
	if err == nil {
		t.Error("a recognizer that stops answering should surface an error")
	}
	if text != "今天天气" {
		t.Errorf("transcript = %q; the partial should still be returned", text)
	}
}

func TestSessionAppendAfterCloseDoesNotPanic(t *testing.T) {
	fake := &fakeRecognizer{}
	session := dialFake(t, fake.serve(t), nil)
	if err := session.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// Closing twice is what happens when a turn ends and the cleanup runs.
	if err := session.Close(); err == nil {
		t.Log("second close returned no error, which is fine")
	}
	if err := session.Append(context.Background(), silentFrame()); err == nil {
		t.Log("append after close reported no error; it must at least not panic")
	}
}

func TestBackendReportsAMissingModelRatherThanFailingLater(t *testing.T) {
	backend := &SherpaBackend{Root: t.TempDir()}
	err := backend.Ready(context.Background())
	if err == nil {
		t.Fatal("an empty install root should not look ready")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("error = %v; want it to say the model is missing", err)
	}
	if _, err := backend.Start(context.Background(), SessionOptions{}); err == nil {
		t.Error("starting without a model should fail at the seam, not inside the child process")
	}
}

func TestBackendNameIsStable(t *testing.T) {
	// Surfaced in diagnostics and settings, so it is part of the contract.
	if got := (&SherpaBackend{}).Name(); got != "sherpa-onnx" {
		t.Errorf("Name() = %q", got)
	}
}
