package voice

import (
	"archive/tar"
	"compress/bzip2"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Fetching the catalogue onto disk.
//
// The ordering rule throughout is that nothing incomplete is ever visible
// under its final name. Bytes land in a temporary file beside their
// destination, are hashed as they arrive, and are renamed into place only
// after the digest matches. A download killed halfway — closed lid, dropped
// wifi, task manager — leaves a stray temp file and nothing else, so the next
// run sees "not installed" rather than a truncated model that fails somewhere
// inside the recognizer with no clue why.

// Progress reports how far an install has got. Sent often enough for a bar to
// move smoothly and rarely enough not to flood the bridge.
type Progress struct {
	// BundleID identifies which install this is about.
	BundleID string
	// File is the download currently in flight, for a caller that wants to
	// name it. Empty during verification of an already-present file.
	File string
	// Done and Total are byte counts across the whole bundle, not the
	// current file, so a caller can render one bar for the operation the
	// user actually asked for.
	Done  int64
	Total int64
}

// Percent is Done/Total clamped to 0-100, for a caller that just wants a
// number. Zero when the total is unknown, rather than a division panic.
func (p Progress) Percent() int {
	if p.Total <= 0 {
		return 0
	}
	if p.Done >= p.Total {
		return 100
	}
	return int(p.Done * 100 / p.Total)
}

// Installer fetches bundles into a directory.
type Installer struct {
	// Root is the directory bundles are installed under. Each bundle gets a
	// subdirectory named for its ID.
	Root string
	// Client is the HTTP client used for downloads. Nil means a client with
	// a generous timeout, because 226 MB on a slow connection is not a
	// stuck request.
	Client *http.Client
}

// client allows a long transfer but not a hung one. The timeout covers the
// whole request including the body, so it has to accommodate the largest
// download in the catalogue over a connection worth waiting for.
//
// The transport is not the default one, and the reason is worth recording.
// Go's default proxy resolution reads HTTP_PROXY and HTTPS_PROXY from the
// environment, which almost nobody on Windows sets: proxies are configured
// once in Internet Options and every normal application picks them up from
// there. Measured on a machine behind such a proxy, the runtime download ran
// at 36 KB/s through the default transport and 2.4 MB/s through anything
// that honoured the system setting, and Hugging Face was unreachable
// entirely. So this asks Windows.
func (in *Installer) client() *http.Client {
	if in.Client != nil {
		return in.Client
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = proxyResolver()
	return &http.Client{Timeout: 2 * time.Hour, Transport: transport}
}

// BundleDir is where a bundle's files live once installed.
func (in *Installer) BundleDir(bundleID string) string {
	return filepath.Join(in.Root, bundleID)
}

// Installed reports whether every file in the bundle is present with the
// right digest.
//
// It re-hashes rather than trusting a marker file. Hashing 226 MB costs about
// a second, which is affordable once at startup, and the alternative is
// believing a marker written before a disk error, an antivirus quarantine, or
// a half-finished sync. The recognizer's failure mode on a corrupt model is
// an unexplained crash inside ONNX, which is a bad thing to debug from a user
// report.
func (in *Installer) Installed(bundle Bundle) bool {
	dir := in.BundleDir(bundle.ID)
	for _, d := range bundle.Downloads {
		if d.Archive != ArchiveNone {
			// An archive's members are not individually pinned, so the
			// receipt written after a verified extraction stands in for
			// them. It records the digest of the archive it came from.
			if !receiptMatches(dir, d) {
				return false
			}
			continue
		}
		digest, err := fileDigest(filepath.Join(dir, d.Path))
		if err != nil || digest != d.SHA256 {
			return false
		}
	}
	return true
}

// Install downloads whatever the bundle is missing and verifies all of it.
// Already-present files are skipped, so an interrupted install resumes at
// file granularity rather than starting over.
//
// progress may be nil.
func (in *Installer) Install(ctx context.Context, bundle Bundle, progress func(Progress)) error {
	dir := in.BundleDir(bundle.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("voice: create %s: %w", bundle.ID, err)
	}

	total := bundle.TotalBytes()
	var done int64
	report := func(file string, fileDone int64) {
		if progress == nil {
			return
		}
		progress(Progress{BundleID: bundle.ID, File: file, Done: done + fileDone, Total: total})
	}
	report("", 0)

	for _, d := range bundle.Downloads {
		if in.present(dir, d) {
			done += d.Bytes
			report("", 0)
			continue
		}
		if err := in.fetch(ctx, dir, d, func(n int64) { report(d.Path, n) }); err != nil {
			return err
		}
		done += d.Bytes
		report("", 0)
	}
	return nil
}

// present reports whether one download is already on disk and intact.
func (in *Installer) present(dir string, d Download) bool {
	if d.Archive != ArchiveNone {
		return receiptMatches(dir, d)
	}
	digest, err := fileDigest(filepath.Join(dir, d.Path))
	return err == nil && digest == d.SHA256
}

// downloadRounds is how many times the whole source list is walked before
// giving up.
//
// Measured rather than guessed: installing the small model over a proxied
// connection, three of its four files arrived and the fourth — 75 KB of
// tokens — died on a TLS handshake timeout. That is not a source being down,
// it is a proxy having a bad second, and abandoning a 226 MB install over one
// is a worse answer than waiting two more.
const downloadRounds = 3

// fetch downloads one file from the first source that answers, verifies it,
// and puts it where it belongs.
//
// A source that fails is tried past rather than treated as fatal. Two things
// are fatal: a cancelled context, because the user closed the dialog and
// should not sit through an attempt on every mirror, and bytes that are
// present but wrong, because asking the next mirror for a different set of
// wrong bytes is not a recovery strategy.
func (in *Installer) fetch(ctx context.Context, dir string, d Download, onBytes func(int64)) error {
	if len(d.URLs) == 0 {
		return fmt.Errorf("voice: %s has no download source", d.Path)
	}
	var last error
	for round := range downloadRounds {
		if round > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(round) * time.Second):
			}
		}
		for _, source := range d.URLs {
			err := in.fetchFrom(ctx, dir, d, source, onBytes)
			if err == nil {
				return nil
			}
			if ctx.Err() != nil || errors.Is(err, ErrDigestMismatch) || errors.Is(err, ErrArchiveUnsafe) {
				return err
			}
			last = err
		}
	}
	return last
}

