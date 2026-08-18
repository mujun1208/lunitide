//go:build windows

package workspaceapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/bridge"
	"golang.org/x/sys/windows"
)

const maxPreviewBytes = 512 * 1024

type Handler struct {
	configPath string
	Select     func(context.Context) (string, error)
	mu         sync.Mutex
}
type rootConfig struct {
	Path string `json:"path"`
}

func New(configPath string) *Handler { return &Handler{configPath: configPath, Select: selectFolder} }

func (h *Handler) HandleHost(ctx context.Context, r bridge.Request) bridge.Response {
	switch bridge.Method(r.Method) {
	case bridge.MethodWorkspaceRootSelect:
		path, err := h.Select(ctx)
		if err != nil {
			return failure(r, "WORKSPACE_SELECTION_CANCELLED", "未选择工作区", false)
		}
		path, err = filepath.Abs(path)
		if err != nil || verifyRoot(path) != nil {
			return failure(r, "WORKSPACE_ROOT_INVALID", "工作区必须是本地普通目录", false)
		}
		data, _ := json.Marshal(rootConfig{Path: path})
		h.mu.Lock()
		err = atomicWrite(h.configPath, data)
		h.mu.Unlock()
		if err != nil {
			return failure(r, "WORKSPACE_ROOT_SAVE_FAILED", "无法保存工作区", true)
		}
		return bridge.Success(r.ID, map[string]any{"name": filepath.Base(path), "path": path})
	case bridge.MethodWorkspaceRootGet:
		root, err := h.root()
		if err != nil {
			return bridge.Success(r.ID, map[string]any{"name": "", "path": "", "bound": false})
		}
		return bridge.Success(r.ID, map[string]any{"name": filepath.Base(root), "path": root, "bound": true})
	case bridge.MethodWorkspaceList:
		var p struct {
			Path string `json:"path"`
		}
		if strictJSON(r.Payload, &p) != nil || len(p.Path) > 1024 {
			return failure(r, "BRIDGE_SCHEMA_INVALID", "目录参数无效", false)
		}
		root, err := h.root()
		if err != nil {
			return failure(r, "WORKSPACE_ROOT_REQUIRED", "请先选择工作区", false)
		}
		target, handles, err := pinnedPath(root, p.Path, true)
		if err != nil {
			return failure(r, "WORKSPACE_PATH_DENIED", "目录不在所选工作区内", false)
		}
		defer closeHandles(handles)
		final := handles[len(handles)-1]
		dir := os.NewFile(uintptr(final), target)
		handles = handles[:len(handles)-1]
		defer dir.Close()
		entries, err := dir.ReadDir(501)
		if err != nil {
			return failure(r, "WORKSPACE_LIST_FAILED", "无法读取目录", false)
		}
		type item struct {
			Name      string `json:"name"`
			Path      string `json:"path"`
			Directory bool   `json:"directory"`
		}
		items := make([]item, 0, min(500, len(entries)))
		for _, entry := range entries {
			if len(items) >= 500 {
				break
			}
			rel := filepath.ToSlash(filepath.Join(p.Path, entry.Name()))
			candidate, checkErr := safePath(root, rel, entry.IsDir())
			if checkErr != nil {
				continue
			}
			info, e := os.Lstat(candidate)
			if e != nil || info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			items = append(items, item{entry.Name(), rel, entry.IsDir()})
		}
		sort.Slice(items, func(i, j int) bool {
			return items[i].Directory && !items[j].Directory || items[i].Directory == items[j].Directory && strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
		})
		return bridge.Success(r.ID, map[string]any{"items": items, "truncated": len(entries) > 500})
	case bridge.MethodWorkspaceRead:
		var p struct {
			Path string `json:"path"`
		}
		if strictJSON(r.Payload, &p) != nil || p.Path == "" || len(p.Path) > 1024 {
			return failure(r, "BRIDGE_SCHEMA_INVALID", "文件参数无效", false)
		}
		root, err := h.root()
		if err != nil {
			return failure(r, "WORKSPACE_ROOT_REQUIRED", "请先选择工作区", false)
		}
		target, handles, err := pinnedPath(root, p.Path, false)
		if err != nil {
			return failure(r, "WORKSPACE_PATH_DENIED", "文件不在所选工作区内", false)
		}
		defer closeHandles(handles)
		final := handles[len(handles)-1]
		var info windows.ByHandleFileInformation
		if windows.GetFileInformationByHandle(final, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.NumberOfLinks != 1 {
			return failure(r, "WORKSPACE_FILE_UNSUPPORTED", "只支持单链接普通文件", false)
		}
		file := os.NewFile(uintptr(final), target)
		handles = handles[:len(handles)-1]
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, maxPreviewBytes+1))
		if err != nil {
			return failure(r, "WORKSPACE_READ_FAILED", "无法读取文件", false)
		}
		if len(data) > maxPreviewBytes {
			return failure(r, "WORKSPACE_PREVIEW_TOO_LARGE", "文件过大，无法预览", false)
		}
		if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
			return failure(r, "WORKSPACE_PREVIEW_BINARY", "二进制文件无法预览", false)
		}
		return bridge.Success(r.ID, map[string]any{"path": filepath.ToSlash(p.Path), "content": string(data), "size": len(data)})
	}
	return failure(r, "BRIDGE_METHOD_NOT_ALLOWED", "方法不受支持", false)
}

