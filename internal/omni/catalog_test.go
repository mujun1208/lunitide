package omni

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelBundleIsQ4AndPinned(t *testing.T) {
	bundle := ModelBundle()
	if bundle.ID != BundleID {
		t.Fatalf("id = %s", bundle.ID)
	}
	if bundle.Title != "MiniCPM-o 4.5 Q4" {
		t.Fatalf("title = %s", bundle.Title)
	}
	if bundle.TotalBytes() < 8<<30 {
		t.Fatalf("total bytes = %d; want at least 8 GiB of GGUF", bundle.TotalBytes())
	}
	if ListenAddr != "127.0.0.1:19080" {
		t.Fatalf("listen = %s; remote bind is out of scope", ListenAddr)
	}
	var llm bool
	for _, d := range bundle.Downloads {
		if d.Path == LLMFile {
			llm = true
		}
		if d.SHA256 == "" {
			t.Errorf("%s missing sha256", d.Path)
		}
		joined := strings.Join(d.URLs, " ")
		if !strings.Contains(joined, Revision) {
			t.Errorf("%s URLs are not revision-pinned", d.Path)
		}
		if !strings.Contains(joined, "huggingface.co") || !strings.Contains(joined, "hf-mirror.com") {
			t.Errorf("%s missing HF + mirror", d.Path)
		}
	}
	if !llm {
		t.Fatal("Q4_K_M LLM file missing from catalogue")
	}
}

func TestRuntimeBundleIsPinnedNotLatest(t *testing.T) {
	bundle := RuntimeBundle()
	if bundle.ID != "comni-runtime-"+RuntimeRevision {
		t.Fatalf("id = %s", bundle.ID)
	}
	if RuntimeRevision == "latest" {
		t.Fatal("runtime revision must not float")
	}
	if len(bundle.Downloads) != 1 {
		t.Fatalf("downloads = %d", len(bundle.Downloads))
	}
	d := bundle.Downloads[0]
	if d.Path != RuntimeSetupFile {
		t.Fatalf("path = %s", d.Path)
	}
	if d.SHA256 != RuntimeSHA256 || d.Bytes != RuntimeBytes {
		t.Fatalf("digest/size mismatch")
	}
	joined := strings.Join(d.URLs, " ")
	if strings.Contains(joined, "/latest/") {
		t.Fatalf("floating latest URL: %s", joined)
	}
	if !strings.Contains(joined, RuntimeRevision) {
		t.Fatalf("URLs are not revision-pinned: %s", joined)
	}
}

func TestWalkRuntimeFindsNestedLayout(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "runtime", "Comni", "bin")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(nested, "llama-omni-server.exe")
	if err := os.WriteFile(want, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := walkRuntime(filepath.Join(root, "runtime"), runtimeWalkDepth); got != want {
		t.Fatalf("walkRuntime = %q; want %q", got, want)
	}
}
