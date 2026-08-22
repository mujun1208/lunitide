//go:build windows

package conversationsapp

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/winexec"
)

type HostHandler struct {
	Select func(context.Context) (string, error)
}

func NewHostHandler() *HostHandler {
	return &HostHandler{Select: selectFolder}
}

func (h *HostHandler) HandleHost(ctx context.Context, r bridge.Request) bridge.Response {
	switch bridge.Method(r.Method) {
	case bridge.MethodConversationsRootSelect:
		path, err := h.Select(ctx)
		if err != nil {
			return bridge.Failure(r.ID, r.TraceID, "CONVERSATIONS_SELECTION_CANCELLED", "未选择目录", false)
		}
		path, err = filepath.Abs(path)
		if err != nil || strings.TrimSpace(path) == "" {
			return bridge.Failure(r.ID, r.TraceID, "CONVERSATIONS_ROOT_INVALID", "目录无效", false)
		}
		return bridge.Success(r.ID, map[string]any{"path": path, "name": filepath.Base(path)})
	default:
		return bridge.Failure(r.ID, r.TraceID, "METHOD_NOT_FOUND", "未知方法", false)
	}
}

func selectFolder(ctx context.Context) (string, error) {
	script := `
Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = '选择对话与产物存储目录'
$d.ShowNewFolderButton = $true
if ($d.ShowDialog() -eq 'OK') {
    [Console]::OutputEncoding = [Text.Encoding]::UTF8
    $d.SelectedPath
}`
	out, err := winexec.HiddenPowerShell(ctx, "-NoProfile", "-STA", "-Command", script).Output()
	if err != nil {
		legacy := `$s=(New-Object -ComObject Shell.Application).BrowseForFolder(0,'选择对话与产物存储目录',0,0);if($s){[Console]::OutputEncoding=[Text.Encoding]::UTF8;$s.Self.Path}`
		out2, err2 := winexec.HiddenPowerShell(ctx, "-NoProfile", "-STA", "-Command", legacy).Output()
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
