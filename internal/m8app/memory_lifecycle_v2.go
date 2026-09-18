package m8app

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/m8core"
)

const canonicalTextMaxBytes = 8192

type canonicalWriter interface {
	CreateCanonicalMemoryItem(context.Context, m8core.CanonicalMemoryWrite) (m8core.CanonicalMemoryResult, error)
}

type canonicalReader interface {
	ListCanonicalMemoryRecords(context.Context, string, string, string, string, int) ([]m8core.CanonicalMemoryRecord, error)
	GetCanonicalMemoryItem(context.Context, string, string) (m8core.CanonicalMemoryRecord, error)
	MemoryDatabaseRevision(context.Context, string) (int64, error)
	ListCanonicalMemoryHistory(context.Context, string, string, int64, int) ([]m8core.CanonicalMemoryRecord, string, error)
	ListMemoryReviews(context.Context, string, string, string, int) ([]m8core.MemoryReviewRecord, error)
}

type canonicalForgetter interface {
	ForgetCanonicalMemoryItem(context.Context, string, string, string, int64) error
}

type canonicalCorrector interface {
	CorrectCanonicalMemoryItem(context.Context, string, string, string, string, string, string, int64, *time.Time) (m8core.CanonicalMemoryRecord, error)
}

type canonicalUndoer interface {
	UndoCanonicalCapture(context.Context, string, string, string, string) (m8core.MemoryUndoResult, error)
}

type memoryReviewResolver interface {
	ResolveMemoryReview(context.Context, string, string, string, string, string, string, int64) (m8core.CanonicalMemoryRecord, error)
	PutMemoryReview(context.Context, string, m8core.MemoryReviewWrite) error
}

type memoryPurgeGranter interface {
	PrepareMemoryPurgeGrant(context.Context, string, string, string, string, string, int64) (m8core.MemoryPurgePrepareResult, error)
	ConsumeMemoryPurgeGrant(context.Context, string, string, string, string, string, int64) (m8core.MemoryOpsCounts, string, error)
}

type userMessageTextReader interface {
	GetUserMessageText(context.Context, string) (string, error)
}

type hybridRecaller interface {
	HybridRecallCanonical(context.Context, m8core.HybridRecallQuery) (m8core.HybridRecallResult, error)
}

type memoryGenerationStore interface {
	BuildMemoryGeneration(context.Context, string, string, string, string, string, string) (m8core.MemoryGeneration, error)
	ListMemoryGenerations(context.Context, string, string, string, string, int) ([]m8core.MemoryGeneration, string, error)
	GetMemoryGeneration(context.Context, string, string) (m8core.MemoryGeneration, error)
	PreviewMemoryGeneration(context.Context, string, string, string, int) (m8core.MemoryGeneration, []m8core.MemoryGenerationChange, string, error)
	ActivateMemoryGeneration(context.Context, string, string, string, string, int64) (m8core.MemoryGeneration, error)
	DiscardMemoryGeneration(context.Context, string, string, string, string, int64) (m8core.MemoryGeneration, error)
}

type memoryImportStore interface {
	SealMemoryArchive(context.Context, string, string, string, []byte) (string, string, error)
	PreviewMemoryImport(context.Context, string, string, string, string) (m8core.MemoryImportPreview, error)
	CommitMemoryImport(context.Context, string, string, string, string, string, string, int64) (m8core.MemoryImportCommit, error)
	ExportCanonicalMemoryArchive(context.Context, string) (m8core.MemoryFabricExport, error)
}

type canonicalFeedbackStore interface {
	RecordCanonicalFeedback(context.Context, string, string, int64, string, string) error
}

// CreateCanonicalItemInput is the explicit save mutation.
type CreateCanonicalItemInput struct {
	ScopeKind      string
	ScopeID        string
	Text           string
	OperationID    string
	IdempotencyKey string
	SourceKind     string
	SourceRef      string
	StartByte      *int64
	EndByte        *int64
	QuoteDigest    string
}

