// M8 slice-2 application service (T-8.2.x): kb.upsertDocument.
//
// The versioned upsert enacts the M8-011/012 contract: optimistic reindex
// with expectedVersion CAS (stale -> KB_VERSION_CONFLICT 409, must create a
// new version), idempotent identical-sha256 resubmission answering the
// original version, and failed index passes parking at index_state='failed'
// with zero chunk projection (KB_INDEX_FAILED, no searchable surface).
package m8app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// M8 slice-2 error family (04 错误矩阵 M8-011/012).
var (
	// ErrKBVersionConflict: stale expectedVersion on reindex (M8-011, 409).
	ErrKBVersionConflict = errors.New("m8app: kb version conflict")
	// ErrKBIndexFailed: the index projection failed; no searchable
	// surface is published (M8-012, 500).
	ErrKBIndexFailed = errors.New("m8app: kb index failed")
	// ErrKBCollectionNotFound: the target collection does not exist.
	ErrKBCollectionNotFound = errors.New("m8app: kb collection not found")
)

// KBCollection is one kb_collections row (local single-owner policy in the
// single-user engine).
type KBCollection struct {
	CollectionID string
	SubjectID    string
	ScopeID      string
	AuthPolicy   string
	CreatedAt    string
}

// KBTx is the slice-2 single-writer transaction.
type KBTx interface {
	PutKBCollectionIfAbsent(KBCollection) error
	PutKBDocument(m8core.KBDocument) error
	GetKBLatestDocument(documentID string) (m8core.KBDocument, bool, error)
	PutKBChunks([]m8core.KBChunk) error
	AppendAuditEvent(audit.Event) (audit.Event, error)
}

// KBUnitOfWork is the slice-2 single-writer boundary.
type KBUnitOfWork interface {
	TransactKB(ctx context.Context, fn func(KBTx) error) error
}

// KBIndexer is the synchronous index projector. It answers the chunk IDs of
// the new version or an error; an error fails the whole version M8-012
// (failed row, no projection). The default derives one chunk per call.
type KBIndexer func(ctx context.Context, doc m8core.KBDocument) ([]string, error)

// DefaultKBIndexer projects a single-chunk document.
func DefaultKBIndexer(ctx context.Context, doc m8core.KBDocument) ([]string, error) {
	return []string{ulid.Make().String()}, nil
}

// KBService implements the slice-2 use cases.
type KBService struct {
	uow     KBUnitOfWork
	clock   Clock
	subject string
	indexer KBIndexer
}

// NewKBService wires the slice-2 service.
func NewKBService(uow KBUnitOfWork, localSubject string) *KBService {
	return &KBService{uow: uow, clock: systemClock{}, subject: localSubject, indexer: DefaultKBIndexer}
}

// SetClock substitutes the clock (tests).
func (s *KBService) SetClock(c Clock) { s.clock = c }

// SetIndexer substitutes the index projector (M8-012 failure tests).
func (s *KBService) SetIndexer(f KBIndexer) { s.indexer = f }

// KBUpsertInput is the kb.upsertDocument command.
type KBUpsertInput struct {
	CollectionID    string
	DocumentID      string
	ExpectedVersion int64 // 0 -> absent (first insert or strict recheck)
	MediaType       string
	ContentRef      string
	SHA256          string
	SourceLocator   string
	RequestID       string
	Actor           string
}

// KBUpsertResult is the kb.upsertDocument outcome.
type KBUpsertResult struct {
	DocumentID string `json:"documentId"`
	Version    int64  `json:"version"`
	IndexState string `json:"indexState"`
}

// EnsureCollection bootstraps the collection row (idempotent).
func (s *KBService) EnsureCollection(ctx context.Context, collectionID, scopeID string) error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	return s.uow.TransactKB(ctx, func(tx KBTx) error {
		return tx.PutKBCollectionIfAbsent(KBCollection{
			CollectionID: collectionID,
			SubjectID:    s.subject,
			ScopeID:      scopeID,
			AuthPolicy:   "local-owner",
			CreatedAt:    s.clock.Now().UTC().Format(time.RFC3339),
		})
	})
}

