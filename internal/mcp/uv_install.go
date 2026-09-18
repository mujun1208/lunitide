package mcp

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
	"sync"
	"time"
	"unicode"

	"github.com/lunitide/lunitide/internal/egressproxy"
)

var (
	errUvDigest   = errors.New("uv: digest mismatch")
	errUvUnsafe   = errors.New("uv: archive member is unsafe")
	errUvBinaries = errors.New("uv: archive is missing uv / uvx")
)

type UvProgress struct {
	File  string
	Done  int64
	Total int64
}

func (p UvProgress) Percent() int {
	if p.Total <= 0 {
		return 0
	}
	if p.Done >= p.Total {
		return 100
	}
	return int(p.Done * 100 / p.Total)
}

// UvInstaller downloads a pinned uv zip into Root and exposes poll snapshots
// for mcp.uv.install, the same on-demand pattern as PP-OCR.
type UvInstaller struct {
	Root   string
	Client *http.Client

	mu           sync.Mutex
	override     *UvBundle
	installFn    func(context.Context, UvBundle, func(UvProgress)) error
	installing   bool
	installState string
	lastErr      string
	progress     UvProgress
}

func NewUvInstaller(root string) *UvInstaller {
	return &UvInstaller{Root: root, installState: "idle"}
}

func (in *UvInstaller) SetBundle(bundle UvBundle) {
	if in == nil {
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	copy := bundle
	in.override = &copy
}

func (in *UvInstaller) SetInstall(fn func(context.Context, UvBundle, func(UvProgress)) error) {
	if in != nil {
		in.installFn = fn
	}
}

func (in *UvInstaller) client() *http.Client {
	if in != nil && in.Client != nil {
		return in.Client
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = egressproxy.Resolver()
	return &http.Client{Timeout: 2 * time.Hour, Transport: transport}
}

func (in *UvInstaller) bundle() (UvBundle, error) {
	if in == nil {
		return UvBundle{}, fmt.Errorf("uv 安装器未装配")
	}
	in.mu.Lock()
	override := in.override
	in.mu.Unlock()
	if override != nil {
		return *override, nil
	}
	return Runtime()
}

func (in *UvInstaller) Installed() bool {
	if in == nil || strings.TrimSpace(in.Root) == "" {
		return false
	}
	uv, uvx := uvBinaryNames()
	if !uvFilePresent(filepath.Join(in.Root, uv)) || !uvFilePresent(filepath.Join(in.Root, uvx)) {
		return false
	}
	return true
}

func uvFilePresent(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func (in *UvInstaller) BeginInstall() {
	if in == nil {
		return
	}
	in.mu.Lock()
	if in.installing {
		in.mu.Unlock()
		return
	}
	in.mu.Unlock()
	if in.Installed() && in.installFn == nil {
		in.mu.Lock()
		in.installState, in.lastErr = "ready", ""
		in.mu.Unlock()
		return
	}
	in.mu.Lock()
	if in.installing {
		in.mu.Unlock()
		return
	}
	in.installing, in.installState, in.lastErr = true, "downloading", ""
	in.mu.Unlock()
	go in.runInstall()
}

func (in *UvInstaller) runInstall() {
	defer func() {
		in.mu.Lock()
		in.installing = false
		in.mu.Unlock()
	}()
	install := in.installFn
	var bundle UvBundle
	if install == nil {
		got, err := in.bundle()
		if err != nil {
			in.fail(err.Error())
			return
		}
		bundle = got
		install = in.Install
	}
	err := install(context.Background(), bundle, func(p UvProgress) {
		in.mu.Lock()
		in.progress = p
		in.mu.Unlock()
	})
	if err != nil {
		in.fail(err.Error())
		return
	}
	if in.installFn == nil && !in.Installed() {
		in.fail("uv 已下载但还找不到可运行文件，请重试")
		return
	}
	in.mu.Lock()
	in.installState, in.lastErr = "ready", ""
	in.mu.Unlock()
}

func (in *UvInstaller) fail(msg string) {
	in.mu.Lock()
	in.installState, in.lastErr = "failed", msg
	in.mu.Unlock()
}

func (in *UvInstaller) Snapshot() map[string]any {
	if in == nil {
		return map[string]any{"state": "failed", "percent": 0, "doneBytes": 0, "totalBytes": 0, "lastError": "uv 安装服务未就绪"}
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	state := in.installState
	if state == "" {
		state = "idle"
	}
	out := map[string]any{
		"state":      state,
		"percent":    in.progress.Percent(),
		"doneBytes":  in.progress.Done,
		"totalBytes": in.progress.Total,
	}
	if in.progress.File != "" {
		out["file"] = in.progress.File
	}
	if in.lastErr != "" {
		out["lastError"] = uvUserLastError(in.lastErr)
	}
	return out
}

func uvUserLastError(msg string) string {
	msg = strings.TrimSpace(strings.TrimPrefix(msg, "uv: "))
	if msg == "" {
		return ""
	}
	if containsHanRunes(msg) {
		return truncateRunes(msg, 512)
	}
	if strings.Contains(msg, "digest mismatch") {
		return "下载文件校验失败，请重试"
	}
	if idx := strings.Index(msg, "HTTP "); idx >= 0 {
		code := ""
		for _, c := range msg[idx+5:] {
			if c >= '0' && c <= '9' {
				code += string(c)
				continue
			}
			break
		}
		if code != "" {
			return "下载失败（HTTP " + code + "）"
		}
	}
	return "下载失败，请检查网络后重试"
}

func containsHanRunes(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

func (in *UvInstaller) Install(ctx context.Context, bundle UvBundle, progress func(UvProgress)) error {
	if in == nil {
		return fmt.Errorf("uv 安装器未装配")
	}
	if progress == nil {
		progress = func(UvProgress) {}
	}
	if err := os.MkdirAll(in.Root, 0o755); err != nil {
		return fmt.Errorf("uv: create install dir: %w", err)
	}
	total := bundle.Bytes
	report := func(file string, done int64) {
		if total <= 0 {
			total = done
		}
		progress(UvProgress{File: file, Done: done, Total: total})
	}
	report(bundle.File, 0)
	digest := strings.ToLower(strings.TrimSpace(bundle.SHA256))
	if digest == "" {
		got, err := in.fetchSidecarDigest(ctx, bundle)
		if err != nil {
			return err
		}
		digest = got
	}
	var last error
	for round := range 3 {
		if round > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(round) * time.Second):
			}
		}
		for _, source := range bundle.URLs {
			err := in.fetchZip(ctx, bundle, source, digest, func(n int64) { report(bundle.File, n) })
			if err == nil {
				return nil
			}
			if ctx.Err() != nil || errors.Is(err, errUvDigest) || errors.Is(err, errUvUnsafe) || errors.Is(err, errUvBinaries) {
				return err
			}
			last = err
		}
	}
	return last
}

func (in *UvInstaller) fetchSidecarDigest(ctx context.Context, bundle UvBundle) (string, error) {
	var last error
	for _, source := range bundle.URLs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source+".sha256", nil)
		if err != nil {
			last = err
			continue
		}
		resp, err := in.client().Do(req)
		if err != nil {
			last = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if readErr != nil {
			last = readErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			last = fmt.Errorf("uv: sidecar HTTP %d", resp.StatusCode)
			continue
		}
		digest, err := parseSHA256Sidecar(body)
		if err != nil {
			last = err
			continue
		}
		return digest, nil
	}
	if last == nil {
		last = fmt.Errorf("uv: missing checksum sidecar")
	}
	return "", last
}

func parseSHA256Sidecar(raw []byte) (string, error) {
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return "", fmt.Errorf("uv: empty checksum sidecar")
	}
	h := strings.ToLower(fields[0])
	if len(h) != 64 {
		return "", fmt.Errorf("uv: checksum sidecar is not SHA-256")
	}
	for _, c := range h {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' {
			continue
		}
		return "", fmt.Errorf("uv: checksum sidecar is not SHA-256")
	}
	return h, nil
}

