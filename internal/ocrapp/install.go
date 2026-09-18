package ocrapp

import (
	"archive/zip"
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

	"github.com/lunitide/lunitide/internal/egressproxy"
)

var (
	ErrDigestMismatch = errors.New("ocr: digest mismatch")
	ErrArchiveUnsafe  = errors.New("ocr: archive member is unsafe")
)

type Progress struct {
	BundleID string
	File     string
	Done     int64
	Total    int64
}

func (p Progress) Percent() int {
	if p.Total <= 0 {
		return 0
	}
	if p.Done >= p.Total {
		return 100
	}
	return int(p.Done * 100 / p.Total)
}

type Installer struct {
	Root   string
	Client *http.Client
}

func (in *Installer) client() *http.Client {
	if in.Client != nil {
		return in.Client
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = egressproxy.Resolver()
	return &http.Client{Timeout: 2 * time.Hour, Transport: transport}
}

func (in *Installer) BundleDir(bundleID string) string {
	return filepath.Join(in.Root, bundleID)
}

func (in *Installer) Installed(bundle Bundle) bool {
	dir := in.BundleDir(bundle.ID)
	for _, d := range bundle.Downloads {
		if d.Archive != ArchiveNone {
			if !receiptMatches(dir, d) {
				return false
			}
			continue
		}
		if !fileMatches(filepath.Join(dir, d.Path), d) {
			return false
		}
	}
	pack := DetectPPOcrPack(dir)
	return pack.Available || pack.Status == "registered_unwired"
}

func (in *Installer) Install(ctx context.Context, bundle Bundle, progress func(Progress)) error {
	dir := in.BundleDir(bundle.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("ocr: create %s: %w", bundle.ID, err)
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

func (in *Installer) present(dir string, d Download) bool {
	if d.Archive != ArchiveNone {
		return receiptMatches(dir, d) && DetectPPOcrPack(dir).Available
	}
	return fileMatches(filepath.Join(dir, d.Path), d)
}

func fileMatches(path string, d Download) bool {
	if d.SHA256 == "" {
		info, err := os.Stat(path)
		return err == nil && (d.Bytes == 0 || info.Size() == d.Bytes)
	}
	digest, err := fileDigest(path)
	return err == nil && digest == d.SHA256
}

const downloadRounds = 3

func (in *Installer) fetch(ctx context.Context, dir string, d Download, onBytes func(int64)) error {
	if len(d.URLs) == 0 {
		return fmt.Errorf("ocr: %s has no download source", d.Path)
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

func (in *Installer) fetchFrom(ctx context.Context, dir string, d Download, source string, onBytes func(int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return fmt.Errorf("ocr: request %s: %w", d.Path, err)
	}
	resp, err := in.client().Do(req)
	if err != nil {
		return fmt.Errorf("ocr: download %s: %w", d.Path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ocr: download %s: HTTP %d", d.Path, resp.StatusCode)
	}
	if resp.ContentLength > 0 && d.Bytes > 0 && resp.ContentLength != d.Bytes {
		return fmt.Errorf("ocr: download %s: expected %d bytes, server offered %d", d.Path, d.Bytes, resp.ContentLength)
	}
	temp, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return fmt.Errorf("ocr: stage %s: %w", d.Path, err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	defer temp.Close()

	hasher := sha256.New()
	counted := &countingReader{r: io.TeeReader(resp.Body, hasher), onRead: onBytes}
	if _, err := io.Copy(temp, counted); err != nil {
		return fmt.Errorf("ocr: download %s: %w", d.Path, err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("ocr: flush %s: %w", d.Path, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("ocr: close %s: %w", d.Path, err)
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if d.SHA256 != "" && got != d.SHA256 {
		return fmt.Errorf("%w: %s expected %s, got %s", ErrDigestMismatch, d.Path, d.SHA256, got)
	}
	if d.SHA256 == "" && d.Bytes > 0 && counted.total != d.Bytes {
		return fmt.Errorf("ocr: download %s: expected %d bytes, got %d", d.Path, d.Bytes, counted.total)
	}
	if d.Archive == ArchiveZip {
		return extractVerifiedZip(tempName, dir, d)
	}
	destination := filepath.Join(dir, d.Path)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("ocr: create parent of %s: %w", d.Path, err)
	}
	if err := os.Rename(tempName, destination); err != nil {
		return fmt.Errorf("ocr: place %s: %w", d.Path, err)
	}
	return nil
}

func extractVerifiedZip(archivePath, dir string, d Download) error {
	target := filepath.Join(dir, filepath.FromSlash(d.Path))
	if err := os.MkdirAll(target, 0o755); err != nil {
		return fmt.Errorf("ocr: create %s: %w", d.Path, err)
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("ocr: open archive %s: %w", d.Path, err)
	}
	defer reader.Close()
	for _, f := range reader.File {
		name, keep := stripComponents(f.Name, d.StripComponents)
		if !keep {
			continue
		}
		destination, err := safeJoin(target, name)
		if err != nil {
			return err
		}
		if strings.HasSuffix(strings.ReplaceAll(f.Name, `\`, "/"), "/") || f.FileInfo().IsDir() {
			if err := os.MkdirAll(destination, 0o755); err != nil {
				return fmt.Errorf("ocr: create %s: %w", name, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return fmt.Errorf("ocr: create parent of %s: %w", name, err)
		}
		src, err := f.Open()
		if err != nil {
			return fmt.Errorf("ocr: read %s: %w", name, err)
		}
		err = writeMember(destination, src, int64(f.UncompressedSize64))
		src.Close()
		if err != nil {
			return err
		}
	}
	return writeReceipt(dir, d)
}

func writeMember(destination string, r io.Reader, size int64) error {
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("ocr: write %s: %w", filepath.Base(destination), err)
	}
	defer out.Close()
	if _, err := io.CopyN(out, r, size); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("ocr: write %s: %w", filepath.Base(destination), err)
	}
	return out.Close()
}

func safeJoin(root, name string) (string, error) {
	slashed := strings.ReplaceAll(name, `\`, "/")
	if slashed == "" {
		return "", fmt.Errorf("%w: empty member name", ErrArchiveUnsafe)
	}
	if strings.HasPrefix(slashed, "/") {
		return "", fmt.Errorf("%w: %s is an absolute path", ErrArchiveUnsafe, name)
	}
	if strings.Contains(slashed, ":") {
		return "", fmt.Errorf("%w: %s names a volume or stream", ErrArchiveUnsafe, name)
	}
	for _, part := range strings.Split(slashed, "/") {
		if part == ".." {
			return "", fmt.Errorf("%w: %s traverses out of the extraction directory", ErrArchiveUnsafe, name)
		}
	}
	joined := filepath.Join(root, filepath.FromSlash(path.Clean(slashed)))
	if joined != root && !strings.HasPrefix(joined, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %s escapes the extraction directory", ErrArchiveUnsafe, name)
	}
	return joined, nil
}

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

func receiptPath(dir string, d Download) string {
	return filepath.Join(dir, filepath.FromSlash(d.Path), ".lunitide-bundle")
}

func writeReceipt(dir string, d Download) error {
	if err := os.WriteFile(receiptPath(dir, d), []byte(d.SHA256), 0o644); err != nil {
		return fmt.Errorf("ocr: write receipt for %s: %w", d.Path, err)
	}
	return nil
}

func receiptMatches(dir string, d Download) bool {
	recorded, err := os.ReadFile(receiptPath(dir, d))
	return err == nil && strings.TrimSpace(string(recorded)) == d.SHA256
}

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
