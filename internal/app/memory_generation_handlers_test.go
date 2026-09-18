package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
)

func TestMemoryGenerationBridgeContract(t *testing.T) {
	e := newMemoryV2Engine(t)
	ctx := context.Background()
	createOp := ulid.Make().String()
	created := e.Handle(ctx, memoryItemRequest("memory.item.create", `{"scopeKind":"user","text":"我住在杭州","operationId":"`+createOp+`"}`, createOp))
	if !created.OK {
		t.Fatalf("create: %+v", created.Error)
	}

	userScope := e.Handle(ctx, validRequest("memory.generation.list", `{"scopeKind":"user","scopeId":"`+ulid.Make().String()+`"}`))
	if userScope.OK || userScope.Error == nil || userScope.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("user list with scopeId %+v", userScope)
	}
	projectMissing := e.Handle(ctx, validRequest("memory.generation.list", `{"scopeKind":"project"}`))
	if projectMissing.OK || projectMissing.Error == nil || projectMissing.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("project list missing scopeId %+v", projectMissing)
	}

	buildKey := ulid.Make().String()
	ready, err := e.m8memory.BuildMemoryGeneration(ctx, memoryOpsLegacySubject, "user", "", "", buildKey, buildKey)
	if err != nil || ready.GenerationID == "" || ready.State != "ready" {
		t.Fatalf("build %+v err=%v", ready, err)
	}
	listed := e.Handle(ctx, validRequest("memory.generation.list", `{"scopeKind":"user","limit":20}`))
	if !listed.OK {
		t.Fatalf("list: %+v", listed.Error)
	}
	var listPayload struct {
		Items []struct {
			GenerationID string `json:"generationId"`
			State        string `json:"state"`
			MemberCount  int64  `json:"memberCount"`
			Revision     int64  `json:"revision"`
			ScopeKind    string `json:"scopeKind"`
			ScopeID      *string `json:"scopeId"`
		} `json:"items"`
		DatabaseRevision int64 `json:"databaseRevision"`
	}
	if err := json.Unmarshal(mustJSON(listed.Payload), &listPayload); err != nil {
		t.Fatal(err)
	}
	if listPayload.DatabaseRevision < 1 || len(listPayload.Items) == 0 || listPayload.Items[0].GenerationID != ready.GenerationID ||
		listPayload.Items[0].State != "ready" || listPayload.Items[0].MemberCount < 1 || listPayload.Items[0].ScopeKind != "user" ||
		listPayload.Items[0].ScopeID != nil {
		t.Fatalf("list payload %+v", listPayload)
	}

	preview := e.Handle(ctx, validRequest("memory.generation.preview", `{"generationId":"`+ready.GenerationID+`"}`))
	if !preview.OK {
		t.Fatalf("preview: %+v", preview.Error)
	}
	var previewPayload struct {
		Generation struct {
			GenerationID string `json:"generationId"`
			State        string `json:"state"`
		} `json:"generation"`
		Changes []struct {
			Change    string  `json:"change"`
			FactID    string  `json:"factId"`
			AfterText *string `json:"afterText"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(mustJSON(preview.Payload), &previewPayload); err != nil ||
		previewPayload.Generation.GenerationID != ready.GenerationID || len(previewPayload.Changes) == 0 ||
		previewPayload.Changes[0].Change != "add" || previewPayload.Changes[0].AfterText == nil ||
		!strings.Contains(*previewPayload.Changes[0].AfterText, "杭州") {
		t.Fatalf("preview %+v err=%v", previewPayload, err)
	}

	activateOp := ulid.Make().String()
	activatePayload := `{"generationId":"` + ready.GenerationID + `","expectedRevision":` + itoa(listPayload.Items[0].Revision) + `,"operationId":"` + activateOp + `"}`
	activated := e.Handle(ctx, memoryItemRequest("memory.generation.activate", activatePayload, activateOp))
	if !activated.OK {
		t.Fatalf("activate: %+v", activated.Error)
	}
	var activateOut struct {
		Generation struct {
			State string `json:"state"`
		} `json:"generation"`
		DatabaseRevision int64 `json:"databaseRevision"`
	}
	if err := json.Unmarshal(mustJSON(activated.Payload), &activateOut); err != nil || activateOut.Generation.State != "active" {
		t.Fatalf("activate payload %+v err=%v", activateOut, err)
	}
	replay := e.Handle(ctx, memoryItemRequest("memory.generation.activate", activatePayload, activateOp))
	if !replay.OK {
		t.Fatalf("activate replay: %+v", replay.Error)
	}
	otherGen, err := e.m8memory.BuildMemoryGeneration(ctx, memoryOpsLegacySubject, "user", "", "", ulid.Make().String(), ulid.Make().String())
	if err != nil {
		t.Fatal(err)
	}
	mismatch := e.Handle(ctx, memoryItemRequest("memory.generation.activate",
		`{"generationId":"`+otherGen.GenerationID+`","expectedRevision":`+itoa(listPayload.DatabaseRevision)+`,"operationId":"`+ulid.Make().String()+`"}`, activateOp))
	if mismatch.OK || mismatch.Error == nil || mismatch.Error.Code != "OPERATION_REPLAY_MISMATCH" {
		t.Fatalf("activate mismatch %+v", mismatch)
	}

	discardActiveOp := ulid.Make().String()
	discardActive := e.Handle(ctx, memoryItemRequest("memory.generation.discard",
		`{"generationId":"`+ready.GenerationID+`","expectedRevision":`+itoa(activateOut.DatabaseRevision)+`,"operationId":"`+discardActiveOp+`"}`, discardActiveOp))
	if discardActive.OK || discardActive.Error == nil || discardActive.Error.Code != "MEMORY_GENERATION_ACTIVE" {
		t.Fatalf("discard active %+v", discardActive)
	}

	stale := e.Handle(ctx, memoryItemRequest("memory.generation.activate",
		`{"generationId":"`+otherGen.GenerationID+`","expectedRevision":99,"operationId":"`+ulid.Make().String()+`"}`, ulid.Make().String()))
	if stale.OK || stale.Error == nil || stale.Error.Code != "REVISION_CONFLICT" {
		t.Fatalf("stale activate %+v", stale)
	}

	readyB, err := e.m8memory.BuildMemoryGeneration(ctx, memoryOpsLegacySubject, "user", "", "", ulid.Make().String(), ulid.Make().String())
	if err != nil {
		t.Fatal(err)
	}
	discardOp := ulid.Make().String()
	discarded := e.Handle(ctx, memoryItemRequest("memory.generation.discard",
		`{"generationId":"`+readyB.GenerationID+`","expectedRevision":1,"operationId":"`+discardOp+`"}`, discardOp))
	if !discarded.OK {
		t.Fatalf("discard ready: %+v", discarded.Error)
	}
	var discardOut struct {
		Generation struct {
			State string `json:"state"`
		} `json:"generation"`
	}
	if err := json.Unmarshal(mustJSON(discarded.Payload), &discardOut); err != nil || discardOut.Generation.State != "discarded" {
		t.Fatalf("discard payload %+v err=%v", discardOut, err)
	}
}

func TestMemoryImportBridgeContract(t *testing.T) {
	e := newMemoryV2Engine(t)
	ctx := context.Background()
	missingOp := ulid.Make().String()
	missing := e.Handle(ctx, memoryItemRequest("memory.import.preview",
		`{"sourceArtifactId":"`+ulid.Make().String()+`","operationId":"`+missingOp+`"}`, missingOp))
	if missing.OK || missing.Error == nil || missing.Error.Code != "MEMORY_IMPORT_SOURCE_MISSING" {
		t.Fatalf("missing source %+v", missing)
	}

	archive, err := json.Marshal(map[string]any{
		"schema": "lunitide.memory.fabric_v2",
		"records": []map[string]any{{
			"kind":      "preference",
			"factId":    ulid.Make().String(),
			"text":      "默认使用中文回答",
			"scopeKind": "user",
			"forgotten": false,
		}},
		"manifest": map[string]any{"source": "test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	artifactID, _, err := e.m8memory.SealMemoryArchive(ctx, memoryOpsLegacySubject, "user", "", archive)
	if err != nil {
		t.Fatal(err)
	}
	before := e.Handle(ctx, validRequest("memory.item.list", `{"scopeKind":"user"}`))
	if !before.OK {
		t.Fatalf("list before: %+v", before.Error)
	}
	previewOp := ulid.Make().String()
	preview := e.Handle(ctx, memoryItemRequest("memory.import.preview",
		`{"sourceArtifactId":"`+artifactID+`","operationId":"`+previewOp+`"}`, previewOp))
	if !preview.OK {
		t.Fatalf("preview: %+v", preview.Error)
	}
	var previewPayload struct {
		PreviewID        string `json:"previewId"`
		SourceArtifactID string `json:"sourceArtifactId"`
		ArchiveDigest    string `json:"archiveDigest"`
		ManifestDigest   string `json:"manifestDigest"`
		DatabaseRevision int64  `json:"databaseRevision"`
		ExpiresAt        string `json:"expiresAt"`
		Counts           struct {
			Total      int `json:"total"`
			Accepted   int `json:"accepted"`
			Tombstones int `json:"tombstones"`
		} `json:"counts"`
		OperationID string `json:"operationId"`
	}
	if err := json.Unmarshal(mustJSON(preview.Payload), &previewPayload); err != nil {
		t.Fatal(err)
	}
	if previewPayload.PreviewID == "" || previewPayload.SourceArtifactID != artifactID || len(previewPayload.ArchiveDigest) != 64 ||
		len(previewPayload.ManifestDigest) != 64 || previewPayload.DatabaseRevision < 1 || previewPayload.ExpiresAt == "" ||
		previewPayload.Counts.Total != 1 || previewPayload.Counts.Accepted != 1 || previewPayload.OperationID != previewOp {
		t.Fatalf("preview payload %+v", previewPayload)
	}
	afterPreview := e.Handle(ctx, validRequest("memory.item.list", `{"scopeKind":"user"}`))
	if !afterPreview.OK {
		t.Fatalf("list after preview: %+v", afterPreview.Error)
	}
	var beforeItems, afterItems struct {
		Items []struct {
			FactID string `json:"factId"`
		} `json:"items"`
	}
	if err := json.Unmarshal(mustJSON(before.Payload), &beforeItems); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mustJSON(afterPreview.Payload), &afterItems); err != nil {
		t.Fatal(err)
	}
	if len(beforeItems.Items) != len(afterItems.Items) {
		t.Fatalf("preview mutated active facts before=%s after=%s", mustJSON(before.Payload), mustJSON(afterPreview.Payload))
	}

	badDigest := strings.Repeat("ab", 32)
	badCommitOp := ulid.Make().String()
	badCommit := e.Handle(ctx, memoryItemRequest("memory.import.commit",
		`{"previewId":"`+previewPayload.PreviewID+`","archiveDigest":"`+badDigest+`","manifestDigest":"`+previewPayload.ManifestDigest+`","expectedDatabaseRevision":`+itoa(previewPayload.DatabaseRevision)+`,"operationId":"`+badCommitOp+`"}`, badCommitOp))
	if badCommit.OK || badCommit.Error == nil || badCommit.Error.Code != "MEMORY_IMPORT_DIGEST_MISMATCH" {
		t.Fatalf("digest mismatch %+v", badCommit)
	}

	commitOp := ulid.Make().String()
	committed := e.Handle(ctx, memoryItemRequest("memory.import.commit",
		`{"previewId":"`+previewPayload.PreviewID+`","archiveDigest":"`+previewPayload.ArchiveDigest+`","manifestDigest":"`+previewPayload.ManifestDigest+`","expectedDatabaseRevision":`+itoa(previewPayload.DatabaseRevision)+`,"operationId":"`+commitOp+`"}`, commitOp))
	if !committed.OK {
		t.Fatalf("commit: %+v", committed.Error)
	}
	var commitPayload struct {
		State         string `json:"state"`
		ImportedCount int    `json:"importedCount"`
		SkippedCount  int    `json:"skippedCount"`
	}
	if err := json.Unmarshal(mustJSON(committed.Payload), &commitPayload); err != nil || commitPayload.State != "committed" || commitPayload.ImportedCount != 1 {
		t.Fatalf("commit payload %+v err=%v", commitPayload, err)
	}
	listed := e.Handle(ctx, validRequest("memory.item.list", `{"scopeKind":"user"}`))
	if !listed.OK {
		t.Fatalf("list after commit: %+v", listed.Error)
	}
	var listPayload struct {
		Items []struct {
			FactID string  `json:"factId"`
			Text   *string `json:"text"`
		} `json:"items"`
	}
	if err := json.Unmarshal(mustJSON(listed.Payload), &listPayload); err != nil || len(listPayload.Items) != 1 ||
		listPayload.Items[0].Text == nil || *listPayload.Items[0].Text != "默认使用中文回答" {
		t.Fatalf("imported list %+v err=%v", listPayload, err)
	}

	forgetOp := ulid.Make().String()
	got := e.Handle(ctx, validRequest("memory.item.get", `{"factId":"`+listPayload.Items[0].FactID+`"}`))
	if !got.OK {
		t.Fatalf("get imported: %+v", got.Error)
	}
	var gotItem struct {
		Revision int64 `json:"revision"`
	}
	if err := json.Unmarshal(mustJSON(got.Payload), &gotItem); err != nil {
		t.Fatal(err)
	}
	forgotten := e.Handle(ctx, memoryItemRequest("memory.item.forget",
		`{"factId":"`+listPayload.Items[0].FactID+`","mode":"fact_history","expectedRevision":`+itoa(gotItem.Revision)+`,"operationId":"`+forgetOp+`"}`, forgetOp))
	if !forgotten.OK {
		t.Fatalf("forget: %+v", forgotten.Error)
	}

	tombArchive, err := json.Marshal(map[string]any{
		"schema": "lunitide.memory.fabric_v2",
		"records": []map[string]any{{
			"kind":      "preference",
			"factId":    ulid.Make().String(),
			"text":      "默认使用中文回答",
			"scopeKind": "user",
			"forgotten": false,
		}},
		"manifest": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	tombArtifact, _, err := e.m8memory.SealMemoryArchive(ctx, memoryOpsLegacySubject, "user", "", tombArchive)
	if err != nil {
		t.Fatal(err)
	}
	tombPreviewOp := ulid.Make().String()
	tombPreview := e.Handle(ctx, memoryItemRequest("memory.import.preview",
		`{"sourceArtifactId":"`+tombArtifact+`","operationId":"`+tombPreviewOp+`"}`, tombPreviewOp))
	if !tombPreview.OK {
		t.Fatalf("tomb preview: %+v", tombPreview.Error)
	}
	var tombPreviewPayload struct {
		PreviewID        string `json:"previewId"`
		ArchiveDigest    string `json:"archiveDigest"`
		ManifestDigest   string `json:"manifestDigest"`
		DatabaseRevision int64  `json:"databaseRevision"`
		Counts           struct {
			Tombstones int `json:"tombstones"`
			Accepted   int `json:"accepted"`
		} `json:"counts"`
	}
	if err := json.Unmarshal(mustJSON(tombPreview.Payload), &tombPreviewPayload); err != nil || tombPreviewPayload.Counts.Tombstones != 1 || tombPreviewPayload.Counts.Accepted != 0 {
		t.Fatalf("tomb preview payload %+v err=%v", tombPreviewPayload, err)
	}
	tombCommitOp := ulid.Make().String()
	tombCommit := e.Handle(ctx, memoryItemRequest("memory.import.commit",
		`{"previewId":"`+tombPreviewPayload.PreviewID+`","archiveDigest":"`+tombPreviewPayload.ArchiveDigest+`","manifestDigest":"`+tombPreviewPayload.ManifestDigest+`","expectedDatabaseRevision":`+itoa(tombPreviewPayload.DatabaseRevision)+`,"operationId":"`+tombCommitOp+`"}`, tombCommitOp))
	if !tombCommit.OK {
		t.Fatalf("tomb commit: %+v", tombCommit.Error)
	}
	var tombCommitPayload struct {
		ImportedCount int `json:"importedCount"`
		SkippedCount  int `json:"skippedCount"`
	}
	if err := json.Unmarshal(mustJSON(tombCommit.Payload), &tombCommitPayload); err != nil || tombCommitPayload.ImportedCount != 0 || tombCommitPayload.SkippedCount != 1 {
		t.Fatalf("tomb commit payload %+v err=%v", tombCommitPayload, err)
	}
	revived := e.Handle(ctx, validRequest("memory.item.list", `{"scopeKind":"user"}`))
	if !revived.OK {
		t.Fatalf("list after tomb commit: %+v", revived.Error)
	}
	var revivedPayload struct {
		Items []struct{} `json:"items"`
	}
	if err := json.Unmarshal(mustJSON(revived.Payload), &revivedPayload); err != nil || len(revivedPayload.Items) != 0 {
		t.Fatalf("tombstone revived %+v err=%v", revivedPayload, err)
	}
}
