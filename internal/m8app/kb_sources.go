package m8app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/oklog/ulid/v2"
)

type KBSource struct {
	SourceID          string            `json:"sourceId"`
	CollectionID      string            `json:"collectionId"`
	PathKey           string            `json:"-"`
	Path              string            `json:"path"`
	MediaType         string            `json:"mediaType"`
	SourceLocator     string            `json:"sourceLocator"`
	SHA256            string            `json:"sha256"`
	State             string            `json:"state"`
	Version           int64             `json:"version"`
	Revision          int64             `json:"revision"`
	CheckedAt         string            `json:"checkedAt"`
	Error             string            `json:"error"`
	CreatedAt         string            `json:"createdAt"`
	Versions          []KBSourceVersion `json:"versions"`
	NextBeforeVersion int64             `json:"nextBeforeVersion"`
}
type KBSourceVersion struct {
	SourceID  string `json:"-"`
	Version   int64  `json:"version"`
	SHA256    string `json:"sha256"`
	State     string `json:"state"`
	Error     string `json:"error"`
	CreatedAt string `json:"createdAt"`
}
type KBSourceDocument struct {
	SourceID        string
	SourceVersion   int64
	Ordinal         int
	DocumentID      string
	DocumentVersion int64
}

// Separate from KBTx so third-party in-memory repositories keep their original
// interface. Persistent local-file ingest requires this durable extension.
type KBSourceTx interface {
	GetKBSource(collectionID, pathKey string) (KBSource, bool, error)
	GetKBSourceForDocument(documentID string) (KBSource, bool, error)
	ListKBSources(collectionID, after string, limit int) ([]KBSource, error)
	GetKBSourceByID(collectionID, id string) (KBSource, bool, error)
	PutKBSource(source KBSource, expectedRevision int64) error
	PutKBSourceVersion(KBSourceVersion) error
	ListKBSourceVersions(sourceID string, before int64, limit int) ([]KBSourceVersion, error)
	PutKBSourceDocument(KBSourceDocument) error
	ListKBSourceDocuments(sourceID string, version int64) ([]KBSourceDocument, error)
	KBSourceDocumentCurrent(documentID string, version int64) (bool, error)
}
type KBLocalSourceInput struct {
	CollectionID, Path, MediaType, SourceLocator string
	ExpectedRevision                             *int64
}

type KBDeleteSourceInput struct {
	CollectionID, SourceID string
	ExpectedRevision       int64
}
type KBLocalSourceResult struct {
	CollectionID string
	Source       KBSource
	Documents    []KBUpsertResult
}

func sourcePath(path string) (string, string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if !filepath.IsAbs(path) || len(path) > m8core.MaxContentRef {
		return "", "", ErrPayloadInvalid
	}
	key := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		if len(filepath.VolumeName(path)) != 2 || path[1] != ':' || strings.Contains(path[2:], ":") {
			return "", "", fmt.Errorf("%w: only local disk files are supported", ErrPayloadInvalid)
		}
		key = strings.ToLower(key)
	}
	return path, key, nil
}
func SourceDigest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

