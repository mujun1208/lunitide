//go:build windows

package voice

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// End to end against the real thing: the catalogue's own download, the real
// sidecar process, and a recording of someone actually talking.
//
// Everything else in this package runs against fakes. That is the right trade
// for testing protocol and lifecycle, but it cannot answer the only question
// that matters to a user — does it hear them — because a fake recognizer
// returns whatever the test told it to.
//
// Gated because it moves a quarter of a gigabyte and starts a subprocess:
//
//	LUNITIDE_VOICE_E2E=1 go test ./internal/voice/ -run E2E -v -timeout 40m
//
// Point LUNITIDE_VOICE_E2E_ROOT at a directory to keep the download between
// runs. Without it every run pays for the model again.
func requireE2E(t *testing.T) {
	t.Helper()
	if os.Getenv("LUNITIDE_VOICE_E2E") != "1" {
		t.Skip("set LUNITIDE_VOICE_E2E=1 to run against the real recognizer")
	}
}

func e2eRoot(t *testing.T) string {
	t.Helper()
	if root := os.Getenv("LUNITIDE_VOICE_E2E_ROOT"); root != "" {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("prepare e2e root: %v", err)
		}
		return root
	}
	return t.TempDir()
}

// speech is a recording published alongside the model, so its content is
// known to be within what the model was released to handle. A sample of our
// own choosing would confound "the pipeline is broken" with "this clip is
// hard", and only one of those is worth failing a build over.
func clipURLs(clip string) []string {
	path := "/csukuangfj/" + paraformerRepo + "/resolve/" + paraformerRevision + "/test_wavs/" + clip
	return []string{"https://huggingface.co" + path, "https://hf-mirror.com" + path}
}

func TestE2ELocalRecognitionHearsRealSpeech(t *testing.T) {
	requireE2E(t)
	root := e2eRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
	defer cancel()

	installer := &Installer{Root: root}
	for _, bundle := range []Bundle{Runtime(), mustBundle(t, DefaultModel)} {
		if installer.Installed(bundle) {
			t.Logf("%s already installed", bundle.ID)
			continue
		}
		last := -1
		start := time.Now()
		if err := installer.Install(ctx, bundle, func(p Progress) {
			if pct := p.Percent(); pct/10 != last/10 {
				last = pct
				t.Logf("%s %d%% (%s)", p.BundleID, pct, time.Since(start).Round(time.Second))
			}
		}); err != nil {
			t.Fatalf("install %s: %v", bundle.ID, err)
		}
		t.Logf("%s installed in %s", bundle.ID, time.Since(start).Round(time.Second))
	}

	pcm := fetchClip(ctx, t, root, "0.wav")
	t.Logf("speech: %d samples, %.1fs", len(pcm)/BytesPerSample, float64(len(pcm)/BytesPerSample)/SampleRate)

	backend := &SherpaBackend{Root: root, Startup: 5 * time.Minute}
	if err := backend.Ready(ctx); err != nil {
		t.Fatalf("backend not ready after install: %v", err)
	}
	defer backend.Shutdown()

	started := time.Now()
	text, gotPartials, partial := runClip(ctx, t, backend, pcm, 0)
	t.Logf("recognized in %s", time.Since(started).Round(time.Second))

	t.Logf("partials: %d, last partial: %q", gotPartials, partial)
	t.Logf("FINAL TRANSCRIPT: %q", text)

	if strings.TrimSpace(text) == "" {
		t.Fatal("recognizer returned nothing for a clip of clear speech")
	}
	if gotPartials == 0 {
		t.Error("no partial arrived; the caption would stay empty until the user stopped talking")
	}
	// The clip is a bilingual sentence about days of the week. Asserting the
	// exact string would fail on a model update that is otherwise an
	// improvement, so this asks only that the recognizer produced Han
	// characters rather than, say, an empty string or a row of punctuation.
	if !containsHan(text) {
		t.Errorf("transcript has no Chinese characters: %q", text)
	}
}

// runClip feeds one recording through a session and returns what came back.
// padMillis appends that much digital silence after the speech.
func runClip(ctx context.Context, t *testing.T, backend *SherpaBackend, pcm []byte, padMillis int) (text string, partials int, lastPartial string) {
	t.Helper()
	var mu sync.Mutex
	session, err := backend.Start(ctx, SessionOptions{
		Language: "zh-CN",
		OnTranscript: func(tr Transcript) {
			mu.Lock()
			defer mu.Unlock()
			partials++
			if tr.Text != "" {
				lastPartial = tr.Text
			}
		},
	})
	if err != nil {
		t.Fatalf("start session: %v", err)
	}
	defer session.Close()

	feed := pcm
	if padMillis > 0 {
		feed = append(append([]byte(nil), pcm...), make([]byte, SampleRate*BytesPerSample*padMillis/1000)...)
	}
	for offset := 0; offset < len(feed); offset += FrameBytes {
		end := min(offset+FrameBytes, len(feed))
		if err := session.Append(ctx, feed[offset:end]); err != nil {
			t.Fatalf("append at %d: %v", offset, err)
		}
		// Faster than real time, but not unbounded: a streaming recognizer
		// handed its whole input at once is not being asked the question
		// these tests exist to ask.
		time.Sleep(10 * time.Millisecond)
	}
	text, err = session.Finish(ctx)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	return text, partials, lastPartial
}

