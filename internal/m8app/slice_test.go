// M8 slice-2/3/4/5 service tests (T-8.2.x/T-8.3.x/T-8.4.x/T-8.5.x):
// KB optimistic reindex + sha256 idempotency + failed projection, handoff
// idempotent accept + expiry + unread-before-accept, tombstone read-face
// hiding + verified cascade + failure resume, sync push vector conflicts +
// revoked device + stale ACK, and automation dispatch prechecks +
// idempotent replay - against a fully migrated SQLite store.
package m8app_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func openSliceStore(t *testing.T) *storage.Store {
	t.Helper()
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "m8-slices.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func sha64(ch string) string { return strings.Repeat(ch, 64) }

// --- slice 2: kb.upsertDocument ---

func TestKBUpsertVersionsIdempotencyAndConflict(t *testing.T) {
	store := openSliceStore(t)
	repo := store.AgentRuntimeRepository()
	svc := m8app.NewKBService(repo, "local-user")
	ctx := context.Background()

	collID := ulid.Make().String()
	docID := ulid.Make().String()
	if err := svc.EnsureCollection(ctx, collID, "scope-kb"); err != nil {
		t.Fatalf("ensure collection: %v", err)
	}
	up := func(expected int64, sha string) (m8app.KBUpsertResult, error) {
		return svc.UpsertDocument(ctx, m8app.KBUpsertInput{
			CollectionID: collID, DocumentID: docID,
			ExpectedVersion: expected, MediaType: "text/markdown",
			ContentRef: "blob://kb/doc-1", SHA256: sha,
			SourceLocator: "project://docs/readme.md", RequestID: "req-kb",
		})
	}
	// First insert answers v1 ready.
	res, err := up(0, sha64("a"))
	if err != nil || res.Version != 1 || res.IndexState != m8core.KBIndexReady {
		t.Fatalf("first upsert = %+v err=%v", res, err)
	}
	// Identical sha256 resubmission is idempotent (same version).
	res, err = up(0, sha64("a"))
	if err != nil || res.Version != 1 || res.IndexState != m8core.KBIndexReady {
		t.Fatalf("idempotent resubmit = %+v err=%v", res, err)
	}
	// Stale expectedVersion (0 on an existing doc) refuses M8-011.
	if _, err := up(0, sha64("b")); !errors.Is(err, m8app.ErrKBVersionConflict) {
		t.Fatalf("stale expected err = %v, want ErrKBVersionConflict", err)
	}
	// Correct expectedVersion creates v2.
	res, err = up(1, sha64("b"))
	if err != nil || res.Version != 2 || res.IndexState != m8core.KBIndexReady {
		t.Fatalf("reindex = %+v err=%v", res, err)
	}
}

func TestKBIndexFailureParksAtFailedNoProjection(t *testing.T) {
	store := openSliceStore(t)
	svc := m8app.NewKBService(store.AgentRuntimeRepository(), "local-user")
	svc.SetIndexer(func(ctx context.Context, doc m8core.KBDocument) ([]string, error) {
		return nil, errors.New("indexer down")
	})
	ctx := context.Background()
	collID := ulid.Make().String()
	if err := svc.EnsureCollection(ctx, collID, "scope-kb"); err != nil {
		t.Fatalf("ensure collection: %v", err)
	}
	res, err := svc.UpsertDocument(ctx, m8app.KBUpsertInput{
		CollectionID: collID, DocumentID: ulid.Make().String(),
		MediaType: "text/markdown", ContentRef: "blob://kb/doc-2",
		SHA256: sha64("c"), SourceLocator: "project://docs/x.md", RequestID: "req-kb2",
	})
	if !errors.Is(err, m8app.ErrKBIndexFailed) {
		t.Fatalf("err = %v, want ErrKBIndexFailed", err)
	}
	if res.IndexState != m8core.KBIndexFailed {
		t.Fatalf("indexState = %s, want failed", res.IndexState)
	}
}

// --- slice 3: handoff.accept ---

func TestHandoffAcceptExpiryAndIdempotency(t *testing.T) {
	store := openSliceStore(t)
	svc := m8app.NewHandoffService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()

	id := ulid.Make().String()
	_, err := svc.OfferHandoff(ctx, m8app.OfferHandoffInput{
		HandoffID: id, Sender: "local-user", Receiver: "peer",
		Manifest: `{"k":"v"}`, RedactionLog: `{"removed":[]}`,
	})
	if err != nil {
		t.Fatalf("offer: %v", err)
	}
	// Unread before accept (M8-015).
	if _, err := svc.ReadHandoff(ctx, id); !errors.Is(err, m8app.ErrHandoffNotAccepted) {
		t.Fatalf("read-before-accept err = %v, want ErrHandoffNotAccepted", err)
	}
	// Accept answers accepted.
	res, err := svc.AcceptHandoff(ctx, m8app.HandoffAcceptInput{HandoffID: id, RequestID: "req-h1"})
	if err != nil || res.State != "accepted" || res.EffectiveAt == "" {
		t.Fatalf("accept = %+v err=%v", res, err)
	}
	// Repeated accept is idempotent with the original effectiveAt.
	res2, err := svc.AcceptHandoff(ctx, m8app.HandoffAcceptInput{HandoffID: id, RequestID: "req-h2"})
	if err != nil || res2.EffectiveAt != res.EffectiveAt {
		t.Fatalf("re-accept = %+v err=%v, want same effectiveAt", res2, err)
	}
	// Manifest readable after accept.
	if m, err := svc.ReadHandoff(ctx, id); err != nil || m != `{"k":"v"}` {
		t.Fatalf("read-after-accept = %q err=%v", m, err)
	}
	// Missing handoff answers not found.
	if _, err := svc.AcceptHandoff(ctx, m8app.HandoffAcceptInput{HandoffID: ulid.Make().String(), RequestID: "req-h3"}); !errors.Is(err, m8app.ErrHandoffNotFound) {
		t.Fatalf("missing handoff err = %v", err)
	}
}

func TestHandoffExpiredRefusedAndFlipsRow(t *testing.T) {
	store := openSliceStore(t)
	svc := m8app.NewHandoffService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()
	clock := &fakeClock{now: time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)}
	svc.SetClock(clock)

	id := ulid.Make().String()
	if _, err := svc.OfferHandoff(ctx, m8app.OfferHandoffInput{
		HandoffID: id, Sender: "local-user", Receiver: "peer",
		Manifest: `{"k":"v"}`, RedactionLog: `{"removed":[]}`,
	}); err != nil {
		t.Fatalf("offer: %v", err)
	}
	// Advance past expiry.
	clock.now = clock.now.Add(73 * time.Hour)
	if _, err := svc.AcceptHandoff(ctx, m8app.HandoffAcceptInput{HandoffID: id, RequestID: "req-h4"}); !errors.Is(err, m8app.ErrHandoffExpired) {
		t.Fatalf("expired accept err = %v, want ErrHandoffExpired", err)
	}
	// Retry after the row flipped expired still refuses (410).
	if _, err := svc.AcceptHandoff(ctx, m8app.HandoffAcceptInput{HandoffID: id, RequestID: "req-h5"}); !errors.Is(err, m8app.ErrHandoffExpired) {
		t.Fatalf("retry expired err = %v, want ErrHandoffExpired", err)
	}
}