// CreateCanonicalItemResult is the public create receipt.
type CreateCanonicalItemResult struct {
	Item             m8core.CanonicalMemoryRecord
	DatabaseRevision int64
	UndoOperationID  string
	UndoExpiresAt    time.Time
}

func (s *MemoryService) CreateCanonicalItem(ctx context.Context, subjectID string, in CreateCanonicalItemInput) (CreateCanonicalItemResult, error) {
	if s == nil || s.uow == nil {
		return CreateCanonicalItemResult{}, ErrServiceUnavailable
	}
	text := strings.TrimSpace(in.Text)
	if text == "" || m8core.RejectTransientMemory(text) || len(text) > canonicalTextMaxBytes || !utf8.ValidString(text) {
		return CreateCanonicalItemResult{}, ErrMemorySourceInvalid
	}
	if in.ScopeKind == "" {
		in.ScopeKind = "user"
	}
	if in.ScopeKind == "user" {
		in.ScopeID = subjectID
	}
	if in.ScopeKind != "user" && in.ScopeKind != "project" {
		return CreateCanonicalItemResult{}, ErrMemorySourceInvalid
	}
	if in.ScopeKind == "project" && strings.TrimSpace(in.ScopeID) == "" {
		return CreateCanonicalItemResult{}, ErrMemorySourceInvalid
	}
	writer, ok := s.uow.(canonicalWriter)
	if !ok {
		return CreateCanonicalItemResult{}, ErrServiceUnavailable
	}
	created, err := writer.CreateCanonicalMemoryItem(ctx, m8core.CanonicalMemoryWrite{
		SubjectID:      subjectID,
		ScopeKind:      in.ScopeKind,
		ScopeID:        in.ScopeID,
		Kind:           m8core.ClassifyMemoryKind(text),
		Text:           text,
		OperationID:    in.OperationID,
		IdempotencyKey: in.IdempotencyKey,
		SourceKind:     in.SourceKind,
		SourceRef:      in.SourceRef,
		StartByte:      in.StartByte,
		EndByte:        in.EndByte,
		QuoteDigest:    in.QuoteDigest,
	})
	if err != nil {
		return CreateCanonicalItemResult{}, err
	}
	item, err := s.GetCanonicalItem(ctx, subjectID, created.FactID)
	if err != nil {
		return CreateCanonicalItemResult{}, err
	}
	rev, err := s.canonicalRevision(ctx, subjectID)
	if err != nil {
		return CreateCanonicalItemResult{}, err
	}
	return CreateCanonicalItemResult{
		Item:             item,
		DatabaseRevision: rev,
		UndoOperationID:  in.OperationID,
		UndoExpiresAt:    time.Now().UTC().Add(24 * time.Hour),
	}, nil
}

func (s *MemoryService) ListCanonicalItems(ctx context.Context, subjectID, scopeKind, scopeID, kind string, limit int) ([]m8core.CanonicalMemoryRecord, int64, error) {
	reader, ok := s.uow.(canonicalReader)
	if !ok {
		return nil, 0, ErrServiceUnavailable
	}
	if scopeKind == "user" {
		scopeID = subjectID
	}
	items, err := reader.ListCanonicalMemoryRecords(ctx, subjectID, scopeKind, scopeID, kind, limit)
	if err != nil {
		return nil, 0, err
	}
	rev, err := reader.MemoryDatabaseRevision(ctx, subjectID)
	return items, rev, err
}

func (s *MemoryService) GetCanonicalItem(ctx context.Context, subjectID, factID string) (m8core.CanonicalMemoryRecord, error) {
	reader, ok := s.uow.(canonicalReader)
	if !ok {
		return m8core.CanonicalMemoryRecord{}, ErrServiceUnavailable
	}
	return reader.GetCanonicalMemoryItem(ctx, subjectID, factID)
}

