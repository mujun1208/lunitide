package agentorchestration

import (
	"context"
	"sync"
)

// Repository provides an atomic transaction boundary. A durable adapter can
// map one Transact call to one database transaction and event outbox append.
type Repository interface {
	Transact(context.Context, func(Transaction) error) error
}

type Transaction interface {
	Get(id string) (AgentRun, bool)
	Put(AgentRun)
	ListChildren(parentID string) []AgentRun
	ListRuns() []AgentRun
	Append(Event)
	ListEvents(runID string) []Event
}

// MemoryRepository is a concurrency-safe reference/test repository.
type MemoryRepository struct {
	mu           sync.Mutex
	runs         map[string]AgentRun
	events       []Event
	nextSequence uint64
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{runs: make(map[string]AgentRun), nextSequence: 1}
}

func (m *MemoryRepository) Transact(ctx context.Context, fn func(Transaction) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Copy-on-write ensures callback failure rolls back all writes.
	t := &memoryTx{runs: make(map[string]AgentRun, len(m.runs)), events: append([]Event(nil), m.events...), next: m.nextSequence}
	for k, v := range m.runs {
		t.runs[k] = cloneRun(v)
	}
	if err := fn(t); err != nil {
		return err
	}
	m.runs, m.events, m.nextSequence = t.runs, t.events, t.next
	return nil
}

type memoryTx struct {
	runs   map[string]AgentRun
	events []Event
	next   uint64
}

func (t *memoryTx) Get(id string) (AgentRun, bool) { r, ok := t.runs[id]; return cloneRun(r), ok }
func (t *memoryTx) Put(r AgentRun)                 { t.runs[r.ID] = cloneRun(r) }
func (t *memoryTx) ListChildren(id string) []AgentRun {
	var out []AgentRun
	for _, r := range t.runs {
		if r.ParentRunID == id {
			out = append(out, cloneRun(r))
		}
	}
	return out
}
func (t *memoryTx) ListRuns() []AgentRun {
	out := make([]AgentRun, 0, len(t.runs))
	for _, r := range t.runs {
		out = append(out, cloneRun(r))
	}
	return out
}
func (t *memoryTx) Append(e Event) { e.Sequence = t.next; t.next++; t.events = append(t.events, e) }
func (t *memoryTx) ListEvents(id string) []Event {
	var out []Event
	for _, e := range t.events {
		if id == "" || e.RunID == id {
			out = append(out, e)
		}
	}
	return out
}
