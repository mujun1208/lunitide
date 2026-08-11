package handoffapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/compaction"
	"github.com/lunitide/lunitide/internal/domain/handoff"
	"github.com/oklog/ulid/v2"
)

// --- Mocks ---

type mockCheckpointReader struct {
	checkpoint *compaction.Checkpoint
	err        error
}

func (m *mockCheckpointReader) GetCheckpoint(_ context.Context, id string) (*compaction.Checkpoint, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.checkpoint != nil && m.checkpoint.ID == id {
		return m.checkpoint, nil
	}
	return nil, nil
}

type mockCapsuleStore struct {
	capsules  map[string]*handoff.Capsule
	created   []handoff.Capsule
	activated []string
	revoked   []string
	expired   []string
	imports   map[string]time.Time // key: capsuleID+":"+targetSessionID
}

func newMockCapsuleStore() *mockCapsuleStore {
	return &mockCapsuleStore{
		capsules: make(map[string]*handoff.Capsule),
		imports:  make(map[string]time.Time),
	}
}

func (m *mockCapsuleStore) CreateCapsule(_ context.Context, c handoff.Capsule) (handoff.Capsule, error) {
	if c.ID == "" {
		c.ID = ulid.Make().String()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	if c.Status == "" {
		c.Status = handoff.StatusActive
	}
	if c.ActiveTasksJSON == "" {
		c.ActiveTasksJSON = "[]"
	}
	if c.RecentMessageIDs == "" {
		c.RecentMessageIDs = "[]"
	}
	m.capsules[c.ID] = &c
	m.created = append(m.created, c)
	return c, nil
}

func (m *mockCapsuleStore) GetCapsule(_ context.Context, id string) (*handoff.Capsule, error) {
	if c, ok := m.capsules[id]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, nil
}

func (m *mockCapsuleStore) ListCapsulesBySourceSession(_ context.Context, sessionID string, limit int) ([]handoff.Capsule, error) {
	var result []handoff.Capsule
	for _, c := range m.capsules {
		if c.SourceSessionID == sessionID {
			result = append(result, *c)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *mockCapsuleStore) ListActiveCapsules(_ context.Context, sessionID string) ([]handoff.Capsule, error) {
	var result []handoff.Capsule
	for _, c := range m.capsules {
		if c.SourceSessionID == sessionID && c.Status == handoff.StatusActive {
			result = append(result, *c)
		}
	}
	return result, nil
}

func (m *mockCapsuleStore) ActivateCapsule(_ context.Context, id, destSessionID string) error {
	c, ok := m.capsules[id]
	if !ok || c.Status != handoff.StatusActive {
		return errors.New("not active")
	}
	c.Status = handoff.StatusActivated
	now := time.Now().UTC()
	c.ActivatedAt = &now
	dest := destSessionID
	c.DestSessionID = &dest
	m.activated = append(m.activated, id)
	return nil
}

func (m *mockCapsuleStore) RevokeCapsule(_ context.Context, id string) error {
	c, ok := m.capsules[id]
	if !ok || c.Status != handoff.StatusActive {
		return errors.New("not active")
	}
	c.Status = handoff.StatusRevoked
	m.revoked = append(m.revoked, id)
	return nil
}

func (m *mockCapsuleStore) ExpireCapsule(_ context.Context, id string) error {
	c, ok := m.capsules[id]
	if !ok || c.Status != handoff.StatusActive {
		return errors.New("not active")
	}
	c.Status = handoff.StatusExpired
	m.expired = append(m.expired, id)
	return nil
}

func (m *mockCapsuleStore) RecordImport(_ context.Context, capsuleID, targetSessionID string) (importID string, importedAt time.Time, isNew bool, err error) {
	key := capsuleID + ":" + targetSessionID
	if existingAt, ok := m.imports[key]; ok {
		return "", existingAt, false, nil
	}
	now := time.Now().UTC()
	m.imports[key] = now
	return ulid.Make().String(), now, true, nil
}

func (m *mockCapsuleStore) GetImport(_ context.Context, capsuleID, targetSessionID string) (importedAt time.Time, ok bool, err error) {
	key := capsuleID + ":" + targetSessionID
	at, exists := m.imports[key]
	return at, exists, nil
}

func (m *mockCapsuleStore) ListImportedCapsules(_ context.Context, targetSessionID string) ([]handoff.Capsule, error) {
	var result []handoff.Capsule
	for key := range m.imports {
		// key = capsuleID + ":" + targetSessionID
		parts := splitImportKey(key)
		if parts[1] == targetSessionID {
			if c, ok := m.capsules[parts[0]]; ok {
				result = append(result, *c)
			}
		}
	}
	return result, nil
}

// splitImportKey splits a "capsuleID:targetSessionID" key. ULIDs are 26 chars
// fixed-length, so the split is unambiguous.
func splitImportKey(key string) [2]string {
	if len(key) < 27 {
		return [2]string{"", ""}
	}
	return [2]string{key[:26], key[27:]}
}

// --- Helpers ---

func mustULID() string {
	return ulid.Make().String()
}

// validCheckpoint returns a succeeded checkpoint with valid invariants for testing.
func validCheckpoint(sessionID string) compaction.Checkpoint {
	now := time.Now().UTC()
	return compaction.Checkpoint{
		ID:                   mustULID(),
		SessionID:            sessionID,
		Version:              1,
		SourceStartID:        mustULID(),
		SourceEndID:          mustULID(),
		SourceStartSeq:       1,
		SourceEndSeq:         10,
		SourceDigest:         "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		SummarySchemaVersion: compaction.SummarySchemaVersion,
		Trigger:              compaction.TriggerManual,
		TriggerReason:        "test",
		Status:               compaction.StatusSucceeded,
		Provider:             "test-provider",
		Model:                "test-model",
		SummaryJSON:          `{"summary":"test"}`,
		HumanSummary:         "test summary",
		CreatedAt:            now,
		CompletedAt:          &now,
	}
}

func newTestService(cp *compaction.Checkpoint, cpErr error) (*Service, *mockCapsuleStore, *mockCheckpointReader) {
	reader := &mockCheckpointReader{checkpoint: cp, err: cpErr}
	store := newMockCapsuleStore()
	svc := NewService(reader, store)
	return svc, store, reader
}

// --- CreateCapsule Tests ---

func TestService_CreateCapsule_Success(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, store, _ := newTestService(&cp, nil)

	capsule, err := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID:   sessionID,
		CheckpointID:      cp.ID,
		RecentMessageIDs:  []string{mustULID(), mustULID()},
		ActiveTasksJSON:   `[{"task":"T1"}]`,
	})
	if err != nil {
		t.Fatalf("CreateCapsule failed: %v", err)
	}
	if capsule.ID == "" {
		t.Fatal("capsule ID empty")
	}
	if capsule.SourceSessionID != sessionID {
		t.Errorf("source session = %s, want %s", capsule.SourceSessionID, sessionID)
	}
	if capsule.CheckpointID != cp.ID {
		t.Errorf("checkpoint ID = %s, want %s", capsule.CheckpointID, cp.ID)
	}
	if capsule.Status != handoff.StatusActive {
		t.Errorf("status = %s, want active", capsule.Status)
	}
	if len(capsule.Digest) != 64 {
		t.Errorf("digest len = %d, want 64", len(capsule.Digest))
	}
	if capsule.RecentMessageIDs == "[]" {
		t.Error("recent message IDs should not be empty array")
	}
	if capsule.ActiveTasksJSON != `[{"task":"T1"}]` {
		t.Errorf("active tasks = %s", capsule.ActiveTasksJSON)
	}
	if len(store.created) != 1 {
		t.Errorf("created count = %d, want 1", len(store.created))
	}
}

func TestService_CreateCapsule_CheckpointNotFound(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, _, _ := newTestService(&cp, nil)

	// Request with a non-existent checkpoint ID.
	_, err := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    mustULID(), // different ID
	})
	if !errors.Is(err, ErrCheckpointNotFound) {
		t.Errorf("err = %v, want ErrCheckpointNotFound", err)
	}
}

