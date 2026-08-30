package volcsauc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/lunitide/lunitide/internal/voice"
)

// DialFunc opens a websocket. Tests inject a fake; production uses gorilla.
type DialFunc func(ctx context.Context, url string, header http.Header) (*websocket.Conn, *http.Response, error)

func defaultDial(ctx context.Context, url string, header http.Header) (*websocket.Conn, *http.Response, error) {
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = handshakeTimeout
	return dialer.DialContext(ctx, url, header)
}

// Backend recognizes speech through Volc SAUC. One instance per Start:
// credentials are copied in at construction so a secret lease can end.
type Backend struct {
	cfg Config
}

func New(cfg Config) *Backend { return &Backend{cfg: cfg} }

func (b *Backend) Name() string { return Name }

func (b *Backend) Ready(ctx context.Context) error {
	return Probe(ctx, b.cfg)
}

// Probe dials, sends the full client request, and closes. Used by provider.test.
func Probe(ctx context.Context, cfg Config) error {
	ctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	conn, err := dialPreferringOptimized(ctx, cfg)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := writeFullClient(conn, cfg, 1); err != nil {
		return err
	}
	_ = conn.SetReadDeadline(deadline(ctx, 800*time.Millisecond))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("%w: %v", voice.ErrBackendUnavailable, err)
	}
	frame, decErr := DecodeFrame(payload)
	if decErr != nil {
		return fmt.Errorf("%w: %v", voice.ErrBackendUnavailable, decErr)
	}
	if frame.Type == msgErrorServer {
		return &HandshakeError{Code: frame.Error, Message: strings.TrimSpace(string(frame.JSON) + string(frame.Raw))}
	}
	return nil
}

func (b *Backend) Start(ctx context.Context, opts voice.SessionOptions) (voice.Session, error) {
	ctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	conn, err := dialPreferringOptimized(ctx, b.cfg)
	if err != nil {
		return nil, err
	}
	if err := writeFullClient(conn, b.cfg, 1); err != nil {
		_ = conn.Close()
		return nil, err
	}
	sess := &session{
		conn:         conn,
		onTranscript: opts.OnTranscript,
		closed:       make(chan struct{}),
		seq:          1,
	}
	if err := sess.waitHandshake(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	go sess.readLoop()
	return sess, nil
}

func dialPreferringOptimized(ctx context.Context, cfg Config) (*websocket.Conn, error) {
	first, cancel := context.WithTimeout(ctx, firstDialBudget)
	conn, err := dial(first, cfg, true)
	cancel()
	if err == nil {
		return conn, nil
	}
	return dial(ctx, cfg, false)
}

func dial(ctx context.Context, cfg Config, optimized bool) (*websocket.Conn, error) {
	header := http.Header{}
	header.Set("X-Api-Resource-Id", cfg.resourceID())
	header.Set("X-Api-Connect-Id", newConnectID())
	if cfg.AppKey != "" {
		header.Set("X-Api-App-Key", cfg.AppKey)
		header.Set("X-Api-Access-Key", cfg.AccessKey)
	} else {
		header.Set("X-Api-Key", cfg.APIKey)
	}
	fn := cfg.Dial
	if fn == nil {
		if !AllowedSpeechHost(speechHost(cfg.BaseURL)) {
			return nil, fmt.Errorf("%w: unsupported volc speech host", voice.ErrBackendUnavailable)
		}
		fn = defaultDial
	}
	conn, resp, err := fn(ctx, StreamURL(cfg.BaseURL, optimized), header)
	if err != nil {
		status := 0
		body := ""
		if resp != nil {
			status = resp.StatusCode
			raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			_ = resp.Body.Close()
			body = strings.TrimSpace(string(raw))
		}
		msg := err.Error()
		if body != "" {
			msg = body
		}
		if LooksLikeLegacyASRResource(cfg.resourceID()) {
			msg = cfg.resourceID() + " " + msg
		}
		if status != 0 {
			return nil, &HandshakeError{Status: status, Message: msg}
		}
		return nil, fmt.Errorf("%w: %v", voice.ErrBackendUnavailable, err)
	}
	return conn, nil
}

func writeFullClient(conn *websocket.Conn, cfg Config, seq int32) error {
	body, err := json.Marshal(fullClientRequest(cfg))
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.BinaryMessage, EncodeFullClient(seq, body))
}