// fetchFrom downloads one file from one source.
func (in *Installer) fetchFrom(ctx context.Context, dir string, d Download, source string, onBytes func(int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return fmt.Errorf("voice: request %s: %w", d.Path, err)
	}
	resp, err := in.client().Do(req)
	if err != nil {
		return fmt.Errorf("voice: download %s: %w", d.Path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("voice: download %s: HTTP %d", d.Path, resp.StatusCode)
	}
	// A mismatched Content-Length is worth failing on early rather than
	// after transferring 226 MB to find the digest wrong. Length is only
	// advisory when it is absent or chunked, hence the > 0 guard.
	if resp.ContentLength > 0 && d.Bytes > 0 && resp.ContentLength != d.Bytes {
		return fmt.Errorf("voice: download %s: expected %d bytes, server offered %d", d.Path, d.Bytes, resp.ContentLength)
	}

	temp, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return fmt.Errorf("voice: stage %s: %w", d.Path, err)
	}
	tempName := temp.Name()
	// Both cleanups are unconditional and both are expected to fail on the
	// success path, where the file has already been closed and renamed.
	defer os.Remove(tempName)
	defer temp.Close()

	hasher := sha256.New()
	counted := &countingReader{r: io.TeeReader(resp.Body, hasher), onRead: onBytes}
	if _, err := io.Copy(temp, counted); err != nil {
		return fmt.Errorf("voice: download %s: %w", d.Path, err)
	}
	// Durability before the rename, so a crash cannot leave a file that has
	// the right name and no contents.
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("voice: flush %s: %w", d.Path, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("voice: close %s: %w", d.Path, err)
	}

	if got := hex.EncodeToString(hasher.Sum(nil)); got != d.SHA256 {
		return fmt.Errorf("%w: %s expected %s, got %s", ErrDigestMismatch, d.Path, d.SHA256, got)
	}

	if d.Archive == ArchiveTarBz2 {
		return extractVerified(tempName, dir, d)
	}
	destination := filepath.Join(dir, d.Path)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("voice: create parent of %s: %w", d.Path, err)
	}
	if err := os.Rename(tempName, destination); err != nil {
		return fmt.Errorf("voice: place %s: %w", d.Path, err)
	}
	return nil
}

// extractVerified unpacks an archive whose digest has already been checked,
// then records a receipt so a later Installed call need not unpack it again.
func extractVerified(archivePath, dir string, d Download) error {
	target := filepath.Join(dir, filepath.FromSlash(d.Path))
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("voice: create %s: %w", d.Path, err)
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("voice: open archive %s: %w", d.Path, err)
	}
	defer file.Close()
	if err := extractTarBz2(file, target, d.StripComponents); err != nil {
		return err
	}
	return writeReceipt(dir, d)
}

// extractTarBz2 unpacks a bzip2 tar into target. The decompressor is the
// standard library's, so nothing external is needed to install the runtime.
func extractTarBz2(r io.Reader, target string, strip int) error {
	return extractTar(bzip2.NewReader(r), target, strip)
}

