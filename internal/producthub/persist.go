package producthub

import (
	"context"
	"sync"
)

type Persist interface {
	ProductHubLoadAuth(ctx context.Context) (hash string, err error)
	ProductHubSaveAuth(ctx context.Context, hash string) error
	ProductHubLoadLatest(ctx context.Context) (*Edition, error)
	ProductHubSaveEdition(ctx context.Context, ed Edition) error
	ProductHubLoadTags(ctx context.Context) ([]NodeTag, error)
	ProductHubSaveTag(ctx context.Context, tag NodeTag) error
	ProductHubLoadEnrichments(ctx context.Context) ([]Enrichment, error)
	ProductHubSaveEnrichment(ctx context.Context, en Enrichment) error
	ProductHubLoadApplies(ctx context.Context) ([]ApplyLog, error)
	ProductHubSaveApply(ctx context.Context, rec ApplyLog) error
}

type MemoryPersist struct {
	mu           sync.Mutex
	hash         string
	ed           *Edition
	tags         []NodeTag
	enrichments  []Enrichment
	applies      []ApplyLog
}

func (m *MemoryPersist) ProductHubLoadAuth(context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hash, nil
}

func (m *MemoryPersist) ProductHubSaveAuth(_ context.Context, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hash = hash
	return nil
}

func (m *MemoryPersist) ProductHubLoadLatest(context.Context) (*Edition, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ed, nil
}

func (m *MemoryPersist) ProductHubSaveEdition(_ context.Context, ed Edition) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copy := ed
	m.ed = &copy
	return nil
}

func (m *MemoryPersist) ProductHubLoadTags(context.Context) ([]NodeTag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]NodeTag, len(m.tags))
	copy(out, m.tags)
	return out, nil
}

func (m *MemoryPersist) ProductHubSaveTag(_ context.Context, tag NodeTag) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, t := range m.tags {
		if t.StableKey == tag.StableKey && t.Vocab == tag.Vocab && t.Value == tag.Value {
			m.tags[i] = tag
			return nil
		}
	}
	m.tags = append(m.tags, tag)
	return nil
}

func (m *MemoryPersist) ProductHubLoadEnrichments(context.Context) ([]Enrichment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Enrichment, len(m.enrichments))
	copy(out, m.enrichments)
	return out, nil
}

func (m *MemoryPersist) ProductHubSaveEnrichment(_ context.Context, en Enrichment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, e := range m.enrichments {
		if e.StableKey == en.StableKey {
			m.enrichments[i] = en
			return nil
		}
	}
	m.enrichments = append(m.enrichments, en)
	return nil
}

func (m *MemoryPersist) ProductHubLoadApplies(context.Context) ([]ApplyLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ApplyLog, len(m.applies))
	copy(out, m.applies)
	return out, nil
}

func (m *MemoryPersist) ProductHubSaveApply(_ context.Context, rec ApplyLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applies = append(m.applies, rec)
	return nil
}