// UpsertDocument enacts the versioned upsert with CAS, sha256 idempotency
// and the synchronous fail-closed index projection. Old versions stay
// recallable with their version labels; only the latest ready row is the
// searchable projection.
func (s *KBService) UpsertDocument(ctx context.Context, in KBUpsertInput) (KBUpsertResult, error) {
	if s == nil || s.uow == nil {
		return KBUpsertResult{}, ErrServiceUnavailable
	}
	if len(in.DocumentID) != 26 || len(in.CollectionID) != 26 ||
		len(in.MediaType) < 1 || len(in.MediaType) > m8core.MaxMediaType ||
		len(in.ContentRef) < 1 || len(in.ContentRef) > m8core.MaxContentRef ||
		!m8core.ValidHexDigest(in.SHA256) ||
		len(in.SourceLocator) < 1 || len(in.SourceLocator) > m8core.MaxSourceLocator {
		return KBUpsertResult{}, fmt.Errorf("%w: kb fields invalid", ErrPayloadInvalid)
	}
	if in.ExpectedVersion < 0 {
		return KBUpsertResult{}, fmt.Errorf("%w: expectedVersion negative", ErrPayloadInvalid)
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	var out KBUpsertResult
	err := s.uow.TransactKB(ctx, func(tx KBTx) error {
		latest, has, err := tx.GetKBLatestDocument(in.DocumentID)
		if err != nil {
			return err
		}
		if has && latest.CollectionID != in.CollectionID {
			return fmt.Errorf("%w: document collection mismatch", ErrKBVersionConflict)
		}
		next := int64(1)
		if has {
			idem, gerr := m8core.KBVersionGuard(latest.Version, in.ExpectedVersion, has, in.SHA256, latest.SHA256)
			if gerr != nil {
				return fmt.Errorf("%w: %v", ErrKBVersionConflict, gerr)
			}
			if idem {
				// Same sha256 resubmission answers the original version.
				out = KBUpsertResult{DocumentID: latest.DocumentID, Version: latest.Version, IndexState: latest.IndexState}
				return nil
			}
			next = latest.Version + 1
		}
		doc := m8core.KBDocument{
			DocumentID:    in.DocumentID,
			CollectionID:  in.CollectionID,
			Version:       next,
			MediaType:     in.MediaType,
			ContentRef:    in.ContentRef,
			SHA256:        in.SHA256,
			SourceLocator: in.SourceLocator,
			IndexState:    m8core.KBIndexPending,
			CreatedAt:     now,
		}
		if err := tx.PutKBDocument(doc); err != nil {
			return err
		}
		// Synchronous index projection: any failure parks the version at
		// failed with zero chunks (M8-012) and answers the error.
		ids, ierr := s.indexer(ctx, doc)
		if ierr == nil {
			proj, perr := m8core.BuildChunkProjection(doc, ids)
			if perr != nil {
				ierr = perr
			} else if err := tx.PutKBChunks(proj.Chunks); err != nil {
				ierr = err
			}
		}
		if ierr != nil {
			failed := doc
			failed.IndexState = m8core.KBIndexFailed
			if err := tx.PutKBDocument(failed); err != nil {
				return err
			}
			if _, err := tx.AppendAuditEvent(audit.Event{
				ID: ulid.Make().String(), Action: "kb.document.index_failed",
				ResourceType: "kb_document", ResourceID: in.DocumentID,
				Actor: actorOr(in.Actor), AfterDigest: in.SHA256,
				CorrelationID: in.RequestID, CreatedAt: now,
			}); err != nil {
				return err
			}
			out = KBUpsertResult{DocumentID: in.DocumentID, Version: next, IndexState: m8core.KBIndexFailed}
			return fmt.Errorf("%w: %v", ErrKBIndexFailed, ierr)
		}
		ready := doc
		ready.IndexState = m8core.KBIndexReady
		if err := tx.PutKBDocument(ready); err != nil {
			return err
		}
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "kb.document.upsert",
			ResourceType: "kb_document", ResourceID: in.DocumentID,
			Actor: actorOr(in.Actor), AfterDigest: in.SHA256,
			CorrelationID: in.RequestID, CreatedAt: now,
		}); err != nil {
			return err
		}
		out = KBUpsertResult{DocumentID: in.DocumentID, Version: next, IndexState: m8core.KBIndexReady}
		return nil
	})
	if err != nil {
		return out, err
	}
	return out, nil
}
