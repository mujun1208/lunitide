package voice

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// serveBlobs stands in for GitHub and Hugging Face: it hands back the exact
// bytes registered for a path and 404s anything else.
func serveBlobs(t *testing.T, blobs map[string][]byte) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := blobs[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		hits.Add(1)
		w.Header().Set("Content-Length", itoa(len(body)))
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestInstallDownloadsVerifiesAndSkipsWhatIsPresent(t *testing.T) {
	weights := []byte("pretend these are onnx weights")
	tokens := []byte("一\n二\n三\n")
	server, hits := serveBlobs(t, map[string][]byte{
		"/encoder.onnx": weights,
		"/tokens.txt":   tokens,
	})

	bundle := Bundle{
		ID:   "test-model",
		Kind: BundleModel,
		Downloads: []Download{
			{Path: "encoder.onnx", URLs: []string{server.URL + "/encoder.onnx"}, SHA256: digestOf(weights), Bytes: int64(len(weights))},
			{Path: "tokens.txt", URLs: []string{server.URL + "/tokens.txt"}, SHA256: digestOf(tokens), Bytes: int64(len(tokens))},
		},
	}

	installer := &Installer{Root: t.TempDir()}
	if installer.Installed(bundle) {
		t.Fatal("a bundle that was never downloaded reported itself installed")
	}

	var last Progress
	if err := installer.Install(context.Background(), bundle, func(p Progress) { last = p }); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !installer.Installed(bundle) {
		t.Fatal("bundle did not report installed after a successful install")
	}
	if last.Done != bundle.TotalBytes() || last.Percent() != 100 {
		t.Fatalf("final progress should be complete, got %d/%d (%d%%)", last.Done, last.Total, last.Percent())
	}

	got, err := os.ReadFile(filepath.Join(installer.BundleDir(bundle.ID), "tokens.txt"))
	if err != nil || !bytes.Equal(got, tokens) {
		t.Fatalf("installed tokens.txt does not match what was served: %v", err)
	}

	// The point of re-running: a second install must not re-transfer.
	before := hits.Load()
	if err := installer.Install(context.Background(), bundle, nil); err != nil {
		t.Fatalf("second install: %v", err)
	}
	if hits.Load() != before {
		t.Fatalf("second install re-downloaded %d files that were already present", hits.Load()-before)
	}
}

func TestInstallRefusesBytesThatDoNotMatchTheDigest(t *testing.T) {
	served := []byte("weights that were tampered with in transit")
	server, _ := serveBlobs(t, map[string][]byte{"/encoder.onnx": served})

	bundle := Bundle{
		ID: "test-model",
		Downloads: []Download{{
			Path:   "encoder.onnx",
			URLs:   []string{server.URL + "/encoder.onnx"},
			SHA256: digestOf([]byte("the weights we asked for")),
			Bytes:  int64(len(served)),
		}},
	}

	installer := &Installer{Root: t.TempDir()}
	err := installer.Install(context.Background(), bundle, nil)
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("expected a digest mismatch, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(installer.BundleDir(bundle.ID), "encoder.onnx")); !os.IsNotExist(err) {
		t.Fatal("rejected bytes were left behind under their final name")
	}
	if installer.Installed(bundle) {
		t.Fatal("bundle reported installed after the digest was rejected")
	}
}

func TestInstallRejectsAShortResponseBeforeTransferring(t *testing.T) {
	body := []byte("short")
	server, _ := serveBlobs(t, map[string][]byte{"/encoder.onnx": body})

	bundle := Bundle{
		ID: "test-model",
		Downloads: []Download{{
			Path:   "encoder.onnx",
			URLs:   []string{server.URL + "/encoder.onnx"},
			SHA256: digestOf(body),
			Bytes:  999999,
		}},
	}

	err := (&Installer{Root: t.TempDir()}).Install(context.Background(), bundle, nil)
	if err == nil || !strings.Contains(err.Error(), "server offered") {
		t.Fatalf("expected the length disagreement to be caught, got %v", err)
	}
}

func TestInstallFallsBackToTheMirrorWhenUpstreamIsUnreachable(t *testing.T) {
	weights := []byte("pretend these are onnx weights")
	mirror, hits := serveBlobs(t, map[string][]byte{"/encoder.onnx": weights})

	// An address nothing is listening on, which is what huggingface.co looks
	// like from a machine that cannot reach it.
	unreachable, _ := serveBlobs(t, nil)
	dead := unreachable.URL
	unreachable.Close()

	bundle := Bundle{
		ID: "test-model",
		Downloads: []Download{{
			Path:   "encoder.onnx",
			URLs:   []string{dead + "/encoder.onnx", mirror.URL + "/encoder.onnx"},
			SHA256: digestOf(weights),
			Bytes:  int64(len(weights)),
		}},
	}

	installer := &Installer{Root: t.TempDir()}
	if err := installer.Install(context.Background(), bundle, nil); err != nil {
		t.Fatalf("install should have fallen through to the mirror: %v", err)
	}
	if !installer.Installed(bundle) {
		t.Fatal("bundle not installed after the mirror served it")
	}
	if hits.Load() != 1 {
		t.Fatalf("mirror served %d times; want 1", hits.Load())
	}
}

func TestInstallDoesNotTryAMirrorAfterBadBytes(t *testing.T) {
	// A digest mismatch means the bytes are wrong, not missing. Asking the
	// next mirror for a different set of wrong bytes is not a recovery.
	wrong := []byte("not what was pinned")
	first, firstHits := serveBlobs(t, map[string][]byte{"/encoder.onnx": wrong})
	second, secondHits := serveBlobs(t, map[string][]byte{"/encoder.onnx": wrong})

	bundle := Bundle{
		ID: "test-model",
		Downloads: []Download{{
			Path:   "encoder.onnx",
			URLs:   []string{first.URL + "/encoder.onnx", second.URL + "/encoder.onnx"},
			SHA256: digestOf([]byte("what was actually pinned")),
			Bytes:  int64(len(wrong)),
		}},
	}

	err := (&Installer{Root: t.TempDir()}).Install(context.Background(), bundle, nil)
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("expected a digest mismatch, got %v", err)
	}
	if firstHits.Load() != 1 || secondHits.Load() != 0 {
		t.Fatalf("mirrors hit %d and %d; the second should never have been tried", firstHits.Load(), secondHits.Load())
	}
}

