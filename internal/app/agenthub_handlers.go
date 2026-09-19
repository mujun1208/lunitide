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
		return r.Fail("FEATURE_DISABLED", "AgentHub 尚未初始化", false)
	}
	switch r.Method {
	case "agentHub.detect":
		var p struct{}
		if decodePayload(r.Payload, &p) != nil {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.detect 参数无效", false)
		}
		return r.Ok(map[string]any{"agents": e.agentHub.Detect()})
	case "agentHub.install":
		var p struct {
			Name      string `json:"name"`
			Confirmed bool   `json:"confirmed"`
		}
		if decodePayload(r.Payload, &p) != nil || (p.Name != "codex" && p.Name != "cursor" && p.Name != "kimi") {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.install 参数无效", false)
		}
		result, err := e.agentHub.Install(context.WithoutCancel(ctx), p.Name, p.Confirmed)
		if err != nil {
			return agentHubFailure(r, err)
		}
		return r.Ok(result)
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
	case "agentHub.thread.create":
		var p agenthub.ThreadCreateRequest
		if decodePayload(r.Payload, &p) != nil || p.HarnessID == "" || p.Scene == "" {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.thread.create 参数无效", false)
		}
		if p.ProjectID != "" {
			if !validCanonicalULID(p.ProjectID) || !projectServiceAvailable(e.projects) {
				return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.thread.create 参数无效", false)
			}
			proj, err := e.projects.Get(ctx, p.ProjectID)
			if err != nil {
				return projectFailure(r, err)
			}
			if strings.TrimSpace(proj.RootPath) == "" {
				return r.Fail("PROJECT_ROOT_REQUIRED", "请先补选项目根目录", false)
			}
			p.WorkspaceRoot = proj.RootPath
		}
		detail, err := e.agentHub.CreateThread(p)
		if err != nil {
			return agentHubFailure(r, err)
		}
		return r.Ok(e.ensureHubSession(ctx, detail))
	case "agentHub.thread.get":
		var p struct {
			ThreadID string `json:"threadId"`
		}
		if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ThreadID) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.thread.get 参数无效", false)
		}
		detail, err := e.agentHub.GetThread(p.ThreadID)
		if err != nil {
			return agentHubFailure(r, err)
		}
		return r.Ok(e.ensureHubSession(ctx, detail))
	case "agentHub.thread.list":
		var p struct {
			HarnessID string `json:"harnessId"`
		}
		if decodePayload(r.Payload, &p) != nil {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.thread.list 参数无效", false)
		}
		items, err := e.agentHub.ListThreads(p.HarnessID)
		if err != nil {
			return agentHubFailure(r, err)
		}
		return r.Ok(map[string]any{"items": items})
	case "agentHub.thread.update":
		var p struct {
			ThreadID      string `json:"threadId"`
			Title         string `json:"title"`
			Pinned        *bool  `json:"pinned"`
			WorkspaceRoot string `json:"workspaceRoot"`
			AccessMode    string `json:"accessMode"`
			ExportDir     string `json:"exportDir"`
			Scene         string `json:"scene"`
		}
		if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ThreadID) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.thread.update 参数无效", false)
		}
		detail, err := e.agentHub.UpdateThread(p.ThreadID, p.Title, p.Pinned, p.WorkspaceRoot, p.AccessMode, p.ExportDir, p.Scene)
		if err != nil {
			return agentHubFailure(r, err)
		}
		return r.Ok(detail)
	case "agentHub.thread.delete":
		var p struct {
			ThreadID string `json:"threadId"`
		}
		if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ThreadID) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.thread.delete 参数无效", false)
		}
		if err := e.agentHub.DeleteThread(p.ThreadID); err != nil {
			return agentHubFailure(r, err)
		}
		return r.Ok(map[string]any{"ok": true})
	case "agentHub.thread.cancel":
		var p struct {
			ThreadID string `json:"threadId"`
		}
		if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ThreadID) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.thread.cancel 参数无效", false)
		}
		detail, err := e.agentHub.CancelThread(p.ThreadID)
		if err != nil {
			return agentHubFailure(r, err)
		}
		return r.Ok(detail)
	case "agentHub.thread.prompt":
		var p struct {
			ThreadID string `json:"threadId"`
			Text     string `json:"text"`
		}
		if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ThreadID) || strings.TrimSpace(p.Text) == "" {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.thread.prompt 参数无效", false)
		}
		detail, err := e.agentHub.PromptThread(p.ThreadID, p.Text)
		if err != nil {
			return agentHubFailure(r, err)
		}
		detail = e.ensureHubSession(ctx, detail)
		e.projectHubThread(ctx, detail)
		return r.Ok(detail)
	case "agentHub.thread.respond":
		var p struct {
			ThreadID string `json:"threadId"`
			CallID   string `json:"callId"`
			OptionID string `json:"optionId"`
			Text     string `json:"text"`
		}
		if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ThreadID) || p.CallID == "" || p.OptionID == "" {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.thread.respond 参数无效", false)
		}
		detail, err := e.agentHub.RespondThread(p.ThreadID, p.CallID, p.OptionID, p.Text)
		if err != nil {
			return agentHubFailure(r, err)
		}
		detail = e.ensureHubSession(ctx, detail)
		e.projectHubThread(ctx, detail)
		return r.Ok(detail)
	case "agentHub.workspace.list":
		var p struct {
			ThreadID     string `json:"threadId"`
			RelativePath string `json:"relativePath"`
		}
		if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ThreadID) {
			return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.workspace.list 参数无效", false)
		}
		items, err := e.agentHub.ListWorkspace(p.ThreadID, p.RelativePath)
		if err != nil {
			return agentHubFailure(r, err)
		}
		return r.Ok(map[string]any{"items": items})
	case "agentHub.file.preview":
		return handleAgentHubPreview(e, ctx, r)
	case "agentHub.file.open":
		return handleAgentHubOpen(e, r)
	default:
		return r.Fail("BRIDGE_SCHEMA_INVALID", "未知的 AgentHub 方法", false)
	}
}

