package omni

import (
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
