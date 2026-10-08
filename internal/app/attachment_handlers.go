package app

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/attachment"
)

// attachmentDTO is the JSON-serializable view of an attachment.
// The parsed_text field is only included in attachment.get responses;
// list responses expose parsed_text_bytes for budget estimation without
// transferring the full text.
type attachmentDTO struct {
	AttachmentID    string    `json:"attachmentId"`
	ProjectID       string    `json:"projectId"`
	SessionID       string    `json:"sessionId,omitempty"`
	OriginalName    string    `json:"originalName"`
	MIME            string    `json:"mime"`
	Size            int64     `json:"size"`
	SHA256          string    `json:"sha256"`
	ParseStatus     string    `json:"parseStatus"`
	ParseErrorCode  string    `json:"parseErrorCode"`
	ParsedTextBytes int64     `json:"parsedTextBytes"`
	CreatedAt       time.Time `json:"createdAt"`
}

func newAttachmentDTO(a attachment.Attachment) attachmentDTO {
	return attachmentDTO{
		AttachmentID:    a.ID,
		ProjectID:       a.ProjectID,
		SessionID:       a.SessionID,
		OriginalName:    a.OriginalName,
		MIME:            a.MIME,
		Size:            a.Size,
		SHA256:          a.SHA256,
		ParseStatus:     string(a.ParseStatus),
		ParseErrorCode:  a.ParseErrorCode,
		ParsedTextBytes: a.ParsedTextBytes,
		CreatedAt:       a.CreatedAt,
	}
}

// handleAttachmentIngest ingests a user-supplied file: validates content,
// writes to the controlled data directory, creates the metadata record, and
// parses text (ADR-005 §7: attachment isolation). The file content is sent
// as base64 within the bridge payload.
func handleAttachmentIngest(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID     string `json:"projectId"`
		SessionID     string `json:"sessionId"`
		OriginalName  string `json:"originalName"`
		MIME          string `json:"mime"`
		ContentBase64 string `json:"contentBase64"`
	}
	if decodePayload(r.Payload, &p) != nil ||
		!validCanonicalULID(p.ProjectID) ||
		strings.TrimSpace(p.OriginalName) == "" ||
		strings.TrimSpace(p.MIME) == "" ||
		p.ContentBase64 == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "attachment.ingest 参数无效", false)
	}
	if p.SessionID != "" && !validCanonicalULID(p.SessionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "attachment.ingest sessionId 无效", false)
	}
	if len(p.ContentBase64) > base64.StdEncoding.EncodedLen(attachmentapp.MaxFileSize) {
		return r.Fail("ATTACHMENT_FILE_TOO_LARGE", attachmentFileTooLargeMessage(), false)
	}
	decodedLen := base64.StdEncoding.DecodedLen(len(p.ContentBase64))
	if strings.HasSuffix(p.ContentBase64, "==") {
		decodedLen -= 2
	} else if strings.HasSuffix(p.ContentBase64, "=") {
		decodedLen--
	}
	if decodedLen > attachmentapp.MaxFileSize {
		return r.Fail("ATTACHMENT_FILE_TOO_LARGE", attachmentFileTooLargeMessage(), false)
	}
	content, err := base64.StdEncoding.DecodeString(p.ContentBase64)
	if err != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "attachment.ingest contentBase64 解码失败", false)
	}
	att, err := e.IngestAttachment(ctx, attachmentapp.IngestFileRequest{
		ProjectID:    p.ProjectID,
		SessionID:    p.SessionID,
		OriginalName: p.OriginalName,
		MIME:         p.MIME,
		Content:      content,
	})
	if err != nil {
		return attachmentFailure(r, err)
	}
	dto := newAttachmentDTO(att)
	return r.Ok(map[string]any{
		"attachmentId":    dto.AttachmentID,
		"projectId":       dto.ProjectID,
		"sessionId":       dto.SessionID,
		"originalName":    dto.OriginalName,
		"mime":            dto.MIME,
		"size":            dto.Size,
		"sha256":          dto.SHA256,
		"parseStatus":     dto.ParseStatus,
		"parseErrorCode":  dto.ParseErrorCode,
		"parsedTextBytes": dto.ParsedTextBytes,
		"createdAt":       dto.CreatedAt,
	})
}

