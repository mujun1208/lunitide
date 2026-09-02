package voice

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// SherpaBackend recognizes speech with a sherpa-onnx child process.
//
// One process serves every session. Loading the acoustic model costs seconds
// and a few hundred megabytes, which is affordable once and absurd per
// utterance, so the server is started on the first turn and kept warm the way
// the Edge synthesizer keeps its socket. Sessions are websocket connections
// to it, and those are cheap.
type SherpaBackend struct {
	// Root is the directory bundles were installed into.
	Root string
	// ModelID selects a bundle from the catalogue.
	ModelID string
	// Startup bounds the wait for the server to accept connections. Loading
	// a 226 MB model off a cold disk is not instant.
	Startup time.Duration
	// Refiner, when set, re-recognizes each finished utterance and its text
	// is the one returned. Nil leaves the streaming transcript as the
	// answer, which is what the tests that only exercise streaming want and
	// what a machine with only the small model installed gets.
	Refiner Transcriber

	// idMu guards ModelID against the model-switch path (VoiceService.selectModel
	// writes it while Start/Ready read it via modelID()). It is deliberately
	// separate from mu: ensureServer reads modelID() while holding mu, so a
	// shared lock would deadlock. ModelID may be set directly at construction,
	// before any goroutine touches the backend.
	idMu sync.Mutex

	mu     sync.Mutex
	server *sherpaServer
}

type sherpaServer struct {
	cmd  *exec.Cmd
	port int
	// log holds what the process wrote to stderr. Without it a startup
	// failure reaches the user as a dial timeout, and the line explaining
	// which model file was unreadable is thrown away.
	log *tailBuffer
}

func (b *SherpaBackend) Name() string { return "sherpa-onnx" }

// Ready reports whether recognition can start without downloading anything.
func (b *SherpaBackend) Ready(context.Context) error {
	installer := &Installer{Root: b.Root}
	if !installer.Installed(Runtime()) {
		return fmt.Errorf("%w: runtime", ErrModelMissing)
	}
	model, err := LookupBundle(b.modelID())
	if err != nil {
		return err
	}
	if !installer.Installed(model) {
		return fmt.Errorf("%w: %s", ErrModelMissing, model.ID)
	}
	return nil
}

func (b *SherpaBackend) modelID() string {
	b.idMu.Lock()
	defer b.idMu.Unlock()
	if b.ModelID == "" {
		return DefaultModel
	}
	return b.ModelID
}

// SetModelID points the backend at a different bundle. Callers must Shutdown()
// afterwards: the child process holds the previous weights and cannot be told
// about new ones.
func (b *SherpaBackend) SetModelID(id string) {
	b.idMu.Lock()
	b.ModelID = id
	b.idMu.Unlock()
}

func (b *SherpaBackend) startup() time.Duration {
	if b.Startup <= 0 {
		return 60 * time.Second
	}
	return b.Startup
}

// Start opens a recognition session, starting the server if it is not up.
func (b *SherpaBackend) Start(ctx context.Context, opts SessionOptions) (Session, error) {
	if err := b.Ready(ctx); err != nil {
		return nil, err
	}
	server, err := b.ensureServer(ctx)
	if err != nil {
		return nil, err
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, fmt.Sprintf("ws://127.0.0.1:%d", server.port), nil)
	if err != nil {
		// A refused dial usually means the process died since it was last
		// used, so its output is the interesting part of the message.
		return nil, fmt.Errorf("%w: connect to recognizer: %v%s", ErrBackendUnavailable, err, server.log.suffix())
	}

	session := &sherpaSession{
		conn:         conn,
		onTranscript: opts.OnTranscript,
		closed:       make(chan struct{}),
		refiner:      b.Refiner,
	}
	go session.readLoop()
	return session, nil
}

// Warm starts the recognizer's process so the first session does not pay for
// it.
//
// Start blocks until the child accepts connections, which is a model load and
// takes seconds. Doing that when the user activates the microphone means they
// spend those seconds talking to a session that does not exist yet — and,
// because the renderer opens the microphone only once the session is up, to a
// device that is not even recording. Called from the status check the stage
// makes when it mounts, which is far enough ahead to cover it.
func (b *SherpaBackend) Warm(ctx context.Context) error {
	if err := b.Ready(ctx); err != nil {
		return err
	}
	_, err := b.ensureServer(ctx)
	return err
}

