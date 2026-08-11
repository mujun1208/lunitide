package compactionapp

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/compaction"
	"github.com/lunitide/lunitide/internal/domain/token"
)

// fakeTokenRepo implements token.Repository for testing.
type fakeTokenRepo struct {
	usageBySession map[string]int64
	entries        map[string]*token.LedgerEntry
}

func newFakeTokenRepo() *fakeTokenRepo {
	return &fakeTokenRepo{
		usageBySession: make(map[string]int64),
		entries:        make(map[string]*token.LedgerEntry),
	}
}

func (f *fakeTokenRepo) UpsertTokenLedger(ctx context.Context, entry token.LedgerEntry) error {
	f.entries[entry.MessageID] = &entry
	return nil
}

func (f *fakeTokenRepo) GetTokenLedger(ctx context.Context, messageID, provider, model, tokenizerRevision string) (*token.LedgerEntry, error) {
	if e, ok := f.entries[messageID]; ok {
		return e, nil
	}
	return nil, nil
}

func (f *fakeTokenRepo) ListTokenLedgerByMessage(ctx context.Context, messageID string) ([]token.LedgerEntry, error) {
	if e, ok := f.entries[messageID]; ok {
		return []token.LedgerEntry{*e}, nil
	}
	return nil, nil
}

func (f *fakeTokenRepo) SumTokenLedgerBySession(ctx context.Context, sessionID, provider, model, tokenizerRevision string) (int64, error) {
	return f.usageBySession[sessionID], nil
}

func (f *fakeTokenRepo) DeleteTokenLedgerByMessage(ctx context.Context, messageID string) error {
	delete(f.entries, messageID)
	return nil
}

// fakeCheckpointStore implements CheckpointStore for testing.
type fakeCheckpointStore struct {
	checkpoints map[string]*compaction.Checkpoint
	bySession   map[string][]*compaction.Checkpoint
}

func newFakeCheckpointStore() *fakeCheckpointStore {
	return &fakeCheckpointStore{
		checkpoints: make(map[string]*compaction.Checkpoint),
		bySession:   make(map[string][]*compaction.Checkpoint),
	}
}

func (f *fakeCheckpointStore) CreateCheckpoint(ctx context.Context, cp compaction.Checkpoint) (compaction.Checkpoint, error) {
	if cp.ID == "" {
		cp.ID = "01JABCDEFGHJKMNPQRSTVWXYZ00"
	}
	cp.CreatedAt = time.Now().UTC()
	f.checkpoints[cp.ID] = &cp
	f.bySession[cp.SessionID] = append(f.bySession[cp.SessionID], &cp)
	return cp, nil
}

func (f *fakeCheckpointStore) GetLatestCheckpoint(ctx context.Context, sessionID string) (*compaction.Checkpoint, error) {
	cps := f.bySession[sessionID]
	if len(cps) == 0 {
		return nil, nil
	}
	latest := cps[len(cps)-1]
	return latest, nil
}

func (f *fakeCheckpointStore) CountCheckpointsBySession(ctx context.Context, sessionID string) (int64, error) {
	return int64(len(f.bySession[sessionID])), nil
}

// fakeMessageReader implements MessageReader for testing.
type fakeMessageReader struct {
	messages []MessageInfo
}

func (f *fakeMessageReader) ListMessages(ctx context.Context, sessionID string, direction string, limit int) ([]MessageInfo, error) {
	if limit > len(f.messages) {
		limit = len(f.messages)
	}
	return f.messages[:limit], nil
}

