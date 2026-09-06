// Package memoryapp coordinates memory storage, retrieval, and keyword search.
package memoryapp

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/memory"
	"github.com/oklog/ulid/v2"
)

var (
	ErrMemoryNotFound = errors.New("memory not found")
	ErrInvalidLayer   = errors.New("invalid memory layer")
	ErrInvalidScope   = errors.New("invalid memory scope")
)

// MemoryReader reads memories from storage.
type MemoryReader interface {
	GetMemory(ctx context.Context, id string) (*memory.Memory, error)
	ListMemoriesByProject(ctx context.Context, projectID string, layer string, limit int) ([]memory.Memory, error)
	SearchMemoriesFTS(ctx context.Context, projectID string, query string, limit int) ([]memory.Memory, error)
}

// MemoryWriter writes and updates memories.
type MemoryWriter interface {
	CreateMemory(ctx context.Context, m memory.Memory) (memory.Memory, error)
	UpdateMemory(ctx context.Context, id string, content string) error
	DeleteMemory(ctx context.Context, id string) error
	IncrementAccessCount(ctx context.Context, id string) error
}

// Clock provides the current time.
type Clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// Service coordinates memory lifecycle and retrieval.
type Service struct {
	read  MemoryReader
	write MemoryWriter
	clock Clock
}

// New creates a memory service.
func New(r MemoryReader, w MemoryWriter) *Service {
	return &Service{read: r, write: w, clock: systemClock{}}
}

// Get retrieves a memory by ID and increments its access count.
func (s *Service) Get(ctx context.Context, id string) (*memory.Memory, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("memory reader unavailable")
	}
	m, err := s.read.GetMemory(ctx, id)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrMemoryNotFound
	}
	// Check expiration.
	if m.ExpiresAt != nil && s.clock.Now().After(*m.ExpiresAt) {
		// Best-effort delete of expired memory.
		_ = s.write.DeleteMemory(ctx, id)
		return nil, ErrMemoryNotFound
	}
	// Increment access count (best-effort, don't fail the read).
	if s.write != nil {
		_ = s.write.IncrementAccessCount(ctx, id)
	}
	return m, nil
}

// Create validates and persists a new memory.
func (s *Service) Create(ctx context.Context, m memory.Memory) (memory.Memory, error) {
	if s == nil || s.write == nil {
		return memory.Memory{}, errors.New("memory writer unavailable")
	}
	if !canonicalULID(m.ProjectID) {
		return memory.Memory{}, errors.New("memory project_id is not a canonical ULID")
	}
	switch m.Layer {
	case memory.LayerWorking, memory.LayerEpisodic, memory.LayerSemantic, memory.LayerProcedural:
	default:
		return memory.Memory{}, ErrInvalidLayer
	}
	switch m.Scope {
	case memory.ScopeWorkspace, memory.ScopeProject, memory.ScopeSession:
	default:
		return memory.Memory{}, ErrInvalidScope
	}
	if len(m.Key) < 1 || len(m.Key) > 256 {
		return memory.Memory{}, errors.New("memory key must be 1-256 characters")
	}
	if len(m.Content) < 1 || len(m.Content) > 65536 {
		return memory.Memory{}, errors.New("memory content must be 1-65536 characters")
	}
	if err := m.Confidence.Validate(); err != nil {
		return memory.Memory{}, err
	}
	if m.ExpiresAt != nil && m.ExpiresAt.Location() != time.UTC {
		return memory.Memory{}, errors.New("memory expires_at must be UTC")
	}
	now := s.clock.Now()
	m.ID = ""
	m.AccessCount = 0
	m.CreatedAt = now
	m.UpdatedAt = now
	return s.write.CreateMemory(ctx, m)
}

// ListByProject returns memories for a project, optionally filtered by layer.
func (s *Service) ListByProject(ctx context.Context, projectID string, layer memory.Layer) ([]memory.Memory, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("memory reader unavailable")
	}
	layerStr := ""
	if layer != "" {
		layerStr = string(layer)
	}
	return s.read.ListMemoriesByProject(ctx, projectID, layerStr, 100)
}

// DefaultSearchLimit and MaxSearchLimit bound the number of memories returned
// by Search. They replace the previous hard-coded 100-row ceiling and the
// in-Go bubble sort (ranking is now done by the memory_fts FTS5 index).
const (
	DefaultSearchLimit = 50
	MaxSearchLimit     = 200
)

// Search performs a keyword search across memory content and key via the
// memory_fts FTS5 index, returning memories sorted by confidence (descending).
func (s *Service) Search(ctx context.Context, projectID string, query string) ([]memory.Memory, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("memory reader unavailable")
	}
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	results, err := s.read.SearchMemoriesFTS(ctx, projectID, query, DefaultSearchLimit)
	if err != nil {
		return nil, err
	}
	now := s.clock.Now()
	out := results[:0]
	for _, m := range results {
		// Skip expired memories.
		if m.ExpiresAt != nil && now.After(*m.ExpiresAt) {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// UpdateContent updates the content of a memory.
func (s *Service) UpdateContent(ctx context.Context, id, content string) error {
	if s == nil || s.write == nil {
		return errors.New("memory writer unavailable")
	}
	if len(content) < 1 || len(content) > 65536 {
		return errors.New("content must be 1-65536 characters")
	}
	m, err := s.read.GetMemory(ctx, id)
	if err != nil {
		return err
	}
	if m == nil {
		return ErrMemoryNotFound
	}
	return s.write.UpdateMemory(ctx, id, content)
}

// Delete removes a memory.
func (s *Service) Delete(ctx context.Context, id string) error {
	if s == nil || s.write == nil {
		return errors.New("memory writer unavailable")
	}
	m, err := s.read.GetMemory(ctx, id)
	if err != nil {
		return err
	}
	if m == nil {
		return ErrMemoryNotFound
	}
	return s.write.DeleteMemory(ctx, id)
}

// PurgeExpired deletes all expired memories for a project (best-effort cleanup).
func (s *Service) PurgeExpired(ctx context.Context, projectID string) (int, error) {
	if s == nil || s.read == nil {
		return 0, errors.New("memory reader unavailable")
	}
	all, err := s.read.ListMemoriesByProject(ctx, projectID, "", 100)
	if err != nil {
		return 0, err
	}
	now := s.clock.Now()
	count := 0
	for _, m := range all {
		if m.ExpiresAt != nil && now.After(*m.ExpiresAt) {
			if s.write != nil {
				if err := s.write.DeleteMemory(ctx, m.ID); err == nil {
					count++
				}
			}
		}
	}
	return count, nil
}

func canonicalULID(v string) bool {
	u, err := ulid.ParseStrict(v)
	return err == nil && u.String() == v && v[0] <= '7'
}
