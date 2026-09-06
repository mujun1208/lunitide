//go:build windows

package credentialsubmission

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/oklog/ulid/v2"
	"golang.org/x/sys/windows"
)

type mcpCredentialRequest struct {
	EndpointID      string `json:"endpointId"`
	ExpectedVersion int64  `json:"expectedVersion"`
	Env             string `json:"env,omitempty"`
	Credential      string `json:"credential,omitempty"`
	Remove          bool   `json:"remove,omitempty"`
	RequestID       string `json:"requestId"`
}
type mcpCredentialTarget struct {
	EndpointID      string            `json:"endpointId"`
	RuntimeID       string            `json:"runtimeId"`
	Transport       string            `json:"transport"`
	URL             string            `json:"url"`
	Command         string            `json:"command"`
	Args            []string          `json:"args"`
	AuthRef         string            `json:"authRef"`
	EnvRefs         map[string]string `json:"envRefs"`
	SecurityVersion int64             `json:"securityVersion"`
	State           string            `json:"state"`
}

func (t mcpCredentialTarget) endpoint() *mcp6.Endpoint {
	return &mcp6.Endpoint{ID: t.RuntimeID, Transport: t.Transport, URL: t.URL, Command: t.Command, Args: t.Args}
}
func (t mcpCredentialTarget) ref(env string) string {
	if env == "" {
		return t.AuthRef
	}
	return t.EnvRefs[env]
}

type mcpCredentialJournal struct {
	Request   mcpCredentialRequest `json:"request"`
	Binding   secret.Ref           `json:"binding"`
	Previous  secret.Ref           `json:"previous"`
	ExpiresAt time.Time            `json:"expiresAt"`
	Committed bool                 `json:"committed"`
	Version   int64                `json:"version"`
}

var mcpRequestID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

func (h *HostHandler) mcpTarget(ctx context.Context, id string) (mcpCredentialTarget, error) {
	resp, err := (RPCResolver{Engine: h.Engine}).call(ctx, "internal.mcp.credential.resolve", map[string]any{"endpointId": id})
	if err != nil {
		return mcpCredentialTarget{}, err
	}
	b, err := json.Marshal(resp.Payload)
	var out mcpCredentialTarget
	if err != nil || json.Unmarshal(b, &out) != nil || out.EndpointID != id || out.RuntimeID == "" {
		return out, errors.New("MCP target unavailable")
	}
	return out, nil
}

