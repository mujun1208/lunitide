// workspace.artifactReview.* and workspace.artifact.preview: the P2-2
// artifact acceptance loop (comment → revise → accept) plus kind-aware
// preview of chat-pipeline artifacts. Reviews persist through the
// append-only artifactreview log; preview reads session-workspace bytes
// through the same containment-checked runtime path the tools use.
package app

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lunitide/lunitide/internal/artifactreview"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/imagepreview"
	"github.com/lunitide/lunitide/internal/officetools"
)

// artifactKindValid accepts the kinds the chat pipeline can emit as cards.
func artifactKindValid(kind string) bool {
	switch kind {
	case "html", "xlsx", "docx", "pptx", "pdf", "image", "md", "txt":
		return true
	}
	return false
}

func handleWorkspaceArtifactReviewAppend(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.artifactReviews == nil {
		return r.Fail("FEATURE_DISABLED", "产物评审存储未初始化", false)
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
		return r.Fail("BRIDGE_SCHEMA_INVALID", "workspace.artifactReview.append 参数无效", false)
	}
	p.Path = filepath.ToSlash(filepath.Clean(strings.ReplaceAll(p.Path, "\\", "/")))
	review, err := e.artifactReviews.Append(p.SessionID, p.CallID, p.ToolName, p.Kind, p.Path, p.Action, p.Note)
	if err != nil {
		return r.Fail("ARTIFACT_REVIEW_INVALID", "产物评审记录无效", false)
	}
	return r.Ok(review)
}

func handleWorkspaceArtifactReviewList(e *Engine, _ context.Context, r bridge.Request) bridge.Response {
	if e.artifactReviews == nil {
		return r.Fail("FEATURE_DISABLED", "产物评审存储未初始化", false)
	}
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "workspace.artifactReview.list 参数无效", false)
	}
	items, accepted, err := e.artifactReviews.ListBySession(p.SessionID)
	if err != nil {
		return r.Fail("ARTIFACT_REVIEW_INVALID", "产物评审记录读取失败", false)
	}
	if items == nil {
		items = []artifactreview.Review{}
	}
	paths := make([]string, 0, len(accepted))
	for path := range accepted {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return r.Ok(map[string]any{"items": items, "acceptedPaths": paths})
}

// handleWorkspaceArtifactPreview answers a kind-aware text preview of one
// session-workspace artifact: xlsx → the ParseXLSX JSON grid, docx/pptx →
// extracted plain text, html → raw bounded content, pdf → size-only note.
func handleWorkspaceArtifactPreview(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	if e.tools == nil {
		return r.Fail("FEATURE_DISABLED", "工具运行时未初始化", false)
	}
	var p struct {
		SessionID string `json:"sessionId"`
		Path      string `json:"path"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SessionID) || p.Path == "" || len(p.Path) > 512 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "workspace.artifact.preview 参数无效", false)
	}
	target, err := e.tools.ResolveSessionArtifact(p.SessionID, p.Path)
	if err != nil {
		return r.Fail("ARTIFACT_NOT_FOUND", "产物文件不存在或不在当前授权目录中", false)
	}
	info, err := os.Stat(target)
	if err != nil || !info.Mode().IsRegular() {
		return r.Fail("ARTIFACT_NOT_FOUND", "产物文件不存在或不可读", false)
	}
	kind := strings.TrimPrefix(strings.ToLower(filepath.Ext(target)), ".")
	content, notice := "", ""
	switch kind {
	case "png", "jpg", "jpeg", "gif":
		kind = "image"
	case "html", "xlsx", "docx", "pptx", "pdf":
	case "txt", "md", "json", "csv", "ts", "tsx", "js", "jsx", "go", "py", "yaml", "yml", "css", "sql", "xml", "log":
		kind = "text"
	default:
		kind = "file"
	}
	if kind == "file" {
		notice = "请用本机软件打开查看完整内容"
	} else if info.Size() > 8<<20 {
		notice = "文件较大，请用本机软件打开查看完整内容"
	} else {
		data, readErr := e.tools.ReadWorkspaceFile(p.SessionID, p.Path, 8<<20)
		if readErr != nil {
			return r.Fail("ARTIFACT_NOT_FOUND", "产物文件不存在或不可读", false)
		}
		switch kind {
		case "xlsx":
			content, err = officetools.ParseXLSX(data)
		case "docx":
			content, err = officetools.ExtractDocxText(data)
		case "pptx":
			content, err = officetools.ExtractPptxText(data)
		case "html", "text":
			content = strings.ToValidUTF8(string(data), "�")
		case "image":
			content, err = imagepreview.Encode(ctx, data)
		case "pdf":
			if len(data) == 0 || len(data) > 4<<20 {
				notice = "请用本机软件打开查看完整内容"
			} else {
				content = base64.StdEncoding.EncodeToString(data)
			}
		}
		if err != nil {
			notice = "无法生成预览，可用本机软件打开原文件"
			content = ""
		}
	}
	if kind != "image" && kind != "pdf" && len(content) > 256<<10 {
		runes := []rune(content)
		keep := len(runes) * (256 << 10) / len(content)
		content = string(runes[:keep]) + "…"
		notice = "仅展示部分内容，本机打开可查看全文"
	}
	return r.Ok(map[string]any{"kind": kind, "path": filepath.ToSlash(p.Path), "absolutePath": filepath.ToSlash(target), "size": info.Size(), "content": content, "notice": notice})
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
		return r.Fail("FEATURE_DISABLED", "工具运行时未初始化", false)
	}
	var p struct {
		SessionID string `json:"sessionId"`
		Path      string `json:"path"`
		Target    string `json:"target"`
		Overwrite bool   `json:"overwrite"`
	}
	if decodePayload(r.Payload, &p) != nil || p.Path == "" || len(p.Path) > 512 || p.Target == "" || len(p.Target) > 400 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "workspace.artifact.export 参数无效", false)
	}
	p.Path = filepath.ToSlash(filepath.Clean(strings.ReplaceAll(p.Path, "\\", "/")))
	kind := strings.TrimPrefix(strings.ToLower(filepath.Ext(p.Path)), ".")
	if !artifactKindValid(kind) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "产物格式不支持导出", false)
	}
	dir, err := resolveExportDir(p.Target)
	if err != nil {
		return r.Fail("ARTIFACT_EXPORT_TARGET_INVALID", "导出目录无效或不存在", false)
	}
	data, err := e.tools.ReadWorkspaceFile(p.SessionID, p.Path, 32<<20)
	if err != nil {
		return r.Fail("ARTIFACT_NOT_FOUND", "产物文件不存在或不可读", false)
	}
	name := filepath.Base(strings.ReplaceAll(p.Path, "/", string(filepath.Separator)))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "产物文件名无效", false)
	}
	dest := filepath.Join(dir, name)
	if !p.Overwrite {
		if _, err := os.Stat(dest); err == nil {
			return r.Fail("ARTIFACT_EXPORT_EXISTS", "目标目录已存在同名文件，需确认覆盖", false)
		}
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return r.Fail("ARTIFACT_EXPORT_FAILED", "导出写入失败", false)
	}
	return r.Ok(map[string]any{"exportedPath": filepath.ToSlash(dest), "size": len(data)})
}
