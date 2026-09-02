package tts

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/voice"
)

func TestLoadRefEnginePackManifestEnvWins(t *testing.T) {
	t.Setenv("LUNITIDE_REF_ENGINE_PACK_URL", "https://host/pack.tar.bz2")
	t.Setenv("LUNITIDE_REF_ENGINE_PACK_SHA256", "ABCDEF")
	t.Setenv("LUNITIDE_REF_ENGINE_PACK_BYTES", "1234")
	t.Setenv("LUNITIDE_REF_ENGINE_PACK_STRIP", "2")
	m, err := LoadRefEnginePackManifest(t.TempDir())
	if err != nil {
		t.Fatalf("env manifest: %v", err)
	}
	if m.URL != "https://host/pack.tar.bz2" {
		t.Fatalf("url = %q", m.URL)
	}
	if m.SHA256 != "abcdef" {
		t.Fatalf("sha must be lowercased; got %q", m.SHA256)
	}
	if m.Bytes != 1234 || m.StripComponents != 2 {
		t.Fatalf("bytes/strip = %d/%d", m.Bytes, m.StripComponents)
	}
}

func TestLoadRefEnginePackManifestFromFile(t *testing.T) {
	t.Setenv("LUNITIDE_REF_ENGINE_PACK_URL", "")
	root := t.TempDir()
	dir := filepath.Join(root, RefEngineBundleID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(RefEnginePackManifest{URL: "https://mirror/pack.tar.bz2", SHA256: "AA", Bytes: 9})
	if err := os.WriteFile(filepath.Join(dir, refEngineManifestFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadRefEnginePackManifest(root)
	if err != nil {
		t.Fatalf("file manifest: %v", err)
	}
	if m.URL != "https://mirror/pack.tar.bz2" || m.SHA256 != "aa" {
		t.Fatalf("manifest = %+v", m)
	}
}

func TestLoadRefEnginePackManifestNotConfigured(t *testing.T) {
	t.Setenv("LUNITIDE_REF_ENGINE_PACK_URL", "")
	if _, err := LoadRefEnginePackManifest(t.TempDir()); !errors.Is(err, ErrRefEnginePackNotConfigured) {
		t.Fatalf("want ErrRefEnginePackNotConfigured; got %v", err)
	}
	// A file present but with no URL is also "not configured", not a parse error.
	root := t.TempDir()
	dir := filepath.Join(root, RefEngineBundleID)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, refEngineManifestFile), []byte(`{"sha256":"aa"}`), 0o644)
	if _, err := LoadRefEnginePackManifest(root); !errors.Is(err, ErrRefEnginePackNotConfigured) {
		t.Fatalf("URL-less file must be not-configured; got %v", err)
	}
}

func TestRefEngineBundleDefaults(t *testing.T) {
	b := RefEngineBundle(RefEnginePackManifest{URL: "https://h/p.tar.bz2", SHA256: "aa", Bytes: 5})
	if b.ID != RefEngineBundleID || b.Kind != voice.BundleRuntime {
		t.Fatalf("bundle id/kind = %q/%q", b.ID, b.Kind)
	}
	if len(b.Downloads) != 1 {
		t.Fatalf("want one download; got %d", len(b.Downloads))
	}
	d := b.Downloads[0]
	if d.Archive != voice.ArchiveTarBz2 {
		t.Fatalf("archive = %v", d.Archive)
	}
	if d.StripComponents != 1 {
		t.Fatalf("default strip should be 1; got %d", d.StripComponents)
	}
	if d.Path != "" {
		t.Fatalf("archive extracts to bundle root; path = %q", d.Path)
	}
	if b.Title == "" || b.Detail == "" {
		t.Fatalf("defaults must fill title/detail")
	}
}

func TestRefEngineBundleHonoursExplicitStrip(t *testing.T) {
	b := RefEngineBundle(RefEnginePackManifest{URL: "u", StripComponents: 3})
	if b.Downloads[0].StripComponents != 3 {
		t.Fatalf("explicit strip lost; got %d", b.Downloads[0].StripComponents)
	}
}
