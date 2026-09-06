package desktopfiles

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
)

const (
	allowTTL          = 10 * time.Minute
	maxItems          = 20
	maxSkipped        = 20
	maxChunk          = 32768
	maxPickFileSize   = 100 * 1024 * 1024
	codeDenied        = "ATTACHMENT_PATH_DENIED"
	codeUnavailable   = "DESKTOP_PICK_UNAVAILABLE"
	codeFailed        = "DESKTOP_PICK_FAILED"
	codeSchemaInvalid = "BRIDGE_SCHEMA_INVALID"
)

var (
	ErrCanceled    = errors.New("desktop pick canceled")
	ErrUnavailable = errors.New("desktop pick unavailable")
)

type Item struct {
	Path     string
	FileName string
	MIME     string
	Size     int64
}

type Handler struct {
	Pick  func(folder, multiple bool) ([]Item, []string, error)
	Now   func() time.Time
	mu    sync.Mutex
	allow map[string]time.Time
}

func New() *Handler {
	return &Handler{
		Pick:  pickOS,
		Now:   time.Now,
		allow: map[string]time.Time{},
	}
}

func (h *Handler) HandleHost(_ context.Context, r bridge.Request) bridge.Response {
	switch bridge.Method(r.Method) {
	case "desktop.files.pick":
		return h.pick(r)
	case "desktop.files.readChunk":
		return h.readChunk(r)
	default:
		return fail(r, codeSchemaInvalid, "未知的桌面文件方法", false)
	}
}

func (h *Handler) pick(r bridge.Request) bridge.Response {
	var p struct {
		Folder   bool `json:"folder"`
		Multiple bool `json:"multiple"`
	}
	if len(r.Payload) > 0 && string(r.Payload) != "null" {
		if err := json.Unmarshal(r.Payload, &p); err != nil {
			return fail(r, codeSchemaInvalid, "desktop.files.pick 参数无效", false)
		}
	}
	items, skipped, err := h.Pick(p.Folder, p.Multiple || !p.Folder)
	if errors.Is(err, ErrCanceled) {
		return bridge.Success(r.ID, map[string]any{"canceled": true, "items": []any{}})
	}
	if errors.Is(err, ErrUnavailable) {
		return fail(r, codeUnavailable, "系统没打开文件框，请再试一次。", false)
	}
	if err != nil {
		return fail(r, codeFailed, "系统没打开文件框，请再试一次。", false)
	}
	if len(items) > maxItems {
		items = items[:maxItems]
	}
	if len(skipped) > maxSkipped {
		skipped = skipped[:maxSkipped]
	}
	out := make([]map[string]any, 0, len(items))
	now := h.now()
	h.mu.Lock()
	if h.allow == nil {
		h.allow = map[string]time.Time{}
	}
	for _, item := range items {
		abs, err := normalizeRegularFile(item.Path)
		if err != nil {
			continue
		}
		h.allow[abs] = now.Add(allowTTL)
		out = append(out, map[string]any{
			"path":     abs,
			"fileName": item.FileName,
			"mime":     item.MIME,
			"size":     item.Size,
		})
	}
	h.mu.Unlock()
	named := make([]string, 0, len(skipped))
	for _, name := range skipped {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if len(name) > 256 {
			name = name[:256]
		}
		named = append(named, name)
		if len(named) >= maxSkipped {
			break
		}
	}
	return bridge.Success(r.ID, map[string]any{"canceled": false, "items": out, "skipped": named})
}

func (h *Handler) readChunk(r bridge.Request) bridge.Response {
	var p struct {
		Path   string `json:"path"`
		Offset int64  `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if json.Unmarshal(r.Payload, &p) != nil || strings.TrimSpace(p.Path) == "" || p.Offset < 0 || p.Limit < 1 || p.Limit > maxChunk {
		return fail(r, codeSchemaInvalid, "desktop.files.readChunk 参数无效", false)
	}
	abs, err := normalizeRegularFile(p.Path)
	if err != nil || !h.allowed(abs) {
		return fail(r, codeDenied, "不能读取未选择的文件", false)
	}
	f, err := os.Open(abs)
	if err != nil {
		return fail(r, codeFailed, "无法读取所选文件", true)
	}
	defer f.Close()
	if _, err := f.Seek(p.Offset, io.SeekStart); err != nil {
		return fail(r, codeFailed, "无法读取所选文件", true)
	}
	buf := make([]byte, p.Limit)
	n, err := io.ReadFull(f, buf)
	eof := false
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		eof = true
		buf = buf[:n]
		err = nil
	}
	if err != nil {
		return fail(r, codeFailed, "无法读取所选文件", true)
	}
	if n == 0 {
		eof = true
	}
	return bridge.Success(r.ID, map[string]any{
		"contentBase64": base64.StdEncoding.EncodeToString(buf),
		"nextOffset":    p.Offset + int64(len(buf)),
		"eof":           eof || int64(len(buf)) < int64(p.Limit),
	})
}

func (h *Handler) allowed(path string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	exp, ok := h.allow[path]
	if !ok {
		return false
	}
	if h.now().After(exp) {
		delete(h.allow, path)
		return false
	}
	return true
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func normalizeRegularFile(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", os.ErrInvalid
	}
	return abs, nil
}

func fail(r bridge.Request, code, message string, retryable bool) bridge.Response {
	return bridge.Failure(r.ID, r.TraceID, code, message, retryable)
}

var folderExt = map[string]string{
	".txt": "text/plain", ".md": "text/markdown", ".json": "application/json", ".csv": "text/csv",
	".html": "text/html", ".xml": "application/xml", ".js": "text/javascript", ".ts": "text/plain",
	".py": "text/plain", ".go": "text/plain", ".java": "text/plain", ".c": "text/plain",
	".cpp": "text/plain", ".rs": "text/plain", ".yaml": "text/yaml", ".yml": "text/yaml",
	".sh": "text/plain", ".sql": "text/plain",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp",
}

func itemFromPath(path string) (Item, error) {
	abs, err := normalizeRegularFile(path)
	if err != nil {
		return Item{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Item{}, err
	}
	if info.Size() > maxPickFileSize {
		return Item{}, os.ErrInvalid
	}
	ext := strings.ToLower(filepath.Ext(abs))
	mime := folderExt[ext]
	if mime == "" {
		mime = "application/octet-stream"
	}
	return Item{Path: abs, FileName: filepath.Base(abs), MIME: mime, Size: info.Size()}, nil
}

func listFolder(dir string) ([]Item, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	var items []Item
	var skipped []string
	for _, entry := range entries {
		if entry.IsDir() || !entry.Type().IsRegular() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if _, ok := folderExt[ext]; !ok {
			if len(skipped) < maxSkipped {
				skipped = append(skipped, entry.Name())
			}
			continue
		}
		if len(items) >= maxItems {
			continue
		}
		item, err := itemFromPath(filepath.Join(dir, entry.Name()))
		if err != nil {
			if len(skipped) < maxSkipped {
				skipped = append(skipped, entry.Name())
			}
			continue
		}
		items = append(items, item)
	}
	return items, skipped, nil
}