// ensureServer returns a running server, starting one if needed.
func (b *SherpaBackend) ensureServer(ctx context.Context) (*sherpaServer, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.server != nil && b.server.alive() {
		return b.server, nil
	}
	b.server = nil

	model, err := LookupBundle(b.modelID())
	if err != nil {
		return nil, err
	}
	arch, err := Architecture(model.ID)
	if err != nil {
		return nil, err
	}

	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBackendUnavailable, err)
	}
	installer := &Installer{Root: b.Root}
	args, err := serverArgs(arch, installer.BundleDir(model.ID), port)
	if err != nil {
		return nil, err
	}

	server, err := spawnServer(serverExecutable(installer.BundleDir(Runtime().ID)), args, port)
	if err != nil {
		return nil, err
	}
	if err := server.waitUntilListening(ctx, b.startup()); err != nil {
		server.stop()
		return nil, err
	}
	b.server = server
	return server, nil
}

// Shutdown stops the child process. Callers that skip it leave a process
// holding a few hundred megabytes until the parent exits.
func (b *SherpaBackend) Shutdown() {
	b.mu.Lock()
	server := b.server
	b.server = nil
	b.mu.Unlock()
	if server != nil {
		server.stop()
	}
}

func (s *sherpaServer) alive() bool {
	return s.cmd != nil && s.cmd.Process != nil && s.cmd.ProcessState == nil
}