func fullClientRequest(cfg Config) map[string]any {
	return map[string]any{
		"user": map[string]any{"uid": cfg.uid()},
		"audio": map[string]any{
			"format":  "pcm",
			"codec":   "raw",
			"rate":    voice.SampleRate,
			"bits":    16,
			"channel": voice.Channels,
		},
		"request": map[string]any{
			"model_name":      DefaultModelName,
			"enable_itn":      true,
			"enable_punc":     true,
			"enable_ddc":      true,
			"show_utterances": true,
			"result_type":     "single",
			"end_window_size": cfg.endWindowMS(),
			"corpus": map[string]any{
				"context": strings.Join(DefaultHotwords, "\n"),
			},
		},
	}
}

func newConnectID() string {
	var b [connectIDBytes]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func deadline(ctx context.Context, fallback time.Duration) time.Time {
	if d, ok := ctx.Deadline(); ok {
		return d
	}
	return time.Now().Add(fallback)
}

type session struct {
	conn         *websocket.Conn
	onTranscript func(voice.Transcript)

	writeMu sync.Mutex
	seq     int32

	mu          sync.Mutex
	latest      string
	latestFinal bool
	pending     []byte
	err         error

	closeOnce sync.Once
	closed    chan struct{}
}

func (s *session) Append(_ context.Context, pcm []byte) error {
	if !voice.ValidFrame(pcm) {
		return fmt.Errorf("voice: frame of %d bytes is not whole 16-bit samples", len(pcm))
	}
	select {
	case <-s.closed:
		return s.finalError()
	default:
	}
	s.mu.Lock()
	s.pending = append(s.pending, pcm...)
	ready := len(s.pending) >= voice.FrameBytes*2
	var out []byte
	if ready {
		out = s.pending
		s.pending = nil
	}
	s.mu.Unlock()
	if len(out) == 0 {
		return nil
	}
	return s.writeAudio(out, false)
}

func (s *session) Finish(ctx context.Context) (string, error) {
	s.mu.Lock()
	tail := s.pending
	s.pending = nil
	s.mu.Unlock()
	if err := s.writeAudio(tail, true); err != nil && s.best() == "" {
		return "", err
	}
	timer := time.NewTimer(800 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-s.closed:
		return s.best(), s.finalError()
	case <-ctx.Done():
		return s.best(), ctx.Err()
	case <-timer.C:
		return s.best(), s.finalError()
	}
}

func (s *session) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

func (s *session) writeAudio(pcm []byte, last bool) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.seq++
	seq := s.seq
	if err := s.conn.WriteMessage(websocket.BinaryMessage, EncodeAudio(seq, pcm, last)); err != nil {
		return fmt.Errorf("voice: send audio: %w", err)
	}
	return nil
}

func (s *session) waitHandshake(ctx context.Context) error {
	deadline := time.Now().Add(handshakeTimeout)
	if d, ok := ctx.Deadline(); ok {
		deadline = d
	}
	if err := s.conn.SetReadDeadline(deadline); err != nil {
		return err
	}
	_, data, err := s.conn.ReadMessage()
	_ = s.conn.SetReadDeadline(time.Time{})
	if err != nil {
		return fmt.Errorf("%w: %v", voice.ErrBackendUnavailable, err)
	}
	frame, err := DecodeFrame(data)
	if err != nil {
		return fmt.Errorf("%w: %v", voice.ErrBackendUnavailable, err)
	}
	if frame.Type == msgErrorServer {
		return &HandshakeError{Code: frame.Error, Message: strings.TrimSpace(string(frame.JSON) + string(frame.Raw))}
	}
	s.applyFrame(frame)
	return nil
}

func (s *session) readLoop() {
	defer s.closeOnce.Do(func() { close(s.closed) })
	for {
		_, payload, err := s.conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				s.setError(fmt.Errorf("voice: read transcript: %w", err))
			}
			return
		}
		frame, decErr := DecodeFrame(payload)
		if decErr != nil {
			continue
		}
		if frame.Type == msgErrorServer {
			s.setError(&HandshakeError{Code: frame.Error, Message: strings.TrimSpace(string(frame.JSON) + string(frame.Raw))})
			return
		}
		s.applyFrame(frame)
	}
}

func (s *session) applyFrame(frame Frame) {
	text, final, ok := TranscriptFromJSON(frame.JSON)
	if !ok {
		return
	}
	s.mu.Lock()
	s.latest, s.latestFinal = text, final
	s.mu.Unlock()
	if s.onTranscript != nil {
		s.onTranscript(voice.Transcript{Text: text, Final: final})
	}
}

func (s *session) Latest() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	text := strings.TrimSpace(s.latest)
	final := s.latestFinal
	if final {
		s.latestFinal = false
		s.latest = ""
	}
	return text, final
}

func (s *session) best() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(s.latest)
}

func (s *session) setError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func (s *session) finalError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
