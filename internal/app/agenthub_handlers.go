package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/agenthub"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/imagepreview"
)

func handleAgentHub(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.agentHub == nil {
		return r.Fail("FEATURE_DISABLED", "Agent 调度台尚未初始化", false)
	}
	switch r.Method {
	case "agentHub.detect":
		var p struct{}
		if decodePayload(r.Payload, &p) != nil {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.detect 参数无效", false)
		}
		return r.Ok(map[string]any{"agents": e.agentHub.Detect()})
	case "agentHub.dir.pick":
		var p struct{}
		if decodePayload(r.Payload, &p) != nil {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.dir.pick 参数无效", false)
		}
		path, err := e.agentHub.ChooseDir()
		if errors.Is(err, agenthub.ErrPickCanceled) {
			return r.Ok(map[string]any{"canceled": true, "path": ""})
		}
		if err != nil {
			return agentHubFailure(r, err)
		}
		return r.Ok(map[string]any{"canceled": false, "path": path})
	case "agentHub.task.start":
		var p agenthub.TaskRequest
		if decodePayload(r.Payload, &p) != nil {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.task.start 参数无效", false)
		}
		detail, err := e.agentHub.StartTask(p)
		if err != nil {
			return agentHubFailure(r, err)
		}
		return r.Ok(detail)
	case "agentHub.task.get":
		var p struct {
			TaskID string `json:"taskId"`
		}
		if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.TaskID) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.task.get 参数无效", false)
		}
		detail, err := e.agentHub.GetTask(p.TaskID)
		if err != nil {
			return agentHubFailure(r, err)
		}
		return r.Ok(detail)
	case "agentHub.task.cancel":
		var p struct {
			TaskID string `json:"taskId"`
		}
		if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.TaskID) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.task.cancel 参数无效", false)
		}
		detail, err := e.agentHub.Cancel(p.TaskID)
		if err != nil {
			return agentHubFailure(r, err)
		}
		return r.Ok(detail)
	case "agentHub.task.list":
		var p struct {
			Agent    string `json:"agent"`
			Status   string `json:"status"`
			DateFrom string `json:"dateFrom"`
			DateTo   string `json:"dateTo"`
		}
		if decodePayload(r.Payload, &p) != nil {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.task.list 参数无效", false)
		}
		listed, err := e.agentHub.List(agenthub.ListFilter{Agent: p.Agent, Status: p.Status, DateFrom: p.DateFrom, DateTo: p.DateTo})
		if err != nil {
			return agentHubFailure(r, err)
		}
		return r.Ok(listed)
	case "agentHub.artifact.list":
		var p struct {
			Agent    string `json:"agent"`
			DateFrom string `json:"dateFrom"`
			DateTo   string `json:"dateTo"`
			Ext      string `json:"ext"`
		}
		if decodePayload(r.Payload, &p) != nil {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.artifact.list 参数无效", false)
		}
		items, err := e.agentHub.Artifacts(agenthub.ListFilter{Agent: p.Agent, DateFrom: p.DateFrom, DateTo: p.DateTo, Ext: p.Ext})
		if err != nil {
			return agentHubFailure(r, err)
		}
		return r.Ok(map[string]any{"items": items})
	case "agentHub.inbox":
		var p struct {
			Action  string `json:"action"`
			WorkDir string `json:"workDir"`
			Name    string `json:"name"`
		}
		if decodePayload(r.Payload, &p) != nil || (p.Action != "files" && p.Action != "folder" && p.Action != "list" && p.Action != "drop") {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.inbox 参数无效", false)
		}
		canceled, dir, files, skipped, err := e.agentHub.Inbox(p.Action, p.WorkDir, p.Name)
		if errors.Is(err, agenthub.ErrPickCanceled) {
			canceled, err = true, nil
			files = []agenthub.InboxFile{}
		}
		if err != nil {
			return agentHubFailure(r, err)
		}
		if files == nil {
			files = []agenthub.InboxFile{}
		}
		payload := map[string]any{"canceled": canceled, "workDir": dir, "files": files}
		if len(skipped) > 0 {
			payload["skipped"] = skipped
		}
		return r.Ok(payload)
	case "agentHub.file.preview":
		return handleAgentHubPreview(e, ctx, r)
	case "agentHub.file.open":
		return handleAgentHubOpen(e, r)
	default:
		return r.Fail("BRIDGE_SCHEMA_INVALID", "未知的调度台方法", false)
	}
}

