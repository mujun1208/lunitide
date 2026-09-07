package mcp

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
)

var ErrCredentialRequired = errors.New("mcp: configure required credentials before connecting")

type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type DiagnosticError struct {
	Code  string
	Cause error
}

func (e *DiagnosticError) Error() string { return "mcp: " + e.Code }
func (e *DiagnosticError) Unwrap() error { return e.Cause }

// ConnectionDiagnostic exposes only controlled text. Remote errors and child
// diagnostics can contain secrets; no raw message, path or URL reaches the UI.
func ConnectionDiagnostic(err error) Diagnostic {
	if err == nil {
		return Diagnostic{}
	}
	code := "MCP_CONNECT_FAILED"
	var typed *DiagnosticError
	var network net.Error
	switch {
	case errors.As(err, &typed):
		code = typed.Code
	case errors.Is(err, ErrCredentialRequired):
		code = "MCP_CREDENTIAL_REQUIRED"
	case errors.Is(err, context.DeadlineExceeded):
		code = "MCP_CONNECT_TIMEOUT"
	case errors.Is(err, context.Canceled):
		code = "MCP_CONNECT_CANCELED"
	case errors.Is(err, ErrLaunchLock):
		code = "MCP_PACKAGE_VERSION"
	case errors.As(err, &network):
		code = "MCP_NETWORK_FAILED"
	case errors.Is(err, ErrStdioLaunch):
		code = "MCP_RUNTIME_UNAVAILABLE"
	case errors.Is(err, ErrStdioProtocol), errors.Is(err, ErrRemoteProtocol):
		code = "MCP_PROTOCOL_FAILED"
	}
	messages := map[string]string{
		"MCP_CONNECT_FAILED":      "未能建立连接。请重新连接查看当前结果；检查服务器启动配置及所需凭据。",
		"MCP_CREDENTIAL_REQUIRED": "尚未配置此服务所需凭据。请先完成官方授权，再通过“凭据”配置后重新连接。",
		"MCP_CONNECT_TIMEOUT":     "连接超时。首次启动可能仍在下载运行环境或依赖；请检查网络后重新连接。",
		"MCP_CONNECT_CANCELED":    "连接已取消，可以重新连接。",
		"MCP_PACKAGE_VERSION":     "无法锁定软件包版本，请核对包名及版本，并检查 npm 或 PyPI 访问。",
		"MCP_PACKAGE_NOT_FOUND":   "软件源中找不到此包或版本，请核对启动配置，或使用市场中对应的最新配置。",
		"MCP_NETWORK_FAILED":      "软件源或服务器网络访问失败，请检查网络、代理及证书设置后重新连接。",
		"MCP_RUNTIME_UNAVAILABLE": "无法启动本地运行环境，请检查 Node.js / npx 或 uv / uvx 是否可用。",
		"MCP_DEPENDENCY_FAILED":   "本地 Python 或软件依赖未能准备完成，请检查运行环境版本及软件源后重新连接。",
		"MCP_PROTOCOL_FAILED":     "服务器握手或工具目录响应不符合支持的 MCP 协议，请检查启动配置及服务器版本。",
	}
	message, ok := messages[code]
	if !ok {
		code = "MCP_CONNECT_FAILED"
		message = messages[code]
	}
	return Diagnostic{Code: code, Message: message}
}

// stderrClassifier remembers only a bounded rolling window and a category;
// raw child output is neither persisted nor included in the returned error.
type stderrClassifier struct {
	mu   sync.Mutex
	tail string
	code string
}

func (w *stderrClassifier) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	const limit = 8192
	if len(p) > limit {
		p = p[len(p)-limit:]
	}
	w.tail += strings.ToLower(string(p))
	if len(w.tail) > limit {
		w.tail = w.tail[len(w.tail)-limit:]
	}
	switch {
	case strings.Contains(w.tail, "credentials not found"), strings.Contains(w.tail, "invalid_grant"):
		w.code = "MCP_CREDENTIAL_REQUIRED"
	case strings.Contains(w.tail, "e404"), strings.Contains(w.tail, "no matching version found"):
		w.code = "MCP_PACKAGE_NOT_FOUND"
	case strings.Contains(w.tail, "enotfound"), strings.Contains(w.tail, "etimedout"), strings.Contains(w.tail, "econnreset"), strings.Contains(w.tail, "certificate verify failed"), strings.Contains(w.tail, "failed to download"):
		w.code = "MCP_NETWORK_FAILED"
	case strings.Contains(w.tail, "no solution found"), strings.Contains(w.tail, "requires-python"), strings.Contains(w.tail, "modulenotfounderror"), strings.Contains(w.tail, "failed to build"):
		w.code = "MCP_DEPENDENCY_FAILED"
	}
	return n, nil
}
func (w *stderrClassifier) classify(err error) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.tail = ""
	if w.code != "" {
		return &DiagnosticError{Code: w.code, Cause: err}
	}
	return err
}