func TestInstallRetriesATransientFailure(t *testing.T) {
	// The failure this was written for: a proxy dropping one handshake in
	// the middle of a multi-file install.
	weights := []byte("pretend these are onnx weights")
	var attempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			// Hang up mid-response, which is what a dropped connection
			// looks like from the client side.
			w.Header().Set("Content-Length", itoa(len(weights)))
			w.WriteHeader(http.StatusOK)
			if hijacker, ok := w.(http.Hijacker); ok {
				conn, _, _ := hijacker.Hijack()
				_ = conn.Close()
			}
			return
		}
		w.Header().Set("Content-Length", itoa(len(weights)))
		_, _ = w.Write(weights)
	}))
	t.Cleanup(server.Close)

	bundle := Bundle{
		ID: "test-model",
		Downloads: []Download{{
			Path:   "encoder.onnx",
			URLs:   []string{server.URL + "/encoder.onnx"},
			SHA256: digestOf(weights),
			Bytes:  int64(len(weights)),
		}},
	}

	installer := &Installer{Root: t.TempDir()}
	if err := installer.Install(context.Background(), bundle, nil); err != nil {
		t.Fatalf("a transient failure should have been retried: %v", err)
	}
	if !installer.Installed(bundle) {
		t.Fatal("bundle not installed after the retry succeeded")
	}
	if attempts.Load() < 2 {
		t.Fatalf("server saw %d attempts; the first should have failed and been retried", attempts.Load())
	}
}

func TestInstallReportsTheLastFailureWhenEverySourceIsDown(t *testing.T) {
	server, _ := serveBlobs(t, map[string][]byte{})
	bundle := Bundle{
		ID: "test-model",
		Downloads: []Download{{
			Path:   "encoder.onnx",
			URLs:   []string{server.URL + "/a.onnx", server.URL + "/b.onnx"},
			SHA256: digestOf(nil),
			Bytes:  1,
		}},
	}
	err := (&Installer{Root: t.TempDir()}).Install(context.Background(), bundle, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("expected the final source's failure to be reported, got %v", err)
	}
}

