// refaudio_test.go exercises the reference-timbre engine against a fake
// GPT-SoVITS api_v2 server plus the directory browser and WAV duration
// parsing.
package tts

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// wavFixture builds a minimal WAV whose data chunk is size bytes at
// byteRate bytes/second.
func wavFixture(byteRate, dataSize int) []byte {
	out := make([]byte, 44+dataSize)
	copy(out, "RIFF")
	binary.LittleEndian.PutUint32(out[4:], uint32(36+dataSize))
	copy(out[8:], "WAVE")
	copy(out[12:], "fmt ")
	binary.LittleEndian.PutUint32(out[16:], 16)
	binary.LittleEndian.PutUint16(out[20:], 1)  // PCM
	binary.LittleEndian.PutUint16(out[22:], 1)  // mono
	binary.LittleEndian.PutUint32(out[24:], uint32(byteRate))
	binary.LittleEndian.PutUint32(out[28:], uint32(byteRate))
	binary.LittleEndian.PutUint16(out[32:], 2)
	binary.LittleEndian.PutUint16(out[34:], 16)
	copy(out[36:], "data")
	binary.LittleEndian.PutUint32(out[40:], uint32(dataSize))
	return out
}

func TestRefSynthesizeViaLocalService(t *testing.T) {
	var gotPath, gotPrompt, gotText string
	wav := wavFixture(32000, 32000) // exactly 1s
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tts" {
			t.Errorf("request path = %s, want /tts", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		gotText, _ = body["text"].(string)
		gotPath, _ = body["ref_audio_path"].(string)
		gotPrompt, _ = body["prompt_text"].(string)
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wav)
	}))
	defer srv.Close()

	ref := filepath.Join(t.TempDir(), "reference.wav")
	if err := os.WriteFile(ref, wavFixture(16000, 1600), 0o600); err != nil {
		t.Fatal(err)
	}

	engine := NewRefEngine()
	res, fallback, err := engine.Synthesize(SynthesizeInput{
		Text: "你好月汐", Engine: EngineRef,
		RefEndpoint: srv.URL, RefWavPath: ref, RefPromptText: "参考台词",
	})
	if err != nil || fallback {
		t.Fatalf("synthesize err=%v fallback=%v", err, fallback)
	}
	if gotText != "你好月汐" || gotPath != ref || gotPrompt != "参考台词" {
		t.Fatalf("service payload text=%q path=%q prompt=%q", gotText, gotPath, gotPrompt)
	}
	if !strings.HasPrefix(res.WavBase64, "UklGR") {
		t.Fatalf("result is not RIFF base64")
	}
	if res.DurationHint < 0.99 || res.DurationHint > 1.01 {
		t.Fatalf("duration hint = %v, want ~1s", res.DurationHint)
	}
}

func TestRefSynthesizeValidation(t *testing.T) {
	engine := NewRefEngine()
	cases := []struct {
		name string
		in   SynthesizeInput
	}{
		{"bad endpoint", SynthesizeInput{Text: "段", Engine: EngineRef, RefEndpoint: "ftp://x", RefWavPath: "x.wav"}},
		{"no reference", SynthesizeInput{Text: "段", Engine: EngineRef, RefEndpoint: "http://127.0.0.1:9880"}},
		{"missing file", SynthesizeInput{Text: "段", Engine: EngineRef, RefEndpoint: "http://127.0.0.1:9880", RefWavPath: filepath.Join(t.TempDir(), "nope.wav")}},
	}
	for _, tc := range cases {
		if _, _, err := engine.Synthesize(tc.in); !errors.Is(err, ErrSynthesisFailed) {
			t.Fatalf("%s: err = %v, want ErrSynthesisFailed", tc.name, err)
		}
	}
}

func TestRefSynthesizeServiceError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	engine := NewRefEngine()
	_, _, err := engine.Synthesize(SynthesizeInput{Text: "段", Engine: EngineRef, RefEndpoint: srv.URL, RefWavPath: "any.wav"})
	if !errors.Is(err, ErrSynthesisFailed) {
		t.Fatalf("err = %v, want ErrSynthesisFailed", err)
	}
}

func TestWavDurationWithListChunk(t *testing.T) {
	wav := wavFixture(24000, 24000)
	// Insert a LIST chunk between fmt and data: chunks must be walked.
	list := make([]byte, 0, 12+4)
	list = append(list, "LIST"...)
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, 4)
	list = append(list, size...)
	list = append(list, "INFO"...)
	withList := append(append(append([]byte{}, wav[:36]...), list...), wav[36:]...)
	if got := wavDurationSeconds(withList); got < 0.99 || got > 1.01 {
		t.Fatalf("duration = %v, want ~1s", got)
	}
	if wavDurationSeconds([]byte("not a wav")) != 0 {
		t.Fatalf("garbage input must yield 0")
	}
}

func TestListRefAudioEntries(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "配音集"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.wav", "B.MP3", "c.flac", "readme.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	clean, entries, err := ListRefAudioEntries(dir)
	if err != nil {
		t.Fatal(err)
	}
	if clean != filepath.Clean(dir) {
		t.Fatalf("clean = %s", clean)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	joined := strings.Join(names, "|")
	if !strings.Contains(joined, "..") || !strings.Contains(joined, "配音集/") {
		t.Fatalf("directory entries missing: %v", names)
	}
	if strings.Contains(joined, "readme.txt") {
		t.Fatalf("non-audio file leaked into listing: %v", names)
	}
	if !strings.Contains(joined, "a.wav") || !strings.Contains(joined, "B.MP3") || !strings.Contains(joined, "c.flac") {
		t.Fatalf("audio files missing: %v", names)
	}
	if _, _, err := ListRefAudioEntries(filepath.Join(dir, "does-not-exist")); !os.IsNotExist(err) {
		t.Fatalf("missing dir err = %v, want IsNotExist", err)
	}
}
