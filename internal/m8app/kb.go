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
	"strings"
	"time"
	"unicode/utf8"

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
	// ErrKBDocumentNotReady: a referenced document is missing or not yet
	// indexed to a searchable (ready) surface. It guards downstream binders
	// (mro.manual.register) from linking dangling or unindexed ids.
	ErrKBDocumentNotReady = errors.New("m8app: kb document not ready")
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
	GetKBCollectionByScope(scopeID string) (KBCollection, bool, error)
	PutKBDocument(m8core.KBDocument) error
	GetKBLatestDocument(documentID string) (m8core.KBDocument, bool, error)
	ListKBDocumentsByCollection(collectionID string) ([]m8core.KBDocument, error)
	PutKBChunks([]m8core.KBChunk) error
	GetKBChunk(chunkID string) (m8core.KBChunk, error)
	SearchKBChunkFTS(scopeID, query string, limit int) ([]KBSearchHit, error)
	CountKBStats(collectionID string) (docs, ready, chunks int, err error)
	GetGrowthPath(expertID string) (GrowthPath, bool, error)
	PutGrowthPath(GrowthPath) error
	AppendAuditEvent(audit.Event) (audit.Event, error)
	ListAuditEvents() ([]audit.Event, error)
}

// KBUnitOfWork is the slice-2 single-writer boundary.
type KBUnitOfWork interface {
	TransactKB(ctx context.Context, fn func(KBTx) error) error
}

// KBIndexer is the synchronous index projector. It answers the chunk IDs of
// the new version or an error; an error fails the whole version M8-012
// (failed row, no projection). The default derives one chunk per call.
type KBIndexer func(ctx context.Context, doc m8core.KBDocument) ([]string, error)

// KBChunkProjector returns chunks that already carry body and locator.
// Expert/MRO ingest sets this; the global default stays ID-only.
type KBChunkProjector func(ctx context.Context, doc m8core.KBDocument) ([]m8core.KBChunk, error)

// DefaultKBIndexer projects a single-chunk document.
func DefaultKBIndexer(ctx context.Context, doc m8core.KBDocument) ([]string, error) {
	return []string{ulid.Make().String()}, nil
}

// KBService implements the slice-2 use cases.
type KBService struct {
	uow       KBUnitOfWork
	clock     Clock
	subject   string
	indexer   KBIndexer
	projector KBChunkProjector
}

// NewKBService wires the slice-2 service.
func NewKBService(uow KBUnitOfWork, localSubject string) *KBService {
	return &KBService{uow: uow, clock: systemClock{}, subject: localSubject, indexer: DefaultKBIndexer}
}

// SetClock substitutes the clock (tests).
func (s *KBService) SetClock(c Clock) { s.clock = c }

// SetIndexer substitutes the index projector (M8-012 failure tests).
func (s *KBService) SetIndexer(f KBIndexer) { s.indexer = f }

// SetChunkProjector enables the body-carrying projection path.
func (s *KBService) SetChunkProjector(f KBChunkProjector) { s.projector = f }

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
	Projector       KBChunkProjector // per-call override; nil uses service projector
}

// KBUpsertResult is the kb.upsertDocument outcome.
type KBUpsertResult struct {
	DocumentID string   `json:"documentId"`
	Version    int64    `json:"version"`
	IndexState string   `json:"indexState"`
	Preview    []string `json:"preview,omitempty"`
	FailReason string   `json:"failReason,omitempty"`
}

// AuditRow is one read-only m7 ledger event shown on the workbench.
type AuditRow struct {
	ID           string `json:"id"`
	Action       string `json:"action"`
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
	CreatedAt    string `json:"createdAt"`
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
		var ierr error
		var preview []string
		projector := s.projector
		if in.Projector != nil {
			projector = in.Projector
		}
		if projector != nil {
			chunks, perr := projector(ctx, doc)
			if perr != nil {
				ierr = perr
			} else {
				proj, perr := m8core.BuildChunkProjectionFromChunks(doc, chunks)
				if perr != nil {
					ierr = perr
				} else if err := tx.PutKBChunks(proj.Chunks); err != nil {
					ierr = err
				} else {
					preview = previewBodies(proj.Chunks)
				}
			}
		} else {
			ids, jerr := s.indexer(ctx, doc)
			if jerr != nil {
				ierr = jerr
			} else {
				proj, perr := m8core.BuildChunkProjection(doc, ids)
				if perr != nil {
					ierr = perr
				} else if err := tx.PutKBChunks(proj.Chunks); err != nil {
					ierr = err
				}
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
			out = KBUpsertResult{DocumentID: in.DocumentID, Version: next, IndexState: m8core.KBIndexFailed, FailReason: clipReason(ierr)}
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
		out = KBUpsertResult{DocumentID: in.DocumentID, Version: next, IndexState: m8core.KBIndexReady, Preview: preview}
		return nil
	})
	if err != nil {
		return out, err
	}
	return out, nil
}

// DocumentsReady verifies every id resolves to a ready (searchable) kb
// document. Any missing, malformed, or index-failed id fails the whole set so
// callers never bind a dangling or unindexed reference.
func (s *KBService) DocumentsReady(ctx context.Context, ids []string) error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	return s.uow.TransactKB(ctx, func(tx KBTx) error {
		for _, raw := range ids {
			id := strings.TrimSpace(raw)
			if len(id) != 26 {
				return fmt.Errorf("%w: %q", ErrKBDocumentNotReady, raw)
			}
			doc, ok, err := tx.GetKBLatestDocument(id)
			if err != nil {
				return err
			}
			if !ok || doc.IndexState != m8core.KBIndexReady {
				return fmt.Errorf("%w: %s", ErrKBDocumentNotReady, id)
			}
		}
		return nil
	})
}

func previewBodies(chunks []m8core.KBChunk) []string {
	out := []string{}
	for _, c := range chunks {
		body := strings.TrimSpace(c.Body)
		if body == "" {
			continue
		}
		if utf8.RuneCountInString(body) > 240 {
			body = string([]rune(body)[:240])
		}
		out = append(out, body)
		if len(out) >= 3 {
			break
		}
	}
	return out
}

func clipReason(err error) string {
	if err == nil {
		return ""
	}
	s := strings.TrimSpace(err.Error())
	if utf8.RuneCountInString(s) > 200 {
		s = string([]rune(s)[:200])
	}
	return s
}

// ListRecentKBAudit returns recent kb/mro ledger rows for workbench replay.
func (s *KBService) ListRecentKBAudit(ctx context.Context, limit int) ([]AuditRow, error) {
	if s == nil || s.uow == nil {
		return nil, ErrServiceUnavailable
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	var rows []AuditRow
	err := s.uow.TransactKB(ctx, func(tx KBTx) error {
		events, err := tx.ListAuditEvents()
		if err != nil {
			return err
		}
		filtered := make([]AuditRow, 0, len(events))
		for _, ev := range events {
			if !isWorkbenchAudit(ev) {
				continue
			}
			filtered = append(filtered, AuditRow{
				ID: ev.ID, Action: ev.Action, ResourceType: ev.ResourceType,
				ResourceID: ev.ResourceID, CreatedAt: ev.CreatedAt,
			})
		}
		if n := len(filtered); n > limit {
			filtered = filtered[n-limit:]
		}
		rows = filtered
		return nil
	})
	if rows == nil {
		rows = []AuditRow{}
	}
	return rows, err
}

func isWorkbenchAudit(ev audit.Event) bool {
	return strings.HasPrefix(ev.Action, "kb.") || ev.ResourceType == "kb_document" || strings.HasPrefix(ev.Action, "mro.")
}