func TestInstallRejectsABundleWithNoSource(t *testing.T) {
	bundle := Bundle{ID: "test-model", Downloads: []Download{{Path: "a.onnx", SHA256: digestOf(nil), Bytes: 1}}}
	err := (&Installer{Root: t.TempDir()}).Install(context.Background(), bundle, nil)
	if err == nil || !strings.Contains(err.Error(), "no download source") {
		t.Fatalf("expected a bundle with no URLs to be rejected, got %v", err)
	}
}

func TestInstallSurfacesAMissingFile(t *testing.T) {
	server, _ := serveBlobs(t, map[string][]byte{})
	bundle := Bundle{
		ID:        "test-model",
		Downloads: []Download{{Path: "gone.onnx", URLs: []string{server.URL + "/gone.onnx"}, SHA256: digestOf(nil), Bytes: 1}},
	}
	err := (&Installer{Root: t.TempDir()}).Install(context.Background(), bundle, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("expected the 404 to be reported, got %v", err)
	}
}

func TestInstalledDetectsAFileCorruptedAfterInstall(t *testing.T) {
	weights := []byte("pretend these are onnx weights")
	server, _ := serveBlobs(t, map[string][]byte{"/encoder.onnx": weights})
	bundle := Bundle{
		ID:        "test-model",
		Downloads: []Download{{Path: "encoder.onnx", URLs: []string{server.URL + "/encoder.onnx"}, SHA256: digestOf(weights), Bytes: int64(len(weights))}},
	}

	installer := &Installer{Root: t.TempDir()}
	if err := installer.Install(context.Background(), bundle, nil); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Antivirus quarantine, a failed sync, a bad sector: the file is there
	// and wrong. Trusting a marker file would miss this.
	target := filepath.Join(installer.BundleDir(bundle.ID), "encoder.onnx")
	if err := os.WriteFile(target, []byte("truncated"), 0o644); err != nil {
		t.Fatalf("corrupt the file: %v", err)
	}
	if installer.Installed(bundle) {
		t.Fatal("a corrupted model still reported itself installed")
	}
}

