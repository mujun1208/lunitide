package tts

import (
	"os"
	"testing"
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