func handleInternalAttachmentImportPath(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID    string `json:"projectId"`
		SessionID    string `json:"sessionId"`
		Path         string `json:"path"`
		OriginalName string `json:"originalName"`
		MIME         string `json:"mime"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || strings.TrimSpace(p.Path) == "" || strings.ContainsRune(p.Path, 0) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "附件路径无效", false)
	}
	if p.SessionID != "" && !validCanonicalULID(p.SessionID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "附件 sessionId 无效", false)
	}
	att, err := e.ImportAttachmentPath(ctx, p.ProjectID, p.SessionID, p.Path, p.OriginalName, p.MIME)
	if err != nil {
		return attachmentFailure(r, err)
	}
	return attachmentResult(r, att)
}

func attachmentResult(r bridge.Request, a attachment.Attachment) bridge.Response {
	d := newAttachmentDTO(a)
	return r.Ok(map[string]any{"attachmentId": d.AttachmentID, "projectId": d.ProjectID, "sessionId": d.SessionID, "originalName": d.OriginalName, "mime": d.MIME, "size": d.Size, "sha256": d.SHA256, "parseStatus": d.ParseStatus, "parseErrorCode": d.ParseErrorCode, "parsedTextBytes": d.ParsedTextBytes, "createdAt": d.CreatedAt})
}
func handleAttachmentUploadBegin(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID    string `json:"projectId"`
		SessionID    string `json:"sessionId"`
		OriginalName string `json:"originalName"`
		MIME         string `json:"mime"`
		Size         int64  `json:"size"`
		SHA256       string `json:"sha256"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || (p.SessionID != "" && !validCanonicalULID(p.SessionID)) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "attachment.upload.begin 参数无效", false)
	}
	id, expires, err := e.BeginAttachmentUpload(ctx, attachmentapp.BeginUploadRequest{ProjectID: p.ProjectID, SessionID: p.SessionID, OriginalName: p.OriginalName, MIME: p.MIME, Size: p.Size, SHA256: p.SHA256})
	if err != nil {
		return attachmentFailure(r, err)
	}
	return r.Ok(map[string]any{"uploadId": id, "chunkSize": attachmentapp.RecommendedUploadChunkBytes, "expiresAt": expires})
}
func handleAttachmentUploadChunk(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		UploadID      string `json:"uploadId"`
		Offset        int64  `json:"offset"`
		ContentBase64 string `json:"contentBase64"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.UploadID) || len(p.ContentBase64) > base64.StdEncoding.EncodedLen(attachmentapp.MaxUploadChunkBytes) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "attachment.upload.chunk 参数无效", false)
	}
	data, err := base64.StdEncoding.DecodeString(p.ContentBase64)
	if err != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "chunk base64 无效", false)
	}
	next, err := e.AppendAttachmentChunk(ctx, p.UploadID, p.Offset, data)
	if err != nil {
		return attachmentFailure(r, err)
	}
	return r.Ok(map[string]any{"uploadId": p.UploadID, "nextOffset": next})
}
func handleAttachmentUploadCommit(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		UploadID  string `json:"uploadId"`
		ProjectID string `json:"projectId"`
		SessionID string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.UploadID) || !validCanonicalULID(p.ProjectID) || (p.SessionID != "" && !validCanonicalULID(p.SessionID)) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "attachment.upload.commit 参数无效", false)
	}
	a, err := e.CommitAttachmentUpload(ctx, p.UploadID, p.ProjectID, p.SessionID)
	if err != nil {
		return attachmentFailure(r, err)
	}
	return attachmentResult(r, a)
}
func handleAttachmentUploadAbort(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		UploadID  string `json:"uploadId"`
		ProjectID string `json:"projectId"`
		SessionID string `json:"sessionId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.UploadID) || !validCanonicalULID(p.ProjectID) || (p.SessionID != "" && !validCanonicalULID(p.SessionID)) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "attachment.upload.abort 参数无效", false)
	}
	if err := e.AbortAttachmentUpload(ctx, p.UploadID, p.ProjectID, p.SessionID); err != nil {
		return attachmentFailure(r, err)
	}
	return r.Ok(map[string]any{"uploadId": p.UploadID, "aborted": true})
}

