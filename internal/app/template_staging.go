package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/attachmentapp"
	"github.com/lunitide/lunitide/internal/bridge"
)

const templateStageTTL = time.Hour
const templateStageCapacity = 64

type templateChunkReceipt struct {
	digest [32]byte
	last   bool
	bytes  int64
}
type templateStageUpload struct {
	mu               sync.Mutex
	name, mime, path string
	orgID            string
	file             *os.File
	size             int64
	ready, closed    bool
	chunks           []templateChunkReceipt
	digest           [32]byte
	updated          time.Time
}
type templateStageState struct {
	mu      sync.Mutex
	uploads map[string]*templateStageUpload
}

func (e *Engine) templateStage() *templateStageState {
	e.templateStageOnce.Do(func() { e.templateStageState = &templateStageState{uploads: make(map[string]*templateStageUpload)} })
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
	if len(contentBase64) > 360000 {
		return nil, errors.New("chunk too large")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(contentBase64))
	if err != nil || len(raw) == 0 {
		return nil, errors.New("invalid chunk")
	}
	return raw, nil
}
func closeTemplateUpload(up *templateStageUpload) error {
	up.closed = true
	up.ready = false
	if up.file != nil {
		if err := up.file.Close(); err != nil {
			log.Printf("template stage close: %v", err)
		}
		up.file = nil
	}
	err := os.Remove(up.path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Eviction touches uploads owned by this service only. Unknown files from a
// previous process cannot be consumed by guessing their path.
func (state *templateStageState) evictExpired(now time.Time) {
	for id, up := range state.uploads {
		up.mu.Lock()
		if now.Sub(up.updated) > templateStageTTL {
			if err := closeTemplateUpload(up); err != nil {
				log.Printf("template stage cleanup pending: %v", err)
			} else {
				delete(state.uploads, id)
			}
		}
		up.mu.Unlock()
	}
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
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.UploadID) || p.Index < 0 || p.Index > 4096 || len(p.FileName) > 260 || len(p.FileMIME) > 128 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.file.stage 参数无效", false)
	}
	if ctx.Err() != nil {
		return r.Fail("STORAGE_UNAVAILABLE", "模板上传已取消", false)
	}
	raw, err := decodeTemplateStageChunk(p.ContentBase64)
	if err != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "template.file.stage 参数无效", false)
	}
	digest := sha256.Sum256(raw)
	orgID, err := e.boundOrgID(ctx)
	if err != nil {
		return r.Fail("DATA_SCOPE_UNAVAILABLE", "组织状态无法确认，请重试", true)
	}
	state := e.templateStage()
	state.mu.Lock()
	state.evictExpired(time.Now())
	up := state.uploads[p.UploadID]
	if up == nil {
		if p.Index != 0 {
			state.mu.Unlock()
			return r.Fail("BRIDGE_SCHEMA_INVALID", "请从第 0 个分片开始上传", false)
		}
		if len(state.uploads) >= templateStageCapacity {
			state.mu.Unlock()
			return r.Fail("STORAGE_UNAVAILABLE", "模板暂存数量已达上限，请稍后重试", true)
		}
		dir, err := e.templateStageDir()
		if err != nil {
			state.mu.Unlock()
			return r.Fail("STORAGE_UNAVAILABLE", "模板分片暂存不可用", true)
		}
		f, err := os.CreateTemp(dir, "up-"+p.UploadID+"-")
		if err != nil {
			state.mu.Unlock()
			return r.Fail("STORAGE_UNAVAILABLE", "模板分片暂存不可用", true)
		}
		up = &templateStageUpload{name: p.FileName, mime: p.FileMIME, path: f.Name(), file: f, updated: time.Now(), orgID: orgID}
		state.uploads[p.UploadID] = up
	}
	up.mu.Lock()
	state.mu.Unlock()
	defer up.mu.Unlock()
	if up.closed || up.name != p.FileName || up.mime != p.FileMIME || up.orgID != orgID {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "上传已失效或分片文件信息不一致", false)
	}
	if p.Index < len(up.chunks) {
		receipt := up.chunks[p.Index]
		if receipt.digest != digest || receipt.last != p.Last {
			return r.Fail("IDEMPOTENCY_CONFLICT", "该分片编号已用于不同内容", false)
		}
		up.updated = time.Now()
		return r.Ok(map[string]any{"ready": receipt.last, "uploadId": p.UploadID, "bytes": receipt.bytes})
	}
	if p.Index != len(up.chunks) || up.ready || up.file == nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "分片顺序不正确或上传已结束", false)
	}
	if ctx.Err() != nil {
		return r.Fail("STORAGE_UNAVAILABLE", "模板上传已取消", false)
	}
	if up.size+int64(len(raw)) > attachmentapp.MaxFileSize {
		return r.Fail("TEMPLATE_FILE_TOO_LARGE", "模板附件超过 10 MiB 限制", false)
	}
	written, err := up.file.WriteAt(raw, up.size)
	if err != nil || written != len(raw) {
		_ = up.file.Truncate(up.size)
		return r.Fail("STORAGE_UNAVAILABLE", "模板分片暂存失败", true)
	}
	if err = up.file.Sync(); err != nil {
		_ = up.file.Truncate(up.size)
		return r.Fail("STORAGE_UNAVAILABLE", "模板分片暂存失败", true)
	}
	if p.Last {
		data, readErr := os.ReadFile(up.path)
		if readErr != nil {
			_ = up.file.Truncate(up.size)
			return r.Fail("STORAGE_UNAVAILABLE", "模板完整性校验失败", true)
		}
		up.digest = sha256.Sum256(data)
		if err = up.file.Close(); err != nil {
			if cleanupErr := closeTemplateUpload(up); cleanupErr != nil {
				log.Printf("template stage cleanup pending: %v", cleanupErr)
			}
			return r.Fail("STORAGE_UNAVAILABLE", "模板分片暂存失败，请重新上传", false)
		}
		up.file = nil
		up.ready = true
	}
	up.size += int64(len(raw))
	up.updated = time.Now()
	up.chunks = append(up.chunks, templateChunkReceipt{digest, p.Last, up.size})
	return r.Ok(map[string]any{"ready": up.ready, "uploadId": p.UploadID, "bytes": up.size})
}
func (e *Engine) consumeTemplateStage(uploadID string) ([]byte, error) {
	if !validCanonicalULID(uploadID) {
		return nil, fmt.Errorf("invalid upload id")
	}
	state := e.templateStage()
	state.mu.Lock()
	up := state.uploads[uploadID]
	if up == nil {
		state.mu.Unlock()
		return nil, errors.New("upload not found in this service")
	}
	up.mu.Lock()
	state.mu.Unlock()
	defer up.mu.Unlock()
	if !up.ready || up.closed || time.Since(up.updated) > templateStageTTL {
		return nil, errors.New("upload incomplete or expired")
	}
	data, err := os.ReadFile(up.path)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != up.size || len(data) == 0 || len(data) > attachmentapp.MaxFileSize || sha256.Sum256(data) != up.digest {
		return nil, errors.New("staged file integrity failed")
	}
	up.updated = time.Now()
	return data, nil
}
func (e *Engine) finishTemplateStage(uploadID string) {
	if !validCanonicalULID(uploadID) {
		return
	}
	state := e.templateStage()
	state.mu.Lock()
	up := state.uploads[uploadID]
	delete(state.uploads, uploadID)
	if up != nil {
		up.mu.Lock()
		if err := closeTemplateUpload(up); err != nil {
			log.Printf("template stage cleanup pending: %v", err)
		}
		up.mu.Unlock()
	}
	state.mu.Unlock()
}
