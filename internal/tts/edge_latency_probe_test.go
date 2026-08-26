package tts

import (
	"os"
	"testing"
	"time"
)

// TestEdgeSynthesisLatency measures the wait between asking for a sentence and
// holding its audio, cold and warm.
//
// The number a user feels is the gap between her reply appearing on screen and
// her starting to say it, and this is most of it. Kept as a probe rather than
// an assertion because it measures Microsoft's servers and the network in
// front of them, neither of which a test can hold to a threshold — the point
// is to be able to tell "the connection is being rebuilt every turn" from
// "the service is simply this slow today" without guessing.
func TestEdgeSynthesisLatency(t *testing.T) {
	if os.Getenv("LUNITIDE_EDGE_LIVE") == "" {
		t.Skip("set LUNITIDE_EDGE_LIVE=1 to measure against Microsoft Edge TTS")
	}
	eng := NewEdgeEngine()
	in := SynthesizeInput{
		Text:    "你好呀，我是月汐。",
		VoiceID: edgeVoiceStyleID(edgeDefaultVoice, "chat"),
		Volume:  88,
		Rate:    2,
	}

	// Four turns: the first pays for the dial, the rest should not.
	for turn := 1; turn <= 4; turn++ {
		started := time.Now()
		res, _, err := eng.Synthesize(in)
		elapsed := time.Since(started)
		if err != nil {
			// Not a failure. Being unable to reach the service is one of the
			// answers this probe exists to give — it is the difference
			// between a slow connection and no usable cloud voice at all,
			// and on a restricted network the second is the common case.
			t.Logf("turn %d: unreachable after %v: %v", turn, elapsed.Round(time.Millisecond), err)
			continue
		}
		t.Logf("turn %d: %v for %.2fs of audio (%d base64 bytes)",
			turn, elapsed.Round(time.Millisecond), res.DurationHint, len(res.WavBase64))
	}
}
