package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

func newMemoryV2Engine(t *testing.T) *Engine {
	t.Helper()
	mem, ops, _ := openAppMemory(t)
	e := NewEngine(nil, "test")
	e.SetM8MemoryServices(mem)
	e.SetMemoryOpsService(ops)
	return e
}

func engineWithStoreMemory(t *testing.T, store *storage.Store) *Engine {
	t.Helper()
	e := NewEngine(nil, "test")
	e.SetM8MemoryServices(m8app.NewMemoryService(store.AgentRuntimeRepository(), "local-user"))
	e.SetMemoryOpsService(m8app.NewMemoryOpsService(store))
	return e
}

func appendUserMessage(t *testing.T, store *storage.Store, sessionID, text string) message.Message {
	t.Helper()
	messages, err := messageapp.New(store, store, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	msg, err := messages.Append(context.Background(), "msg-key", "test", nil, message.Message{SessionID: sessionID, Text: text})
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func memoryItemRequest(method, payload, idempotencyKey string) bridge.Request {
	req := validRequest(method, payload)
	req.IdempotencyKey = idempotencyKey
	return req
}

func TestMemoryItemCreateListGetForget(t *testing.T) {
	e := newMemoryV2Engine(t)
	ctx := context.Background()
	op := ulid.Make().String()
	created := e.Handle(ctx, memoryItemRequest("memory.item.create", `{"scopeKind":"user","text":"我喜欢简洁的回答","operationId":"`+op+`"}`, op))
	if !created.OK {
		t.Fatalf("create: %+v", created.Error)
	}
	var createdPayload struct {
		Item struct {
			FactID    string  `json:"factId"`
			Revision  int64   `json:"revision"`
			Version   int64   `json:"version"`
			ScopeKind string  `json:"scopeKind"`
			Text      *string `json:"text"`
			Forgotten bool    `json:"forgotten"`
			Kind      string  `json:"kind"`
		} `json:"item"`
		DatabaseRevision int64  `json:"databaseRevision"`
		UndoOperationID  string `json:"undoOperationId"`
	}
	if err := json.Unmarshal(mustJSON(created.Payload), &createdPayload); err != nil {
		t.Fatal(err)
	}
	if createdPayload.Item.FactID == "" || createdPayload.Item.Revision != 1 || createdPayload.Item.Version != 1 ||
		createdPayload.Item.ScopeKind != "user" || createdPayload.Item.Forgotten || createdPayload.Item.Kind != "preference" ||
		createdPayload.Item.Text == nil || *createdPayload.Item.Text != "我喜欢简洁的回答" || createdPayload.UndoOperationID != op {
		t.Fatalf("created %+v", createdPayload)
	}
	listed := e.Handle(ctx, validRequest("memory.item.list", `{"scopeKind":"user"}`))
	if !listed.OK {
		t.Fatalf("list: %+v", listed.Error)
	}
	var listPayload struct {
		Items []struct {
			FactID string  `json:"factId"`
			Text   *string `json:"text"`
		} `json:"items"`
	}
	if err := json.Unmarshal(mustJSON(listed.Payload), &listPayload); err != nil || len(listPayload.Items) != 1 || listPayload.Items[0].FactID != createdPayload.Item.FactID {
		t.Fatalf("list %+v err=%v", listPayload, err)
	}
	got := e.Handle(ctx, validRequest("memory.item.get", `{"factId":"`+createdPayload.Item.FactID+`"}`))
	if !got.OK {
		t.Fatalf("get: %+v", got.Error)
	}
	replay := e.Handle(ctx, memoryItemRequest("memory.item.create", `{"scopeKind":"user","text":"我喜欢简洁的回答","operationId":"`+op+`"}`, op))
	if !replay.OK {
		t.Fatalf("replay: %+v", replay.Error)
	}
	var replayPayload struct {
		Item struct {
			FactID string `json:"factId"`
		} `json:"item"`
	}
	if err := json.Unmarshal(mustJSON(replay.Payload), &replayPayload); err != nil || replayPayload.Item.FactID != createdPayload.Item.FactID {
		t.Fatalf("replay %+v err=%v", replayPayload, err)
	}
	forgetOp := ulid.Make().String()
	forgotten := e.Handle(ctx, memoryItemRequest("memory.item.forget", `{"factId":"`+createdPayload.Item.FactID+`","mode":"fact_history","expectedRevision":1,"operationId":"`+forgetOp+`"}`, forgetOp))
	if !forgotten.OK {
		t.Fatalf("forget: %+v", forgotten.Error)
	}
	after := e.Handle(ctx, validRequest("memory.item.get", `{"factId":"`+createdPayload.Item.FactID+`"}`))
	if !after.OK {
		t.Fatalf("get forgotten: %+v", after.Error)
	}
	var afterItem struct {
		Forgotten bool    `json:"forgotten"`
		Text      *string `json:"text"`
	}
	if err := json.Unmarshal(mustJSON(after.Payload), &afterItem); err != nil || !afterItem.Forgotten || afterItem.Text != nil {
		t.Fatalf("forgotten item %+v err=%v", afterItem, err)
	}
	empty := e.Handle(ctx, validRequest("memory.item.list", `{"scopeKind":"user"}`))
	if !empty.OK {
		t.Fatalf("list after forget: %+v", empty.Error)
	}
	var emptyPayload struct {
		Items []struct{} `json:"items"`
	}
	if err := json.Unmarshal(mustJSON(empty.Payload), &emptyPayload); err != nil || len(emptyPayload.Items) != 0 {
		t.Fatalf("list after forget %+v err=%v", emptyPayload, err)
	}
}

func TestMemoryItemCreateRejectedWhenOff(t *testing.T) {
	mem, ops, _ := openAppMemory(t)
	ctx := context.Background()
	if err := ops.SettingsUpdate(ctx, m8core.MemorySettings{
		SubjectID: memoryOpsLegacySubject, MemoryEnabled: false, AutoNominate: false, CaptureMode: "off", GrowthDays: 14,
	}); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(nil, "test")
	e.SetM8MemoryServices(mem)
	e.SetMemoryOpsService(ops)
	op := ulid.Make().String()
	resp := e.Handle(ctx, memoryItemRequest("memory.item.create", `{"scopeKind":"user","text":"我喜欢简洁的回答","operationId":"`+op+`"}`, op))
	if resp.OK || resp.Error == nil || resp.Error.Code != "MEMORY_MODE_OFF" {
		t.Fatalf("off create %+v", resp)
	}
}

func TestMemoryItemCreateReplayMismatch(t *testing.T) {
	e := newMemoryV2Engine(t)
	ctx := context.Background()
	key := ulid.Make().String()
	first := e.Handle(ctx, memoryItemRequest("memory.item.create", `{"scopeKind":"user","text":"我喜欢简洁的回答","operationId":"`+ulid.Make().String()+`"}`, key))
	if !first.OK {
		t.Fatalf("first: %+v", first.Error)
	}
	second := e.Handle(ctx, memoryItemRequest("memory.item.create", `{"scopeKind":"user","text":"回答默认使用中文","operationId":"`+ulid.Make().String()+`"}`, key))
	if second.OK || second.Error == nil || second.Error.Code != "OPERATION_REPLAY_MISMATCH" {
		t.Fatalf("mismatch %+v", second)
	}
}

func TestMemoryItemForgetRevisionConflict(t *testing.T) {
	e := newMemoryV2Engine(t)
	ctx := context.Background()
	op := ulid.Make().String()
	created := e.Handle(ctx, memoryItemRequest("memory.item.create", `{"scopeKind":"user","text":"我喜欢简洁的回答","operationId":"`+op+`"}`, op))
	if !created.OK {
		t.Fatalf("create: %+v", created.Error)
	}
	var createdPayload struct {
		Item struct {
			FactID string `json:"factId"`
		} `json:"item"`
	}
	if err := json.Unmarshal(mustJSON(created.Payload), &createdPayload); err != nil {
		t.Fatal(err)
	}
	forgetOp := ulid.Make().String()
	resp := e.Handle(ctx, memoryItemRequest("memory.item.forget", `{"factId":"`+createdPayload.Item.FactID+`","mode":"fact_history","expectedRevision":9,"operationId":"`+forgetOp+`"}`, forgetOp))
	if resp.OK || resp.Error == nil || resp.Error.Code != "REVISION_CONFLICT" {
		t.Fatalf("cas %+v", resp)
	}
}

func TestMemoryItemCreateSourceRefMissing(t *testing.T) {
	e := newMemoryV2Engine(t)
	op := ulid.Make().String()
	messageID := ulid.Make().String()
	resp := e.Handle(context.Background(), memoryItemRequest("memory.item.create", `{"scopeKind":"user","sourceRef":{"messageId":"`+messageID+`"},"operationId":"`+op+`"}`, op))
	if resp.OK || resp.Error == nil || resp.Error.Code != "MEMORY_SOURCE_INVALID" {
		t.Fatalf("missing source %+v", resp)
	}
}

func TestMemoryItemCreateRequiresIdempotencyKey(t *testing.T) {
	e := newMemoryV2Engine(t)
	op := ulid.Make().String()
	resp := e.Handle(context.Background(), validRequest("memory.item.create", `{"scopeKind":"user","text":"我喜欢简洁的回答","operationId":"`+op+`"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("missing key %+v", resp)
	}
}

func TestMemoryItemCreateWholeMessageSource(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "whole-message.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	proj, err := projectapp.New(store, store).Create(ctx, "proj-key", "test", nil, project.Project{Name: "Memory"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessionapp.New(store, store).Create(ctx, "sess-key", "test", nil, session.Session{ProjectID: proj.ID, Title: "Chat"})
	if err != nil {
		t.Fatal(err)
	}
	body := "我喜欢简洁的回答"
	msg := appendUserMessage(t, store, sess.ID, body)
	e := engineWithStoreMemory(t, store)
	op := ulid.Make().String()
	created := e.Handle(ctx, memoryItemRequest("memory.item.create", `{"scopeKind":"user","sourceRef":{"messageId":"`+msg.ID+`"},"operationId":"`+op+`"}`, op))
	if !created.OK {
		t.Fatalf("create: %+v", created.Error)
	}
	var createdPayload struct {
		Item struct {
			FactID string  `json:"factId"`
			Text   *string `json:"text"`
		} `json:"item"`
	}
	if err := json.Unmarshal(mustJSON(created.Payload), &createdPayload); err != nil || createdPayload.Item.Text == nil || *createdPayload.Item.Text != body {
		t.Fatalf("created %+v err=%v", createdPayload, err)
	}
	kind, ref, startByte, endByte, digest, err := store.CanonicalEvidenceSpan(ctx, createdPayload.Item.FactID)
	raw := []byte(body)
	sum := sha256.Sum256(raw)
	wantDigest := hex.EncodeToString(sum[:])
	if err != nil || kind != m8core.MemorySourceUserMessage || ref != msg.ID || !startByte.Valid || startByte.Int64 != 0 ||
		!endByte.Valid || endByte.Int64 != int64(len(raw)) || digest != wantDigest {
		t.Fatalf("evidence kind=%s ref=%s start=%v end=%v digest=%s err=%v", kind, ref, startByte, endByte, digest, err)
	}
}

func TestMemoryItemCreateExplicitSpanSource(t *testing.T) {
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "span-message.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	proj, err := projectapp.New(store, store).Create(ctx, "proj-key", "test", nil, project.Project{Name: "Memory"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessionapp.New(store, store).Create(ctx, "sess-key", "test", nil, session.Session{ProjectID: proj.ID, Title: "Chat"})
	if err != nil {
		t.Fatal(err)
	}
	body := "我喜欢简洁的回答"
	msg := appendUserMessage(t, store, sess.ID, body)
	raw := []byte(body)
	start, end := int64(0), int64(len([]byte("我喜欢")))
	e := engineWithStoreMemory(t, store)
	op := ulid.Make().String()
	payload := `{"scopeKind":"user","sourceRef":{"messageId":"` + msg.ID + `","startByte":` + itoa(start) + `,"endByte":` + itoa(end) + `},"operationId":"` + op + `"}`
	created := e.Handle(ctx, memoryItemRequest("memory.item.create", payload, op))
	if !created.OK {
		t.Fatalf("create: %+v", created.Error)
	}
	var createdPayload struct {
		Item struct {
			FactID string  `json:"factId"`
			Text   *string `json:"text"`
		} `json:"item"`
	}
	if err := json.Unmarshal(mustJSON(created.Payload), &createdPayload); err != nil || createdPayload.Item.Text == nil || *createdPayload.Item.Text != "我喜欢" {
		t.Fatalf("created %+v err=%v", createdPayload, err)
	}
	kind, ref, startByte, endByte, digest, err := store.CanonicalEvidenceSpan(ctx, createdPayload.Item.FactID)
	sum := sha256.Sum256(raw[start:end])
	if err != nil || kind != m8core.MemorySourceUserMessage || ref != msg.ID || !startByte.Valid || startByte.Int64 != start ||
		!endByte.Valid || endByte.Int64 != end || digest != hex.EncodeToString(sum[:]) {
		t.Fatalf("evidence kind=%s ref=%s start=%v end=%v digest=%s err=%v", kind, ref, startByte, endByte, digest, err)
	}
	mid := int64(1)
	badOp := ulid.Make().String()
	bad := e.Handle(ctx, memoryItemRequest("memory.item.create", `{"scopeKind":"user","sourceRef":{"messageId":"`+msg.ID+`","startByte":`+itoa(mid)+`},"operationId":"`+badOp+`"}`, badOp))
	if bad.OK || bad.Error == nil || bad.Error.Code != "MEMORY_SOURCE_INVALID" {
		t.Fatalf("single offset %+v", bad)
	}
}

func TestMemoryPurgePrepareContract(t *testing.T) {
	TestMemoryItemHistoryCorrectUndoAndPurgeGrant(t)
}

func TestMemoryItemHistoryCorrectUndoAndPurgeGrant(t *testing.T) {
	e := newMemoryV2Engine(t)
	ctx := context.Background()
	empty := e.Handle(ctx, validRequest("memory.purge", `{}`))
	if empty.OK || empty.Error == nil || empty.Error.Code != "PURGE_CONFIRMATION_REQUIRED" {
		t.Fatalf("empty purge %+v", empty)
	}
	createOp := ulid.Make().String()
	created := e.Handle(ctx, memoryItemRequest("memory.item.create", `{"scopeKind":"user","text":"我喜欢简洁的回答","operationId":"`+createOp+`"}`, createOp))
	if !created.OK {
		t.Fatalf("create: %+v", created.Error)
	}
	var createdPayload struct {
		Item struct {
			FactID   string `json:"factId"`
			Revision int64  `json:"revision"`
		} `json:"item"`
		UndoOperationID  string `json:"undoOperationId"`
		DatabaseRevision int64  `json:"databaseRevision"`
	}
	if err := json.Unmarshal(mustJSON(created.Payload), &createdPayload); err != nil || createdPayload.Item.FactID == "" {
		t.Fatalf("created %+v err=%v", createdPayload, err)
	}
	history := e.Handle(ctx, validRequest("memory.item.history", `{"factId":"`+createdPayload.Item.FactID+`"}`))
	if !history.OK {
		t.Fatalf("history: %+v", history.Error)
	}
	correctOp := ulid.Make().String()
	corrected := e.Handle(ctx, memoryItemRequest("memory.item.correct", `{"factId":"`+createdPayload.Item.FactID+`","replacementText":"我喜欢更准确的回答","reason":"更准确","operationId":"`+correctOp+`","expectedRevision":`+itoa(createdPayload.Item.Revision)+`}`, correctOp))
	if !corrected.OK {
		t.Fatalf("correct: %+v", corrected.Error)
	}
	undoOp := ulid.Make().String()
	undone := e.Handle(ctx, memoryItemRequest("memory.capture.undo", `{"undoOperationId":"`+createdPayload.UndoOperationID+`","operationId":"`+undoOp+`"}`, undoOp))
	if undone.OK || undone.Error == nil || undone.Error.Code != "MEMORY_UNDO_CONFLICT" {
		t.Fatalf("undo after correct %+v", undone)
	}
	freshOp := ulid.Make().String()
	fresh := e.Handle(ctx, memoryItemRequest("memory.item.create", `{"scopeKind":"user","text":"我住在上海","operationId":"`+freshOp+`"}`, freshOp))
	if !fresh.OK {
		t.Fatalf("fresh create: %+v", fresh.Error)
	}
	var freshPayload struct {
		UndoOperationID  string `json:"undoOperationId"`
		DatabaseRevision int64  `json:"databaseRevision"`
	}
	if err := json.Unmarshal(mustJSON(fresh.Payload), &freshPayload); err != nil {
		t.Fatal(err)
	}
	okUndoOp := ulid.Make().String()
	okUndo := e.Handle(ctx, memoryItemRequest("memory.capture.undo", `{"undoOperationId":"`+freshPayload.UndoOperationID+`","operationId":"`+okUndoOp+`"}`, okUndoOp))
	if !okUndo.OK {
		t.Fatalf("undo: %+v", okUndo.Error)
	}
	listed := e.Handle(ctx, validRequest("memory.item.list", `{"scopeKind":"user"}`))
	if !listed.OK {
		t.Fatalf("list: %+v", listed.Error)
	}
	var listPayload struct {
		DatabaseRevision int64 `json:"databaseRevision"`
	}
	if err := json.Unmarshal(mustJSON(listed.Payload), &listPayload); err != nil {
		t.Fatal(err)
	}
	prepOp := ulid.Make().String()
	prepared := e.Handle(ctx, memoryItemRequest("memory.purge.prepare", `{"scopeKind":"user","expectedDatabaseRevision":`+itoa(listPayload.DatabaseRevision)+`,"operationId":"`+prepOp+`"}`, prepOp))
	if !prepared.OK {
		t.Fatalf("prepare: %+v", prepared.Error)
	}
	var grant struct {
		ConfirmationToken string `json:"confirmationToken"`
		SnapshotDigest    string `json:"snapshotDigest"`
		Counts            struct {
			Facts int64 `json:"facts"`
		} `json:"counts"`
	}
	if err := json.Unmarshal(mustJSON(prepared.Payload), &grant); err != nil || grant.ConfirmationToken == "" {
		t.Fatalf("grant %+v err=%v", grant, err)
	}
	purgeOp := ulid.Make().String()
	purged := e.Handle(ctx, memoryItemRequest("memory.purge", `{"confirmationToken":"`+grant.ConfirmationToken+`","snapshotDigest":"`+grant.SnapshotDigest+`","expectedDatabaseRevision":`+itoa(listPayload.DatabaseRevision)+`,"operationId":"`+purgeOp+`"}`, purgeOp))
	if !purged.OK {
		t.Fatalf("purge: %+v", purged.Error)
	}
}

func TestMemoryReviewListAndResolve(t *testing.T) {
	e := newMemoryV2Engine(t)
	ctx := context.Background()
	reviewID := ulid.Make().String()
	if err := e.m8memory.PutMemoryReview(ctx, e.memorySubjectID(), m8core.MemoryReviewWrite{
		CandidateID: reviewID,
		Kind:        "preference",
		Novelty:     "new",
		ReasonCodes: []string{"quiet_review"},
		Text:        "我喜欢绿茶",
		ScopeKind:   "user",
	}); err != nil {
		t.Fatal(err)
	}
	listed := e.Handle(ctx, validRequest("memory.review.list", `{"scopeKind":"user"}`))
	if !listed.OK {
		t.Fatalf("review list: %+v", listed.Error)
	}
	var listPayload struct {
		Items []struct {
			ReviewID string  `json:"reviewId"`
			Text     *string `json:"text"`
		} `json:"items"`
		DatabaseRevision int64 `json:"databaseRevision"`
	}
	if err := json.Unmarshal(mustJSON(listed.Payload), &listPayload); err != nil || len(listPayload.Items) != 1 || listPayload.Items[0].ReviewID != reviewID {
		t.Fatalf("list %+v err=%v", listPayload, err)
	}
	op := ulid.Make().String()
	resolved := e.Handle(ctx, memoryItemRequest("memory.review.resolve", `{"reviewId":"`+reviewID+`","decision":"accept","operationId":"`+op+`","expectedRevision":`+itoa(listPayload.DatabaseRevision)+`}`, op))
	if !resolved.OK {
		t.Fatalf("resolve: %+v", resolved.Error)
	}
}

func TestMemoryExport(t *testing.T) {
	e := newMemoryV2Engine(t)
	ctx := context.Background()
	op := ulid.Make().String()
	created := e.Handle(ctx, memoryItemRequest("memory.item.create", `{"scopeKind":"user","text":"我喜欢绿茶","operationId":"`+op+`"}`, op))
	if !created.OK {
		t.Fatalf("create: %+v", created.Error)
	}
	legacy := e.Handle(ctx, validRequest("memory.export", `{}`))
	if !legacy.OK {
		t.Fatalf("legacy export: %+v", legacy.Error)
	}
	if !strings.Contains(string(mustJSON(legacy.Payload)), `"facts"`) {
		t.Fatalf("legacy shape missing facts: %s", mustJSON(legacy.Payload))
	}
	fabric := e.Handle(ctx, validRequest("memory.export", `{"format":"fabric_v2"}`))
	if !fabric.OK {
		t.Fatalf("fabric export: %+v", fabric.Error)
	}
	raw := mustJSON(fabric.Payload)
	var out m8core.MemoryFabricExport
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Format != "fabric_v2" || out.ArtifactID == "" || len(out.ArchiveDigest) != 64 || out.Counts.Current < 1 {
		t.Fatalf("fabric metadata %+v", out)
	}
	if strings.Contains(string(raw), "我喜欢绿茶") {
		t.Fatal("fabric export must not return archive records")
	}
}
