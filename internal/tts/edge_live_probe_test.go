package tts

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestEdgeLiveSynthesis(t *testing.T) {
	if os.Getenv("LUNITIDE_EDGE_LIVE") == "" {
		t.Skip("set LUNITIDE_EDGE_LIVE=1 to hit Microsoft Edge TTS")
	}
	eng := NewEdgeEngine()
	voices, err := eng.Voices()
	if err != nil {
		t.Fatalf("voices: %v", err)
	}
	if len(voices) == 0 {
		t.Fatal("no voices")
	}
	res, _, err := eng.Synthesize(SynthesizeInput{Text: "你好，我是月汐", VoiceID: "zh-CN-YunxiNeural", Rate: 0, Volume: 100})
	if err != nil {
		t.Fatalf("synth: %v", err)
	}
	if len(res.WavBase64) < 100 {
		t.Fatalf("short wav: %d", len(res.WavBase64))
	}
}

func TestEdgeWebSocketLive(t *testing.T) {
	if os.Getenv("LUNITIDE_EDGE_LIVE") == "" {
		t.Skip("set LUNITIDE_EDGE_LIVE=1 to hit Microsoft Edge TTS")
	}
	targets := []struct {
		host string
		path string
	}{
		{edgeSynthHost, edgeSynthPath},
		{edgeSynthHostAlt, edgeSynthPathAlt},
	}
	var failures []error
	for _, target := range targets {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		started := time.Now()
		conn, err := dialEdgeWS(ctx, target.host, target.path, edgeSecMSGEC(time.Now()))
		if err != nil {
			cancel()
			t.Logf("%s%s handshake failed after %v: %v", target.host, target.path, time.Since(started).Round(time.Millisecond), err)
			failures = append(failures, err)
			continue
		}
		edgeConn := &edgeConn{conn: conn, host: target.host, path: target.path, createdAt: time.Now()}
		firstAudio := time.Duration(0)
		chunks := 0
		_, _, err = edgeSynthesizeTurn(ctx, edgeConn, SynthesizeInput{
			Text: "你好，我是月汐。", VoiceID: edgeDefaultVoice, Volume: 88,
		}, 0, func(chunk []byte) error {
			if chunks == 0 {
				firstAudio = time.Since(started)
			}
			chunks++
			return nil
		})
		conn.Close()
		cancel()
		if err == nil {
			t.Logf("%s%s first audio=%v chunks=%d", target.host, target.path, firstAudio.Round(time.Millisecond), chunks)
			return
		}
		t.Logf("%s%s stream failed after %v: %v", target.host, target.path, time.Since(started).Round(time.Millisecond), err)
		failures = append(failures, err)
	}
	t.Fatalf("all Edge WebSocket targets failed: %v", failures)
}
