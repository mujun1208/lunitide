package voice

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The rest of the install tests serve their own bytes, which proves the
// logic and proves nothing about the catalogue: a typo in a URL, a digest
// copied from the wrong row, or an upstream that moved its files would all
// pass. This one actually goes and gets them.
//
// It is off by default and stays off in CI. It transfers tens of megabytes
// from GitHub and Hugging Face, so it belongs to whoever is changing the
// catalogue, run deliberately:
//
//	LUNITIDE_VOICE_NETWORK_TEST=1 go test ./internal/voice/ -run Network -v
//
// The large model is excluded even then. Its digest comes from the same API
// response as the small one, so 226 MB buys no additional confidence.
func requireNetworkTest(t *testing.T) {
	t.Helper()
	if os.Getenv("LUNITIDE_VOICE_NETWORK_TEST") != "1" {
		t.Skip("set LUNITIDE_VOICE_NETWORK_TEST=1 to download from the real catalogue")
	}
}

func TestNetworkRuntimeDownloadsAndExtracts(t *testing.T) {
	requireNetworkTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	installer := &Installer{Root: t.TempDir()}
	bundle := Runtime()
	if err := installer.Install(ctx, bundle, func(p Progress) {
		if p.File != "" && p.Percent()%25 == 0 {
			t.Logf("%s %d%%", p.BundleID, p.Percent())
		}
	}); err != nil {
		t.Fatalf("install runtime: %v", err)
	}
	if !installer.Installed(bundle) {
		t.Fatal("runtime did not report installed after a successful install")
	}

	// The reason to extract at all: the sidecar needs this program.
	exe := filepath.Join(installer.BundleDir(bundle.ID), "bin", "sherpa-onnx-online-websocket-server.exe")
	info, err := os.Stat(exe)
	if err != nil {
		listing, _ := os.ReadDir(filepath.Join(installer.BundleDir(bundle.ID), "bin"))
		var names []string
		for _, e := range listing {
			names = append(names, e.Name())
		}
		t.Fatalf("websocket server missing after extraction: %v\nbin/ contains: %v", err, names)
	}
	if info.Size() == 0 {
		t.Fatal("websocket server extracted as an empty file")
	}
}

func TestNetworkSmallModelDownloads(t *testing.T) {
	requireNetworkTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	installer := &Installer{Root: t.TempDir()}
	bundle, err := LookupBundle(ModelZipformerZh14M)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if err := installer.Install(ctx, bundle, nil); err != nil {
		t.Fatalf("install model: %v", err)
	}
	if !installer.Installed(bundle) {
		t.Fatal("model did not report installed after a successful install")
	}
	for _, d := range bundle.Downloads {
		info, err := os.Stat(filepath.Join(installer.BundleDir(bundle.ID), d.Path))
		if err != nil {
			t.Fatalf("%s missing: %v", d.Path, err)
		}
		if info.Size() != d.Bytes {
			t.Fatalf("%s is %d bytes, catalogue says %d", d.Path, info.Size(), d.Bytes)
		}
	}
}
