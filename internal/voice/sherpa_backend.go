package voice

import (
	"context"
	"errors"
	"fmt"
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
	if b.ModelID == "" {
		return DefaultModel
	}
	return b.ModelID
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

	session := &sherpaSession{conn: conn, onTranscript: opts.OnTranscript, closed: make(chan struct{})}
	go session.readLoop()
	return session, nil
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

// sherpaSession is one utterance, or one continuous stretch of listening.
type sherpaSession struct {
	conn         *websocket.Conn
	onTranscript func(Transcript)

	// writeMu serializes writes: gorilla permits one concurrent writer, and
	// Append and Finish can be called from different goroutines.
	writeMu sync.Mutex

	mu          sync.Mutex
	latest      string
	latestFinal bool
	err         error

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

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.conn.WriteMessage(websocket.BinaryMessage, pcmToFloat32(pcm)); err != nil {
		return fmt.Errorf("voice: send audio: %w", err)
	}
	return nil
}

// Finish tells the recognizer no more audio is coming and returns the text.
func (s *sherpaSession) Finish(ctx context.Context) (string, error) {
	s.writeMu.Lock()
	err := s.conn.WriteMessage(websocket.TextMessage, []byte(doneRequest))
	s.writeMu.Unlock()
	if err != nil {
		return s.best(), fmt.Errorf("voice: close audio stream: %w", err)
	}

	// The reader closes the channel when the server acknowledges. Waiting on
	// the context too means a server that stops answering costs the caller a
	// deadline rather than the turn.
	select {
	case <-s.closed:
		return s.best(), s.finalError()
	case <-ctx.Done():
		return s.best(), ctx.Err()
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