// IngestLocalSource binds the actual bytes to a stable source identity. A
// generation is published with all its documents in one transaction; failed or
// interrupted generations never make an older subset appear current.
func (s *KBService) IngestLocalSource(ctx context.Context, in KBLocalSourceInput) (out KBLocalSourceResult, err error) {
	if s == nil || s.uow == nil {
		return out, ErrServiceUnavailable
	}
	out.CollectionID = in.CollectionID
	path, key, err := sourcePath(in.Path)
	if err != nil {
		return out, err
	}
	if in.ExpectedRevision != nil && *in.ExpectedRevision < 1 {
		return out, ErrPayloadInvalid
	}
	var previous KBSource
	var exists bool
	err = s.uow.TransactKB(ctx, func(tx KBTx) error {
		st, ok := tx.(KBSourceTx)
		if !ok {
			return ErrServiceUnavailable
		}
		// The caller already resolved its expert collection; ensure it belongs to
		// this service subject before recording any source or reading a path.
		owner, ok := tx.(interface {
			KBCollectionOwned(string, string) (bool, error)
		})
		if ok {
			owned, e := owner.KBCollectionOwned(in.CollectionID, s.subject)
			if e != nil {
				return e
			}
			if !owned {
				return ErrPayloadInvalid
			}
		}
		var e error
		previous, exists, e = st.GetKBSource(in.CollectionID, key)
		return e
	})
	if err != nil {
		return out, err
	}
	if in.ExpectedRevision != nil && (!exists || previous.Revision != *in.ExpectedRevision) {
		return out, ErrKBVersionConflict
	}
	raw, readErr := doctext.ReadSource(path)
	digest := ""
	if readErr == nil {
		digest = SourceDigest(raw)
	}
	if exists && previous.State == "fresh" && readErr == nil && digest == previous.SHA256 && (in.SourceLocator == "" || in.SourceLocator == previous.SourceLocator) && (in.MediaType == "" || in.MediaType == previous.MediaType) {
		out.Source = previous
		err = s.uow.TransactKB(ctx, func(tx KBTx) error {
			st := tx.(KBSourceTx)
			current, ok, e := st.GetKBSource(in.CollectionID, key)
			if e != nil {
				return e
			}
			if !ok || current.Revision != previous.Revision {
				return ErrKBVersionConflict
			}
			refs, e := st.ListKBSourceDocuments(previous.SourceID, previous.Version)
			if e != nil {
				return e
			}
			for _, ref := range refs {
				out.Documents = append(out.Documents, KBUpsertResult{DocumentID: ref.DocumentID, Version: ref.DocumentVersion, IndexState: m8core.KBIndexReady})
			}
			return nil
		})
		return out, err
	}
	now := s.clock.Now().UTC().Format(time.RFC3339Nano)
	source := previous
	if !exists {
		source = KBSource{SourceID: ulid.Make().String(), CollectionID: in.CollectionID, PathKey: key, Path: path, Version: 0, Revision: 0, CreatedAt: now, Versions: []KBSourceVersion{}}
	}
	source.Version++
	source.Revision++
	source.State = "refreshing"
	source.SHA256 = digest
	source.CheckedAt = now
	source.Error = ""
	if in.MediaType != "" {
		source.MediaType = in.MediaType
	}
	if source.MediaType == "" {
		source.MediaType = "application/octet-stream"
	}
	if in.SourceLocator != "" {
		source.SourceLocator = in.SourceLocator
	}
	if source.SourceLocator == "" {
		source.SourceLocator = path
	}
	if len(source.MediaType) > m8core.MaxMediaType || len(source.SourceLocator) > m8core.MaxSourceLocator {
		return out, ErrPayloadInvalid
	}
	err = s.uow.TransactKB(ctx, func(tx KBTx) error {
		st := tx.(KBSourceTx)
		if e := st.PutKBSource(source, previous.Revision); e != nil {
			return e
		}
		if previous.State == "refreshing" {
			return st.PutKBSourceVersion(KBSourceVersion{previous.SourceID, previous.Version, previous.SHA256, "failed", "刷新被后续请求取代或上次进程中断", now})
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	out.Source = source
	claim := source
	defer func() {
		if err == nil || errors.Is(err, ErrKBVersionConflict) {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		cleanupErr := s.uow.TransactKB(cleanupCtx, func(tx KBTx) error {
			st := tx.(KBSourceTx)
			current, ok, e := st.GetKBSource(claim.CollectionID, claim.PathKey)
			if e != nil {
				return e
			}
			if !ok || current.Revision != claim.Revision {
				return nil
			}
			current.State = "failed"
			current.Error = clipReason(err)
			current.Revision++
			if e = st.PutKBSource(current, claim.Revision); e != nil {
				return e
			}
			if e = st.PutKBSourceVersion(KBSourceVersion{claim.SourceID, claim.Version, claim.SHA256, "failed", current.Error, now}); e != nil {
				return e
			}
			out.Source = current
			return nil
		})
		if cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()
	var extracted doctext.Result
	parseErr := readErr
	if parseErr == nil {
		extracted, parseErr = doctext.ExtractContext(ctx, path, raw, source.MediaType)
	}
	if parseErr == nil {
		// Recheck the source after a potentially slow parser. The bytes indexed and
		// the published source digest must describe the same file generation.
		current, e := doctext.ReadSource(path)
		if e != nil {
			parseErr = e
		} else if SourceDigest(current) != digest {
			parseErr = fmt.Errorf("source changed during parsing")
		}
	}
	if ctx.Err() != nil {
		parseErr = ctx.Err()
	}
	var groups [][]string
	if parseErr == nil {
		parts := SplitSearchableParts(extracted.Media, extracted.Text)
		for len(parts) > 0 {
			n := min(len(parts), m8core.MaxKBChunksPerVersion)
			groups = append(groups, parts[:n])
			parts = parts[n:]
		}
		if len(groups) == 0 {
			parseErr = doctext.ErrNoTextLayer
		}
	}
	if parseErr != nil {
		groups = [][]string{nil}
	}
	commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var allChunks []m8core.KBChunk
	err = s.uow.TransactKB(commitCtx, func(tx KBTx) error {
		st := tx.(KBSourceTx)
		current, ok, e := st.GetKBSource(in.CollectionID, key)
		if e != nil {
			return e
		}
		if !ok || current.Revision != source.Revision {
			return ErrKBVersionConflict
		}
		old, e := st.ListKBSourceDocuments(source.SourceID, previous.Version)
		if e != nil {
			return e
		}
		source.State = "fresh"
		source.Error = ""
		if parseErr != nil {
			source.State = "failed"
			if errors.Is(parseErr, os.ErrNotExist) {
				source.State = "missing"
			}
			source.Error = clipReason(parseErr)
		}
		for ordinal, group := range groups {
			id := ulid.Make().String()
			version := int64(1)
			if ordinal < len(old) {
				id = old[ordinal].DocumentID
				latest, has, e := tx.GetKBLatestDocument(id)
				if e != nil {
					return e
				}
				if has {
					version = latest.Version + 1
				}
			}
			media := source.MediaType
			if extracted.Media != "" {
				media = extracted.Media
			}
			docDigest := digest
			if docDigest == "" {
				docDigest = SourceDigest(nil)
			}
			doc := m8core.KBDocument{DocumentID: id, CollectionID: in.CollectionID, Version: version, MediaType: media, ContentRef: path, SHA256: docDigest, SourceLocator: source.SourceLocator, IndexState: m8core.KBIndexReady, CreatedAt: now}
			result := KBUpsertResult{DocumentID: id, Version: version, IndexState: doc.IndexState}
			if parseErr != nil {
				doc.IndexState = m8core.KBIndexFailed
				result.IndexState = doc.IndexState
				result.FailReason = source.Error
			}
			if e = tx.PutKBDocument(doc); e != nil {
				return e
			}
			if parseErr == nil {
				chunks, e := ChunksFromParts(doc, group)
				if e != nil {
					return e
				}
				projection, e := m8core.BuildChunkProjectionFromChunks(doc, chunks)
				if e != nil {
					return e
				}
				chunks = projection.Chunks
				if e = tx.PutKBChunks(chunks); e != nil {
					return e
				}
				allChunks = append(allChunks, chunks...)
				result.Preview = previewBodies(chunks)
			}
			if e = st.PutKBSourceDocument(KBSourceDocument{source.SourceID, source.Version, ordinal, id, version}); e != nil {
				return e
			}
			out.Documents = append(out.Documents, result)
		}
		state := "fresh"
		if parseErr != nil {
			state = "failed"
		}
		if e = st.PutKBSourceVersion(KBSourceVersion{source.SourceID, source.Version, digest, state, source.Error, now}); e != nil {
			return e
		}
		source.Revision++
		source.CheckedAt = s.clock.Now().UTC().Format(time.RFC3339Nano)
		if e = st.PutKBSource(source, current.Revision); e != nil {
			return e
		}
		_, e = tx.AppendAuditEvent(audit.Event{ID: ulid.Make().String(), Action: "kb.source." + state, ResourceType: "kb_source", ResourceID: source.SourceID, Actor: "local", AfterDigest: SourceDigest([]byte(source.SourceID + digest + state)), CreatedAt: now})
		return e
	})
	if err != nil {
		out.Documents = nil
		return out, err
	}
	out.Source = source
	if parseErr != nil {
		return out, fmt.Errorf("%w: %v", ErrKBIndexFailed, parseErr)
	}
	s.embedChunksAfterCommit(ctx, allChunks)
	return out, nil
}

type kbSourceCheckKey struct{}

func withSourceCheckCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, kbSourceCheckKey{}, map[string]KBSource{})
}
func (s *KBService) checkSource(ctx context.Context, source KBSource) (KBSource, error) {
	if source.State != "fresh" {
		return source, nil
	}
	cache, _ := ctx.Value(kbSourceCheckKey{}).(map[string]KBSource)
	cacheKey := fmt.Sprintf("%s/%d", source.SourceID, source.Revision)
	if checked, ok := cache[cacheKey]; ok {
		return checked, nil
	}
	if err := ctx.Err(); err != nil {
		return source, err
	}
	raw, err := doctext.ReadSource(source.Path)
	state, reason := "fresh", ""
	if err != nil {
		state = "failed"
		reason = clipReason(err)
		if errors.Is(err, os.ErrNotExist) {
			state = "missing"
		}
	} else if SourceDigest(raw) != source.SHA256 {
		state = "stale"
		reason = "原文件已变更，请刷新索引"
	}
	err = s.uow.TransactKB(ctx, func(tx KBTx) error {
		source.State = state
		source.Error = reason
		source.CheckedAt = s.clock.Now().UTC().Format(time.RFC3339Nano)
		rev := source.Revision
		if state != "fresh" {
			source.Revision++
		}
		return tx.(KBSourceTx).PutKBSource(source, rev)
	})
	if err == nil && cache != nil {
		cache[cacheKey] = source
	}
	return source, err
}

func (s *KBService) DeleteLocalSource(ctx context.Context, in KBDeleteSourceInput) error {
	if s == nil || s.uow == nil || in.CollectionID == "" || in.SourceID == "" {
		return ErrPayloadInvalid
	}
	return s.uow.TransactKB(ctx, func(tx KBTx) error {
		st, ok := tx.(KBSourceTx)
		if !ok {
			return ErrServiceUnavailable
		}
		if owner, ok := tx.(interface {
			KBCollectionOwned(string, string) (bool, error)
		}); ok {
			owned, e := owner.KBCollectionOwned(in.CollectionID, s.subject)
			if e != nil {
				return e
			}
			if !owned {
				return ErrPayloadInvalid
			}
		}
		source, exists, err := st.GetKBSourceByID(in.CollectionID, in.SourceID)
		if err != nil {
			return err
		}
		if !exists {
			return ErrPayloadInvalid
		}
		if in.ExpectedRevision != 0 && source.Revision != in.ExpectedRevision {
			return ErrKBVersionConflict
		}
		rev := source.Revision
		source.State = "failed"
		source.Error = "tombstone:deleted"
		source.Revision++
		now := source.CheckedAt
		if s.clock != nil {
			now = s.clock.Now().UTC().Format(time.RFC3339Nano)
		}
		source.CheckedAt = now
		if err := st.PutKBSource(source, rev); err != nil {
			return err
		}
		_, err = tx.AppendAuditEvent(audit.Event{ID: ulid.Make().String(), Action: "kb.source.tombstone", ResourceType: "kb_source", ResourceID: source.SourceID, Actor: "local", AfterDigest: SourceDigest([]byte(source.SourceID + "tombstone")), CreatedAt: now})
		return err
	})
}

func (s *KBService) sourceDocumentUsable(ctx context.Context, doc m8core.KBDocument) (bool, error) {
	var source KBSource
	var tracked bool
	err := s.uow.TransactKB(ctx, func(tx KBTx) error {
		st, ok := tx.(KBSourceTx)
		if !ok {
			return nil
		}
		var e error
		source, tracked, e = st.GetKBSourceForDocument(doc.DocumentID)
		return e
	})
	if err != nil {
		return false, err
	}
	if !tracked {
		// New legacy callers using local UpsertDocument are still checked against
		// actual bytes even before a source refresh establishes grouped identity.
		if filepath.IsAbs(doc.ContentRef) {
			raw, e := doctext.ReadSource(doc.ContentRef)
			if e != nil || SourceDigest(raw) != doc.SHA256 {
				return false, nil
			}
		}
		var current bool
		err = s.uow.TransactKB(ctx, func(tx KBTx) error {
			latest, ok, e := tx.GetKBLatestDocument(doc.DocumentID)
			current = ok && latest.Version == doc.Version && latest.IndexState == m8core.KBIndexReady
			return e
		})
		return current, err
	}
	source, err = s.checkSource(ctx, source)
	if err != nil {
		return false, err
	}
	if source.State != "fresh" {
		return false, nil
	}
	var current bool
	err = s.uow.TransactKB(ctx, func(tx KBTx) error {
		var e error
		current, e = tx.(KBSourceTx).KBSourceDocumentCurrent(doc.DocumentID, doc.Version)
		return e
	})
	return current, err
}

// KBSourcePage bounds source verification to four local files (128 MiB at the
// parser input limit). History is independently navigable in 50-version pages.
type KBSourcePage struct {
	SourcesAfter, HistorySourceID string
	HistoryBeforeVersion          int64
}

func (s *KBService) sourcesForCollection(ctx context.Context, collectionID string, page KBSourcePage) ([]KBSource, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sources := []KBSource{}
	next := ""
	readPage := func(st KBSourceTx) error {
		if page.HistorySourceID != "" {
			source, ok, e := st.GetKBSourceByID(collectionID, page.HistorySourceID)
			if e != nil {
				return e
			}
			if !ok {
				return ErrPayloadInvalid
			}
			sources = []KBSource{source}
			return nil
		}
		var e error
		sources, e = st.ListKBSources(collectionID, page.SourcesAfter, 5)
		if e != nil {
			return e
		}
		if len(sources) > 4 {
			sources = sources[:4]
			next = sources[3].SourceID
		}
		return nil
	}
	err := s.uow.TransactKB(ctx, func(tx KBTx) error {
		st, ok := tx.(KBSourceTx)
		if !ok {
			return nil
		}
		return readPage(st)
	})
	if err != nil {
		return nil, "", err
	}
	for _, source := range sources {
		if _, e := s.checkSource(ctx, source); e != nil && !errors.Is(e, ErrKBVersionConflict) {
			return nil, "", e
		}
	}
	err = s.uow.TransactKB(ctx, func(tx KBTx) error {
		st, ok := tx.(KBSourceTx)
		if !ok {
			return nil
		}
		if e := readPage(st); e != nil {
			return e
		}
		for i := range sources {
			before := int64(0)
			if page.HistorySourceID == sources[i].SourceID {
				before = page.HistoryBeforeVersion
			}
			versions, e := st.ListKBSourceVersions(sources[i].SourceID, before, 51)
			if e != nil {
				return e
			}
			if len(versions) > 50 {
				versions = versions[:50]
				sources[i].NextBeforeVersion = versions[49].Version
			}
			sources[i].Versions = versions
		}
		return nil
	})
	return sources, next, err
}