func TestInstallStopsWhenTheContextIsCancelled(t *testing.T) {
	weights := []byte("pretend these are onnx weights")
	server, _ := serveBlobs(t, map[string][]byte{"/encoder.onnx": weights})
	bundle := Bundle{
		ID:        "test-model",
		Downloads: []Download{{Path: "encoder.onnx", URLs: []string{server.URL + "/encoder.onnx"}, SHA256: digestOf(weights), Bytes: int64(len(weights))}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (&Installer{Root: t.TempDir()}).Install(ctx, bundle, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the cancelled context to abort the install, got %v", err)
	}
}

// tarball builds an uncompressed tar in memory. The archive layer is tested
// on its own because the standard library cannot write bzip2.
func tarball(t *testing.T, members map[string]string, dirs []string, links map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, dir := range dirs {
		if err := w.WriteHeader(&tar.Header{Name: dir, Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
			t.Fatalf("write dir header: %v", err)
		}
	}
	for name, body := range members {
		if err := w.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("write body: %v", err)
		}
	}
	for name, target := range links {
		if err := w.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeSymlink, Linkname: target, Mode: 0o777}); err != nil {
			t.Fatalf("write link header: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	return buf.Bytes()
}

func TestExtractTarStripsTheVersionedWrapperDirectory(t *testing.T) {
	archive := tarball(t, map[string]string{
		"sherpa-onnx-v1.13.6-win-x64/bin/sherpa-onnx.exe": "MZ...",
		"sherpa-onnx-v1.13.6-win-x64/lib/onnxruntime.dll": "MZ...",
	}, []string{"sherpa-onnx-v1.13.6-win-x64/", "sherpa-onnx-v1.13.6-win-x64/bin/"}, nil)

	target := t.TempDir()
	if err := extractTar(bytes.NewReader(archive), target, 1); err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, want := range []string{"bin/sherpa-onnx.exe", "lib/onnxruntime.dll"} {
		if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(want))); err != nil {
			t.Fatalf("%s missing after extraction: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "sherpa-onnx-v1.13.6-win-x64")); !os.IsNotExist(err) {
		t.Fatal("the wrapper directory was not stripped")
	}
}

func TestExtractTarRefusesAMemberThatEscapesTheDirectory(t *testing.T) {
	for name, member := range map[string]string{
		"parent traversal": "wrapper/../../../evil.txt",
		"absolute path":    "/etc/evil.txt",
		"windows drive":    `C:\evil.txt`,
	} {
		t.Run(name, func(t *testing.T) {
			archive := tarball(t, map[string]string{member: "owned"}, nil, nil)
			target := t.TempDir()
			err := extractTar(bytes.NewReader(archive), target, 0)
			if !errors.Is(err, ErrArchiveUnsafe) {
				t.Fatalf("expected the escape to be refused, got %v", err)
			}
			// The refusal has to happen before anything lands outside.
			outside := filepath.Join(filepath.Dir(target), "evil.txt")
			if _, err := os.Stat(outside); err == nil {
				t.Fatalf("a file was written outside the extraction directory at %s", outside)
			}
		})
	}
}

func TestExtractTarRefusesSymlinks(t *testing.T) {
	archive := tarball(t, nil, nil, map[string]string{"link": "../../../../Windows/System32"})
	err := extractTar(bytes.NewReader(archive), t.TempDir(), 0)
	if !errors.Is(err, ErrArchiveUnsafe) {
		t.Fatalf("expected the symlink to be refused, got %v", err)
	}
}

func TestExtractTarTruncatesAMemberToItsDeclaredSize(t *testing.T) {
	archive := tarball(t, map[string]string{"a.txt": "exactly this"}, nil, nil)
	target := t.TempDir()
	if err := extractTar(bytes.NewReader(archive), target, 0); err != nil {
		t.Fatalf("extract: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(target, "a.txt"))
	if err != nil || string(got) != "exactly this" {
		t.Fatalf("member contents wrong: %q %v", got, err)
	}
}

func TestStripComponents(t *testing.T) {
	for _, tc := range []struct {
		name  string
		strip int
		want  string
		keep  bool
	}{
		{"wrapper/bin/a.exe", 1, "bin/a.exe", true},
		{"wrapper/a.exe", 1, "a.exe", true},
		{"wrapper/", 1, "", false},
		{"wrapper", 1, "", false},
		{"./wrapper/a.exe", 1, "a.exe", true},
		{"a.exe", 0, "a.exe", true},
		{`wrapper\bin\a.exe`, 1, "bin/a.exe", true},
	} {
		got, keep := stripComponents(tc.name, tc.strip)
		if got != tc.want || keep != tc.keep {
			t.Errorf("stripComponents(%q, %d) = %q,%v; want %q,%v", tc.name, tc.strip, got, keep, tc.want, tc.keep)
		}
	}
}

func TestSafeJoinRejectsASiblingSharingAPrefix(t *testing.T) {
	root := filepath.Join(t.TempDir(), "models")
	if _, err := safeJoin(root, "../models-evil/payload"); !errors.Is(err, ErrArchiveUnsafe) {
		t.Fatalf("a sibling directory with a shared prefix was accepted: %v", err)
	}
	if _, err := safeJoin(root, "nested/ok.onnx"); err != nil {
		t.Fatalf("a legitimate nested member was rejected: %v", err)
	}
}

func TestProgressPercent(t *testing.T) {
	for _, tc := range []struct {
		done, total int64
		want        int
	}{
		{0, 0, 0},
		{5, 0, 0},
		{0, 100, 0},
		{50, 100, 50},
		{100, 100, 100},
		{150, 100, 100},
	} {
		if got := (Progress{Done: tc.done, Total: tc.total}).Percent(); got != tc.want {
			t.Errorf("Progress{%d,%d}.Percent() = %d; want %d", tc.done, tc.total, got, tc.want)
		}
	}
}
