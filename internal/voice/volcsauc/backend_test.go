package volcsauc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/lunitide/lunitide/internal/voice"
)

func TestSessionPartialThenDefinite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "test-key" {
			http.Error(w, "missing key", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-Api-Resource-Id") != DefaultResourceID {
			http.Error(w, "bad resource", http.StatusForbidden)
			return
		}
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// full client
		if _, frame, err := conn.ReadMessage(); err != nil {
			return
		} else if decoded, _ := DecodeFrame(frame); decoded.Type != msgFullClient {
			t.Errorf("first packet type = %d", decoded.Type)
		}
		partial, _ := jsonResult("打开网", false)
		_ = conn.WriteMessage(websocket.BinaryMessage, partial)
		// audio
		_, _, _ = conn.ReadMessage()
		final, _ := jsonResult("打开网络。", true)
		_ = conn.WriteMessage(websocket.BinaryMessage, final)
		_, _, _ = conn.ReadMessage() // last
	}))
	t.Cleanup(server.Close)

	got := make(chan voice.Transcript, 4)
	b := New(Config{
		BaseURL:    "http://" + strings.TrimPrefix(server.URL, "http://"),
		APIKey:     "test-key",
		ResourceID: DefaultResourceID,
		Dial:       insecureWSDial(server),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sess, err := b.Start(ctx, voice.SessionOptions{OnTranscript: func(tr voice.Transcript) { got <- tr }})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	pcm := make([]byte, voice.FrameBytes)
	if err := sess.Append(ctx, pcm); err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(ctx, pcm); err != nil { // 200ms flush
		t.Fatal(err)
	}

	first := waitTranscript(t, got)
	if first.Text != "打开网" || first.Final {
		t.Fatalf("partial = %+v", first)
	}
	second := waitTranscript(t, got)
	if second.Text != "打开网络。" || !second.Final {
		t.Fatalf("final = %+v", second)
	}

	latest, ok := sess.(interface{ Latest() (string, bool) })
	if !ok {
		t.Fatal("session missing Latest")
	}
	text, final := latest.Latest()
	if text != "打开网络。" || !final {
		t.Fatalf("Latest = %q %v", text, final)
	}
	text, final = latest.Latest()
	if final || text != "" {
		t.Fatalf("consumed Latest must not re-serve the utterance: %q %v", text, final)
	}
}

func TestProbeRejectsForeignHost(t *testing.T) {
	err := Probe(context.Background(), Config{BaseURL: "https://evil.example", APIKey: "x"})
	if err == nil || !strings.Contains(err.Error(), "unsupported volc speech host") {
		t.Fatalf("err = %v", err)
	}
}

func TestSpeechHostRemapsAgentPlanTextOrigin(t *testing.T) {
	if got := speechHost("https://ark.cn-beijing.volces.com/api/plan/v3"); got != DefaultHost {
		t.Fatalf("speechHost = %s", got)
	}
	if !AllowedSpeechHost(speechHost("https://ark.cn-beijing.volces.com/api/plan/v3")) {
		t.Fatal("agent plan text origin must remap onto the speech host")
	}
}

func TestLiveAgentPlanHandshake(t *testing.T) {
	if os.Getenv("VOLC_SPEECH_LIVE") != "1" || strings.TrimSpace(os.Getenv("VOLC_SPEECH_API_KEY")) == "" {
		t.Skip("set VOLC_SPEECH_LIVE=1 and VOLC_SPEECH_API_KEY to probe Agent Plan ASR")
	}
	key := strings.TrimSpace(os.Getenv("VOLC_SPEECH_API_KEY"))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cfg := Config{BaseURL: "https://ark.cn-beijing.volces.com/api/plan/v3", APIKey: key, ResourceID: DefaultResourceID}
	err := Probe(ctx, cfg)
	if err == nil {
		return
	}
	payg := cfg
	payg.BaseURL = "https://openspeech.bytedance.com/api/v3/sauc/bigmodel_async"
	if paygErr := Probe(ctx, payg); paygErr == nil {
		t.Fatalf("agent-plan path failed (%s) but payg path worked; keep Agent Plan URL", SanitizeProbeError(err))
	}
	t.Fatalf("agent-plan handshake: %s", SanitizeProbeError(err))
}

func TestStartHonorsHandshakeTimeout(t *testing.T) {
	started := time.Now()
	b := New(Config{
		BaseURL: "https://" + DefaultHost,
		APIKey:  "x",
		Dial: func(ctx context.Context, _ string, _ http.Header) (*websocket.Conn, *http.Response, error) {
			<-ctx.Done()
			return nil, nil, ctx.Err()
		},
	})
	_, err := b.Start(context.Background(), voice.SessionOptions{})
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("expected handshake timeout")
	}
	if elapsed < time.Second || elapsed > handshakeTimeout+2*time.Second {
		t.Fatalf("elapsed = %s; want about %s", elapsed, handshakeTimeout)
	}
}

func TestProbeUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)
	err := Probe(context.Background(), Config{
		BaseURL: server.URL,
		APIKey:  "bad",
		Dial:    insecureWSDial(server),
	})
	he, ok := err.(*HandshakeError)
	if !ok || he.Status != http.StatusForbidden {
		t.Fatalf("err = %v", err)
	}
}

func jsonResult(text string, definite bool) ([]byte, error) {
	def := "false"
	if definite {
		def = "true"
	}
	body := []byte(`{"result":{"text":"` + text + `","utterances":[{"text":"` + text + `","definite":` + def + `}]}}`)
	// Reuse encoder: fake a server full response with seq 1.
	return EncodeFullClient(1, body), nil
}

func waitTranscript(t *testing.T, ch <-chan voice.Transcript) voice.Transcript {
	t.Helper()
	select {
	case tr := <-ch:
		return tr
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for transcript")
		return voice.Transcript{}
	}
}

func insecureWSDial(server *httptest.Server) DialFunc {
	return func(ctx context.Context, _ string, header http.Header) (*websocket.Conn, *http.Response, error) {
		url := "ws://" + strings.TrimPrefix(server.URL, "http://")
		return websocket.DefaultDialer.DialContext(ctx, url, header)
	}
}