// Accuracy on plain Mandarin, which is the only accuracy this product needs.
//
// The bilingual clip the test above uses is a deliberate stress case: it
// switches language mid-sentence twice, and every way of running this model
// mangles the same stretch of it — sherpa's own CLI and the transcript
// recorded in sherpa's own documentation included, each in a different place.
// Grading the recognizer on it measures the clip, not the recognizer.
//
// These are ordinary sentences. If they come back wrong, the companion cannot
// hold a conversation and no amount of latency work will hide it.
func TestE2EMandarinClipsAreRecognized(t *testing.T) {
	requireE2E(t)
	root := e2eRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	backend := &SherpaBackend{Root: root, ModelID: DefaultModel, Startup: 5 * time.Minute}
	if err := backend.Ready(ctx); err != nil {
		t.Skipf("model not installed; run TestE2ELocalRecognitionHearsRealSpeech first: %v", err)
	}
	defer backend.Shutdown()

	// Anchors rather than whole transcripts. The model revision is pinned, so
	// the output is stable enough to assert on, but a whole-string match would
	// also pin the English technical chatter in these recordings — which the
	// model does get wrong, and which says nothing about whether it can follow
	// a conversation in Chinese.
	for _, clip := range []struct {
		name  string
		wants []string
	}{
		{"1.wav", []string{"第一种", "第二种"}},
		{"2.wav", []string{"频繁", "frequently"}},
		{"3.wav", []string{"第一句", "什么时态"}},
	} {
		pcm := fetchClip(ctx, t, root, clip.name)
		text, partials, _ := runClip(ctx, t, backend, pcm, 0)
		t.Logf("%s (%.1fs, %d partials) -> %q", clip.name, float64(len(pcm)/BytesPerSample)/SampleRate, partials, text)
		if partials == 0 {
			t.Errorf("%s produced no partial; a caption would stay blank until the speaker stopped", clip.name)
		}
		for _, want := range clip.wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s: transcript is missing %q: %q", clip.name, want, text)
			}
		}
	}
}

func mustBundle(t *testing.T, id string) Bundle {
	t.Helper()
	bundle, err := LookupBundle(id)
	if err != nil {
		t.Fatalf("lookup %s: %v", id, err)
	}
	return bundle
}

func containsHan(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// fetchClip downloads one recording once and returns its PCM payload.
func fetchClip(ctx context.Context, t *testing.T, root, clip string) []byte {
	t.Helper()
	cache := filepath.Join(root, "e2e-"+clip)
	if raw, err := os.ReadFile(cache); err == nil {
		pcm, err := wavPCM(raw)
		if err == nil {
			return pcm
		}
		t.Logf("cached clip unusable (%v); downloading again", err)
	}
	var lastErr error
	// The same three rounds the installer takes. huggingface.co refuses a TLS
	// handshake often enough from here that one attempt per host is not a
	// fair test of whether the clip is reachable, and a test that fails on
	// someone's flaky afternoon teaches nothing.
	for round := range 3 {
		if round > 0 {
			time.Sleep(time.Duration(round) * 2 * time.Second)
		}
		for _, source := range clipURLs(clip) {
			raw, err := getAll(ctx, source)
			if err != nil {
				lastErr = err
				continue
			}
			pcm, err := wavPCM(raw)
			if err != nil {
				lastErr = err
				continue
			}
			if err := os.WriteFile(cache, raw, 0o644); err != nil {
				t.Logf("cache clip: %v", err)
			}
			return pcm
		}
	}
	t.Fatalf("fetch %s: %v", clip, lastErr)
	return nil
}

func getAll(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// Reuses the installer's client so the clip travels the same proxy the
	// model does; a test that reaches the network by a different route is
	// testing a route no user takes.
	resp, err := (&Installer{}).client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

var errNotPCM = errors.New("not 16-bit mono PCM at 16 kHz")

// wavPCM extracts the sample payload from a RIFF/WAVE file, rejecting
// anything that is not the format the recognizer is fed at runtime.
func wavPCM(raw []byte) ([]byte, error) {
	if len(raw) < 12 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, errors.New("not a RIFF/WAVE file")
	}
	var sawFormat bool
	for offset := 12; offset+8 <= len(raw); {
		id := string(raw[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(raw[offset+4 : offset+8]))
		body := offset + 8
		if size < 0 || body+size > len(raw) {
			return nil, errors.New("truncated chunk")
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, errors.New("short format chunk")
			}
			format := binary.LittleEndian.Uint16(raw[body : body+2])
			channels := binary.LittleEndian.Uint16(raw[body+2 : body+4])
			rate := binary.LittleEndian.Uint32(raw[body+4 : body+8])
			bits := binary.LittleEndian.Uint16(raw[body+14 : body+16])
			if format != 1 || channels != Channels || rate != SampleRate || bits != 8*BytesPerSample {
				return nil, fmt.Errorf("%w: format=%d channels=%d rate=%d bits=%d", errNotPCM, format, channels, rate, bits)
			}
			sawFormat = true
		case "data":
			if !sawFormat {
				return nil, errors.New("data chunk before format chunk")
			}
			return raw[body : body+size], nil
		}
		offset = body + size + size%2
	}
	return nil, errors.New("no data chunk")
}
