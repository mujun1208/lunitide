//go:build windows

// onecore_voice_test.go pins the OneCore vtable bridge on a real
// machine: binding two different natural voices must produce
// measurably different waveforms for the same text. When SetVoice
// silently fails every voice renders through the default engine and
// all audio is byte-similar — exactly the "every voice sounds the
// same" regression this test guards against.
package tts

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestOneCoreVoicesRenderDistinctAudio(t *testing.T) {
	voices := oneCoreVoices()
	if len(voices) < 2 {
		t.Skip("machine has fewer than 2 OneCore voices")
	}
	first, second := voices[0], voices[1]

	engine := NewPlatformEngine()
	render := func(v Voice) []byte {
		t.Helper()
		result, fallback, err := engine.Synthesize(SynthesizeInput{
			Text:    "月汐语音测试，上海今天多云。",
			VoiceID: v.VoiceID,
			Rate:    0,
			Volume:  90,
		})
		if err != nil {
			t.Fatalf("synthesize with %s: %v", v.DisplayName, err)
		}
		if fallback {
			t.Fatalf("%s fell back to the default voice (SetVoice failed)", v.DisplayName)
		}
		decoded, err := decodeWav(result.WavBase64)
		if err != nil {
			t.Fatalf("decode wav: %v", err)
		}
		return decoded
	}

	a, b := render(first), render(second)
	if len(a) < wavHeaderBytes+3200 || len(b) < wavHeaderBytes+3200 {
		t.Fatalf("suspiciously short output: %d / %d bytes", len(a), len(b))
	}
	// Compare PCM payloads after the 44-byte header; drop the first
	// 100ms (attack transients are near-identical) so a genuine voice
	// difference dominates the diff.
	pcmA, pcmB := a[wavHeaderBytes+3200:], b[wavHeaderBytes+3200:]
	if len(pcmA) != len(pcmB) {
		t.Logf("different lengths %d vs %d — already distinct voices", len(pcmA), len(pcmB))
		return
	}
	diff := 0
	for i := 0; i < len(pcmA); i++ {
		if pcmA[i] != pcmB[i] {
			diff++
		}
	}
	if diff < len(pcmA)/100 {
		t.Fatalf("%s and %s produced near-identical audio (%d/%d differing bytes) — SetVoice did not take effect", first.DisplayName, second.DisplayName, diff, len(pcmA))
	}
}

func decodeWav(b64 string) ([]byte, error) {
	out, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	if len(out) < 4 || !bytes.Equal(out[:4], []byte("RIFF")) {
		return nil, errInvalidWav
	}
	return out, nil
}

var errInvalidWav = &testError{"invalid wav output"}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