func TestTrigger_BelowHighWatermark(t *testing.T) {
	tokenRepo := newFakeTokenRepo()
	tokenRepo.usageBySession["s1"] = 5000 // 5000 tokens
	checkpointStore := newFakeCheckpointStore()
	msgReader := &fakeMessageReader{
		messages: makeMessages(50),
	}

	config := DefaultWatermarkConfig()
	trigger := NewTrigger(config, tokenRepo, checkpointStore, msgReader)

	result, err := trigger.CheckAndTrigger(context.Background(), "s1", "p1", "m1", "v1", 100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Triggered {
		t.Errorf("expected no trigger, got triggered: %s", result.Reason)
	}
	// 5000 < 100000 * 0.80 = 80000
	expectedFraction := 5000.0 / 80000.0
	if result.UsageFraction != expectedFraction {
		t.Errorf("expected usage fraction %f, got %f", expectedFraction, result.UsageFraction)
	}
}

func TestTrigger_AboveHighWatermark(t *testing.T) {
	tokenRepo := newFakeTokenRepo()
	// 100,000 tokens > 100,000 * 0.80 = 80,000
	tokenRepo.usageBySession["s1"] = 100000
	checkpointStore := newFakeCheckpointStore()
	// Create 50 messages. Each message has 2000 tokens.
	// 50 * 2000 = 100,000 tokens. Low watermark = 60,000.
	// Newest 30 messages (60,000 tokens) fit within low watermark.
	// Oldest 20 messages (40,000 tokens) need compaction.
	messages := makeMessages(50)
	for i := range messages {
		tokenRepo.entries[messages[i].ID] = &token.LedgerEntry{
			MessageID:  messages[i].ID,
			TokenCount: 2000,
		}
	}
	msgReader := &fakeMessageReader{messages: messages}

	config := DefaultWatermarkConfig()
	trigger := NewTrigger(config, tokenRepo, checkpointStore, msgReader)

	result, err := trigger.CheckAndTrigger(context.Background(), "s1", "p1", "m1", "v1", 100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Triggered {
		t.Errorf("expected trigger, got no trigger: %s", result.Reason)
	}
	if result.CheckpointID == "" {
		t.Error("expected non-empty checkpoint ID")
	}
}

func TestTrigger_Cooldown(t *testing.T) {
	tokenRepo := newFakeTokenRepo()
	tokenRepo.usageBySession["s1"] = 100000
	checkpointStore := newFakeCheckpointStore()
	messages := makeMessages(50)
	for i := range messages {
		tokenRepo.entries[messages[i].ID] = &token.LedgerEntry{
			MessageID:  messages[i].ID,
			TokenCount: 2000,
		}
	}
	msgReader := &fakeMessageReader{messages: messages}

	config := DefaultWatermarkConfig()
	trigger := NewTrigger(config, tokenRepo, checkpointStore, msgReader)

	// First trigger should succeed.
	result, err := trigger.CheckAndTrigger(context.Background(), "s1", "p1", "m1", "v1", 100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Triggered {
		t.Fatal("expected first trigger to succeed")
	}

	// Second trigger within cooldown should be blocked.
	result2, err := trigger.CheckAndTrigger(context.Background(), "s1", "p1", "m1", "v1", 100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result2.Triggered {
		t.Errorf("expected cooldown to block trigger, got triggered: %s", result2.Reason)
	}
}

func TestTrigger_CompactionInProgress(t *testing.T) {
	tokenRepo := newFakeTokenRepo()
	tokenRepo.usageBySession["s1"] = 100000
	checkpointStore := newFakeCheckpointStore()
	// Pre-create a pending checkpoint.
	checkpointStore.checkpoints["cp1"] = &compaction.Checkpoint{
		ID:        "cp1",
		SessionID: "s1",
		Status:    compaction.StatusPending,
		Version:   1,
	}
	checkpointStore.bySession["s1"] = []*compaction.Checkpoint{checkpointStore.checkpoints["cp1"]}

	messages := makeMessages(50)
	for i := range messages {
		tokenRepo.entries[messages[i].ID] = &token.LedgerEntry{
			MessageID:  messages[i].ID,
			TokenCount: 2000,
		}
	}
	msgReader := &fakeMessageReader{messages: messages}

	config := DefaultWatermarkConfig()
	trigger := NewTrigger(config, tokenRepo, checkpointStore, msgReader)

	result, err := trigger.CheckAndTrigger(context.Background(), "s1", "p1", "m1", "v1", 100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Triggered {
		t.Errorf("expected no trigger when compaction in progress, got: %s", result.Reason)
	}
}

func TestTrigger_InsufficientMessages(t *testing.T) {
	tokenRepo := newFakeTokenRepo()
	tokenRepo.usageBySession["s1"] = 100000
	checkpointStore := newFakeCheckpointStore()
	// Only 5 messages, below MinMessagesBeforeCompaction=10
	messages := makeMessages(5)
	for i := range messages {
		tokenRepo.entries[messages[i].ID] = &token.LedgerEntry{
			MessageID:  messages[i].ID,
			TokenCount: 20000,
		}
	}
	msgReader := &fakeMessageReader{messages: messages}

	config := DefaultWatermarkConfig()
	trigger := NewTrigger(config, tokenRepo, checkpointStore, msgReader)

	result, err := trigger.CheckAndTrigger(context.Background(), "s1", "p1", "m1", "v1", 100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Triggered {
		t.Errorf("expected no trigger with insufficient messages, got: %s", result.Reason)
	}
}

func TestTrigger_ResetCooldown(t *testing.T) {
	tokenRepo := newFakeTokenRepo()
	tokenRepo.usageBySession["s1"] = 100000
	checkpointStore := newFakeCheckpointStore()
	messages := makeMessages(50)
	for i := range messages {
		tokenRepo.entries[messages[i].ID] = &token.LedgerEntry{
			MessageID:  messages[i].ID,
			TokenCount: 2000,
		}
	}
	msgReader := &fakeMessageReader{messages: messages}

	config := DefaultWatermarkConfig()
	trigger := NewTrigger(config, tokenRepo, checkpointStore, msgReader)

	// First trigger.
	result, err := trigger.CheckAndTrigger(context.Background(), "s1", "p1", "m1", "v1", 100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Triggered {
		t.Fatal("expected first trigger to succeed")
	}

	// Mark the checkpoint as succeeded to simulate compaction completion.
	cp := checkpointStore.checkpoints[result.CheckpointID]
	cp.Status = compaction.StatusSucceeded

	// Reset cooldown.
	trigger.ResetCooldown("s1")

	// Second trigger should succeed after reset.
	result2, err := trigger.CheckAndTrigger(context.Background(), "s1", "p1", "m1", "v1", 100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result2.Triggered {
		t.Errorf("expected trigger after cooldown reset, got: %s", result2.Reason)
	}
}

func TestTrigger_AllMessagesFitLowWatermark(t *testing.T) {
	tokenRepo := newFakeTokenRepo()
	// 50,000 tokens. High watermark: 80,000. Low watermark: 60,000.
	// All messages fit within low watermark, so no compaction needed.
	tokenRepo.usageBySession["s1"] = 50000
	checkpointStore := newFakeCheckpointStore()
	messages := makeMessages(50)
	for i := range messages {
		tokenRepo.entries[messages[i].ID] = &token.LedgerEntry{
			MessageID:  messages[i].ID,
			TokenCount: 500,
		}
	}
	msgReader := &fakeMessageReader{messages: messages}

	config := DefaultWatermarkConfig()
	trigger := NewTrigger(config, tokenRepo, checkpointStore, msgReader)

	// Even though usage is 50,000 which is below 80,000 (high watermark),
	// the test verifies that we don't falsely trigger.
	result, err := trigger.CheckAndTrigger(context.Background(), "s1", "p1", "m1", "v1", 100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Triggered {
		t.Errorf("expected no trigger, got: %s", result.Reason)
	}
}

func TestDefaultWatermarkConfig(t *testing.T) {
	config := DefaultWatermarkConfig()
	if config.HighWatermark != 0.80 {
		t.Errorf("expected high watermark 0.80, got %f", config.HighWatermark)
	}
	if config.LowWatermark != 0.60 {
		t.Errorf("expected low watermark 0.60, got %f", config.LowWatermark)
	}
	if config.MinMessagesBeforeCompaction != 10 {
		t.Errorf("expected min messages 10, got %d", config.MinMessagesBeforeCompaction)
	}
	if config.CheckCooldown != 30*time.Second {
		t.Errorf("expected cooldown 30s, got %v", config.CheckCooldown)
	}
}

func TestTrigger_CheckpointVersionIncrement(t *testing.T) {
	tokenRepo := newFakeTokenRepo()
	tokenRepo.usageBySession["s1"] = 100000
	checkpointStore := newFakeCheckpointStore()
	// Pre-create a succeeded checkpoint at version 3.
	checkpointStore.checkpoints["cp-old"] = &compaction.Checkpoint{
		ID:           "cp-old",
		SessionID:    "s1",
		Status:       compaction.StatusSucceeded,
		Version:      3,
		SourceDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	checkpointStore.bySession["s1"] = []*compaction.Checkpoint{checkpointStore.checkpoints["cp-old"]}

	messages := makeMessages(50)
	for i := range messages {
		tokenRepo.entries[messages[i].ID] = &token.LedgerEntry{
			MessageID:  messages[i].ID,
			TokenCount: 2000,
		}
	}
	msgReader := &fakeMessageReader{messages: messages}

	config := DefaultWatermarkConfig()
	trigger := NewTrigger(config, tokenRepo, checkpointStore, msgReader)

	result, err := trigger.CheckAndTrigger(context.Background(), "s1", "p1", "m1", "v1", 100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Triggered {
		t.Fatal("expected trigger to succeed")
	}

	created := checkpointStore.checkpoints[result.CheckpointID]
	if created == nil {
		t.Fatal("checkpoint not found in store")
	}
	if created.Version != 4 {
		t.Errorf("expected version 4, got %d", created.Version)
	}
	if created.PrevCheckpointID == nil || *created.PrevCheckpointID != "cp-old" {
		t.Errorf("expected prev_checkpoint_id to be cp-old, got %v", created.PrevCheckpointID)
	}
	if created.Trigger != compaction.TriggerAutomatic {
		t.Errorf("expected trigger automatic, got %s", created.Trigger)
	}
}

func makeMessages(n int) []MessageInfo {
	msgs := make([]MessageInfo, n)
	// Generate in backward order (newest first) to match the reader contract.
	for i := 0; i < n; i++ {
		seq := int64(n - i)
		msgs[i] = MessageInfo{
			ID:       fmt.Sprintf("msg-%d", seq),
			Sequence: seq,
		}
	}
	return msgs
}

// makeForwardMessages generates n messages in forward order (oldest first).
func makeForwardMessages(n int) []MessageInfo {
	msgs := make([]MessageInfo, n)
	for i := 0; i < n; i++ {
		seq := int64(i + 1)
		msgs[i] = MessageInfo{
			ID:       fmt.Sprintf("msg-%d", seq),
			Sequence: seq,
		}
	}
	return msgs
}

func TestTriggerManualCreatesCheckpoint(t *testing.T) {
	tokenRepo := newFakeTokenRepo()
	checkpointStore := newFakeCheckpointStore()
	msgReader := &fakeMessageReader{messages: makeForwardMessages(50)}

	config := DefaultWatermarkConfig()
	trigger := NewTrigger(config, tokenRepo, checkpointStore, msgReader)

	result, err := trigger.TriggerManual(context.Background(), "s1", "p1", "m1", 1, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Triggered {
		t.Fatalf("expected trigger, got: %s", result.Reason)
	}
	if result.MessageCount != 20 {
		t.Fatalf("expected 20 messages in range, got %d", result.MessageCount)
	}
	if result.SourceStartSeq != 1 || result.SourceEndSeq != 20 {
		t.Fatalf("expected range 1-20, got %d-%d", result.SourceStartSeq, result.SourceEndSeq)
	}

	// Verify checkpoint was created with manual trigger.
	latest, err := checkpointStore.GetLatestCheckpoint(context.Background(), "s1")
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil {
		t.Fatal("no checkpoint created")
	}
	if latest.Trigger != compaction.TriggerManual {
		t.Fatalf("expected trigger manual, got %s", latest.Trigger)
	}
	if latest.Status != compaction.StatusPending {
		t.Fatalf("expected pending status, got %s", latest.Status)
	}
	if latest.Version != 1 {
		t.Fatalf("expected version 1, got %d", latest.Version)
	}
}

func TestTriggerManualInvalidRange(t *testing.T) {
	trigger := NewTrigger(DefaultWatermarkConfig(), newFakeTokenRepo(), newFakeCheckpointStore(), &fakeMessageReader{})

	_, err := trigger.TriggerManual(context.Background(), "s1", "p1", "m1", 10, 5)
	if err == nil {
		t.Fatal("expected error for start > end")
	}

	_, err = trigger.TriggerManual(context.Background(), "s1", "p1", "m1", 0, 5)
	if err == nil {
		t.Fatal("expected error for start < 1")
	}
}

func TestTriggerManualInsufficientMessages(t *testing.T) {
	config := DefaultWatermarkConfig() // MinMessagesBeforeCompaction = 10
	msgReader := &fakeMessageReader{messages: makeForwardMessages(5)}
	trigger := NewTrigger(config, newFakeTokenRepo(), newFakeCheckpointStore(), msgReader)

	result, err := trigger.TriggerManual(context.Background(), "s1", "p1", "m1", 1, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Triggered {
		t.Fatal("expected no trigger for insufficient messages")
	}
}

func TestTriggerManualAlreadyInProgress(t *testing.T) {
	checkpointStore := newFakeCheckpointStore()
	// Pre-create a pending checkpoint.
	checkpointStore.checkpoints["cp1"] = &compaction.Checkpoint{
		ID:        "cp1",
		SessionID: "s1",
		Version:   1,
		Status:    compaction.StatusPending,
	}
	checkpointStore.bySession["s1"] = []*compaction.Checkpoint{checkpointStore.checkpoints["cp1"]}

	msgReader := &fakeMessageReader{messages: makeForwardMessages(50)}
	trigger := NewTrigger(DefaultWatermarkConfig(), newFakeTokenRepo(), checkpointStore, msgReader)

	result, err := trigger.TriggerManual(context.Background(), "s1", "p1", "m1", 1, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Triggered {
		t.Fatal("expected no trigger when compaction in progress")
	}
}

func TestTriggerManualLinksToPrevSucceeded(t *testing.T) {
	checkpointStore := newFakeCheckpointStore()
	// Pre-create a succeeded checkpoint.
	succeededDigest := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	checkpointStore.checkpoints["cp1"] = &compaction.Checkpoint{
		ID:           "cp1",
		SessionID:    "s1",
		Version:      1,
		Status:       compaction.StatusSucceeded,
		SourceDigest: succeededDigest,
	}
	checkpointStore.bySession["s1"] = []*compaction.Checkpoint{checkpointStore.checkpoints["cp1"]}

	msgReader := &fakeMessageReader{messages: makeForwardMessages(50)}
	trigger := NewTrigger(DefaultWatermarkConfig(), newFakeTokenRepo(), checkpointStore, msgReader)

	result, err := trigger.TriggerManual(context.Background(), "s1", "p1", "m1", 1, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Triggered {
		t.Fatalf("expected trigger, got: %s", result.Reason)
	}

	latest, _ := checkpointStore.GetLatestCheckpoint(context.Background(), "s1")
	if latest.Version != 2 {
		t.Fatalf("expected version 2, got %d", latest.Version)
	}
	if latest.PrevCheckpointID == nil || *latest.PrevCheckpointID != "cp1" {
		t.Fatalf("expected prev_checkpoint_id cp1, got %v", latest.PrevCheckpointID)
	}
	if latest.PrevCheckpointDigest == nil || *latest.PrevCheckpointDigest != succeededDigest {
		t.Fatalf("expected prev_checkpoint_digest, got %v", latest.PrevCheckpointDigest)
	}
}