// handleAttachmentGet returns an attachment by ID, including its parsed text
// for display (ADR-005 §7).
func handleAttachmentGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		AttachmentID string `json:"attachmentId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.AttachmentID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "attachment.get 参数无效", false)
	}
	att, err := e.GetAttachment(ctx, p.AttachmentID)
	if err != nil {
		return attachmentFailure(r, err)
	}
	dto := newAttachmentDTO(*att)
	body := map[string]any{
		"attachmentId":    dto.AttachmentID,
		"projectId":       dto.ProjectID,
		"sessionId":       dto.SessionID,
		"originalName":    dto.OriginalName,
		"mime":            dto.MIME,
		"size":            dto.Size,
		"sha256":          dto.SHA256,
		"parseStatus":     dto.ParseStatus,
		"parseErrorCode":  dto.ParseErrorCode,
		"parsedText":      att.ParsedText,
		"parsedTextBytes": dto.ParsedTextBytes,
		"createdAt":       dto.CreatedAt,
	}
	if data, ok, previewErr := e.PreviewAttachmentImage(ctx, p.AttachmentID); previewErr == nil && ok {
		body["contentBase64"] = base64.StdEncoding.EncodeToString(data)
	}
	return r.Ok(body)
}

// handleAttachmentOpen opens one attachment as its ORIGINAL file type: the
// SHA256-verified stored bytes are copied to a shell-openable cache file that
// keeps the original name (and therefore its extension), then opened with the
// system default program. reveal=true selects the file in the folder instead
// of launching it.
func handleAttachmentOpen(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		AttachmentID string `json:"attachmentId"`
		Reveal       bool   `json:"reveal"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.AttachmentID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "attachment.open 参数无效", false)
	}
	att, err := e.GetAttachment(ctx, p.AttachmentID)
	if err != nil {
		return attachmentFailure(r, err)
	}
	data, err := e.ReadAttachmentFile(ctx, p.AttachmentID)
	if err != nil {
		return r.Fail("ATTACHMENT_FILE_READ_FAILED", "附件文件暂时无法读取", false)
	}
	target, err := materializeAttachmentOpenCopy(p.AttachmentID, att.OriginalName, data)
	if err != nil {
		return r.Fail("ATTACHMENT_OPEN_FAILED", "无法准备打开附件", false)
	}
	if err := openArtifactTarget(target, true, p.Reveal); err != nil {
		return r.Fail("ATTACHMENT_OPEN_FAILED", "无法打开文件", false)
	}
	return r.Ok(map[string]any{"opened": target})
}

// materializeAttachmentOpenCopy writes the attachment bytes to
// <cache>/<attachmentId>/<sanitized original name>. The original name keeps
// its extension so the OS shell resolves the default program by file type.
func materializeAttachmentOpenCopy(attachmentID, originalName string, data []byte) (string, error) {
	name := sanitizeAttachmentOpenName(originalName, attachmentID)
	dir := filepath.Join(os.TempDir(), "lunitide-attachment-open", attachmentID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	target := filepath.Join(dir, name)
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return "", err
	}
	return target, nil
}

