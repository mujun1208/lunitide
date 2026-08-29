package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
)

type templateStageUpload struct {
	mu   sync.Mutex
	name string
	path string
	file *os.File
	size int64
}

type templateStageState struct {
	mu      sync.Mutex
	uploads map[string]*templateStageUpload
}

func (e *Engine) templateStage() *templateStageState {
	if e.templateStageState == nil {
		e.templateStageState = &templateStageState{uploads: make(map[string]*templateStageUpload)}
	}
	return e.templateStageState
}

func templateStageDir() (string, error) {
	dir := filepath.Join(os.TempDir(), "lunitide-template-stage")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func decodeTemplateStageChunk(contentBase64 string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(contentBase64))
	if err != nil || len(raw) == 0 {
		return nil, errors.New("invalid chunk")
	}
	return raw, nil
}

func handleTemplateFileStage(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		UploadID      string `json:"uploadId"`
		FileName      string `json:"fileName"`
		FileMIME      string `json:"fileMime"`
		Index         int    `json:"index"`
		Last          bool   `json:"last"`
		ContentBase64 string `json:"contentBase64"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.file.stage 参数无效", false)
	}
	if !validCanonicalULID(p.UploadID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.file.stage 参数无效", false)
	}
	raw, err := decodeTemplateStageChunk(p.ContentBase64)
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.file.stage 参数无效", false)
	}
	dir, err := templateStageDir()
	if err != nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "模板分片暂存不可用", true)
	}
	state := e.templateStage()
	state.mu.Lock()
	up := state.uploads[p.UploadID]
	if up == nil {
		path := filepath.Join(dir, "up-"+p.UploadID)
		f, openErr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if openErr != nil {
			state.mu.Unlock()
			return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "模板分片暂存不可用", true)
		}
		up = &templateStageUpload{name: p.FileName, path: path, file: f}
		state.uploads[p.UploadID] = up
	}
	state.mu.Unlock()

	up.mu.Lock()
	defer up.mu.Unlock()
	if up.file == nil {
		if p.Last {
			return bridge.Success(r.ID, map[string]any{"ready": true, "uploadId": p.UploadID, "bytes": up.size})
		}
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "template.file.stage 参数无效", false)
	}
	if up.size+int64(len(raw)) > attachmentapp.MaxFileSize {
		return bridge.Failure(r.ID, r.TraceID, "TEMPLATE_FILE_TOO_LARGE", "模板附件超过 10 MiB 限制", false)
	}
	if _, err := up.file.Write(raw); err != nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "模板分片暂存失败", true)
	}
	up.size += int64(len(raw))
	_ = ctx // staging is synchronous; ctx reserved for future cancellation hooks
	if !p.Last {
		return bridge.Success(r.ID, map[string]any{"ready": false, "uploadId": p.UploadID, "bytes": up.size})
	}
	if err := up.file.Close(); err != nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "模板分片暂存失败", true)
	}
	up.file = nil
	return bridge.Success(r.ID, map[string]any{"ready": true, "uploadId": p.UploadID, "bytes": up.size})
}

func (e *Engine) consumeTemplateStage(uploadID string) ([]byte, error) {
	if !validCanonicalULID(uploadID) {
		return nil, fmt.Errorf("invalid upload id")
	}
	dir, err := templateStageDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "up-"+uploadID)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > attachmentapp.MaxFileSize {
		return nil, fmt.Errorf("staged file invalid")
	}
	return data, nil
}

func (e *Engine) finishTemplateStage(uploadID string) {
	if !validCanonicalULID(uploadID) {
		return
	}
	state := e.templateStage()
	state.mu.Lock()
	delete(state.uploads, uploadID)
	state.mu.Unlock()
	cleanupTemplateStage(uploadID)
}

func cleanupTemplateStage(uploadID string) {
	if !validCanonicalULID(uploadID) {
		return
	}
	dir, err := templateStageDir()
	if err != nil {
		return
	}
	_ = os.Remove(filepath.Join(dir, "up-"+uploadID))
}