func TestService_CreateCapsule_CheckpointNotSucceeded(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	cp.Status = compaction.StatusPending
	svc, _, _ := newTestService(&cp, nil)

	_, err := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})
	if !errors.Is(err, ErrCheckpointNotSucceeded) {
		t.Errorf("err = %v, want ErrCheckpointNotSucceeded", err)
	}
}

func TestService_CreateCapsule_SessionMismatch(t *testing.T) {
	cp := validCheckpoint(mustULID())
	svc, _, _ := newTestService(&cp, nil)

	_, err := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: mustULID(), // different session
		CheckpointID:    cp.ID,
	})
	if err == nil {
		t.Fatal("expected session mismatch error")
	}
}

func TestService_CreateCapsule_GetCheckpointError(t *testing.T) {
	sessionID := mustULID()
	svc, _, _ := newTestService(nil, errors.New("db error"))

	_, err := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    mustULID(),
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestService_CreateCapsule_EmptyFieldsDefault(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, _, _ := newTestService(&cp, nil)

	capsule, err := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
		// RecentMessageIDs and ActiveTasksJSON empty
	})
	if err != nil {
		t.Fatalf("CreateCapsule failed: %v", err)
	}
	if capsule.RecentMessageIDs != "[]" {
		t.Errorf("recent IDs = %s, want []", capsule.RecentMessageIDs)
	}
	if capsule.ActiveTasksJSON != "[]" {
		t.Errorf("active tasks = %s, want []", capsule.ActiveTasksJSON)
	}
}