func (h *HostHandler) setMcpCredential(ctx context.Context, r bridge.Request) bridge.Response {
	fail := func(code, msg string, retry bool) bridge.Response {
		return bridge.Failure(r.ID, r.TraceID, code, msg, retry)
	}
	var p mcpCredentialRequest
	if h.Coordinator == nil || h.Secrets == nil || h.Engine == nil || decodeStrictLocal(r.Payload, &p) != nil || !mcpRequestID.MatchString(p.RequestID) || p.ExpectedVersion < 0 || p.EndpointID == "" || len(p.Credential) > 16384 || (!p.Remove && p.Credential == "") || (p.Remove && p.Credential != "") {
		return fail("MCP_CREDENTIAL_INVALID", "MCP 凭据参数无效", false)
	}
	r.Payload = nil
	value := []byte(p.Credential)
	p.Credential = ""
	defer secret.Zero(value)
	if strings.ContainsAny(string(value), "\x00\r\n") {
		return fail("MCP_CREDENTIAL_INVALID", "凭据包含不允许的控制字符", false)
	}
	refs := map[string]string{}
	if p.Env != "" {
		refs[p.Env] = "secretref:validation"
	}
	if m7app.ValidateMcpSecretRefs("", refs) != nil {
		return fail("MCP_CREDENTIAL_INVALID", "环境变量名称不允许用于凭据", false)
	}
	h.mcpMu.Lock()
	defer h.mcpMu.Unlock()
	target, err := h.mcpTarget(ctx, p.EndpointID)
	if err != nil {
		return fail("MCP_CREDENTIAL_UNAVAILABLE", "无法读取 MCP 配置", true)
	}
	if target.State == "revoked" || (target.Transport == "https" && p.Env != "") || (target.Transport == "stdio" && p.Env == "") {
		return fail("MCP_CREDENTIAL_INVALID", "凭据类型与 MCP 不匹配", false)
	}
	name := "mcp-credential-" + p.RequestID + ".json"
	entry, err := h.readMcpJournal(name)
	if errors.Is(err, os.ErrNotExist) {
		files, listErr := os.ReadDir(h.Coordinator.root.Path())
		if listErr != nil {
			return fail("MCP_CREDENTIAL_UNCERTAIN", "无法读取凭据记录", true)
		}
		count := 0
		for _, f := range files {
			if strings.HasPrefix(f.Name(), "mcp-credential-") {
				count++
			}
		}
		if count >= 1024 {
			return fail("MCP_CREDENTIAL_BUSY", "凭据变更记录正在清理，请稍后重试", true)
		}
		if target.SecurityVersion != p.ExpectedVersion {
			return fail("MCP_CREDENTIAL_CONFLICT", "MCP 配置已变化，请刷新", false)
		}
		ref := ""
		if !p.Remove {
			ref = "secretref:mcp/" + target.RuntimeID + "/" + ulid.Make().String()
		}
		binding, bindingErr := mcp6.CredentialBinding(target.endpoint(), ref)
		if p.Remove {
			binding, bindingErr = mcp6.CredentialBinding(target.endpoint(), "secretref:unused")
		}
		if bindingErr != nil {
			return fail("MCP_CREDENTIAL_INVALID", "MCP 身份无效", false)
		}
		binding.CredentialRef = ref
		confirm := h.McpConfirm
		if confirm == nil {
			confirm = confirmMcpCredentialNative
		}
		accepted, confirmErr := confirm(ctx, RevealTarget{ProviderID: binding.ProviderID, Origin: binding.Origin, Protocol: target.Transport}, p.Env, p.Remove)
		if confirmErr != nil || !accepted {
			return fail("MCP_CREDENTIAL_DENIED", "未确认 MCP 凭据变更", false)
		}
		entry = mcpCredentialJournal{Request: p, Binding: binding, ExpiresAt: time.Now().Add(10 * time.Minute)}
		if previous := target.ref(p.Env); previous != "" {
			entry.Previous, _ = mcp6.CredentialBinding(target.endpoint(), previous)
		}
		if err = h.saveMcpJournal(name, entry); err != nil {
			return fail("MCP_CREDENTIAL_UNCERTAIN", "凭据变更记录尚未保存", true)
		}
	} else if err != nil {
		return fail("MCP_CREDENTIAL_UNCERTAIN", "凭据变更记录无法读取", true)
	}
	if entry.Request != p {
		return fail("MCP_CREDENTIAL_CONFLICT", "此操作标识已用于其他凭据请求", false)
	}
	if !entry.Committed && time.Now().After(entry.ExpiresAt) {
		return fail("MCP_CREDENTIAL_EXPIRED", "凭据操作已过期，请重新发起", false)
	}
	if !p.Remove {
		readErr := h.Secrets.WithSecret(ctx, entry.Binding, func(saved []byte) error {
			if subtle.ConstantTimeCompare(saved, value) != 1 {
				return ErrConflict
			}
			return nil
		})
		if errors.Is(readErr, os.ErrNotExist) && !entry.Committed {
			readErr = h.Secrets.Put(ctx, entry.Binding, value)
		}
		if readErr != nil {
			return fail("MCP_CREDENTIAL_STORE_FAILED", "凭据保存失败或重试内容不同", false)
		}
	}
	current := target.ref(p.Env)
	if entry.Committed && current != entry.Binding.CredentialRef {
		return fail("MCP_CREDENTIAL_CONFLICT", "此凭据已被后续操作替换，请刷新", false)
	}
	if !entry.Committed && target.SecurityVersion >= p.ExpectedVersion+1 && current == entry.Binding.CredentialRef {
		entry.Committed = true
		entry.Version = target.SecurityVersion
	}
	if !entry.Committed {
		resp, bindErr := (RPCResolver{Engine: h.Engine}).call(ctx, "internal.mcp.credential.bind", map[string]any{"endpointId": p.EndpointID, "expectedVersion": p.ExpectedVersion, "ref": entry.Binding.CredentialRef, "env": p.Env})
		if bindErr != nil || !resp.OK {
			h.ScheduleCleanup()
			return fail("MCP_CREDENTIAL_UNCERTAIN", "凭据绑定结果待确认，请重试此操作", true)
		}
		entry.Committed = true
		entry.Version = p.ExpectedVersion + 1
	}
	if err = h.saveMcpJournal(name, entry); err != nil {
		return fail("MCP_CREDENTIAL_UNCERTAIN", "凭据已提交，确认记录待恢复", true)
	}
	h.ScheduleCleanup()
	return bridge.Success(r.ID, map[string]any{"configured": !p.Remove, "securityVersion": max(entry.Version, target.SecurityVersion)})
}