// waitUntilListening polls until the port accepts a connection.
//
// Polling rather than parsing the server's output: the banner it prints is
// not a stable interface, and a socket that accepts is the property actually
// being waited on.
func (s *sherpaServer) waitUntilListening(ctx context.Context, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for {
		if !s.alive() {
			return fmt.Errorf("%w: recognizer exited during startup%s", ErrBackendUnavailable, s.log.suffix())
		}
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", s.port), 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: recognizer did not start within %s%s", ErrBackendUnavailable, budget, s.log.suffix())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// freePort asks the operating system for one and immediately gives it back.
//
// There is a race here — something else may take the port between the close
// and the child's bind — and it is the standard one, accepted because the
// alternative is a fixed port that collides with whatever else the user runs
// and fails every time instead of almost never.
func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// utteranceLimitBytes caps the audio held for refinement: sixty seconds, the
// same ceiling the non-streaming server is configured with. Past it the
// recording stops growing rather than the session failing — the streaming
// transcript still covers the whole turn, and a monologue this long is not
// the case this feature is for.
const utteranceLimitBytes = 60 * SampleRate * Channels * BytesPerSample

// sherpaSession is one utterance, or one continuous stretch of listening.
type sherpaSession struct {
	conn         *websocket.Conn
	onTranscript func(Transcript)
	refiner      Transcriber

	// writeMu serializes writes: gorilla permits one concurrent writer, and
	// Append and Finish can be called from different goroutines.
	writeMu sync.Mutex

	mu          sync.Mutex
	latest      string
	latestFinal bool
	err         error
	// utterance keeps the audio the refiner will re-read. Held here rather
	// than by the caller because the caller hands over one frame at a time
	// and has no reason to know a second recognizer exists.
	utterance []byte

	closeOnce sync.Once
	closed    chan struct{}
}

// Append sends one frame of audio.
func (s *sherpaSession) Append(_ context.Context, pcm []byte) error {
	if !ValidFrame(pcm) {
		return fmt.Errorf("voice: frame of %d bytes is not whole 16-bit samples", len(pcm))
	}
	select {
	case <-s.closed:
		return s.finalError()
	default:
	}

	if s.refiner != nil {
		s.mu.Lock()
		if room := utteranceLimitBytes - len(s.utterance); room > 0 {
			s.utterance = append(s.utterance, pcm[:min(len(pcm), room)]...)
		}
		s.mu.Unlock()
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.conn.WriteMessage(websocket.BinaryMessage, pcmToFloat32(pcm)); err != nil {
		return fmt.Errorf("voice: send audio: %w", err)
	}
	return nil
}

// Finish tells the recognizer no more audio is coming and returns the text.
//
// When a refiner is configured this is where the turn's accurate transcript
// comes from, so it costs one decode — a fifth of a second for a normal
// sentence — before the caller sees anything. That is spent here rather than
// in the caller because every caller would otherwise have to know to spend it.
func (s *sherpaSession) Finish(ctx context.Context) (string, error) {
	// Both recognizers are asked at once. Nothing the refiner needs comes
	// from the streaming one — it has held the audio since Append — so doing
	// them in order would add the streaming flush and the decode together,
	// at the exact moment the user has stopped talking and is waiting. On
	// this repository's clips that ordering cost 400-750ms; overlapping them
	// costs whichever is slower, which is usually the flush.
	refined := s.refineAsync(ctx)

	s.writeMu.Lock()
	err := s.conn.WriteMessage(websocket.TextMessage, []byte(doneRequest))
	s.writeMu.Unlock()
	if err != nil {
		return refined(s.best()), fmt.Errorf("voice: close audio stream: %w", err)
	}

	// The reader closes the channel when the server acknowledges. Waiting on
	// the context too means a server that stops answering costs the caller a
	// deadline rather than the turn.
	select {
	case <-s.closed:
		streamed := s.best()
		if failure := s.finalError(); failure != nil {
			return streamed, failure
		}
		return refined(streamed), nil
	case <-ctx.Done():
		return s.best(), ctx.Err()
	}
}

// refineAsync starts the second recognizer on the recorded utterance and
// returns the function that waits for its answer.
//
// Every failure yields the streaming transcript instead. That is the whole
// point of keeping it: this path involves a second process, a second model
// and a socket, and a turn where any of them misbehaves should cost accuracy,
// not the user's sentence. The failure is logged because a refiner that is
// quietly never working would otherwise look identical to one that is — the
// user would just find the recognizer worse than it was promised to be, with
// nothing anywhere saying why.
func (s *sherpaSession) refineAsync(ctx context.Context) func(streamed string) string {
	keep := func(streamed string) string { return streamed }
	if s.refiner == nil {
		return keep
	}
	s.mu.Lock()
	pcm := s.utterance
	s.mu.Unlock()
	if len(pcm) == 0 {
		return keep
	}

	type answer struct {
		text string
		err  error
	}
	// Buffered so the goroutine can finish and exit even when the caller
	// gave up on it — an abandoned decode is bounded by the refiner's own
	// budget, and blocking it forever on an unread channel would not be.
	done := make(chan answer, 1)
	go func() {
		text, err := s.refiner.Transcribe(ctx, pcm)
		done <- answer{text, err}
	}()

	return func(streamed string) string {
		select {
		case got := <-done:
			if got.err != nil {
				log.Printf("voice: refine utterance (%d ms): %v", FrameDurationMillis(pcm), got.err)
				return streamed
			}
			if strings.TrimSpace(got.text) == "" {
				// The streaming model heard words and the accurate one
				// heard none. Trusting the empty result would turn a
				// working turn into silence.
				if streamed != "" {
					log.Printf("voice: refiner returned nothing for %d ms of audio; keeping streamed text", FrameDurationMillis(pcm))
				}
				return streamed
			}
			return got.text
		case <-ctx.Done():
			return streamed
		}
	}
}

func (s *sherpaSession) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return s.conn.Close()
}

// readLoop delivers transcripts until the server finishes or the socket dies.
func (s *sherpaSession) readLoop() {
	defer s.closeOnce.Do(func() { close(s.closed) })
	for {
		kind, frame, err := s.conn.ReadMessage()
		if err != nil {
			// A close after the server said its piece is the normal end of
			// a session, not a failure worth reporting.
			if !isExpectedClose(err) {
				s.setError(fmt.Errorf("voice: read transcript: %w", err))
			}
			return
		}
		if kind != websocket.TextMessage {
			continue
		}
		if string(frame) == doneMarker {
			return
		}
		transcript, ok := parseSherpaMessage(frame)
		if !ok {
			continue
		}
		s.mu.Lock()
		s.latest, s.latestFinal = transcript.Text, transcript.Final
		s.mu.Unlock()
		if s.onTranscript != nil {
			s.onTranscript(transcript)
		}
	}
}

// Latest reports what the recognizer has heard so far and whether it has
// settled. The bridge rides this back on the reply to each audio frame, so a
// caption updates at the rate speech arrives without a second channel.
func (s *sherpaSession) Latest() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.latest), s.latestFinal
}

// best returns the most complete transcript seen.
//
// It is returned even alongside an error: half a sentence the user actually
// said is more use to them than an empty box and an apology.
func (s *sherpaSession) best() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.latest)
}

func (s *sherpaSession) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func (s *sherpaSession) finalError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func isExpectedClose(err error) bool {
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	return websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure)
}

// tailBuffer keeps the last few kilobytes a process wrote.
//
// Bounded because a server that fails in a loop would otherwise grow this
// without limit, and because only the end of the output explains the exit.
type tailBuffer struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func newTailBuffer(limit int) *tailBuffer { return &tailBuffer{limit: limit} }

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.limit {
		t.buf = t.buf[len(t.buf)-t.limit:]
	}
	return len(p), nil
}

// suffix formats the tail for appending to an error, or nothing if the
// process was silent.
func (t *tailBuffer) suffix() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if text := strings.TrimSpace(string(t.buf)); text != "" {
		return ": " + text
	}
	return ""
}
