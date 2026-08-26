// workspace.artifactReview.* and workspace.artifact.preview: the P2-2
// artifact acceptance loop (comment → revise → accept) plus kind-aware
// preview of chat-pipeline artifacts. Reviews persist through the
// append-only artifactreview log; preview reads session-workspace bytes
// through the same containment-checked runtime path the tools use.
package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lunitide/lunitide/internal/artifactreview"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/officetools"
)

// artifactKindValid accepts the kinds the chat pipeline can emit as cards.
func artifactKindValid(kind string) bool {
	switch kind {
	case "html", "xlsx", "docx", "pptx", "pdf", "image":
		return true
	}
	return false
}

func handleWorkspaceArtifactReviewAppend(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.artifactReviews == nil {
		return bridge.Failure(r.ID, r.TraceID, "FEATURE_DISABLED", "产物评审存储未初始化", false)
	}
	var p struct {
		SessionID string `json:"sessionId"`
		CallID    string `json:"callId"`
		ToolName  string `json:"toolName"`
		Kind      string `json:"kind"`
		Path      string `json:"path"`
		Action    string `json:"action"`
		Note      string `json:"note"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "workspace.artifactReview.append 参数无效", false)
	}
	p.Path = filepath.ToSlash(filepath.Clean(strings.ReplaceAll(p.Path, "\\", "/")))
	review, err := e.artifactReviews.Append(p.SessionID, p.CallID, p.ToolName, p.Kind, p.Path, p.Action, p.Note)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "ARTIFACT_REVIEW_INVALID", "产物评审记录无效", false)
	}
	return bridge.Success(r.ID, review)
}

func handleWorkspaceArtifactReviewList(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.artifactReviews == nil {
		return bridge.Failure(r.ID, r.TraceID, "FEATURE_DISABLED", "产物评审存储未初始化", false)
	}
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "workspace.artifactReview.list 参数无效", false)
	}
	items, accepted, err := e.artifactReviews.ListBySession(p.SessionID)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "ARTIFACT_REVIEW_INVALID", "产物评审记录读取失败", false)
	}
	if items == nil {
		items = []artifactreview.Review{}
	}
	paths := make([]string, 0, len(accepted))
	for path := range accepted {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return bridge.Success(r.ID, map[string]any{"items": items, "acceptedPaths": paths})
}

// handleWorkspaceArtifactPreview answers a kind-aware text preview of one
// session-workspace artifact: xlsx → the ParseXLSX JSON grid, docx/pptx →
// extracted plain text, html → raw bounded content, pdf → size-only note.
func handleWorkspaceArtifactPreview(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.tools == nil {
		return bridge.Failure(r.ID, r.TraceID, "FEATURE_DISABLED", "工具运行时未初始化", false)
	}
	var p struct {
		SessionID string `json:"sessionId"`
		Path      string `json:"path"`
	}
	if decodePayload(r.Payload, &p) != nil || p.Path == "" || len(p.Path) > 512 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "workspace.artifact.preview 参数无效", false)
	}
	kind := strings.TrimPrefix(strings.ToLower(filepath.Ext(p.Path)), ".")
	if !artifactKindValid(kind) || kind == "pdf" {
		// pdf has no text extractor yet: still gated here so the schema and
		// handler stay in sync; the renderer offers export instead.
		return bridge.Failure(r.ID, r.TraceID, "ARTIFACT_PREVIEW_UNSUPPORTED", "该格式暂不支持内容预览", false)
	}
	data, err := e.tools.ReadWorkspaceFile(p.SessionID, p.Path, 8<<20)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "ARTIFACT_NOT_FOUND", "产物文件不存在或不可读", false)
	}
	var content string
	switch kind {
	case "xlsx":
		content, err = officetools.ParseXLSX(data)
	case "docx":
		content, err = officetools.ExtractDocxText(data)
	case "pptx":
		content, err = officetools.ExtractPptxText(data)
	case "html":
		if len(data) > 256<<10 {
			content = string(data[:256<<10]) + "…"
		} else {
			content = string(data)
		}
	}
	if err != nil {
		if errors.Is(err, officetools.ErrLimit) {
			return bridge.Failure(r.ID, r.TraceID, "ARTIFACT_PREVIEW_UNSUPPORTED", "产物超出预览限制", false)
		}
		return bridge.Failure(r.ID, r.TraceID, "ARTIFACT_PREVIEW_FAILED", "产物解析失败", false)
	}
	if len(content) > 256<<10 {
		runes := []rune(content)
		keep := len(runes) * (256 << 10) / len(content)
		content = string(runes[:keep]) + "…"
	}
	return bridge.Success(r.ID, map[string]any{"kind": kind, "path": filepath.ToSlash(p.Path), "size": len(data), "content": content})
}

// resolveExportDir maps a user-authorized export target to an absolute
// directory. desktop/downloads/documents resolve under the profile home
// (created on demand); anything else must already exist as an absolute
// directory the user explicitly granted.
func resolveExportDir(target string) (string, error) {
	trimmed := strings.TrimSpace(target)
	switch strings.ToLower(trimmed) {
	case "desktop", "downloads", "documents":
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", errors.New("home unavailable")
		}
		dir := filepath.Join(home, strings.ToUpper(trimmed[:1])+trimmed[1:])
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		return dir, nil
	}
	if !filepath.IsAbs(trimmed) {
		return "", errors.New("target must be a shortcut or absolute directory")
	}
	info, err := os.Stat(trimmed)
	if err != nil || !info.IsDir() {
		return "", errors.New("target directory missing")
	}
	return filepath.Clean(trimmed), nil
}

// handleWorkspaceArtifactExport copies one session-workspace artifact to a
// user-authorized destination (P2-4 交付落盘). The source is read through
// the same containment-checked runtime path the preview uses; overwrite is
// opt-in so deliveries never clobber silently.
func handleWorkspaceArtifactExport(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.tools == nil {
		return bridge.Failure(r.ID, r.TraceID, "FEATURE_DISABLED", "工具运行时未初始化", false)
	}
	var p struct {
		SessionID string `json:"sessionId"`
		Path      string `json:"path"`
		Target    string `json:"target"`
		Overwrite bool   `json:"overwrite"`
	}
	if decodePayload(r.Payload, &p) != nil || p.Path == "" || len(p.Path) > 512 || p.Target == "" || len(p.Target) > 400 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "workspace.artifact.export 参数无效", false)
	}
	p.Path = filepath.ToSlash(filepath.Clean(strings.ReplaceAll(p.Path, "\\", "/")))
	kind := strings.TrimPrefix(strings.ToLower(filepath.Ext(p.Path)), ".")
	if !artifactKindValid(kind) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "产物格式不支持导出", false)
	}
	dir, err := resolveExportDir(p.Target)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "ARTIFACT_EXPORT_TARGET_INVALID", "导出目录无效或不存在", false)
	}
	data, err := e.tools.ReadWorkspaceFile(p.SessionID, p.Path, 32<<20)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "ARTIFACT_NOT_FOUND", "产物文件不存在或不可读", false)
	}
	name := filepath.Base(strings.ReplaceAll(p.Path, "/", string(filepath.Separator)))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "产物文件名无效", false)
	}
	dest := filepath.Join(dir, name)
	if !p.Overwrite {
		if _, err := os.Stat(dest); err == nil {
			return bridge.Failure(r.ID, r.TraceID, "ARTIFACT_EXPORT_EXISTS", "目标目录已存在同名文件，需确认覆盖", false)
		}
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return bridge.Failure(r.ID, r.TraceID, "ARTIFACT_EXPORT_FAILED", "导出写入失败", false)
	}
	return bridge.Success(r.ID, map[string]any{"exportedPath": filepath.ToSlash(dest), "size": len(data)})
}
