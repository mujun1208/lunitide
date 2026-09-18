package mcp

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func uvTestNames() (string, string) {
	if runtime.GOOS == "windows" {
		return "uv.exe", "uvx.exe"
	}
	return "uv", "uvx"
}

func zipUvBinaries(t *testing.T, nested bool) []byte {
	t.Helper()
	uv, uvx := uvTestNames()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	names := []string{uv, uvx}
	if nested {
		names = []string{"uv-win/" + uv, "uv-win/" + uvx}
	}
	for i, name := range names {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		payload := []byte("uv-bin")
		if i == 1 {
			payload = []byte("uvx-bin")
		}
		if _, err := f.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUvCatalogPinsOfficialRelease(t *testing.T) {
	bundle, err := Runtime()
	if runtime.GOOS != "windows" {
		if err == nil {
			t.Fatal("non-Windows must not advertise a Windows zip")
		}
		if !strings.Contains(err.Error(), "astral") {
			t.Fatalf("platform error: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Version != UvVersion || !strings.Contains(bundle.URLs[0], "github.com/astral-sh/uv/releases/download/"+UvVersion+"/") {
		t.Fatalf("catalog %+v", bundle)
	}
	if len(bundle.URLs) < 2 || !strings.Contains(bundle.URLs[1], "ghfast.top") {
		t.Fatalf("missing mirror %+v", bundle)
	}
}

func TestInstallDownloadsZipAndPlacesUv(t *testing.T) {
	archive := zipUvBinaries(t, true)
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", itoaLen(len(archive)))
		_, _ = w.Write(archive)
	}))
	t.Cleanup(server.Close)
	dest := t.TempDir()
	installer := NewUvInstaller(dest)
	installer.SetBundle(UvBundle{
		Version: "test",
		File:    "uv.zip",
		URLs:    []string{server.URL + "/uv.zip"},
		SHA256:  hex.EncodeToString(sum[:]),
		Bytes:   int64(len(archive)),
	})
	if installer.Installed() {
		t.Fatal("empty dir is not installed")
	}
	bundle, err := installer.bundle()
	if err != nil {
		t.Fatal(err)
	}
	if err := installer.Install(context.Background(), bundle, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !installer.Installed() {
		t.Fatal("uv and uvx must be present after install")
	}
	uv, uvx := uvTestNames()
	if _, err := os.Stat(filepath.Join(dest, uv)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, uvx)); err != nil {
		t.Fatal(err)
	}
}

func TestInstallRejectsBadDigest(t *testing.T) {
	archive := zipUvBinaries(t, false)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	t.Cleanup(server.Close)
	installer := NewUvInstaller(t.TempDir())
	installer.SetBundle(UvBundle{
		File:   "uv.zip",
		URLs:   []string{server.URL + "/uv.zip"},
		SHA256: strings.Repeat("ab", 32),
		Bytes:  int64(len(archive)),
	})
	bundle, err := installer.bundle()
	if err != nil {
		t.Fatal(err)
	}
	err = installer.Install(context.Background(), bundle, nil)
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("want digest error, got %v", err)
	}
}

func TestInstallReadsSidecarDigest(t *testing.T) {
	archive := zipUvBinaries(t, false)
	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = w.Write([]byte(digest + "  uv.zip\n"))
			return
		}
		_, _ = w.Write(archive)
	}))
	t.Cleanup(server.Close)
	installer := NewUvInstaller(t.TempDir())
	installer.SetBundle(UvBundle{
		File: "uv.zip",
		URLs: []string{server.URL + "/uv.zip"},
	})
	bundle, err := installer.bundle()
	if err != nil {
		t.Fatal(err)
	}
	if err := installer.Install(context.Background(), bundle, nil); err != nil {
		t.Fatalf("sidecar install: %v", err)
	}
	if !installer.Installed() {
		t.Fatal("sidecar digest must install uv")
	}
}

func TestStdioLookPathFindsProductRuntimeUv(t *testing.T) {
	dir := t.TempDir()
	_, uvx := uvTestNames()
	target := filepath.Join(dir, uvx)
	if err := os.WriteFile(target, []byte("fake"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("LUNITIDE_UV_HOME", dir)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", t.TempDir())
	} else {
		t.Setenv("HOME", t.TempDir())
	}
	got, err := stdioLookPath("uvx")
	if err != nil || got != target {
		t.Fatalf("product uv: %q %v want %q", got, err, target)
	}
}

func TestUvBeginInstallReportsProgress(t *testing.T) {
	root := t.TempDir()
	installer := NewUvInstaller(root)
	started := make(chan struct{})
	var once sync.Once
	installer.SetInstall(func(ctx context.Context, _ UvBundle, progress func(UvProgress)) error {
		once.Do(func() { close(started) })
		progress(UvProgress{File: "uv.zip", Done: 4, Total: 10})
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(40 * time.Millisecond):
		}
		return nil
	})
	installer.BeginInstall()
	<-started
	snap := installer.Snapshot()
	if snap["state"] != "downloading" && snap["state"] != "ready" {
		t.Fatalf("progress snapshot %+v", snap)
	}
}

func itoaLen(n int) string {
	if n == 0 {
		return "0"
	}
	var b [32]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