// sanitizeAttachmentOpenName reduces an attachment's original name to a
// single safe filename: path separators, drive letters, and Windows-forbidden
// characters are replaced, trailing dots/spaces are trimmed, and reserved
// device names (CON, PRN, AUX, NUL, COM1-9, LPT1-9) are prefixed away.
func sanitizeAttachmentOpenName(originalName, attachmentID string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(originalName) {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', 0:
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	name := strings.TrimRight(strings.TrimSpace(b.String()), ". ")
	if name == "" || name == "." || name == ".." {
		name = "attachment-" + attachmentID
	}
	reserved := map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true,
		"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
		"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true}
	stem := name
	if idx := strings.IndexByte(stem, '.'); idx >= 0 {
		stem = stem[:idx]
	}
	if reserved[strings.ToUpper(stem)] {
		name = "lunitide-" + name
	}
	return name
}

// handleAttachmentList returns attachments for a project, ordered by creation
// time descending (ADR-005 §7). Soft-deleted attachments are excluded.
func handleAttachmentList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		SessionID string `json:"sessionId"`
		Limit     int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || (p.SessionID != "" && !validCanonicalULID(p.SessionID)) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "attachment.list 参数无效", false)
	}
	var atts []attachment.Attachment
	var err error
	if p.SessionID != "" {
		atts, err = e.ListAttachmentsBySession(ctx, p.SessionID, p.Limit)
	} else {
		atts, err = e.ListAttachmentsByProject(ctx, p.ProjectID, p.Limit)
	}
	if err != nil {
		return attachmentFailure(r, err)
	}
	items := make([]attachmentDTO, 0, len(atts))
	for _, a := range atts {
		if a.ProjectID != p.ProjectID {
			continue
		}
		items = append(items, newAttachmentDTO(a))
	}
	return r.Ok(map[string]any{"items": items})
}

// handleAttachmentDelete soft-deletes an attachment and removes the underlying
// file from the data directory (ADR-005 §7). Idempotent.
func handleAttachmentDelete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		AttachmentID string `json:"attachmentId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.AttachmentID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "attachment.delete 参数无效", false)
	}
	if err := e.DeleteAttachment(ctx, p.AttachmentID); err != nil {
		return attachmentFailure(r, err)
	}
	return r.Ok(map[string]any{
		"attachmentId": p.AttachmentID,
		"deleted":      true,
	})
}

// attachmentFailure maps attachment service errors to stable Bridge error codes.
func attachmentFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, attachmentapp.ErrAttachmentNotFound):
		return r.Fail("ATTACHMENT_NOT_FOUND", "附件不存在", false)
	case errors.Is(err, attachmentapp.ErrFileTooLarge):
		return r.Fail("ATTACHMENT_FILE_TOO_LARGE", attachmentFileTooLargeMessage(), false)
	case errors.Is(err, attachmentapp.ErrScopeMismatch):
		return r.Fail("ATTACHMENT_SCOPE_MISMATCH", "附件项目与会话不匹配", false)
	case errors.Is(err, attachmentapp.ErrUnsupportedMIME):
		return r.Fail("ATTACHMENT_UNSUPPORTED_MIME", "不支持的附件 MIME 类型", false)
	case errors.Is(err, attachmentapp.ErrInvalidContent):
		return r.Fail("ATTACHMENT_INVALID_CONTENT", "附件内容无效", false)
	case errors.Is(err, attachmentapp.ErrUploadNotFound):
		return r.Fail("ATTACHMENT_UPLOAD_NOT_FOUND", "上传不存在或已过期", false)
	case errors.Is(err, attachmentapp.ErrUploadOffset):
		return r.Fail("ATTACHMENT_UPLOAD_OFFSET", "附件分块顺序无效", false)
	case errors.Is(err, attachmentapp.ErrUploadDigest):
		return r.Fail("ATTACHMENT_UPLOAD_DIGEST", "附件 SHA-256 校验失败", false)
	default:
		return internalBridgeFailure(r, "ATTACHMENT_OPERATION_FAILED", "附件操作暂时不可用", true, err)
	}
}