// --- GetCapsule Tests ---

func TestService_GetCapsule_Success(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, _, _ := newTestService(&cp, nil)

	created, err := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})
	if err != nil {
		t.Fatalf("CreateCapsule failed: %v", err)
	}

	got, err := svc.GetCapsule(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetCapsule failed: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID = %s, want %s", got.ID, created.ID)
	}
}

func TestService_GetCapsule_NotFound(t *testing.T) {
	svc, _, _ := newTestService(nil, nil)

	_, err := svc.GetCapsule(context.Background(), mustULID())
	if !errors.Is(err, ErrCapsuleNotFound) {
		t.Errorf("err = %v, want ErrCapsuleNotFound", err)
	}
}

// --- ListCapsules Tests ---

func TestService_ListCapsulesBySourceSession_LimitClamped(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, _, _ := newTestService(&cp, nil)

	for i := 0; i < 3; i++ {
		_, err := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
			SourceSessionID: sessionID,
			CheckpointID:    cp.ID,
		})
		if err != nil {
			t.Fatalf("CreateCapsule[%d] failed: %v", i, err)
		}
	}

	// Limit 0 → default 50.
	result, err := svc.ListCapsulesBySourceSession(context.Background(), sessionID, 0)
	if err != nil {
		t.Fatalf("ListCapsules failed: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("len = %d, want 3", len(result))
	}

	// Limit 2.
	result, err = svc.ListCapsulesBySourceSession(context.Background(), sessionID, 2)
	if err != nil {
		t.Fatalf("ListCapsules failed: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("len = %d, want 2", len(result))
	}

	// Limit > 100 → default 50.
	result, err = svc.ListCapsulesBySourceSession(context.Background(), sessionID, 200)
	if err != nil {
		t.Fatalf("ListCapsules failed: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("len = %d, want 3", len(result))
	}
}

func TestService_ListActiveCapsules(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, store, _ := newTestService(&cp, nil)

	_, _ = svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})
	created2, _ := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})

	// Revoke one capsule directly in the store.
	_ = store.RevokeCapsule(context.Background(), created2.ID)

	active, err := svc.ListActiveCapsules(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("ListActiveCapsules failed: %v", err)
	}
	if len(active) != 1 {
		t.Errorf("active count = %d, want 1", len(active))
	}
}

