package app

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/memory"
	"github.com/lunitide/lunitide/internal/memoryapp"
)

type MemoryService interface {
	Get(context.Context, string) (*memory.Memory, error)
	ListByProject(context.Context, string, memory.Layer) ([]memory.Memory, error)
	Search(context.Context, string, string) ([]memory.Memory, error)
	Create(context.Context, memory.Memory) (memory.Memory, error)
	UpdateContent(context.Context, string, string) error
	Delete(context.Context, string) error
}

type memoryDTO struct {
	ID           string             `json:"id"`
	ProjectID    string             `json:"projectId"`
	Layer        memory.Layer       `json:"layer"`
	Scope        memory.MemoryScope `json:"scope"`
	Key          string             `json:"key"`
	Content      string             `json:"content"`
	EmbeddingID  *string            `json:"embeddingId,omitempty"`
	SourceID     *string            `json:"sourceId,omitempty"`
	SourceType   *string            `json:"sourceType,omitempty"`
	Confidence   memory.Confidence  `json:"confidence"`
	AccessCount  int64              `json:"accessCount"`
	LastAccessed *time.Time         `json:"lastAccessed,omitempty"`
	ExpiresAt    *time.Time         `json:"expiresAt,omitempty"`
	CreatedAt    time.Time          `json:"createdAt"`
	UpdatedAt    time.Time          `json:"updatedAt"`
}

func newMemoryDTO(m memory.Memory) memoryDTO {
	return memoryDTO{
		ID:           m.ID,
		ProjectID:    m.ProjectID,
		Layer:        m.Layer,
		Scope:        m.Scope,
		Key:          m.Key,
		Content:      m.Content,
		EmbeddingID:  m.EmbeddingID,
		SourceID:     m.SourceID,
		SourceType:   m.SourceType,
		Confidence:   m.Confidence,
		AccessCount:  m.AccessCount,
		LastAccessed: m.LastAccessed,
		ExpiresAt:    m.ExpiresAt,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
}

func memoryServiceAvailable(service MemoryService) bool {
	if service == nil {
		return false
	}
	v := reflect.ValueOf(service)
	return v.Kind() != reflect.Pointer || !v.IsNil()
}

func handleMemoryGet(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.get 参数无效", false)
	}
	if !memoryServiceAvailable(e.memories) {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆数据暂时不可用", true)
	}
	m, err := e.memories.Get(ctx, p.ID)
	if err != nil {
		return memoryFailure(r, err)
	}
	return r.Ok(newMemoryDTO(*m))
}

func handleMemoryCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID  string     `json:"projectId"`
		Layer      string     `json:"layer"`
		Scope      string     `json:"scope"`
		Key        string     `json:"key"`
		Content    string     `json:"content"`
		Confidence float64    `json:"confidence"`
		ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || strings.TrimSpace(p.Key) == "" || strings.TrimSpace(p.Content) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.create 参数无效", false)
	}
	if !memoryServiceAvailable(e.memories) {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆数据暂时不可用", true)
	}
	m, err := e.memories.Create(ctx, memory.Memory{
		ProjectID:  p.ProjectID,
		Layer:      memory.Layer(p.Layer),
		Scope:      memory.MemoryScope(p.Scope),
		Key:        p.Key,
		Content:    p.Content,
		Confidence: memory.Confidence(p.Confidence),
		ExpiresAt:  p.ExpiresAt,
	})
	if err != nil {
		return memoryFailure(r, err)
	}
	return r.Ok(newMemoryDTO(m))
}

func handleMemoryList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string       `json:"projectId"`
		Layer     memory.Layer `json:"layer"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.list 参数无效", false)
	}
	if p.Layer != "" && p.Layer != memory.LayerWorking && p.Layer != memory.LayerEpisodic && p.Layer != memory.LayerSemantic && p.Layer != memory.LayerProcedural {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.list 参数无效", false)
	}
	if !memoryServiceAvailable(e.memories) {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆数据暂时不可用", true)
	}
	items, err := e.memories.ListByProject(ctx, p.ProjectID, p.Layer)
	if err != nil {
		return memoryFailure(r, err)
	}
	dtos := make([]memoryDTO, len(items))
	for i := range items {
		dtos[i] = newMemoryDTO(items[i])
	}
	return r.Ok(struct {
		Items []memoryDTO `json:"items"`
	}{Items: dtos})
}

func handleMemorySearch(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ProjectID string `json:"projectId"`
		Query     string `json:"query"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ProjectID) || strings.TrimSpace(p.Query) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.search 参数无效", false)
	}
	if !memoryServiceAvailable(e.memories) {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆数据暂时不可用", true)
	}
	items, err := e.memories.Search(ctx, p.ProjectID, p.Query)
	if err != nil {
		return memoryFailure(r, err)
	}
	dtos := make([]memoryDTO, len(items))
	for i := range items {
		dtos[i] = newMemoryDTO(items[i])
	}
	return r.Ok(struct {
		Items []memoryDTO `json:"items"`
	}{Items: dtos})
}

func handleMemoryUpdate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) || strings.TrimSpace(p.Content) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.update 参数无效", false)
	}
	if !memoryServiceAvailable(e.memories) {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆数据暂时不可用", true)
	}
	if err := e.memories.UpdateContent(ctx, p.ID, p.Content); err != nil {
		return memoryFailure(r, err)
	}
	return r.Ok(map[string]any{"updated": true})
}

func handleMemoryDelete(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ID string `json:"id"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.delete 参数无效", false)
	}
	if !memoryServiceAvailable(e.memories) {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆数据暂时不可用", true)
	}
	if err := e.memories.Delete(ctx, p.ID); err != nil {
		return memoryFailure(r, err)
	}
	return r.Ok(map[string]any{"deleted": true})
}

func memoryFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, memoryapp.ErrMemoryNotFound):
		return r.Fail("MEMORY_NOT_FOUND", "记忆不存在", false)
	default:
		return r.Fail("STORAGE_UNAVAILABLE", "记忆数据暂时不可用", true)
	}
}