func (in *UvInstaller) fetchZip(ctx context.Context, bundle UvBundle, source, digest string, onBytes func(int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return fmt.Errorf("uv: request: %w", err)
	}
	resp, err := in.client().Do(req)
	if err != nil {
		return fmt.Errorf("uv: download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("uv: download HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > 0 && bundle.Bytes > 0 && resp.ContentLength != bundle.Bytes {
		return fmt.Errorf("uv: expected %d bytes, server offered %d", bundle.Bytes, resp.ContentLength)
	}
	temp, err := os.CreateTemp(in.Root, ".uv-download-*")
	if err != nil {
		return fmt.Errorf("uv: stage: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	defer temp.Close()
	hasher := sha256.New()
	counted := &uvCountReader{r: io.TeeReader(resp.Body, hasher), onRead: onBytes}
	if _, err := io.Copy(temp, counted); err != nil {
		return fmt.Errorf("uv: download: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("uv: close: %w", err)
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if digest != "" && got != digest {
		return fmt.Errorf("%w: expected %s, got %s", errUvDigest, digest, got)
	}
	if err := extractUvZip(tempName, in.Root); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(in.Root, ".lunitide-uv"), []byte(got+"\n"), 0o644)
}

func extractUvZip(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("uv: open zip: %w", err)
	}
	defer r.Close()
	uvName, uvxName := uvBinaryNames()
	foundUV, foundUvx := false, false
	for _, f := range r.File {
		base, ok := uvMemberBase(f.Name)
		if !ok || f.FileInfo().IsDir() {
			continue
		}
		if err := writeUvMember(dest, base, f); err != nil {
			return err
		}
		switch base {
		case uvName, "uv", "uv.exe":
			if base == uvName {
				foundUV = true
			}
		case uvxName, "uvx", "uvx.exe":
			if base == uvxName {
				foundUvx = true
			}
		}
	}
	if !foundUV || !foundUvx {
		return errUvBinaries
	}
	return nil
}

func uvMemberBase(name string) (string, bool) {
	slashed := strings.ReplaceAll(name, `\`, "/")
	if slashed == "" || strings.HasPrefix(slashed, "/") || strings.Contains(slashed, ":") {
		return "", false
	}
	for _, part := range strings.Split(slashed, "/") {
		if part == ".." {
			return "", false
		}
	}
	base := path.Base(path.Clean(slashed))
	switch base {
	case "uv", "uv.exe", "uvx", "uvx.exe":
		return base, true
	default:
		return "", false
	}
}

func writeUvMember(dest, base string, f *zip.File) error {
	src, err := f.Open()
	if err != nil {
		return fmt.Errorf("uv: read %s: %w", base, err)
	}
	defer src.Close()
	outPath := filepath.Join(dest, base)
	out, err := os.OpenFile(outPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("uv: write %s: %w", base, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, src); err != nil {
		return fmt.Errorf("uv: write %s: %w", base, err)
	}
	return out.Close()
}

type uvCountReader struct {
	r      io.Reader
	total  int64
	onRead func(int64)
}

func (c *uvCountReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.total += int64(n)
		if c.onRead != nil {
			c.onRead(c.total)
		}
	}
	return n, err
}