func (s *MemoryService) ForgetCanonicalItem(ctx context.Context, subjectID, factID, operationID string, expectedRevision int64) error {
	forgetter, ok := s.uow.(canonicalForgetter)
	if !ok {
		return ErrServiceUnavailable
	}
	return forgetter.ForgetCanonicalMemoryItem(ctx, subjectID, factID, operationID, expectedRevision)
}

func (s *MemoryService) HistoryCanonicalItem(ctx context.Context, subjectID, factID string, cursor int64, limit int) ([]m8core.CanonicalMemoryRecord, string, error) {
	reader, ok := s.uow.(canonicalReader)
	if !ok {
		return nil, "", ErrServiceUnavailable
	}
	return reader.ListCanonicalMemoryHistory(ctx, subjectID, factID, cursor, limit)
}

func (s *MemoryService) CorrectCanonicalItem(ctx context.Context, subjectID, factID, text, reason, operationID, idempotencyKey string, expectedRevision int64, validFrom *time.Time) (m8core.CanonicalMemoryRecord, int64, error) {
	if strings.TrimSpace(text) == "" || m8core.RejectTransientMemory(text) || len(strings.TrimSpace(text)) > canonicalTextMaxBytes || !utf8.ValidString(text) {
		return m8core.CanonicalMemoryRecord{}, 0, ErrMemorySourceInvalid
	}
	corrector, ok := s.uow.(canonicalCorrector)
	if !ok {
		return m8core.CanonicalMemoryRecord{}, 0, ErrServiceUnavailable
	}
	item, err := corrector.CorrectCanonicalMemoryItem(ctx, subjectID, factID, text, reason, operationID, idempotencyKey, expectedRevision, validFrom)
	if err != nil {
		return m8core.CanonicalMemoryRecord{}, 0, err
	}
	rev, err := s.canonicalRevision(ctx, subjectID)
	return item, rev, err
}

func (s *MemoryService) UndoCanonicalCapture(ctx context.Context, subjectID, undoOperationID, operationID, idempotencyKey string) (m8core.MemoryUndoResult, error) {
	undoer, ok := s.uow.(canonicalUndoer)
	if !ok {
		return m8core.MemoryUndoResult{}, ErrServiceUnavailable
	}
	return undoer.UndoCanonicalCapture(ctx, subjectID, undoOperationID, operationID, idempotencyKey)
}

func (s *MemoryService) ListMemoryReviews(ctx context.Context, subjectID, scopeKind, scopeID string, limit int) ([]m8core.MemoryReviewRecord, int64, error) {
	reader, ok := s.uow.(canonicalReader)
	if !ok {
		return nil, 0, ErrServiceUnavailable
	}
	items, err := reader.ListMemoryReviews(ctx, subjectID, scopeKind, scopeID, limit)
	if err != nil {
		return nil, 0, err
	}
	rev, err := reader.MemoryDatabaseRevision(ctx, subjectID)
	return items, rev, err
}

func (s *MemoryService) ResolveMemoryReview(ctx context.Context, subjectID, reviewID, decision, text, operationID, idempotencyKey string, expectedRevision int64) (m8core.CanonicalMemoryRecord, int64, error) {
	resolver, ok := s.uow.(memoryReviewResolver)
	if !ok {
		return m8core.CanonicalMemoryRecord{}, 0, ErrServiceUnavailable
	}
	item, err := resolver.ResolveMemoryReview(ctx, subjectID, reviewID, decision, text, operationID, idempotencyKey, expectedRevision)
	if err != nil {
		return m8core.CanonicalMemoryRecord{}, 0, err
	}
	rev, err := s.canonicalRevision(ctx, subjectID)
	return item, rev, err
}

func (s *MemoryService) PutMemoryReview(ctx context.Context, subjectID string, review m8core.MemoryReviewWrite) error {
	resolver, ok := s.uow.(memoryReviewResolver)
	if !ok {
		return ErrServiceUnavailable
	}
	return resolver.PutMemoryReview(ctx, subjectID, review)
}