// --- ActivateCapsule Tests ---

func TestService_ActivateCapsule_Success(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, _, _ := newTestService(&cp, nil)

	created, err := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID:  sessionID,
		CheckpointID:     cp.ID,
		RecentMessageIDs: []string{mustULID()},
	})
	if err != nil {
		t.Fatalf("CreateCapsule failed: %v", err)
	}

	destSession := mustULID()
	result, err := svc.ActivateCapsule(context.Background(), created.ID, destSession)
	if err != nil {
		t.Fatalf("ActivateCapsule failed: %v", err)
	}
	if !result.DigestValid {
		t.Error("DigestValid = false, want true")
	}
	if result.ExpiredCheck {
		t.Error("ExpiredCheck = true, want false")
	}
	if result.Capsule.Status != handoff.StatusActivated {
		t.Errorf("status = %s, want activated", result.Capsule.Status)
	}
	if result.Capsule.DestSessionID == nil || *result.Capsule.DestSessionID != destSession {
		t.Error("dest session not bound")
	}
	if result.Checkpoint == nil || result.Checkpoint.ID != cp.ID {
		t.Error("checkpoint not returned")
	}
}

func TestService_ActivateCapsule_NotFound(t *testing.T) {
	svc, _, _ := newTestService(nil, nil)

	_, err := svc.ActivateCapsule(context.Background(), mustULID(), mustULID())
	if !errors.Is(err, ErrCapsuleNotFound) {
		t.Errorf("err = %v, want ErrCapsuleNotFound", err)
	}
}

func TestService_ActivateCapsule_NotActive(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, store, _ := newTestService(&cp, nil)

	created, _ := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})
	// Revoke to make it non-active.
	_ = store.RevokeCapsule(context.Background(), created.ID)

	_, err := svc.ActivateCapsule(context.Background(), created.ID, mustULID())
	if !errors.Is(err, ErrCapsuleNotActive) {
		t.Errorf("err = %v, want ErrCapsuleNotActive", err)
	}
}

func TestService_ActivateCapsule_Expired(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, _, _ := newTestService(&cp, nil)

	pastTime := time.Now().UTC().Add(-1 * time.Hour)
	created, err := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
		ExpiresAt:       &pastTime,
	})
	if err != nil {
		t.Fatalf("CreateCapsule failed: %v", err)
	}

	_, err = svc.ActivateCapsule(context.Background(), created.ID, mustULID())
	if !errors.Is(err, ErrCapsuleExpired) {
		t.Errorf("err = %v, want ErrCapsuleExpired", err)
	}
}

func TestService_ActivateCapsule_DigestMismatch(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, store, _ := newTestService(&cp, nil)

	created, _ := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})

	// Tamper with the stored capsule's digest.
	store.capsules[created.ID].Digest = "0000000000000000000000000000000000000000000000000000000000000000"

	_, err := svc.ActivateCapsule(context.Background(), created.ID, mustULID())
	if err == nil {
		t.Fatal("expected digest mismatch error")
	}
}

func TestService_ActivateCapsule_CheckpointDeleted(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, store, reader := newTestService(&cp, nil)

	created, _ := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})

	// Simulate checkpoint deletion: reader returns nil.
	reader.checkpoint = nil
	// The store still has the capsule; keep it active.
	_ = store

	_, err := svc.ActivateCapsule(context.Background(), created.ID, mustULID())
	if !errors.Is(err, ErrCheckpointNotFound) {
		t.Errorf("err = %v, want ErrCheckpointNotFound", err)
	}
}