// --- slice 3: tombstone.delete ---

func TestTombstoneDeleteHidesReadFaceAndVerifies(t *testing.T) {
	store := openSliceStore(t)
	repo := store.AgentRuntimeRepository()
	memSvc := m8app.NewMemoryService(repo, "local-user")
	hSvc := m8app.NewHandoffService(repo, "local-user")
	ctx := context.Background()

	// Seed one confirmed fact in scope-a.
	prop, err := memSvc.ProposeCandidate(ctx, m8app.ProposeInput{
		SubjectID: "local-user",
		Doc: m8core.PayloadDoc{
			Content: "stale preference", ScopeID: "scope-a",
			Sensitivity: m8core.SensPrivate,
			Leaves: []m8core.SourceLeafClaim{{JSONPointer: "/content", EvidenceRef: "artifact://e", Digest: sha64("d")}},
		},
		Trust: m8core.TrustUntrusted,
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	confirmed, err := memSvc.ConfirmCandidate(ctx, m8app.ConfirmInput{
		CandidateID: prop.Candidate.CandidateID, Token: prop.ConfirmToken,
		Action: "confirm", RequestID: "req-t0",
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}

	res, err := hSvc.DeleteWithTombstone(ctx, m8app.TombstoneDeleteInput{
		RootRef: "fact:" + confirmed.Fact.FactID, ScopeID: "scope-a",
		ConfirmationToken: sha64("f"), Actor: "local-user",
	})
	if err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	if res.State != "verified" || len(res.AckSet) != 3 || res.CascadeCursor == "" {
		t.Fatalf("tombstone result = %+v", res)
	}
	// Read face hidden: recall of the scope no longer finds the fact.
	recall, err := memSvc.Recall(ctx, m8app.RecallInput{ScopeID: "scope-a", Query: "stale preference"})
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(recall.Hits) != 0 {
		t.Fatalf("recall hits = %+v, want none after tombstone", recall.Hits)
	}
	// Re-delete is idempotent.
	res2, err := hSvc.DeleteWithTombstone(ctx, m8app.TombstoneDeleteInput{
		RootRef: "fact:" + confirmed.Fact.FactID, ScopeID: "scope-a",
		ConfirmationToken: sha64("f"),
	})
	if err != nil || res2.TombstoneID != res.TombstoneID || res2.State != "verified" {
		t.Fatalf("re-delete = %+v err=%v", res2, err)
	}
}

func TestTombstoneCascadeFailureStaysUnreadable(t *testing.T) {
	store := openSliceStore(t)
	svc := m8app.NewHandoffService(store.AgentRuntimeRepository(), "local-user")
	svc.SetPropagator(func(ctx context.Context, tb m8core.Tombstone, projections []string) (string, []string, error) {
		return "", nil, errors.New("replica unreachable")
	})
	root := "fact:" + ulid.Make().String()
	res, err := svc.DeleteWithTombstone(context.Background(), m8app.TombstoneDeleteInput{
		RootRef: root, ScopeID: "scope-b",
		ConfirmationToken: sha64("e"),
	})
	if !errors.Is(err, m8app.ErrTombstoneCascadeFailed) {
		t.Fatalf("err = %v, want ErrTombstoneCascadeFailed", err)
	}
	if res.State != "propagating" {
		t.Fatalf("state = %s, want propagating", res.State)
	}
	// Polling the propagating root answers M8-016 and stays unreadable.
	res2, err := svc.DeleteWithTombstone(context.Background(), m8app.TombstoneDeleteInput{
		RootRef: root, ScopeID: "scope-b", ConfirmationToken: sha64("e"),
	})
	if !errors.Is(err, m8app.ErrTombstoneInProgress) {
		t.Fatalf("poll err = %v, want ErrTombstoneInProgress", err)
	}
	if res2.State != "propagating" || res2.TombstoneID != res.TombstoneID {
		t.Fatalf("poll = %+v, want same propagating tombstone", res2)
	}
}

// --- slice 5: sync.push ---

func TestSyncPushMergesDistinctLeavesAndBoxesSameLeaf(t *testing.T) {
	store := openSliceStore(t)
	svc := m8app.NewHandoffService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()

	dev := ulid.Make().String()
	if err := svc.RegisterDevice(ctx, dev, "local-user"); err != nil {
		t.Fatalf("register: %v", err)
	}
	factA := ulid.Make().String()
	factB := ulid.Make().String()
	push := func(clock map[string]int64, edits ...m8core.SyncEdit) (m8app.SyncPushResult, error) {
		return svc.Push(ctx, m8app.SyncPushInput{DeviceID: dev, VectorClock: clock, Edits: edits})
	}
	edit := func(fact, ptr, val string) m8core.SyncEdit {
		return m8core.SyncEdit{FactID: fact, Version: 1, JSONPointer: ptr, Value: json.RawMessage(val), Source: "device"}
	}
	// Two distinct leaves auto-merge (M8-018 pass-through).
	res, err := push(map[string]int64{dev: 1},
		edit(factA, "/title", `"a"`), edit(factB, "/note", `"b"`))
	if err != nil {
		t.Fatalf("push distinct leaves: %v", err)
	}
	if res.AckWatermark != 1 || len(res.Conflicts) != 0 {
		t.Fatalf("merge result = %+v", res)
	}
	// Same leaf twice in one batch enters the conflict box with both
	// variants preserved.
	res, err = push(map[string]int64{dev: 2},
		edit(factA, "/title", `"x"`), edit(factA, "/title", `"y"`))
	if err != nil {
		t.Fatalf("push same leaf: %v", err)
	}
	if len(res.Conflicts) != 1 || res.Conflicts[0].FactID != factA ||
		len(res.Conflicts[0].Variants) != 2 {
		t.Fatalf("conflict result = %+v", res.Conflicts)
	}
	if res.AckWatermark != 2 {
		t.Fatalf("watermark = %d, want 2 (clock advances, edits boxed)", res.AckWatermark)
	}
}

func TestSyncPushRevokedAndStaleACK(t *testing.T) {
	store := openSliceStore(t)
	svc := m8app.NewHandoffService(store.AgentRuntimeRepository(), "local-user")
	ctx := context.Background()

	dev := ulid.Make().String()
	if err := svc.RegisterDevice(ctx, dev, "local-user"); err != nil {
		t.Fatalf("register: %v", err)
	}
	factA := ulid.Make().String()
	// Establish watermark 5.
	if _, err := svc.Push(ctx, m8app.SyncPushInput{DeviceID: dev,
		VectorClock: map[string]int64{dev: 5},
		Edits:       []m8core.SyncEdit{{FactID: factA, Version: 1, JSONPointer: "/x", Value: json.RawMessage(`1`), Source: "device"}}}); err != nil {
		t.Fatalf("push: %v", err)
	}
	// Stale ACK (device pushes watermark 3 < known 5) refused (M8-020).
	if _, err := svc.Push(ctx, m8app.SyncPushInput{DeviceID: dev,
		VectorClock: map[string]int64{dev: 3}, Edits: nil}); !errors.Is(err, m8app.ErrSyncAckStale) {
		t.Fatalf("stale ack err = %v, want ErrSyncAckStale", err)
	}
	// Revoked device blocked (M8-019).
	if err := svc.RevokeDevice(ctx, dev); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.Push(ctx, m8app.SyncPushInput{DeviceID: dev,
		VectorClock: map[string]int64{dev: 6}, Edits: nil}); !errors.Is(err, m8app.ErrDeviceRevoked) {
		t.Fatalf("revoked err = %v, want ErrDeviceRevoked", err)
	}
	// Unknown device refused.
	if _, err := svc.Push(ctx, m8app.SyncPushInput{DeviceID: ulid.Make().String(),
		VectorClock: map[string]int64{}, Edits: nil}); !errors.Is(err, m8app.ErrDeviceNotFound) {
		t.Fatalf("unknown device err = %v", err)
	}
}

// --- slice 4: automation.dispatch ---

func dispatchInput(bundle string, actions []string, tokens int64, reqID string) m8app.DispatchInput {
	trigger, _ := json.Marshal(map[string]any{"type": "manual", "actions": actions})
	budget, _ := json.Marshal(map[string]any{"maxTokens": tokens})
	return m8app.DispatchInput{BundleID: bundle, BundleVersion: 1, Trigger: trigger, Budget: budget, RequestID: reqID}
}

func TestAutomationDispatchPrechecksAndReplay(t *testing.T) {
	store := openSliceStore(t)
	svc := m8app.NewAutomationService(store.AgentRuntimeRepository())
	ctx := context.Background()

	bundle := ulid.Make().String()
	if err := svc.RegisterBundle(ctx, m8app.RegisterBundleInput{
		BundleID: bundle, Version: 1, Checksum: sha64("1"),
		Permissions: m8core.BundlePermissions{
			Allow: []string{"fs.read", "fs.write"}, HighRisk: []string{"command.run"},
			BudgetCeiling: 10000,
		},
	}); err != nil {
		t.Fatalf("register bundle: %v", err)
	}
	// Allowed, low-risk, in-budget: dispatch.
	res, err := svc.Dispatch(ctx, dispatchInput(bundle, []string{"fs.read"}, 100, "req-a1"))
	if err != nil || res.State != "dispatched" {
		t.Fatalf("dispatch = %+v err=%v", res, err)
	}
	// Idempotent replay answers the original run (no second row).
	res2, err := svc.Dispatch(ctx, dispatchInput(bundle, []string{"fs.read"}, 100, "req-a1"))
	if err != nil || res2.RunID != res.RunID || res2.State != "dispatched" {
		t.Fatalf("replay = %+v err=%v", res2, err)
	}
	// Permission denial: zero dispatch (M8-022).
	if _, err := svc.Dispatch(ctx, dispatchInput(bundle, []string{"net.fetch"}, 100, "req-a2")); !errors.Is(err, m8app.ErrBundlePermissionDenied) {
		t.Fatalf("permission err = %v", err)
	}
	// Budget over ceiling: refused (M8-024).
	if _, err := svc.Dispatch(ctx, dispatchInput(bundle, []string{"fs.read"}, 999999, "req-a3")); !errors.Is(err, m8app.ErrAutomationBudgetExceeded) {
		t.Fatalf("budget err = %v", err)
	}
	// High-risk action waits for confirmation (M8-023).
	resW, err := svc.Dispatch(ctx, dispatchInput(bundle, []string{"command.run"}, 100, "req-a4"))
	if err != nil || resW.State != "waiting_confirmation" {
		t.Fatalf("high-risk = %+v err=%v", resW, err)
	}
	// Unknown bundle answers not found.
	if _, err := svc.Dispatch(ctx, dispatchInput(ulid.Make().String(), []string{"fs.read"}, 100, "req-a5")); !errors.Is(err, m8app.ErrBundleNotFound) {
		t.Fatalf("unknown bundle err = %v", err)
	}
	// Version mismatch answers checksum-invalid family (M8-021).
	in := dispatchInput(bundle, []string{"fs.read"}, 100, "req-a6")
	in.BundleVersion = 2
	if _, err := svc.Dispatch(ctx, in); !errors.Is(err, m8app.ErrBundleChecksumInvalid) {
		t.Fatalf("version mismatch err = %v", err)
	}
}