func (s *MemoryService) PrepareMemoryPurge(ctx context.Context, subjectID, scopeKind, scopeID, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryPurgePrepareResult, error) {
	granter, ok := s.uow.(memoryPurgeGranter)
	if !ok {
		return m8core.MemoryPurgePrepareResult{}, ErrServiceUnavailable
	}
	return granter.PrepareMemoryPurgeGrant(ctx, subjectID, scopeKind, scopeID, operationID, idempotencyKey, expectedRevision)
}

func (s *MemoryService) ConsumeMemoryPurge(ctx context.Context, subjectID, token, snapshotDigest, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryOpsCounts, string, error) {
	granter, ok := s.uow.(memoryPurgeGranter)
	if !ok {
		return m8core.MemoryOpsCounts{}, "", ErrServiceUnavailable
	}
	return granter.ConsumeMemoryPurgeGrant(ctx, subjectID, token, snapshotDigest, operationID, idempotencyKey, expectedRevision)
}

func (s *MemoryService) ResolveUserMessageText(ctx context.Context, messageID string) (string, error) {
	reader, ok := s.uow.(userMessageTextReader)
	if !ok {
		return "", ErrMemorySourceInvalid
	}
	text, err := reader.GetUserMessageText(ctx, messageID)
	if err != nil {
		return "", ErrMemorySourceInvalid
	}
	return text, nil
}

func (s *MemoryService) canonicalRevision(ctx context.Context, subjectID string) (int64, error) {
	reader, ok := s.uow.(canonicalReader)
	if !ok {
		return 0, ErrServiceUnavailable
	}
	return reader.MemoryDatabaseRevision(ctx, subjectID)
}

func (s *MemoryService) HybridRecall(ctx context.Context, q m8core.HybridRecallQuery) (m8core.HybridRecallResult, error) {
	if s == nil || s.uow == nil {
		return m8core.HybridRecallResult{}, ErrServiceUnavailable
	}
	recaller, ok := s.uow.(hybridRecaller)
	if !ok {
		return m8core.HybridRecallResult{}, ErrServiceUnavailable
	}
	return recaller.HybridRecallCanonical(ctx, q)
}

func (s *MemoryService) BuildMemoryGeneration(ctx context.Context, subjectID, scopeKind, scopeID, parentID, operationID, idempotencyKey string) (m8core.MemoryGeneration, error) {
	store, ok := s.uow.(memoryGenerationStore)
	if !ok {
		return m8core.MemoryGeneration{}, ErrServiceUnavailable
	}
	return store.BuildMemoryGeneration(ctx, subjectID, scopeKind, scopeID, parentID, operationID, idempotencyKey)
}

func (s *MemoryService) ListMemoryGenerations(ctx context.Context, subjectID, scopeKind, scopeID, cursor string, limit int) ([]m8core.MemoryGeneration, string, int64, error) {
	store, ok := s.uow.(memoryGenerationStore)
	if !ok {
		return nil, "", 0, ErrServiceUnavailable
	}
	items, next, err := store.ListMemoryGenerations(ctx, subjectID, scopeKind, scopeID, cursor, limit)
	if err != nil {
		return nil, "", 0, err
	}
	rev, err := s.canonicalRevision(ctx, subjectID)
	return items, next, rev, err
}

func (s *MemoryService) PreviewMemoryGeneration(ctx context.Context, subjectID, generationID, cursor string, limit int) (m8core.MemoryGeneration, []m8core.MemoryGenerationChange, string, error) {
	store, ok := s.uow.(memoryGenerationStore)
	if !ok {
		return m8core.MemoryGeneration{}, nil, "", ErrServiceUnavailable
	}
	return store.PreviewMemoryGeneration(ctx, subjectID, generationID, cursor, limit)
}