func handleAgentHubPreview(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		TaskID string `json:"taskId"`
		Path   string `json:"path"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.TaskID) || p.Path == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.file.preview 参数无效", false)
	}
	target, err := e.agentHub.ResolveFile(p.TaskID, p.Path)
	if err != nil {
		return agentHubFailure(r, err)
	}
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() {
		return r.Fail("ARTIFACT_NOT_FOUND", "产物文件不存在或不可读", false)
	}
	kind := strings.TrimPrefix(strings.ToLower(filepath.Ext(target)), ".")
	content, notice := "", ""
	switch kind {
	case "png", "jpg", "jpeg", "gif", "webp":
		kind = "image"
	case "md", "markdown":
		kind = "md"
	case "txt", "json", "csv", "ts", "tsx", "js", "jsx", "go", "py", "yaml", "yml", "css", "sql", "xml", "log":
		kind = "text"
	default:
		kind = "file"
	}
	if kind == "file" {
		notice = "请用本机软件打开查看完整内容"
	} else if info.Size() > 8<<20 {
		notice = "文件较大，请用本机软件打开查看完整内容"
	} else {
		data, readErr := os.ReadFile(target)
		if readErr != nil {
			return r.Fail("ARTIFACT_NOT_FOUND", "产物文件不存在或不可读", false)
		}
		if kind == "image" {
			content, err = imagepreview.Encode(ctx, data)
			if err != nil {
				notice = "无法生成预览，可用本机软件打开原文件"
				content = ""
			}
		} else {
			content = strings.ToValidUTF8(string(data), "�")
		}
	}
	if kind != "image" && len(content) > 256<<10 {
		runes := []rune(content)
		keep := len(runes) * (256 << 10) / len(content)
		content = string(runes[:keep]) + "…"
		notice = "仅展示部分内容，本机打开可查看全文"
	}
	return r.Ok(map[string]any{"kind": kind, "path": filepath.ToSlash(p.Path), "absolutePath": filepath.ToSlash(target), "size": info.Size(), "content": content, "notice": notice})
}

func handleAgentHubOpen(e *Engine, r bridge.Request) bridge.Response {
	var p struct {
		TaskID string `json:"taskId"`
		Path   string `json:"path"`
		Reveal bool   `json:"reveal"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.TaskID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.file.open 参数无效", false)
	}
	target, err := e.agentHub.OpenPath(p.TaskID, p.Path)
	if err != nil {
		return agentHubFailure(r, err)
	}
	info, statErr := os.Stat(target)
	isFile := statErr == nil && info.Mode().IsRegular()
	if err = openArtifactTarget(target, isFile, p.Reveal); err != nil {
		return r.Fail("ARTIFACT_OPEN_FAILED", "无法用本机打开该文件", false)
	}
	return r.Ok(map[string]any{"opened": filepath.ToSlash(target)})
}

func agentHubFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, agenthub.ErrNotAvailable):
		msg := err.Error()
		if i := strings.Index(msg, ": "); i >= 0 {
			msg = msg[i+2:]
		}
		if msg == "" || !strings.Contains(msg, "未") && !strings.ContainsAny(msg, "可用探测") {
			msg = "当前 Agent 不可用"
		}
		return r.Fail("AGENT_NOT_AVAILABLE", msg, false)
	case errors.Is(err, agenthub.ErrPathOutside):
		return r.Fail("PATH_OUTSIDE", "路径不在任务工作目录内", false)
	case errors.Is(err, agenthub.ErrNotFound):
		return r.Fail("NOT_FOUND", "任务或文件不存在", false)
	default:
		msg := err.Error()
		if !strings.ContainsAny(msg, "任务目录参数工作") {
			msg = "调度台操作失败"
		}
		return r.Fail("AGENT_HUB_FAILED", msg, false)
	}
}
