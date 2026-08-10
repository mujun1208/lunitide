// Package memoryapp coordinates memory storage, retrieval, and keyword search.
package memoryapp

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/memory"
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
}

// MemoryWriter writes and updates memories.
type MemoryWriter interface {
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

// Search performs a case-insensitive keyword search across memory content and key.
// Returns memories matching any of the keywords, sorted by confidence (descending).
func (s *Service) Search(ctx context.Context, projectID string, query string) ([]memory.Memory, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("memory reader unavailable")
	}
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return nil, nil
	}
	all, err := s.read.ListMemoriesByProject(ctx, projectID, "", 100)
	if err != nil {
		return nil, err
	}
	keywords := strings.Fields(query)
	var results []memory.Memory
	for _, m := range all {
		// Skip expired memories.
		if m.ExpiresAt != nil && s.clock.Now().After(*m.ExpiresAt) {
			continue
		}
		contentLower := strings.ToLower(m.Content)
		keyLower := strings.ToLower(m.Key)
		matched := false
		for _, kw := range keywords {
			if strings.Contains(contentLower, kw) || strings.Contains(keyLower, kw) {
				matched = true
				break
			}
		}
		if matched {
			results = append(results, m)
		}
	}
	// Sort by confidence descending (simple bubble sort for small lists).
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Confidence > results[i].Confidence {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
	return results, nil
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