func handleAgentHubPreview(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		TaskID   string `json:"taskId"`
		ThreadID string `json:"threadId"`
		Path     string `json:"path"`
	}
	if decodePayload(r.Payload, &p) != nil || p.Path == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.file.preview 参数无效", false)
	}
	hasTask := validCanonicalULID(p.TaskID)
	hasThread := validCanonicalULID(p.ThreadID)
	if hasTask == hasThread {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.file.preview 参数无效", false)
	}
	var target string
	var err error
	if hasTask {
		target, err = e.agentHub.ResolveFile(p.TaskID, p.Path)
	} else {
		target, err = e.agentHub.ResolveThreadFile(p.ThreadID, p.Path)
	}
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
		TaskID   string `json:"taskId"`
		ThreadID string `json:"threadId"`
		Path     string `json:"path"`
		Reveal   bool   `json:"reveal"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.file.open 参数无效", false)
	}
	hasTask := validCanonicalULID(p.TaskID)
	hasThread := validCanonicalULID(p.ThreadID)
	if hasTask == hasThread {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "agentHub.file.open 参数无效", false)
	}
	var target string
	var err error
	if hasTask {
		target, err = e.agentHub.OpenPath(p.TaskID, p.Path)
	} else {
		target, err = e.agentHub.OpenThreadPath(p.ThreadID, p.Path)
	}
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
	case errors.Is(err, agenthub.ErrNoOpenPrompt), errors.Is(err, agenthub.ErrCallIDMismatch):
		return r.Fail("AGENT_HUB_FAILED", "没有待回答的提问或 callId 不匹配", false)
	case errors.Is(err, agenthub.ErrThreadBusy):
		return r.Fail("AGENT_HUB_FAILED", "当前对话正在等待回答", false)
	default:
		return r.Fail("AGENT_HUB_FAILED", agentHubUserMessage(err), false)
	}
}

func agentHubUserMessage(err error) string {
	if err == nil {
		return "AgentHub 操作失败"
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "AgentHub 操作失败"
	}
	if strings.Contains(msg, "无法解析为 node") {
		return "已安装 CLI，但还不能启动对话。需要本机 Node.js，或把 CLI 配成可直接运行的程序。"
	}
	if strings.Contains(msg, "会话未打开") {
		return "会话还没打开，请再发一次。"
	}
	if containsHan(msg) {
		return msg
	}
	mapped := mapAgentHubEnglish(msg)
	if mapped != "" {
		return mapped
	}
	return "对话没发出去：" + clipAgentHubDetail(msg)
}

func mapAgentHubEnglish(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "enoent"), strings.Contains(lower, "not found"), strings.Contains(msg, "未找到"):
		return "对话没发出去：本机没找到对应的 Agent 程序。"
	case strings.Contains(lower, "eacces"), strings.Contains(lower, "permission denied"):
		return "对话没发出去：没有权限启动 Agent 程序。"
	case strings.Contains(lower, "timed out"), strings.Contains(lower, "timeout"), strings.Contains(lower, "deadline"):
		return "对话没发出去：Agent 启动或握手超时，请再试一次。"
	case strings.Contains(lower, "spawn"), strings.Contains(lower, "exec format"):
		return "对话没发出去：Agent 程序无法启动。"
	}
	return ""
}

func clipAgentHubDetail(msg string) string {
	msg = strings.TrimSpace(msg)
	runes := []rune(msg)
	if len(runes) > 160 {
		return string(runes[:160]) + "…"
	}
	return msg
}

func containsHan(s string) bool {
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}
