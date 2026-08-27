package omni

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionPrefillDecodeReturnsTextAndWav(t *testing.T) {
	var lastPrefill map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/v1/stream/omni_init":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true}`))
		case "/v1/stream/prefill":
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &lastPrefill)
			w.WriteHeader(http.StatusOK)
		case "/v1/stream/decode":
			var body struct {
				DebugDir string `json:"debug_dir"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			if body.DebugDir != "" {
				wavDir := filepath.Join(body.DebugDir, "round_000", "tts_wav")
				_ = os.MkdirAll(wavDir, 0o755)
				_ = os.WriteFile(filepath.Join(wavDir, "wav_0.wav"), []byte("RIFF"), 0o644)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"content\":\"你好\",\"is_listen\":false,\"stop\":false}\n\n")
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
		case "/v1/stream/break":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	host := NewHost(t.TempDir())
	host.Endpoint = srv.URL
	host.HTTP = srv.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := OpenSession(ctx, host, "refpack:missing.wav")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)

	turn, err := session.Append(ctx, make([]byte, ChunkBytes))
	if err != nil {
		t.Fatal(err)
	}
	if turn.Text != "你好" {
		t.Fatalf("text = %q", turn.Text)
	}
	if len(turn.WAVs) != 1 {
		t.Fatalf("wavs = %d", len(turn.WAVs))
	}
	if lastPrefill["cnt"] != float64(1) {
		t.Fatalf("first prefill cnt = %v; omni_init owns 0", lastPrefill["cnt"])
	}
	audio, _ := lastPrefill["audio_path_prefix"].(string)
	if !strings.HasSuffix(audio, ".wav") {
		t.Fatalf("audio_path_prefix = %v", lastPrefill["audio_path_prefix"])
	}
}

func TestHostEnsureWithoutModelReportsMissing(t *testing.T) {
	host := NewHost(t.TempDir())
	host.Finder = func() string { return "" }
	state, err := host.Ensure()
	if state != HostMissingModel || err == nil {
		t.Fatalf("state=%s err=%v", state, err)
	}
}

func TestSpawnLockedRejectsNonLoopback(t *testing.T) {
	host := NewHost(t.TempDir())
	host.Listen = "0.0.0.0:19080"
	host.mu.Lock()
	defer host.mu.Unlock()
	if err := host.spawnLocked("llama-omni-server"); err == nil {
		t.Fatal("expected non-loopback listen to fail")
	}
}
