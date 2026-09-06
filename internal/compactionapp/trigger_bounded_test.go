package compactionapp

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/compaction"
	"github.com/lunitide/lunitide/internal/domain/token"
)

// TestLRUTimeMapCapAndEviction verifies the bounded cooldown map never exceeds
// its capacity and evicts the least-recently-used entry (Q-04).
func TestLRUTimeMapCapAndEviction(t *testing.T) {
	m := newLRUTimeMap(3)
	now := time.Now()
	m.Set("a", now)
	m.Set("b", now)
	m.Set("c", now)
	// Touch "a" so it becomes most-recently-used; "b" is now the LRU.
	if _, ok := m.Get("a"); !ok {
		t.Fatal("expected a present")
	}
	m.Set("d", now) // exceeds cap -> evict LRU ("b")
	if m.Len() != 3 {
		t.Fatalf("len = %d, want 3", m.Len())
	}
	if _, ok := m.Get("b"); ok {
		t.Fatal("expected b evicted")
	}
	for _, k := range []string{"a", "c", "d"} {
		if _, ok := m.Get(k); !ok {
			t.Fatalf("expected %s present", k)
		}
	}
	// Overwrite in place must not grow.
	m.Set("a", now.Add(time.Second))
	if m.Len() != 3 {
		t.Fatalf("len after overwrite = %d, want 3", m.Len())
	}
	m.Delete("a")
	if _, ok := m.Get("a"); ok {
		t.Fatal("expected a deleted")
	}
	if m.Len() != 2 {
		t.Fatalf("len after delete = %d, want 2", m.Len())
	}
}

// TestTriggerLastTriggerBounded feeds far more distinct sessions than the LRU
// capacity and asserts the underlying structure is capped, not unbounded.
func TestTriggerLastTriggerBounded(t *testing.T) {
	tokenRepo := newFakeTokenRepo()
	checkpointStore := newFakeCheckpointStore()
	messages := makeMessages(50)
	for i := range messages {
		tokenRepo.entries[messages[i].ID] = &token.LedgerEntry{MessageID: messages[i].ID, TokenCount: 2000}
	}
	trigger := NewTrigger(DefaultWatermarkConfig(), tokenRepo, checkpointStore, &fakeMessageReader{messages: messages})

	// Shrink the cap so the test is fast but still exercises eviction.
	trigger.lastTrigger = newLRUTimeMap(100)

	const sessions = 5000
	for i := 0; i < sessions; i++ {
		sid := fmt.Sprintf("sess-%d", i)
		tokenRepo.usageBySession[sid] = 100000
		if _, err := trigger.CheckAndTrigger(context.Background(), sid, "p1", "m1", "v1", 100000); err != nil {
			t.Fatalf("trigger %s: %v", sid, err)
		}
	}
	if got := trigger.lastTrigger.Len(); got > 100 {
		t.Fatalf("lastTrigger grew to %d, want <= 100 (memory leak)", got)
	}
}

// TestTriggerCooldownRegression confirms cooldown behavior is unchanged by the
// bounded rewrite: a second immediate trigger on the same session is blocked.
func TestTriggerCooldownRegression(t *testing.T) {
	tokenRepo := newFakeTokenRepo()
	tokenRepo.usageBySession["s1"] = 100000
	checkpointStore := newFakeCheckpointStore()
	messages := makeMessages(50)
	for i := range messages {
		tokenRepo.entries[messages[i].ID] = &token.LedgerEntry{MessageID: messages[i].ID, TokenCount: 2000}
	}
	config := DefaultWatermarkConfig()
	trigger := NewTrigger(config, tokenRepo, checkpointStore, &fakeMessageReader{messages: messages})

	r1, err := trigger.CheckAndTrigger(context.Background(), "s1", "p1", "m1", "v1", 100000)
	if err != nil {
		t.Fatalf("first trigger: %v", err)
	}
	if !r1.Triggered {
		t.Fatalf("expected first trigger to fire: %s", r1.Reason)
	}
	// Mark succeeded so "compaction in progress" is not the blocking reason.
	checkpointStore.checkpoints[r1.CheckpointID].Status = compaction.StatusSucceeded

	r2, err := trigger.CheckAndTrigger(context.Background(), "s1", "p1", "m1", "v1", 100000)
	if err != nil {
		t.Fatalf("second trigger: %v", err)
	}
	if r2.Triggered {
		t.Fatal("expected second immediate trigger to be blocked by cooldown")
	}
}

