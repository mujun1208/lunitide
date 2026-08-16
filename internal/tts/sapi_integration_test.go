//go:build windows

// sapi_integration_test.go exercises the real SAPI engine on Windows
// machines (MC1 acceptance: RIFF/16kHz/mono/16-bit WAV synthesis and
// voice enumeration). Skipped under -short.
package tts

import (
	"encoding/base64"
	"math"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSapiIntegrationVoices(t *testing.T) {
	if testing.Short() {
		t.Skip("SAPI integration skipped in -short")
	}
	voices, err := NewPlatformEngine().Voices()
	if err != nil {
		t.Skipf("no SAPI on this machine: %v", err)
	}
	if len(voices) == 0 {
		t.Fatal("voices enumerated but empty")
	}
	for _, v := range voices {
		if v.VoiceID == "" || v.DisplayName == "" || v.Lang == "" {
			t.Fatalf("incomplete voice record: %+v", v)
		}
	}
	t.Logf("enumerated %d voices, first: %+v", len(voices), voices[0])
}

func TestSapiIntegrationSynthesizeWav(t *testing.T) {
	if testing.Short() {
		t.Skip("SAPI integration skipped in -short")
	}
	res, _, err := NewPlatformEngine().Synthesize(SynthesizeInput{
		Text: "月伴对话，本地语音合成自检。", Rate: DefaultRate, Volume: DefaultVolume,
	})
	if err != nil {
		t.Skipf("no SAPI on this machine: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(res.WavBase64)
	if err != nil {
		t.Fatalf("wav base64 invalid: %v", err)
	}
	if len(raw) <= wavHeaderBytes || string(raw[:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		t.Fatalf("not a RIFF/WAVE payload: %d bytes", len(raw))
	}
	// fmt chunk at offset 12 ("fmt "), channel count at offset 22 (mono=1),
	// sample rate at offset 24 (16000), bits per sample at offset 34 (16).
	if string(raw[12:16]) != "fmt " {
		t.Fatalf("missing fmt chunk: %q", raw[12:16])
	}
	channels := uint16(raw[22]) | uint16(raw[23])<<8
	rate := uint32(raw[24]) | uint32(raw[25])<<8 | uint32(raw[26])<<16 | uint32(raw[27])<<24
	bits := uint16(raw[34]) | uint16(raw[35])<<8
	if channels != 1 || rate != 16000 || bits != 16 {
		t.Fatalf("wav format = %dch/%dHz/%dbit, want 1ch/16000Hz/16bit", channels, rate, bits)
	}
	if res.DurationHint <= 0 {
		t.Fatalf("duration hint = %v", res.DurationHint)
	}
	// Windows temp dir must be clean of leftover synthesis artifacts.
	entries, _ := os.ReadDir(os.TempDir())
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "lunitide-tts-") {
			t.Fatalf("leftover temp artifact: %s", e.Name())
		}
	}
}

// TestSapiIntegrationP95Latency is the MC-03 real-machine acceptance:
// synthesizing a ~100-character segment must keep P95 latency <= 2s.
// It drives the raw platform engine directly (bypassing the Service
// dedup cache) so every sample pays the true SAPI cost.
func TestSapiIntegrationP95Latency(t *testing.T) {
	if testing.Short() {
		t.Skip("SAPI integration skipped in -short")
	}
	const (
		samples  = 20
		p95Limit = 2 * time.Second
	)
	text := strings.Repeat("月伴对话语音合成时延自检。", 10) // 120 chars ≈ 百字段
	if utf8.RuneCountInString(text) > MaxSegmentChars {
		t.Fatalf("probe text = %d runes, want <= %d", utf8.RuneCountInString(text), MaxSegmentChars)
	}
	engine := NewPlatformEngine()
	if _, err := engine.Voices(); err != nil {
		t.Skipf("no SAPI on this machine: %v", err)
	}
	// One warm-up round initializes the COM apartment and voice tokens
	// so cold-start cost does not pollute the P95 sample set.
	if _, _, err := engine.Synthesize(SynthesizeInput{Text: "预热", Rate: DefaultRate, Volume: DefaultVolume}); err != nil {
		t.Skipf("no SAPI on this machine: %v", err)
	}
	durations := make([]time.Duration, 0, samples)
	for i := 0; i < samples; i++ {
		start := time.Now()
		res, _, err := engine.Synthesize(SynthesizeInput{Text: text, Rate: DefaultRate, Volume: DefaultVolume})
		if err != nil {
			t.Fatalf("sample %d failed: %v", i, err)
		}
		if res.WavBase64 == "" {
			t.Fatalf("sample %d returned empty wav", i)
		}
		durations = append(durations, time.Since(start))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	// ceil(0.95 * 20) = 19 → the 19th smallest is the P95 sample.
	p95 := durations[int(math.Ceil(0.95*float64(samples)))-1]
	t.Logf("synthesis latency samples=%d min=%v median=%v p95=%v max=%v",
		samples, durations[0], durations[samples/2], p95, durations[samples-1])
	if p95 > p95Limit {
		t.Fatalf("P95 synthesis latency = %v, want <= %v", p95, p95Limit)
	}
}
