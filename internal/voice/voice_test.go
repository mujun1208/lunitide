package voice

import (
	"errors"
	"strings"
	"testing"
)

func TestFrameBytesMatchesTheCaptureFormat(t *testing.T) {
	// 16 kHz mono int16 for 100ms. Written out rather than recomputed from
	// the same constants, so a change to any of them fails here and gets
	// compared against pcmFrames.ts on the renderer side.
	if FrameBytes != 3200 {
		t.Fatalf("FrameBytes = %d; the renderer sends 3200-byte frames", FrameBytes)
	}
}

func TestValidFrame(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
		want bool
	}{
		{"one frame", FrameBytes, true},
		{"a short tail", 640, true},
		{"one sample", BytesPerSample, true},
		{"empty", 0, false},
		{"half a sample", 3, false},
		{"ten frames", FrameBytes * 10, true},
		{"more than ten frames", FrameBytes*10 + BytesPerSample, false},
	} {
		if got := ValidFrame(make([]byte, tc.size)); got != tc.want {
			t.Errorf("ValidFrame(%s, %d bytes) = %v; want %v", tc.name, tc.size, got, tc.want)
		}
	}
}

func TestFrameDurationMillis(t *testing.T) {
	if got := FrameDurationMillis(make([]byte, FrameBytes)); got != FrameMillis {
		t.Errorf("a full frame reported %dms; want %d", got, FrameMillis)
	}
	if got := FrameDurationMillis(make([]byte, FrameBytes/2)); got != FrameMillis/2 {
		t.Errorf("half a frame reported %dms; want %d", got, FrameMillis/2)
	}
	if got := FrameDurationMillis(nil); got != 0 {
		t.Errorf("no audio reported %dms; want 0", got)
	}
}

func TestCatalogueBundlesAreWellFormed(t *testing.T) {
	bundles := append([]Bundle{Runtime()}, Models()...)
	seen := map[string]bool{}
	for _, b := range bundles {
		if b.ID == "" || b.Title == "" {
			t.Errorf("bundle %+v is missing an ID or title", b)
		}
		if seen[b.ID] {
			t.Errorf("duplicate bundle ID %q", b.ID)
		}
		seen[b.ID] = true
		if len(b.Downloads) == 0 {
			t.Errorf("bundle %q has nothing to download", b.ID)
		}
		for _, d := range b.Downloads {
			if len(d.SHA256) != 64 || strings.ToLower(d.SHA256) != d.SHA256 {
				t.Errorf("bundle %q file %q: digest %q is not a lowercase hex sha256", b.ID, d.Path, d.SHA256)
			}
			if d.Bytes <= 0 {
				t.Errorf("bundle %q file %q has no expected size", b.ID, d.Path)
			}
			if len(d.URLs) == 0 {
				t.Errorf("bundle %q file %q has no source", b.ID, d.Path)
			}
			for _, source := range d.URLs {
				// Immutability is the whole reason the digests mean
				// anything: a branch name would let the bytes move
				// underneath them.
				if !strings.HasPrefix(source, "https://") {
					t.Errorf("bundle %q file %q is not fetched over https: %s", b.ID, d.Path, source)
				}
				if strings.Contains(source, "/resolve/main/") || strings.Contains(source, "/raw/main/") {
					t.Errorf("bundle %q file %q points at a branch instead of a pinned revision: %s", b.ID, d.Path, source)
				}
			}
		}
	}
}

func TestArchitectureIsDerivedFromTheBundle(t *testing.T) {
	if got, err := Architecture(ModelParaformerZhEn); err != nil || got != ArchParaformer {
		t.Errorf("paraformer bundle reported %q (%v)", got, err)
	}
	if got, err := Architecture(ModelZipformerZh14M); err != nil || got != ArchTransducer {
		t.Errorf("zipformer bundle reported %q (%v)", got, err)
	}
	if _, err := Architecture("something-else"); !errors.Is(err, ErrUnknownBundle) {
		t.Errorf("an unknown bundle should report ErrUnknownBundle, got %v", err)
	}
}

func TestLookupBundleCoversTheRuntimeAndEveryModel(t *testing.T) {
	for _, id := range []string{RuntimeSherpa, ModelParaformerZhEn, ModelZipformerZh14M} {
		b, err := LookupBundle(id)
		if err != nil || b.ID != id {
			t.Errorf("LookupBundle(%q) = %q, %v", id, b.ID, err)
		}
	}
	if _, err := LookupBundle("nope"); !errors.Is(err, ErrUnknownBundle) {
		t.Errorf("LookupBundle on an unknown ID should report ErrUnknownBundle, got %v", err)
	}
}

func TestModelsCarryAMainlandMirror(t *testing.T) {
	// Not cosmetic. huggingface.co is unreachable from much of China, and
	// without a second source local recognition is a feature that silently
	// times out for a large share of this product's users.
	for _, b := range Models() {
		for _, d := range b.Downloads {
			var mirrored bool
			for _, source := range d.URLs {
				if strings.Contains(source, "hf-mirror.com") {
					mirrored = true
				}
			}
			if !mirrored {
				t.Errorf("bundle %q file %q has no mainland mirror: %v", b.ID, d.Path, d.URLs)
			}
		}
	}
}

func TestDefaultModelIsInstallable(t *testing.T) {
	if _, err := LookupBundle(DefaultModel); err != nil {
		t.Fatalf("the default model is not in the catalogue: %v", err)
	}
}

func TestTotalBytesSumsTheBundle(t *testing.T) {
	b := Bundle{Downloads: []Download{{Bytes: 100}, {Bytes: 250}}}
	if got := b.TotalBytes(); got != 350 {
		t.Errorf("TotalBytes() = %d; want 350", got)
	}
	if got := (Bundle{}).TotalBytes(); got != 0 {
		t.Errorf("an empty bundle reported %d bytes", got)
	}
}