func (s *MemoryService) ActivateMemoryGeneration(ctx context.Context, subjectID, generationID, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryGeneration, int64, error) {
	store, ok := s.uow.(memoryGenerationStore)
	if !ok {
		return m8core.MemoryGeneration{}, 0, ErrServiceUnavailable
	}
	gen, err := store.ActivateMemoryGeneration(ctx, subjectID, generationID, operationID, idempotencyKey, expectedRevision)
	if err != nil {
		return m8core.MemoryGeneration{}, 0, err
	}
	rev, err := s.canonicalRevision(ctx, subjectID)
	return gen, rev, err
}

func (s *MemoryService) DiscardMemoryGeneration(ctx context.Context, subjectID, generationID, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryGeneration, int64, error) {
	store, ok := s.uow.(memoryGenerationStore)
	if !ok {
		return m8core.MemoryGeneration{}, 0, ErrServiceUnavailable
	}
	gen, err := store.DiscardMemoryGeneration(ctx, subjectID, generationID, operationID, idempotencyKey, expectedRevision)
	if err != nil {
		return m8core.MemoryGeneration{}, 0, err
	}
	rev, err := s.canonicalRevision(ctx, subjectID)
	return gen, rev, err
}

func (s *MemoryService) SealMemoryArchive(ctx context.Context, subjectID, scopeKind, scopeID string, raw []byte) (string, string, error) {
	store, ok := s.uow.(memoryImportStore)
	if !ok {
		return "", "", ErrServiceUnavailable
	}
	return store.SealMemoryArchive(ctx, subjectID, scopeKind, scopeID, raw)
}

func (s *MemoryService) PreviewMemoryImport(ctx context.Context, subjectID, sourceArtifactID, operationID, idempotencyKey string) (m8core.MemoryImportPreview, error) {
	store, ok := s.uow.(memoryImportStore)
	if !ok {
		return m8core.MemoryImportPreview{}, ErrServiceUnavailable
	}
	return store.PreviewMemoryImport(ctx, subjectID, sourceArtifactID, operationID, idempotencyKey)
}

func (s *MemoryService) CommitMemoryImport(ctx context.Context, subjectID, previewID, archiveDigest, manifestDigest, operationID, idempotencyKey string, expectedRevision int64) (m8core.MemoryImportCommit, error) {
	store, ok := s.uow.(memoryImportStore)
	if !ok {
		return m8core.MemoryImportCommit{}, ErrServiceUnavailable
	}
	return store.CommitMemoryImport(ctx, subjectID, previewID, archiveDigest, manifestDigest, operationID, idempotencyKey, expectedRevision)
}

func (s *MemoryService) ExportCanonicalMemoryArchive(ctx context.Context, subjectID string) (m8core.MemoryFabricExport, error) {
	store, ok := s.uow.(memoryImportStore)
	if !ok {
		return m8core.MemoryFabricExport{}, ErrServiceUnavailable
	}
	return store.ExportCanonicalMemoryArchive(ctx, subjectID)
}

func (s *MemoryService) RecordCanonicalFeedback(ctx context.Context, subjectID, factID string, version int64, turnID, outcome string) error {
	store, ok := s.uow.(canonicalFeedbackStore)
	if !ok {
		return ErrServiceUnavailable
	}
	return store.RecordCanonicalFeedback(ctx, subjectID, factID, version, turnID, outcome)
}

func ExtractUTF8Span(text string, start, end int64) (string, error) {
	raw := []byte(text)
	if start < 0 || end < start || end > int64(len(raw)) {
		return "", ErrMemorySourceInvalid
	}
	if start > 0 && !utf8.RuneStart(raw[start]) {
		return "", ErrMemorySourceInvalid
	}
	if end < int64(len(raw)) && end > 0 && !utf8.RuneStart(raw[end]) {
		return "", ErrMemorySourceInvalid
	}
	out := strings.TrimSpace(string(raw[start:end]))
	if out == "" {
		return "", ErrMemorySourceInvalid
	}
	return out, nil
}
