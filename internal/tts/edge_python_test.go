package tts

import (
	"context"
	"os"
	"testing"
)

func TestEdgePythonSynthLive(t *testing.T) {
	if os.Getenv("LUNITIDE_EDGE_LIVE") == "" {
		t.Skip("set LUNITIDE_EDGE_LIVE=1")
	}
	if edgeFindPython() == "" {
		t.Fatal("edge_tts python helper not found")
	}
	res, _, err := edgeSynthesizePython(context.Background(), SynthesizeInput{
		Text:    "你好，我是月汐",
		VoiceID: "zh-CN-XiaoxiaoNeural",
		Style:   "chat",
		Rate:    0,
		Volume:  100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.WavBase64) < 100 {
		t.Fatalf("short audio %d", len(res.WavBase64))
	}
}
