package app

import (
	"context"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m8core"
)

type generationSummaryDTO struct {
	GenerationID       string  `json:"generationId"`
	ParentGenerationID *string `json:"parentGenerationId"`
	ScopeKind          string  `json:"scopeKind"`
	ScopeID            *string `json:"scopeId"`
	State              string  `json:"state"`
	SourceCutoffSeq    int64   `json:"sourceCutoffSeq"`
	BuilderVersion     string  `json:"builderVersion"`
	MemberCount        int64   `json:"memberCount"`
	Revision           int64   `json:"revision"`
	CreatedAt          string  `json:"createdAt"`
	ReadyAt            *string `json:"readyAt"`
	ActivatedAt        *string `json:"activatedAt"`
	ErrorCode          *string `json:"errorCode"`
}

type generationChangeDTO struct {
	Change      string   `json:"change"`
	FactID      string   `json:"factId"`
	FromVersion *int64   `json:"fromVersion"`
	ToVersion   *int64   `json:"toVersion"`
	BeforeText  *string  `json:"beforeText"`
	AfterText   *string  `json:"afterText"`
	ReasonCodes []string `json:"reasonCodes"`
}

func publicGeneration(g m8core.MemoryGeneration) generationSummaryDTO {
	out := generationSummaryDTO{
		GenerationID:    g.GenerationID,
		ScopeKind:       g.ScopeKind,
		State:           g.State,
		SourceCutoffSeq: g.SourceCutoffSeq,
		BuilderVersion:  g.BuilderVersion,
		MemberCount:     g.MemberCount,
		Revision:        atLeastOneRevision(g.Revision),
		CreatedAt:       rfc3339OrRaw(g.CreatedAt),
	}
	if g.ParentGenerationID != "" {
		id := g.ParentGenerationID
		out.ParentGenerationID = &id
	}
	if g.ScopeKind == "project" && g.ScopeID != "" {
		id := g.ScopeID
		out.ScopeID = &id
	}
	if g.ReadyAt != "" {
		ts := rfc3339OrRaw(g.ReadyAt)
		out.ReadyAt = &ts
	}
	if g.ActivatedAt != "" {
		ts := rfc3339OrRaw(g.ActivatedAt)
		out.ActivatedAt = &ts
	}
	if g.ErrorCode != "" {
		code := g.ErrorCode
		out.ErrorCode = &code
	}
	return out
}

func publicGenerationChange(chg m8core.MemoryGenerationChange) generationChangeDTO {
	out := generationChangeDTO{
		Change:      chg.Change,
		FactID:      chg.FactID,
		FromVersion: chg.FromVersion,
		ToVersion:   chg.ToVersion,
		ReasonCodes: chg.ReasonCodes,
	}
	if out.ReasonCodes == nil {
		out.ReasonCodes = []string{}
	}
	if strings.TrimSpace(chg.BeforeText) != "" {
		text := clipGenerationDTOText(chg.BeforeText)
		out.BeforeText = &text
	}
	if strings.TrimSpace(chg.AfterText) != "" {
		text := clipGenerationDTOText(chg.AfterText)
		out.AfterText = &text
	}
	return out
}

func handleMemoryGenerationList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ScopeKind string `json:"scopeKind"`
		ScopeID   string `json:"scopeId"`
		Cursor    string `json:"cursor"`
		Limit     int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || (p.ScopeKind != "user" && p.ScopeKind != "project") {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.generation.list 参数无效", false)
	}
	if p.ScopeKind == "user" && p.ScopeID != "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.generation.list 参数无效", false)
	}
	if p.ScopeKind == "project" && !validCanonicalULID(p.ScopeID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.generation.list 参数无效", false)
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	items, next, rev, err := e.m8memory.ListMemoryGenerations(ctx, e.memorySubjectID(), p.ScopeKind, p.ScopeID, p.Cursor, p.Limit)
	if err != nil {
		return memoryV2Failure(r, err)
	}
	out := make([]generationSummaryDTO, 0, len(items))
	for _, item := range items {
		out = append(out, publicGeneration(item))
	}
	var nextCursor *string
	if next != "" {
		nextCursor = &next
	}
	return r.Ok(struct {
		Items            []generationSummaryDTO `json:"items"`
		NextCursor       *string                `json:"nextCursor"`
		DatabaseRevision int64                  `json:"databaseRevision"`
	}{Items: out, NextCursor: nextCursor, DatabaseRevision: atLeastOneRevision(rev)})
}

func handleMemoryGenerationPreview(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		GenerationID string `json:"generationId"`
		Cursor       string `json:"cursor"`
		Limit        int    `json:"limit"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.GenerationID) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.generation.preview 参数无效", false)
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	gen, changes, next, err := e.m8memory.PreviewMemoryGeneration(ctx, e.memorySubjectID(), p.GenerationID, p.Cursor, p.Limit)
	if err != nil {
		return memoryV2Failure(r, err)
	}
	out := make([]generationChangeDTO, 0, len(changes))
	for _, chg := range changes {
		out = append(out, publicGenerationChange(chg))
	}
	var nextCursor *string
	if next != "" {
		nextCursor = &next
	}
	return r.Ok(struct {
		Generation generationSummaryDTO `json:"generation"`
		Changes    []generationChangeDTO `json:"changes"`
		NextCursor *string               `json:"nextCursor"`
	}{Generation: publicGeneration(gen), Changes: out, NextCursor: nextCursor})
}

func handleMemoryGenerationActivate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		GenerationID     string `json:"generationId"`
		ExpectedRevision int64  `json:"expectedRevision"`
		OperationID      string `json:"operationId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.GenerationID) || !validCanonicalULID(p.OperationID) ||
		p.ExpectedRevision < 1 || strings.TrimSpace(r.IdempotencyKey) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.generation.activate 参数无效", false)
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	gen, rev, err := e.m8memory.ActivateMemoryGeneration(ctx, e.memorySubjectID(), p.GenerationID, p.OperationID, r.IdempotencyKey, p.ExpectedRevision)
	if err != nil {
		return memoryV2Failure(r, err)
	}
	return r.Ok(struct {
		Generation       generationSummaryDTO `json:"generation"`
		DatabaseRevision int64                `json:"databaseRevision"`
		OperationID      string               `json:"operationId"`
	}{Generation: publicGeneration(gen), DatabaseRevision: atLeastOneRevision(rev), OperationID: p.OperationID})
}