func TestService_ActivateCapsule_NoExpiry(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, _, _ := newTestService(&cp, nil)

	created, err := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
		// ExpiresAt nil → never expires.
	})
	if err != nil {
		t.Fatalf("CreateCapsule failed: %v", err)
	}

	_, err = svc.ActivateCapsule(context.Background(), created.ID, mustULID())
	if err != nil {
		t.Fatalf("ActivateCapsule failed: %v", err)
	}
}

// --- RevokeCapsule Tests ---

func TestService_RevokeCapsule_Success(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, _, _ := newTestService(&cp, nil)

	created, _ := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})

	err := svc.RevokeCapsule(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("RevokeCapsule failed: %v", err)
	}

	got, _ := svc.GetCapsule(context.Background(), created.ID)
	if got.Status != handoff.StatusRevoked {
		t.Errorf("status = %s, want revoked", got.Status)
	}
}

func TestService_RevokeCapsule_NotFound(t *testing.T) {
	svc, _, _ := newTestService(nil, nil)

	err := svc.RevokeCapsule(context.Background(), mustULID())
	if !errors.Is(err, ErrCapsuleNotFound) {
		t.Errorf("err = %v, want ErrCapsuleNotFound", err)
	}
}

func TestService_RevokeCapsule_NotActive(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, store, _ := newTestService(&cp, nil)

	created, _ := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})
	// Already revoke it.
	_ = store.RevokeCapsule(context.Background(), created.ID)

	err := svc.RevokeCapsule(context.Background(), created.ID)
	if !errors.Is(err, ErrCapsuleNotActive) {
		t.Errorf("err = %v, want ErrCapsuleNotActive", err)
	}
}

// --- Digest Integrity Test ---

func TestService_DigestBinding_RoundTrip(t *testing.T) {
	// Verify that the digest computed at creation matches the one verified at activation.
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, _, _ := newTestService(&cp, nil)

	recentIDs := []string{mustULID(), mustULID(), mustULID()}
	tasks := `[{"id":"T1","status":"in-progress"},{"id":"T2","status":"blocked"}]`

	created, err := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID:  sessionID,
		CheckpointID:     cp.ID,
		RecentMessageIDs: recentIDs,
		ActiveTasksJSON:  tasks,
	})
	if err != nil {
		t.Fatalf("CreateCapsule failed: %v", err)
	}

	result, err := svc.ActivateCapsule(context.Background(), created.ID, mustULID())
	if err != nil {
		t.Fatalf("ActivateCapsule failed: %v", err)
	}
	if !result.DigestValid {
		t.Error("digest should be valid when checkpoint and carried state are unchanged")
	}
}

// --- ImportCapsule Tests ---

func TestService_ImportCapsule_Success(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, store, _ := newTestService(&cp, nil)

	created, err := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})
	if err != nil {
		t.Fatalf("CreateCapsule failed: %v", err)
	}

	targetSession := mustULID()
	result, err := svc.ImportCapsule(context.Background(), created.ID, targetSession)
	if err != nil {
		t.Fatalf("ImportCapsule failed: %v", err)
	}
	if !result.DigestValid {
		t.Error("DigestValid = false, want true")
	}
	if result.ExpiredCheck {
		t.Error("ExpiredCheck = true, want false")
	}
	if result.AlreadyImported {
		t.Error("AlreadyImported = true on first import")
	}
	if result.ImportedAt.IsZero() {
		t.Error("ImportedAt should be set")
	}
	if result.Checkpoint == nil || result.Checkpoint.ID != cp.ID {
		t.Error("checkpoint not returned or mismatched")
	}
	// Capsule status should remain active (import does not change status).
	if result.Capsule.Status != handoff.StatusActive {
		t.Errorf("status = %s, want active (import must not change status)", result.Capsule.Status)
	}
	// Import should be recorded in the store.
	if len(store.imports) != 1 {
		t.Errorf("imports count = %d, want 1", len(store.imports))
	}
}

