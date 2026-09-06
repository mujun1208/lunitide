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
	ErrMemoryNotFound  = errors.New("memory not found")
	ErrInvalidLayer    = errors.New("invalid memory layer")
	ErrInvalidScope    = errors.New("invalid memory scope")
	ErrPurgeIncomplete = errors.New("memory cleanup incomplete: storage does not support batched expiry cleanup")
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

func (s *Service) SetClock(c Clock) {
	if s != nil && c != nil {
		s.clock = c
	}
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
	if expiredAt(*m, s.clock.Now()) {
		// Best-effort delete of expired memory.
		if s.write != nil {
			if err := s.write.DeleteMemory(ctx, id); err != nil {
				return nil, errors.Join(ErrMemoryNotFound, err)
			}
		}
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
	now := s.clock.Now()
	var rows []memory.Memory
	var err error
	if active, ok := s.read.(interface {
		ListActiveMemoriesByProject(context.Context, string, string, time.Time, int) ([]memory.Memory, error)
	}); ok {
		rows, err = active.ListActiveMemoriesByProject(ctx, projectID, layerStr, now, 100)
	} else {
		rows, err = s.read.ListMemoriesByProject(ctx, projectID, layerStr, 100)
	}
	if err != nil {
		return nil, err
	}
	out := rows[:0]
	for _, m := range rows {
		if !expiredAt(m, now) {
			out = append(out, m)
		}
	}
	return out, nil
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
	now := s.clock.Now()
	var results []memory.Memory
	var err error
	if active, ok := s.read.(interface {
		SearchActiveMemoriesFTS(context.Context, string, string, time.Time, int) ([]memory.Memory, error)
	}); ok {
		results, err = active.SearchActiveMemoriesFTS(ctx, projectID, query, now, DefaultSearchLimit)
	} else {
		results, err = s.read.SearchMemoriesFTS(ctx, projectID, query, DefaultSearchLimit)
	}
	if err != nil {
		return nil, err
	}
	out := results[:0]
	for _, m := range results {
		// Skip expired memories.
		if expiredAt(m, now) {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// UpdateContent updates the content of a memory.
func (s *Service) UpdateContent(ctx context.Context, id, content string) error {
	if s == nil || s.write == nil || s.read == nil {
		return errors.New("memory writer unavailable")
	}
	if len(content) < 1 || len(content) > 65536 {
		return errors.New("content must be 1-65536 characters")
	}
	m, err := s.read.GetMemory(ctx, id)
	if err != nil {
		return err
	}
	if m == nil || expiredAt(*m, s.clock.Now()) {
		return ErrMemoryNotFound
	}
	return s.write.UpdateMemory(ctx, id, content)
}

// Delete removes a memory.
func (s *Service) Delete(ctx context.Context, id string) error {
	if s == nil || s.write == nil || s.read == nil {
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

// PurgeExpired processes every expired row in bounded SQLite batches. It
// returns the committed count plus any failure; a partial scan is never success.
func (s *Service) PurgeExpired(ctx context.Context, projectID string) (int, error) {
	if s == nil || s.write == nil {
		return 0, errors.New("memory writer unavailable")
	}
	now := s.clock.Now()
	count := 0
	if purge, ok := s.write.(interface {
		PurgeExpiredMemories(context.Context, string, time.Time, int) (int, error)
	}); ok {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		for {
			n, err := purge.PurgeExpiredMemories(ctx, projectID, now, 256)
			count += n
			if err != nil {
				return count, err
			}
			if n < 256 {
				return count, nil
			}
		}
	}
	if s.read == nil {
		return 0, errors.New("memory reader unavailable")
	}
	all, err := s.read.ListMemoriesByProject(ctx, projectID, "", 100)
	if err != nil {
		return 0, err
	}
	for _, m := range all {
		if expiredAt(m, now) {
			if err = s.write.DeleteMemory(ctx, m.ID); err != nil {
				return count, err
			}
			count++
		}
	}
	if len(all) >= 100 {
		return count, ErrPurgeIncomplete
	}
	return count, nil
}
func expiredAt(m memory.Memory, now time.Time) bool {
	return m.ExpiresAt != nil && !m.ExpiresAt.After(now)
}

func canonicalULID(v string) bool {
	u, err := ulid.ParseStrict(v)
	return err == nil && u.String() == v && v[0] <= '7'
}