func handleMemoryGenerationDiscard(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		GenerationID     string `json:"generationId"`
		ExpectedRevision int64  `json:"expectedRevision"`
		OperationID      string `json:"operationId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.GenerationID) || !validCanonicalULID(p.OperationID) ||
		p.ExpectedRevision < 1 || strings.TrimSpace(r.IdempotencyKey) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.generation.discard 参数无效", false)
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	gen, rev, err := e.m8memory.DiscardMemoryGeneration(ctx, e.memorySubjectID(), p.GenerationID, p.OperationID, r.IdempotencyKey, p.ExpectedRevision)
	if err != nil {
		return memoryV2Failure(r, err)
	}
	return r.Ok(struct {
		Generation       generationSummaryDTO `json:"generation"`
		DatabaseRevision int64                `json:"databaseRevision"`
		OperationID      string               `json:"operationId"`
	}{Generation: publicGeneration(gen), DatabaseRevision: atLeastOneRevision(rev), OperationID: p.OperationID})
}

func handleMemoryImportPreview(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		SourceArtifactID string `json:"sourceArtifactId"`
		OperationID      string `json:"operationId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.SourceArtifactID) || !validCanonicalULID(p.OperationID) ||
		strings.TrimSpace(r.IdempotencyKey) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.import.preview 参数无效", false)
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	preview, err := e.m8memory.PreviewMemoryImport(ctx, e.memorySubjectID(), p.SourceArtifactID, p.OperationID, r.IdempotencyKey)
	if err != nil {
		return memoryV2Failure(r, err)
	}
	warnings := preview.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	expires := preview.ExpiresAt
	if _, err := time.Parse(time.RFC3339, expires); err != nil {
		if ts, err := time.Parse(time.RFC3339Nano, expires); err == nil {
			expires = ts.UTC().Format(time.RFC3339)
		}
	}
	return r.Ok(struct {
		PreviewID        string                   `json:"previewId"`
		SourceArtifactID string                   `json:"sourceArtifactId"`
		ArchiveDigest    string                   `json:"archiveDigest"`
		ManifestDigest   string                   `json:"manifestDigest"`
		DatabaseRevision int64                    `json:"databaseRevision"`
		ExpiresAt        string                   `json:"expiresAt"`
		Counts           m8core.MemoryImportCounts `json:"counts"`
		Warnings         []string                 `json:"warnings"`
		OperationID      string                   `json:"operationId"`
	}{
		PreviewID:        preview.PreviewID,
		SourceArtifactID: preview.SourceArtifactID,
		ArchiveDigest:    preview.ArchiveDigest,
		ManifestDigest:   preview.ManifestDigest,
		DatabaseRevision: atLeastOneRevision(preview.DatabaseRevision),
		ExpiresAt:        expires,
		Counts:           preview.Counts,
		Warnings:         warnings,
		OperationID:      preview.OperationID,
	})
}

func handleMemoryImportCommit(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		PreviewID                string `json:"previewId"`
		ArchiveDigest            string `json:"archiveDigest"`
		ManifestDigest           string `json:"manifestDigest"`
		ExpectedDatabaseRevision int64  `json:"expectedDatabaseRevision"`
		OperationID              string `json:"operationId"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.PreviewID) || !validCanonicalULID(p.OperationID) ||
		len(p.ArchiveDigest) != 64 || len(p.ManifestDigest) != 64 || p.ExpectedDatabaseRevision < 1 ||
		strings.TrimSpace(r.IdempotencyKey) == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "memory.import.commit 参数无效", false)
	}
	if e.m8memory == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "记忆内核服务暂时不可用", true)
	}
	res, err := e.m8memory.CommitMemoryImport(ctx, e.memorySubjectID(), p.PreviewID, p.ArchiveDigest, p.ManifestDigest, p.OperationID, r.IdempotencyKey, p.ExpectedDatabaseRevision)
	if err != nil {
		return memoryV2Failure(r, err)
	}
	return r.Ok(struct {
		PreviewID        string `json:"previewId"`
		State            string `json:"state"`
		ImportedCount    int    `json:"importedCount"`
		ReviewCount      int    `json:"reviewCount"`
		SkippedCount     int    `json:"skippedCount"`
		ConflictCount    int    `json:"conflictCount"`
		DatabaseRevision int64  `json:"databaseRevision"`
		OperationID      string `json:"operationId"`
	}{
		PreviewID:        res.PreviewID,
		State:            "committed",
		ImportedCount:    res.ImportedCount,
		ReviewCount:      res.ReviewCount,
		SkippedCount:     res.SkippedCount,
		ConflictCount:    res.ConflictCount,
		DatabaseRevision: atLeastOneRevision(res.DatabaseRevision),
		OperationID:      p.OperationID,
	})
}

func atLeastOneRevision(n int64) int64 {
	if n < 1 {
		return 1
	}
	return n
}

func rfc3339OrRaw(s string) string {
	if ts, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return ts.UTC().Format(time.RFC3339)
	}
	return s
}

func clipGenerationDTOText(s string) string {
	if len(s) > 8192 {
		return s[:8192]
	}
	return s
}