// extractTar unpacks a tar into target.
//
// Every member is checked before anything is written. The archive comes off
// the internet, and tar can name any path it likes: `../../autostart` is a
// legal member name, and so is a symlink pointing at somewhere interesting
// followed by a write through it. Neither is something this archive needs, so
// both are refused rather than sanitized — a sherpa release that suddenly
// contains a symlink is a reason to stop, not to carry on carefully.
//
// Split from the bzip2 wrapper so this can be tested: the standard library
// decompresses bzip2 but does not compress it, so a test cannot build a
// fixture for the combined form.
func extractTar(r io.Reader, target string, strip int) error {
	root, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("voice: resolve %s: %w", target, err)
	}
	reader := tar.NewReader(r)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("voice: read archive: %w", err)
		}
		switch header.Typeflag {
		case tar.TypeDir, tar.TypeReg:
		default:
			return fmt.Errorf("%w: %s is not a regular file or directory", ErrArchiveUnsafe, header.Name)
		}
		name, keep := stripComponents(header.Name, strip)
		if !keep {
			continue
		}
		destination, err := safeJoin(root, name)
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(destination, 0o755); err != nil {
				return fmt.Errorf("voice: create %s: %w", name, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return fmt.Errorf("voice: create parent of %s: %w", name, err)
		}
		if err := writeMember(destination, reader, header.Size); err != nil {
			return err
		}
	}
}

// writeMember writes one archive member, copying no more than the header
// promised so a lying header cannot fill the disk.
func writeMember(destination string, r io.Reader, size int64) error {
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("voice: write %s: %w", filepath.Base(destination), err)
	}
	defer out.Close()
	if _, err := io.CopyN(out, r, size); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("voice: write %s: %w", filepath.Base(destination), err)
	}
	return out.Close()
}

// safeJoin resolves a member name under root and refuses anything that could
// land outside it.
//
// Refusing rather than sanitizing is the point. Rooting the name at "/" and
// cleaning it would turn `wrapper/../../../evil.txt` into `evil.txt` and file
// it neatly inside the directory: safe, and silent. But an archive that tried
// to traverse is not the archive whose digest we pinned, and quietly
// installing a tidied-up version of it destroys the only signal that
// something is wrong. So traversal, absolute paths, and drive letters are
// errors that abandon the whole extraction.
func safeJoin(root, name string) (string, error) {
	slashed := strings.ReplaceAll(name, `\`, "/")
	if slashed == "" {
		return "", fmt.Errorf("%w: empty member name", ErrArchiveUnsafe)
	}
	if strings.HasPrefix(slashed, "/") {
		return "", fmt.Errorf("%w: %s is an absolute path", ErrArchiveUnsafe, name)
	}
	// A drive letter or an NTFS alternate data stream. Neither occurs in a
	// legitimate tar, and both mean something other than a relative name to
	// the Windows path resolver.
	if strings.Contains(slashed, ":") {
		return "", fmt.Errorf("%w: %s names a volume or stream", ErrArchiveUnsafe, name)
	}
	for _, part := range strings.Split(slashed, "/") {
		if part == ".." {
			return "", fmt.Errorf("%w: %s traverses out of the extraction directory", ErrArchiveUnsafe, name)
		}
	}
	joined := filepath.Join(root, filepath.FromSlash(path.Clean(slashed)))
	// Belt and braces. Whatever the components looked like, the result has
	// to sit under root — compared with a separator appended so a sibling
	// sharing a name prefix is not mistaken for a child.
	if joined != root && !strings.HasPrefix(joined, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %s escapes the extraction directory", ErrArchiveUnsafe, name)
	}
	return joined, nil
}

// stripComponents drops leading path segments, reporting false when a member
// lies entirely inside the stripped prefix and should be skipped.
func stripComponents(name string, strip int) (string, bool) {
	clean := strings.TrimPrefix(path.Clean(strings.ReplaceAll(name, `\`, "/")), "./")
	if strip <= 0 {
		return clean, clean != "" && clean != "."
	}
	parts := strings.Split(clean, "/")
	if len(parts) <= strip {
		return "", false
	}
	return strings.Join(parts[strip:], "/"), true
}

// A receipt records that an archive was extracted here, and which archive it
// was. Its contents are the archive's digest, so changing the pinned build in
// the catalogue invalidates every existing install without a version scheme.
func receiptPath(dir string, d Download) string {
	return filepath.Join(dir, filepath.FromSlash(d.Path), ".lunitide-bundle")
}

func writeReceipt(dir string, d Download) error {
	if err := os.WriteFile(receiptPath(dir, d), []byte(d.SHA256), 0o644); err != nil {
		return fmt.Errorf("voice: write receipt for %s: %w", d.Path, err)
	}
	return nil
}

func receiptMatches(dir string, d Download) bool {
	recorded, err := os.ReadFile(receiptPath(dir, d))
	return err == nil && strings.TrimSpace(string(recorded)) == d.SHA256
}

// fileDigest hashes a file, streaming so a 226 MB model does not become 226
// MB of resident memory.
func fileDigest(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// countingReader reports cumulative progress as bytes pass through.
type countingReader struct {
	r      io.Reader
	total  int64
	onRead func(int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.total += int64(n)
		if c.onRead != nil {
			c.onRead(c.total)
		}
	}
	return n, err
}