func TestService_ImportCapsule_IdempotentReimport(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, store, _ := newTestService(&cp, nil)

	created, _ := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})

	targetSession := mustULID()
	// First import.
	result1, err := svc.ImportCapsule(context.Background(), created.ID, targetSession)
	if err != nil {
		t.Fatalf("first ImportCapsule failed: %v", err)
	}
	if result1.AlreadyImported {
		t.Error("first import should not be AlreadyImported")
	}
	// Second import of the same capsule into the same session.
	result2, err := svc.ImportCapsule(context.Background(), created.ID, targetSession)
	if err != nil {
		t.Fatalf("second ImportCapsule failed: %v", err)
	}
	if !result2.AlreadyImported {
		t.Error("second import should be AlreadyImported (idempotent)")
	}
	// imported_at should be the same.
	if !result1.ImportedAt.Equal(result2.ImportedAt) {
		t.Errorf("imported_at changed: %v vs %v", result1.ImportedAt, result2.ImportedAt)
	}
	// Only one import record should exist.
	if len(store.imports) != 1 {
		t.Errorf("imports count = %d, want 1 (idempotent)", len(store.imports))
	}
}

func TestService_ImportCapsule_DifferentTargets(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, store, _ := newTestService(&cp, nil)

	created, _ := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})

	target1 := mustULID()
	target2 := mustULID()
	_, err := svc.ImportCapsule(context.Background(), created.ID, target1)
	if err != nil {
		t.Fatalf("ImportCapsule to target1 failed: %v", err)
	}
	_, err = svc.ImportCapsule(context.Background(), created.ID, target2)
	if err != nil {
		t.Fatalf("ImportCapsule to target2 failed: %v", err)
	}
	// Two distinct import records.
	if len(store.imports) != 2 {
		t.Errorf("imports count = %d, want 2 (multi-session import)", len(store.imports))
	}
}

func TestService_ImportCapsule_NotFound(t *testing.T) {
	svc, _, _ := newTestService(nil, nil)

	_, err := svc.ImportCapsule(context.Background(), mustULID(), mustULID())
	if !errors.Is(err, ErrCapsuleNotFound) {
		t.Errorf("err = %v, want ErrCapsuleNotFound", err)
	}
}

func TestService_ImportCapsule_NotActive(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, store, _ := newTestService(&cp, nil)

	created, _ := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})
	// Revoke the capsule to make it terminal.
	_ = store.RevokeCapsule(context.Background(), created.ID)

	_, err := svc.ImportCapsule(context.Background(), created.ID, mustULID())
	if !errors.Is(err, ErrCapsuleNotActive) {
		t.Errorf("err = %v, want ErrCapsuleNotActive", err)
	}
}

func TestService_ImportCapsule_Expired(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, _, _ := newTestService(&cp, nil)

	pastTime := time.Now().UTC().Add(-1 * time.Hour)
	created, err := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
		ExpiresAt:       &pastTime,
	})
	if err != nil {
		t.Fatalf("CreateCapsule failed: %v", err)
	}

	_, err = svc.ImportCapsule(context.Background(), created.ID, mustULID())
	if !errors.Is(err, ErrCapsuleExpired) {
		t.Errorf("err = %v, want ErrCapsuleExpired", err)
	}
}

func TestService_ImportCapsule_SourceDeleted(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, _, reader := newTestService(&cp, nil)

	created, _ := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})

	// Simulate checkpoint deletion: reader returns nil.
	reader.checkpoint = nil

	_, err := svc.ImportCapsule(context.Background(), created.ID, mustULID())
	if !errors.Is(err, ErrSourceDeleted) {
		t.Errorf("err = %v, want ErrSourceDeleted", err)
	}
}

func TestService_ImportCapsule_DigestMismatch(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, store, _ := newTestService(&cp, nil)

	created, _ := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})

	// Tamper with the stored capsule's digest.
	store.capsules[created.ID].Digest = "0000000000000000000000000000000000000000000000000000000000000000"

	_, err := svc.ImportCapsule(context.Background(), created.ID, mustULID())
	if !errors.Is(err, ErrDigestMismatch) {
		t.Errorf("err = %v, want ErrDigestMismatch", err)
	}
}

