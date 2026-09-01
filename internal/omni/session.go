package omni

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/tts"
)

// Session is one duplex conversation against llama-omni-server.
//
// Capture PCM arrives from the renderer; this side writes 1s WAV chunks,
// calls prefill/decode, and returns new TTS WAVs as base64.
type Session struct {
	host   *Host
	dir    string
	voice  string
	client *http.Client

	mu       sync.Mutex
	cnt      int
	pcm      []byte
	played   map[string]struct{}
	closed   bool
	ready    bool
	readyErr error
}

// Turn is one append result.
type Turn struct {
	Text      string
	Listening bool
	WAVs      []string
}

// OpenSession creates a temp dir and runs omni_init with the clone reference.
func OpenSession(ctx context.Context, host *Host, personaID string) (*Session, error) {
	dir, err := os.MkdirTemp(host.Root, "session-*")
	if err != nil {
		return nil, err
	}
	img := filepath.Join(dir, "blank.png")
	if err := writeBlankPNG(img); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	voicePath := ""
	if resolved, ok := tts.RefResolveVoice(personaID, ""); ok {
		if _, err := os.Stat(resolved); err == nil {
			voicePath = resolved
		}
	}
	s := &Session{
		host:   host,
		dir:    dir,
		voice:  voicePath,
		client: &http.Client{Timeout: 60 * time.Second},
		cnt:    1,
		played: map[string]struct{}{},
	}
	// omni_init can take 10–60s. The renderer deadline is 30s, so this
	// must not block OpenSession — Append returns empty turns until ready.
	go func() {
		err := s.init(context.Background())
		s.mu.Lock()
		if err != nil {
			s.readyErr = err
		} else {
			s.ready = true
		}
		s.mu.Unlock()
		if err != nil {
			s.Close()
		}
	}()
	return s, nil
}

// Ready is true after omni_init has succeeded.
func (s *Session) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready
}

// InitErr is the omni_init failure, if any.
func (s *Session) InitErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readyErr
}

func (s *Session) init(ctx context.Context) error {
	body := map[string]any{
		"media_type":       2,
		"use_tts":          true,
		"duplex_mode":      true,
		"model_dir":        s.host.ModelDir(),
		"tts_bin_dir":      filepath.Join(s.host.ModelDir(), "tts"),
		"tts_gpu_layers":   100,
		"token2wav_device": "gpu:0",
		"output_dir":       filepath.Join(s.dir, "out"),
	}
	if s.voice != "" {
		body["voice_audio"] = s.voice
	}
	if err := os.MkdirAll(filepath.Join(s.dir, "out"), 0o755); err != nil {
		return err
	}
	resp, err := s.postJSON(ctx, "/v1/stream/omni_init", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("omni_init: HTTP %d %s", resp.StatusCode, bytes.TrimSpace(raw))
	}
	return nil
}

// Append buffers 16 kHz PCM and, once a 1s chunk is full, prefills and decodes.
func (s *Session) Append(ctx context.Context, pcm []byte) (Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Turn{}, fmt.Errorf("omni: session closed")
	}
	s.pcm = append(s.pcm, pcm...)
	if !s.ready {
		if s.readyErr != nil {
			return Turn{}, s.readyErr
		}
		return Turn{}, nil
	}
	var text strings.Builder
	listening := false
	var wavs []string
	for len(s.pcm) >= ChunkBytes {
		chunk := s.pcm[:ChunkBytes]
		s.pcm = s.pcm[ChunkBytes:]
		turn, err := s.round(ctx, chunk)
		if err != nil {
			return Turn{}, err
		}
		text.WriteString(turn.Text)
		listening = listening || turn.Listening
		wavs = append(wavs, turn.WAVs...)
	}
	return Turn{Text: text.String(), Listening: listening, WAVs: wavs}, nil
}

func (s *Session) round(ctx context.Context, pcm []byte) (Turn, error) {
	wavPath := filepath.Join(s.dir, fmt.Sprintf("in_%d.wav", s.cnt))
	if err := WritePCM16MonoWAV(wavPath, SampleRate, pcm); err != nil {
		return Turn{}, err
	}
	prefill := map[string]any{
		"audio_path_prefix": wavPath,
		"img_path_prefix":   filepath.Join(s.dir, "blank.png"),
		"cnt":               s.cnt,
	}
	resp, err := s.postJSON(ctx, "/v1/stream/prefill", prefill)
	if err != nil {
		return Turn{}, err
	}
	// Drain the (bounded) body so the connection can be reused; a read error
	// here only costs that reuse, so it is deliberately ignored.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return Turn{}, fmt.Errorf("prefill: HTTP %d", resp.StatusCode)
	}
	s.cnt++

	decodeBody := map[string]any{
		"debug_dir": filepath.Join(s.dir, "out"),
		"stream":    true,
	}
	dresp, err := s.postJSON(ctx, "/v1/stream/decode", decodeBody)
	if err != nil {
		return Turn{}, err
	}
	defer dresp.Body.Close()
	if dresp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(dresp.Body, 2048))
		return Turn{}, fmt.Errorf("decode: HTTP %d %s", dresp.StatusCode, bytes.TrimSpace(raw))
	}
	text, listening, err := readSSE(dresp.Body)
	if err != nil {
		return Turn{}, err
	}
	wavs, err := s.collectWAVs()
	if err != nil {
		return Turn{}, err
	}
	return Turn{Text: text, Listening: listening, WAVs: wavs}, nil
}

func readSSE(r io.Reader) (string, bool, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var text strings.Builder
	listening := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var ev struct {
			Content  string `json:"content"`
			IsListen bool   `json:"is_listen"`
			Stop     bool   `json:"stop"`
		}
		if json.Unmarshal([]byte(payload), &ev) != nil {
			continue
		}
		text.WriteString(ev.Content)
		if ev.IsListen {
			listening = true
		}
	}
	return text.String(), listening, sc.Err()
}

func (s *Session) collectWAVs() ([]string, error) {
	root := filepath.Join(s.dir, "out")
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".wav") {
			return nil
		}
		if _, seen := s.played[path]; seen {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil || len(raw) == 0 {
			return nil
		}
		s.played[path] = struct{}{}
		out = append(out, base64.StdEncoding.EncodeToString(raw))
		return nil
	})
	return out, err
}

func (s *Session) postJSON(ctx context.Context, path string, body any) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.host.baseURL()+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return s.client.Do(req)
}

// Close drops the temp dir. Safe twice.
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	dir := s.dir
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := s.postJSON(ctx, "/v1/stream/break", map[string]any{})
	if err == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
	}
	_ = os.RemoveAll(dir)
}
