package memory

import (
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
)

// Layer represents the four-layer memory hierarchy.
type Layer string

const (
	LayerWorking    Layer = "working"
	LayerEpisodic   Layer = "episodic"
	LayerSemantic   Layer = "semantic"
	LayerProcedural Layer = "procedural"
)

// MemoryScope defines the visibility boundary of a memory.
type MemoryScope string

const (
	ScopeWorkspace MemoryScope = "workspace"
	ScopeProject   MemoryScope = "project"
	ScopeSession   MemoryScope = "session"
)

// Confidence represents the reliability of a memory (0.0 - 1.0).
type Confidence float64

func (c Confidence) Validate() error {
	if c < 0 || c > 1 {
		return errors.New("confidence must be between 0.0 and 1.0")
	}
	return nil
}

// Memory is a retrievable piece of information with provenance, scope,
// confidence, and lifecycle. It is the core unit of the four-layer memory system.
type Memory struct {
	ID           string      `json:"id"`
	ProjectID    string      `json:"projectId"`
	Layer        Layer       `json:"layer"`
	Scope        MemoryScope `json:"scope"`
	Key          string      `json:"key"`
	Content      string      `json:"content"`
	EmbeddingID  *string     `json:"embeddingId,omitempty"`
	SourceID     *string     `json:"sourceId,omitempty"`
	SourceType   *string     `json:"sourceType,omitempty"`
	Confidence   Confidence  `json:"confidence"`
	AccessCount  int64       `json:"accessCount"`
	LastAccessed *time.Time  `json:"lastAccessed,omitempty"`
	ExpiresAt    *time.Time  `json:"expiresAt,omitempty"`
	CreatedAt    time.Time   `json:"createdAt"`
	UpdatedAt    time.Time   `json:"updatedAt"`
}

// Validate checks invariants for a memory.
func (m Memory) Validate() error {
	if !canonicalULID(m.ID) || !canonicalULID(m.ProjectID) {
		return errors.New("memory id or project_id is not a canonical ULID")
	}
	switch m.Layer {
	case LayerWorking, LayerEpisodic, LayerSemantic, LayerProcedural:
	default:
		return errors.New("memory layer invalid")
	}
	switch m.Scope {
	case ScopeWorkspace, ScopeProject, ScopeSession:
	default:
		return errors.New("memory scope invalid")
	}
	if len(m.Key) < 1 || len(m.Key) > 256 {
		return errors.New("memory key must be 1-256 characters")
	}
	if len(m.Content) < 1 || len(m.Content) > 65536 {
		return errors.New("memory content must be 1-65536 characters")
	}
	if m.EmbeddingID != nil && !canonicalULID(*m.EmbeddingID) {
		return errors.New("memory embedding_id is not a canonical ULID")
	}
	if m.SourceID != nil && !canonicalULID(*m.SourceID) {
		return errors.New("memory source_id is not a canonical ULID")
	}
	if m.SourceType != nil && len(*m.SourceType) > 64 {
		return errors.New("memory source_type too long")
	}
	if err := m.Confidence.Validate(); err != nil {
		return err
	}
	if m.AccessCount < 0 {
		return errors.New("memory access_count must be non-negative")
	}
	if m.CreatedAt.IsZero() || m.CreatedAt.Location() != time.UTC {
		return errors.New("memory created_at must be UTC")
	}
	if m.UpdatedAt.IsZero() || m.UpdatedAt.Location() != time.UTC {
		return errors.New("memory updated_at must be UTC")
	}
	if m.UpdatedAt.Before(m.CreatedAt) {
		return errors.New("memory updated_at must be >= created_at")
	}
	if m.ExpiresAt != nil && m.ExpiresAt.Location() != time.UTC {
		return errors.New("memory expires_at must be UTC")
	}
	if m.LastAccessed != nil && m.LastAccessed.Location() != time.UTC {
		return errors.New("memory last_accessed must be UTC")
	}
	return nil
}

// IsExpired returns true if the memory has an expiration time in the past.
func (m Memory) IsExpired(now time.Time) bool {
	return m.ExpiresAt != nil && now.After(*m.ExpiresAt)
}

// RecallCandidate is a memory returned by a retrieval operation with a relevance score.
type RecallCandidate struct {
	Memory   Memory
	Score    float64 `json:"score"`
	RecallID string  `json:"recallId"`
}

// RecallResult is the result of a memory recall operation.
type RecallResult struct {
	Candidates []RecallCandidate `json:"candidates"`
	QueryID    string            `json:"queryId"`
	TotalFound int64             `json:"totalFound"`
}

func canonicalULID(v string) bool {
	u, err := ulid.ParseStrict(v)
	return err == nil && u.String() == v && v[0] <= '7'
}