// TestShardedLatchGetStable verifies same key -> same latch and pool is bounded.
func TestShardedLatchGetStable(t *testing.T) {
	s := newShardedLatch(8)
	if len(s.latches) != 8 {
		t.Fatalf("pool size = %d, want 8", len(s.latches))
	}
	first := s.get("abc")
	second := s.get("abc")
	if first != second {
		t.Fatal("same key must map to the same latch")
	}
}

// safeCheckpointStore is a concurrency-safe CheckpointStore for the -race test.
type safeCheckpointStore struct {
	mu        sync.Mutex
	bySession map[string][]*compaction.Checkpoint
	nextID    int
}

func newSafeCheckpointStore() *safeCheckpointStore {
	return &safeCheckpointStore{bySession: make(map[string][]*compaction.Checkpoint)}
}

func (f *safeCheckpointStore) CreateCheckpoint(ctx context.Context, cp compaction.Checkpoint) (compaction.Checkpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	cp.ID = fmt.Sprintf("cp-%d", f.nextID)
	cp.CreatedAt = time.Now().UTC()
	f.bySession[cp.SessionID] = append(f.bySession[cp.SessionID], &cp)
	return cp, nil
}

func (f *safeCheckpointStore) GetLatestCheckpoint(ctx context.Context, sessionID string) (*compaction.Checkpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cps := f.bySession[sessionID]
	if len(cps) == 0 {
		return nil, nil
	}
	cp := *cps[len(cps)-1]
	return &cp, nil
}

func (f *safeCheckpointStore) CountCheckpointsBySession(ctx context.Context, sessionID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.bySession[sessionID])), nil
}

func (f *safeCheckpointStore) ListCheckpointsByStatus(ctx context.Context, status compaction.Status, limit int) ([]compaction.Checkpoint, error) {
	return nil, nil
}

// safeTokenRepo is a concurrency-safe token.Repository for the -race test.
type safeTokenRepo struct {
	mu    sync.RWMutex
	usage map[string]int64
}

func newSafeTokenRepo() *safeTokenRepo {
	return &safeTokenRepo{usage: make(map[string]int64)}
}

func (f *safeTokenRepo) setUsage(sessionID string, v int64) {
	f.mu.Lock()
	f.usage[sessionID] = v
	f.mu.Unlock()
}

func (f *safeTokenRepo) UpsertTokenLedger(ctx context.Context, entry token.LedgerEntry) error {
	return nil
}

func (f *safeTokenRepo) GetTokenLedger(ctx context.Context, messageID, provider, model, tokenizerRevision string) (*token.LedgerEntry, error) {
	return &token.LedgerEntry{MessageID: messageID, TokenCount: 2000}, nil
}

func (f *safeTokenRepo) ListTokenLedgerByMessage(ctx context.Context, messageID string) ([]token.LedgerEntry, error) {
	return nil, nil
}

func (f *safeTokenRepo) SumTokenLedgerBySession(ctx context.Context, sessionID, provider, model, tokenizerRevision string) (int64, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.usage[sessionID], nil
}

func (f *safeTokenRepo) DeleteTokenLedgerByMessage(ctx context.Context, messageID string) error {
	return nil
}

// TestShardedLatchConcurrentSerialization exercises many sessions concurrently
// to prove there is no data race in the bounded structures (run with -race) and
// that same-session mutual exclusion still holds.
func TestShardedLatchConcurrentSerialization(t *testing.T) {
	tokenRepo := newSafeTokenRepo()
	checkpointStore := newSafeCheckpointStore()
	messages := makeMessages(50) // read-only; concurrent reads are safe
	trigger := NewTrigger(DefaultWatermarkConfig(), tokenRepo, checkpointStore, &fakeMessageReader{messages: messages})

	const sessions = 200
	const perSession = 4

	// Pre-populate usage so no concurrent map writes occur on the repo.
	sids := make([]string, sessions)
	for i := 0; i < sessions; i++ {
		sids[i] = fmt.Sprintf("cc-%d", i)
		tokenRepo.setUsage(sids[i], 100000)
	}

	var wg sync.WaitGroup
	for _, sid := range sids {
		for j := 0; j < perSession; j++ {
			wg.Add(1)
			go func(sid string) {
				defer wg.Done()
				if _, err := trigger.CheckAndTrigger(context.Background(), sid, "p1", "m1", "v1", 100000); err != nil {
					t.Errorf("trigger %s: %v", sid, err)
				}
			}(sid)
		}
	}
	wg.Wait()

	// Same-session serialization + cooldown: each session created exactly one
	// checkpoint despite concurrent callers.
	for _, sid := range sids {
		if n, _ := checkpointStore.CountCheckpointsBySession(context.Background(), sid); n != 1 {
			t.Fatalf("session %s created %d checkpoints, want 1 (serialization/cooldown broken)", sid, n)
		}
	}
}