func (h *HostHandler) readMcpJournal(name string) (mcpCredentialJournal, error) {
	var entry mcpCredentialJournal
	path, err := h.Coordinator.root.FilePath(name)
	if err != nil {
		return entry, err
	}
	if err = h.Coordinator.root.ProtectRegularFile(name); err != nil {
		return entry, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return entry, err
	}
	if info.Size() > 16384 {
		return entry, errors.New("MCP journal too large")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return entry, err
	}
	if err = decodeStrictLocal(b, &entry); err != nil {
		return entry, err
	}
	if !mcpRequestID.MatchString(entry.Request.RequestID) || entry.Request.Credential != "" || entry.Request.EndpointID == "" || entry.ExpiresAt.IsZero() {
		return entry, errors.New("invalid MCP credential journal")
	}
	return entry, nil
}

func (h *HostHandler) saveMcpJournal(name string, entry mcpCredentialJournal) error {
	b, err := json.Marshal(entry)
	if err != nil || len(b) > 16384 {
		return errors.New("invalid MCP credential journal")
	}
	root := h.Coordinator.root
	path, err := root.FilePath(name)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(root.Path(), ".mcp-credential-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.Write(b)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = root.ProtectRegularFile(filepath.Base(tmpName)); err != nil {
		return err
	}
	from, _ := windows.UTF16PtrFromString(tmpName)
	to, _ := windows.UTF16PtrFromString(path)
	if err = windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return err
	}
	return root.ProtectRegularFile(name)
}

func (h *HostHandler) ReconcileMcpCredentials(ctx context.Context) error {
	if h.Coordinator == nil || h.Secrets == nil || h.Engine == nil {
		return nil
	}
	h.mcpMu.Lock()
	defer h.mcpMu.Unlock()
	entries, err := os.ReadDir(h.Coordinator.root.Path())
	if err != nil {
		return err
	}
	processed := 0
	matched := false
	for _, file := range entries {
		if file.IsDir() || !strings.HasPrefix(file.Name(), "mcp-credential-") || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		if file.Name() <= h.mcpCleanupCursor {
			continue
		}
		matched = true
		if processed >= 256 {
			break
		}
		h.mcpCleanupCursor = file.Name()
		processed++
		entry, err := h.readMcpJournal(file.Name())
		if err != nil {
			return err
		}
		target, err := h.mcpTarget(ctx, entry.Request.EndpointID)
		if err != nil {
			return err
		}
		current := target.ref(entry.Request.Env)
		if current == entry.Binding.CredentialRef && target.SecurityVersion >= entry.Request.ExpectedVersion+1 {
			entry.Committed = true
			entry.Version = entry.Request.ExpectedVersion + 1
		}
		if entry.Committed {
			if entry.Previous.CredentialRef != "" && entry.Previous.CredentialRef != current {
				if err = h.Secrets.Delete(ctx, entry.Previous); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
				entry.Previous = secret.Ref{}
			}
			if err = h.saveMcpJournal(file.Name(), entry); err != nil {
				return err
			}
		} else if time.Now().After(entry.ExpiresAt) || target.SecurityVersion > entry.Request.ExpectedVersion {
			if entry.Binding.CredentialRef != "" && entry.Binding.CredentialRef != current {
				if err = h.Secrets.Delete(ctx, entry.Binding); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			path, _ := h.Coordinator.root.FilePath(file.Name())
			if err = os.Remove(path); err != nil {
				return err
			}
		}
		if entry.Committed && time.Now().After(entry.ExpiresAt.Add(7*24*time.Hour)) {
			path, _ := h.Coordinator.root.FilePath(file.Name())
			if err = os.Remove(path); err != nil {
				return err
			}
		}
	}
	if !matched || processed < 256 {
		h.mcpCleanupCursor = ""
	}
	return nil
}

func confirmMcpCredentialNative(ctx context.Context, target RevealTarget, env string, remove bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	revealConfirmationMu.Lock()
	defer revealConfirmationMu.Unlock()
	action := "保存或轮换凭据"
	if remove {
		action = "撤销凭据"
	}
	use := "HTTPS Bearer"
	if env != "" {
		use = "进程环境变量 " + env
	}
	message := fmt.Sprintf("允许此 MCP %s？\n\n目标：%s\n端点：%s\n用途：%s\n\n凭据仅供此端点使用。", action, target.Origin, target.ProviderID, use)
	text, err := syscall.UTF16PtrFromString(message)
	if err != nil {
		return false, err
	}
	title, _ := syscall.UTF16PtrFromString("确认 MCP 凭据变更")
	result, _, callErr := messageBoxW.Call(0, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(title)), messageBoxYesNo|messageBoxIconWarning|messageBoxDefaultNo|messageBoxTaskModal|messageBoxSetForeground)
	if result == 0 {
		return false, callErr
	}
	return result == messageBoxResultYes, nil
}