func strictJSON(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return errors.New("trailing json")
	}
	return nil
}
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".workspace-root-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
func (h *Handler) root() (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	data, err := os.ReadFile(h.configPath)
	if err != nil {
		return "", err
	}
	var c rootConfig
	if len(data) > 4096 || strictJSON(data, &c) != nil || c.Path == "" || len(c.Path) > 1024 {
		return "", errors.New("invalid config")
	}
	if err = verifyRoot(c.Path); err != nil {
		return "", err
	}
	return filepath.Clean(c.Path), nil
}
func verifyRoot(path string) error {
	clean := filepath.Clean(path)
	upper := strings.ToUpper(clean)
	if !filepath.IsAbs(clean) || strings.HasPrefix(clean, `\\`) || strings.HasPrefix(upper, `\\?\`) || strings.HasPrefix(upper, `\\.\`) {
		return errors.New("namespace root")
	}
	v, _ := windows.UTF16PtrFromString(filepath.VolumeName(clean) + `\`)
	if v == nil || windows.GetDriveType(v) != windows.DRIVE_FIXED {
		return errors.New("remote drive")
	}
	info, err := os.Lstat(clean)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid root")
	}
	attrs, err := windows.GetFileAttributes(windows.StringToUTF16Ptr(clean))
	if err != nil || attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("reparse root")
	}
	return nil
}
func safePath(root, relative string, wantDir bool) (string, error) {
	if len(relative) > 1024 || filepath.IsAbs(relative) || strings.Contains(relative, ":") {
		return "", errors.New("absolute")
	}
	clean := filepath.Clean(relative)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("traversal")
	}
	target := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("escape")
	}
	current := root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("link")
		}
		attrs, e := windows.GetFileAttributes(windows.StringToUTF16Ptr(current))
		if e != nil || attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return "", errors.New("reparse")
		}
	}
	info, err := os.Stat(target)
	if err != nil || wantDir && !info.IsDir() || !wantDir && !info.Mode().IsRegular() {
		return "", errors.New("type")
	}
	return target, nil
}
func pinnedPath(root, relative string, wantDir bool) (string, []windows.Handle, error) {
	target, err := safePath(root, relative, wantDir)
	if err != nil {
		return "", nil, err
	}
	rel, _ := filepath.Rel(root, target)
	parts := []string{"."}
	if rel != "." {
		parts = append(parts, strings.Split(rel, string(filepath.Separator))...)
	}
	handles := make([]windows.Handle, 0, len(parts))
	current := root
	for i, part := range parts {
		if part != "." {
			current = filepath.Join(current, part)
		}
		directory := i < len(parts)-1 || wantDir
		flags := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT)
		if directory {
			flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
		}
		ptr, e := windows.UTF16PtrFromString(current)
		if e != nil {
			closeHandles(handles)
			return "", nil, e
		}
		h, e := windows.CreateFile(ptr, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, flags, 0)
		if e != nil {
			closeHandles(handles)
			return "", nil, e
		}
		var info windows.ByHandleFileInformation
		if e = windows.GetFileInformationByHandle(h, &info); e != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			windows.CloseHandle(h)
			closeHandles(handles)
			return "", nil, errors.New("reparse component")
		}
		handles = append(handles, h)
	}
	return target, handles, nil
}
func closeHandles(h []windows.Handle) {
	for i := len(h) - 1; i >= 0; i-- {
		windows.CloseHandle(h[i])
	}
}
func failure(r bridge.Request, code, message string, retryable bool) bridge.Response {
	return bridge.Failure(r.ID, r.TraceID, code, message, retryable)
}
func selectFolder(ctx context.Context) (string, error) {
	// Use System.Windows.Forms.FolderBrowserDialog instead of Shell.Application.BrowseForFolder.
	// The latter passes 0 as the parent HWND, causing the dialog to appear behind the main window
	// on some Windows configurations (e.g. when the app is not the foreground window).
	// FolderBrowserDialog creates a proper modal dialog that stays on top of its owner.
	script := `
Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = '选择 Lunitide 工作区'
$d.ShowNewFolderButton = $true
if ($d.ShowDialog() -eq 'OK') {
    [Console]::OutputEncoding = [Text.Encoding]::UTF8
    $d.SelectedPath
}`
	out, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-STA", "-Command", script).Output()
	if err != nil {
		// Fallback: if .NET Forms is unavailable, try the legacy COM approach
		legacy := `$s=(New-Object -ComObject Shell.Application).BrowseForFolder(0,'选择 Lunitide 工作区',0,0);if($s){[Console]::OutputEncoding=[Text.Encoding]::UTF8;$s.Self.Path}`
		out2, err2 := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-STA", "-Command", legacy).Output()
		if err2 != nil {
			return "", err2
		}
		path := strings.TrimSpace(string(out2))
		if path == "" {
			return "", errors.New("cancelled")
		}
		return path, nil
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", errors.New("cancelled")
	}
	return path, nil
}