// --- InspectCapsule Tests ---

func TestService_InspectCapsule_Success(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, _, _ := newTestService(&cp, nil)

	created, _ := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})

	result, err := svc.InspectCapsule(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("InspectCapsule failed: %v", err)
	}
	if result.Capsule.ID != created.ID {
		t.Errorf("capsule ID = %s, want %s", result.Capsule.ID, created.ID)
	}
	if result.Checkpoint == nil || result.Checkpoint.ID != cp.ID {
		t.Error("checkpoint not returned or mismatched")
	}
}

func TestService_InspectCapsule_SourceDeleted(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, _, reader := newTestService(&cp, nil)

	created, _ := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})

	// Simulate checkpoint deletion.
	reader.checkpoint = nil

	result, err := svc.InspectCapsule(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("InspectCapsule should not fail when source deleted, got: %v", err)
	}
	if result.Checkpoint != nil {
		t.Error("checkpoint should be nil when source deleted")
	}
	if result.Capsule.ID != created.ID {
		t.Errorf("capsule ID = %s, want %s", result.Capsule.ID, created.ID)
	}
}

func TestService_InspectCapsule_NotFound(t *testing.T) {
	svc, _, _ := newTestService(nil, nil)

	_, err := svc.InspectCapsule(context.Background(), mustULID())
	if !errors.Is(err, ErrCapsuleNotFound) {
		t.Errorf("err = %v, want ErrCapsuleNotFound", err)
	}
}

// --- ListImportedCapsuleContexts Tests ---

func TestService_ListImportedCapsuleContexts_FailClosedOnDeletion(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, _, reader := newTestService(&cp, nil)

	created, _ := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})

	targetSession := mustULID()
	_, err := svc.ImportCapsule(context.Background(), created.ID, targetSession)
	if err != nil {
		t.Fatalf("ImportCapsule failed: %v", err)
	}

	// Before deletion: one capsule context with non-nil checkpoint.
	contexts, err := svc.ListImportedCapsuleContexts(context.Background(), targetSession)
	if err != nil {
		t.Fatalf("ListImportedCapsuleContexts failed: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("contexts count = %d, want 1", len(contexts))
	}
	if contexts[0].Checkpoint == nil {
		t.Error("checkpoint should not be nil before deletion")
	}

	// After source checkpoint deletion: capsule context with nil checkpoint.
	reader.checkpoint = nil
	contexts, err = svc.ListImportedCapsuleContexts(context.Background(), targetSession)
	if err != nil {
		t.Fatalf("ListImportedCapsuleContexts after deletion failed: %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("contexts count = %d, want 1 (capsule still listed, checkpoint nil)", len(contexts))
	}
	if contexts[0].Checkpoint != nil {
		t.Error("checkpoint should be nil after source deletion (fail-closed)")
	}
}

func TestService_ListImportedCapsuleContexts_SkipsRevoked(t *testing.T) {
	sessionID := mustULID()
	cp := validCheckpoint(sessionID)
	svc, store, _ := newTestService(&cp, nil)

	created, _ := svc.CreateCapsule(context.Background(), CreateCapsuleRequest{
		SourceSessionID: sessionID,
		CheckpointID:    cp.ID,
	})

	targetSession := mustULID()
	_, _ = svc.ImportCapsule(context.Background(), created.ID, targetSession)

	// Revoke the capsule.
	_ = store.RevokeCapsule(context.Background(), created.ID)

	contexts, err := svc.ListImportedCapsuleContexts(context.Background(), targetSession)
	if err != nil {
		t.Fatalf("ListImportedCapsuleContexts failed: %v", err)
	}
	if len(contexts) != 0 {
		t.Errorf("contexts count = %d, want 0 (revoked capsules skipped)", len(contexts))
	}
}
