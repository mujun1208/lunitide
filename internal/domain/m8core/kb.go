// M8 slice-2 domain (T-8.2.x): the versioned knowledge-base document core.
// kb_documents are optimistic-reindex versioned rows (document_id, version):
// a stale expectedVersion answers KB_VERSION_CONFLICT (M8-011, 409 - a new
// version must be created), a repeated identical sha256 is idempotent and
// answers the original version, and a failed index pass parks the row at
// index_state='failed' with no searchable chunk projection (M8-012).
package m8core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// KB document index states (migration 0062 CHECK).
const (
	KBIndexPending  = "pending"
	KBIndexIndexing = "indexing"
	KBIndexReady    = "ready"
	KBIndexFailed   = "failed"
)

// Field limits mirroring migration 0062 CHECKs.
const (
	MaxMediaType     = 128
	MaxContentRef    = 512
	MaxSourceLocator = 1024
	// MaxKBChunksPerVersion caps the synchronous chunk projection of one
	// document version (a projection larger than this fails M8-012).
	MaxKBChunksPerVersion = 512
	// MaxKBChunkBody is the searchable body cap of one chunk.
	MaxKBChunkBody = 16384
)

// KBDocument is one immutable (document_id, version) row.
type KBDocument struct {
	DocumentID    string
	CollectionID  string
	Version       int64
	MediaType     string
	ContentRef    string
	SHA256        string
	SourceLocator string
	IndexState    string
	CreatedAt     string
}

// KBChunk is one searchable projection row of a document version.
type KBChunk struct {
	ChunkID         string
	DocumentID      string
	DocumentVersion int64
	Ordinal         int64
	ContentDigest   string
	LocatorJSON     string
	Body            string
	CreatedAt       string
	Embedding       []byte
}

// KBVersionGuard enacts the M8-011 optimistic-reindex rule: when the document
// exists, expectedVersion must equal the current version (or be omitted for
// a first insert); a stale or missing expectation refuses the reindex and
// demands a fresh version. An identical sha256 resubmission is idempotent.
func KBVersionGuard(current int64, expected int64, hasCurrent bool, sha string, currentSHA string) (idempotent bool, err error) {
	if hasCurrent && sha == currentSHA && sha != "" {
		return true, nil
	}
	if hasCurrent && expected != current {
		return false, fmt.Errorf("m8core: kb expectedVersion %d != current %d", expected, current)
	}
	return false, nil
}

// ChunkProjection is the synchronous index projection of one document
// version: fixed-size locator rows whose digests chain the source bytes.
// The engine injects the real indexer; a projection error fails M8-012.
type ChunkProjection struct {
	Chunks []KBChunk
}

// BuildChunkProjection derives the deterministic chunk set of one document
// version from its canonical locator document. Chunks are content-addressed
// (sha256 of document+ordinal+locator) so identical reindexes agree.
func BuildChunkProjection(doc KBDocument, chunkIDs []string) (*ChunkProjection, error) {
	if len(chunkIDs) < 1 || len(chunkIDs) > MaxKBChunksPerVersion {
		return nil, fmt.Errorf("m8core: chunk count %d out of range", len(chunkIDs))
	}
	out := &ChunkProjection{Chunks: make([]KBChunk, len(chunkIDs))}
	for i, id := range chunkIDs {
		locator := locatorDoc{DocumentID: doc.DocumentID, Version: doc.Version, Ordinal: int64(i)}
		lb, err := json.Marshal(locator)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256([]byte(doc.SHA256 + "|" + id))
		out.Chunks[i] = KBChunk{
			ChunkID:         id,
			DocumentID:      doc.DocumentID,
			DocumentVersion: doc.Version,
			Ordinal:         int64(i),
			ContentDigest:   hex.EncodeToString(sum[:]),
			LocatorJSON:     string(lb),
			CreatedAt:       doc.CreatedAt,
		}
	}
	return out, nil
}

type locatorDoc struct {
	DocumentID string `json:"documentId"`
	Version    int64  `json:"version"`
	Ordinal    int64  `json:"ordinal"`
}

// BuildChunkProjectionFromChunks keeps caller-supplied body and locator.
// Empty locator falls back to {documentId,version,ordinal}. Empty body or
// invalid locator JSON fails the projection (M8-012).
func BuildChunkProjectionFromChunks(doc KBDocument, chunks []KBChunk) (*ChunkProjection, error) {
	if len(chunks) < 1 || len(chunks) > MaxKBChunksPerVersion {
		return nil, fmt.Errorf("m8core: chunk count %d out of range", len(chunks))
	}
	out := &ChunkProjection{Chunks: make([]KBChunk, len(chunks))}
	for i, in := range chunks {
		body := strings.TrimSpace(in.Body)
		if body == "" || len(in.Body) > MaxKBChunkBody {
			return nil, fmt.Errorf("m8core: chunk %d body invalid", i)
		}
		locator := strings.TrimSpace(in.LocatorJSON)
		if locator == "" {
			lb, err := json.Marshal(locatorDoc{DocumentID: doc.DocumentID, Version: doc.Version, Ordinal: int64(i)})
			if err != nil {
				return nil, err
			}
			locator = string(lb)
		} else if !json.Valid([]byte(locator)) {
			return nil, fmt.Errorf("m8core: chunk %d locator is not JSON", i)
		}
		id := strings.TrimSpace(in.ChunkID)
		if len(id) != 26 {
			return nil, fmt.Errorf("m8core: chunk %d id invalid", i)
		}
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", doc.SHA256, body, i)))
		out.Chunks[i] = KBChunk{
			ChunkID:         id,
			DocumentID:      doc.DocumentID,
			DocumentVersion: doc.Version,
			Ordinal:         int64(i),
			ContentDigest:   hex.EncodeToString(sum[:]),
			LocatorJSON:     locator,
			Body:            in.Body,
			CreatedAt:       doc.CreatedAt,
		}
	}
	return out, nil
}
