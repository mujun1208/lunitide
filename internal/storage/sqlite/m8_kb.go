// M8 slice-2 storage (T-8.2.x): kb_collections / kb_documents / kb_chunks
// on the agent-runtime single-writer transaction.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

// TransactKB runs an m8app slice-2 use case on the shared single-writer
// transaction.
func (r *AgentRuntimeRepository) TransactKB(ctx context.Context, fn func(m8app.KBTx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		ktx, ok := tx.(m8app.KBTx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m8app.KBTx")
		}
		return fn(ktx)
	})
}

const m8kbdocColumns = `document_id,collection_id,version,media_type,content_ref,sha256,source_locator,index_state,created_at`

func scanKBDocument(s interface{ Scan(...any) error }) (m8core.KBDocument, error) {
	var d m8core.KBDocument
	err := s.Scan(&d.DocumentID, &d.CollectionID, &d.Version, &d.MediaType,
		&d.ContentRef, &d.SHA256, &d.SourceLocator, &d.IndexState, &d.CreatedAt)
	return d, err
}

// PutKBCollectionIfAbsent inserts the collection row once (idempotent
// bootstrap).
func (t *agentRuntimeTx) PutKBCollectionIfAbsent(c m8app.KBCollection) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO kb_collections
		(collection_id,subject_id,scope_id,auth_policy,created_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(collection_id) DO NOTHING`,
		c.CollectionID, c.SubjectID, c.ScopeID, c.AuthPolicy, c.CreatedAt)
	return t.fail(err)
}

// PutKBDocument upserts one (document_id, version) row: the pending insert
// and the terminal index_state update land on the same primary key.
func (t *agentRuntimeTx) PutKBDocument(d m8core.KBDocument) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO kb_documents
		(document_id,collection_id,version,media_type,content_ref,sha256,source_locator,index_state,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(document_id, version) DO UPDATE SET index_state=excluded.index_state`,
		d.DocumentID, d.CollectionID, d.Version, d.MediaType, d.ContentRef,
		d.SHA256, d.SourceLocator, d.IndexState, d.CreatedAt)
	return t.fail(err)
}

// GetKBLatestDocument answers the highest version row of one document.
func (t *agentRuntimeTx) GetKBLatestDocument(documentID string) (m8core.KBDocument, bool, error) {
	row := t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8kbdocColumns+` FROM kb_documents WHERE document_id=? ORDER BY version DESC LIMIT 1`, documentID)
	d, err := scanKBDocument(row)
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.KBDocument{}, false, nil
	}
	if err != nil {
		return m8core.KBDocument{}, false, t.fail(err)
	}
	return d, true, nil
}

// PutKBChunks writes the chunk projection rows of one version.
func (t *agentRuntimeTx) PutKBChunks(chunks []m8core.KBChunk) error {
	for _, c := range chunks {
		var emb any
		if len(c.Embedding) > 0 {
			emb = c.Embedding
		}
		if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO kb_chunks
			(chunk_id,document_id,document_version,ordinal,content_digest,locator_json,embedding,created_at,body)
			VALUES(?,?,?,?,?,?,?,?,?)`,
			c.ChunkID, c.DocumentID, c.DocumentVersion, c.Ordinal,
			c.ContentDigest, c.LocatorJSON, emb, c.CreatedAt, c.Body); err != nil {
			return t.fail(err)
		}
	}
	return nil
}

func (t *agentRuntimeTx) UpdateKBChunkEmbedding(chunkID string, blob []byte) error {
	if strings.TrimSpace(chunkID) == "" || len(blob) == 0 {
		return nil
	}
	_, err := t.tx.ExecContext(t.ctx, `UPDATE kb_chunks SET embedding=? WHERE chunk_id=?`, blob, chunkID)
	return t.fail(err)
}

func (t *agentRuntimeTx) ListKBChunkEmbeddings(scopeID string) ([]m8app.KBChunkEmbedding, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT c.chunk_id,c.document_id,c.document_version,c.ordinal,c.content_digest,c.locator_json,c.created_at,c.body,c.embedding,`+
		`d.document_id,d.collection_id,d.version,d.media_type,d.content_ref,d.sha256,d.source_locator,d.index_state,d.created_at
		FROM kb_chunks c
		JOIN kb_documents d ON d.document_id=c.document_id AND d.version=c.document_version
		JOIN kb_collections col ON col.collection_id=d.collection_id
		WHERE col.scope_id=? AND d.index_state='ready' AND c.embedding IS NOT NULL`, scopeID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m8app.KBChunkEmbedding
	for rows.Next() {
		var row m8app.KBChunkEmbedding
		if err := rows.Scan(
			&row.Chunk.ChunkID, &row.Chunk.DocumentID, &row.Chunk.DocumentVersion, &row.Chunk.Ordinal,
			&row.Chunk.ContentDigest, &row.Chunk.LocatorJSON, &row.Chunk.CreatedAt, &row.Chunk.Body, &row.Chunk.Embedding,
			&row.Document.DocumentID, &row.Document.CollectionID, &row.Document.Version, &row.Document.MediaType,
			&row.Document.ContentRef, &row.Document.SHA256, &row.Document.SourceLocator, &row.Document.IndexState,
			&row.Document.CreatedAt,
		); err != nil {
			return nil, t.fail(err)
		}
		out = append(out, row)
	}
	return out, t.fail(rows.Err())
}

func (t *agentRuntimeTx) GetKBChunk(chunkID string) (m8core.KBChunk, error) {
	row := t.tx.QueryRowContext(t.ctx, `SELECT chunk_id,document_id,document_version,ordinal,content_digest,locator_json,created_at,body
		FROM kb_chunks WHERE chunk_id=?`, chunkID)
	var c m8core.KBChunk
	err := row.Scan(&c.ChunkID, &c.DocumentID, &c.DocumentVersion, &c.Ordinal,
		&c.ContentDigest, &c.LocatorJSON, &c.CreatedAt, &c.Body)
	if err != nil {
		return m8core.KBChunk{}, t.fail(err)
	}
	return c, nil
}

func (t *agentRuntimeTx) GetKBCollectionByScope(scopeID string) (m8app.KBCollection, bool, error) {
	row := t.tx.QueryRowContext(t.ctx, `SELECT collection_id,subject_id,scope_id,auth_policy,created_at
		FROM kb_collections WHERE scope_id=?`, scopeID)
	var c m8app.KBCollection
	err := row.Scan(&c.CollectionID, &c.SubjectID, &c.ScopeID, &c.AuthPolicy, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return m8app.KBCollection{}, false, nil
	}
	if err != nil {
		return m8app.KBCollection{}, false, t.fail(err)
	}
	return c, true, nil
}

func (t *agentRuntimeTx) ListKBDocumentsByCollection(collectionID string) ([]m8core.KBDocument, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+m8kbdocColumns+` FROM kb_documents WHERE collection_id=? ORDER BY document_id, version`, collectionID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m8core.KBDocument
	for rows.Next() {
		d, err := scanKBDocument(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, d)
	}
	return out, t.fail(rows.Err())
}

func (t *agentRuntimeTx) SearchKBChunkFTS(scopeID, query string, limit int) ([]m8app.KBSearchHit, error) {
	if limit < 1 {
		limit = 6
	}
	q := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(query), `"`, ""), `'`, "")
	if q == "" {
		return nil, nil
	}
	const cols = `c.chunk_id,c.document_id,c.document_version,c.ordinal,c.content_digest,c.locator_json,c.created_at,c.body,` +
		`d.document_id,d.collection_id,d.version,d.media_type,d.content_ref,d.sha256,d.source_locator,d.index_state,d.created_at`
	var (
		rows *sql.Rows
		err  error
	)
	if utf8.RuneCountInString(q) < 3 {
		rows, err = t.tx.QueryContext(t.ctx, `SELECT `+cols+` FROM kb_chunks c
			JOIN kb_documents d ON d.document_id=c.document_id AND d.version=c.document_version
			JOIN kb_collections col ON col.collection_id=d.collection_id
			WHERE col.scope_id=? AND d.index_state='ready' AND c.body LIKE '%' || ? || '%'
			LIMIT ?`, scopeID, q, limit)
	} else {
		rows, err = t.tx.QueryContext(t.ctx, `SELECT `+cols+` FROM kb_chunk_fts
			JOIN kb_chunks c ON c.chunk_id=kb_chunk_fts.chunk_id
			JOIN kb_documents d ON d.document_id=c.document_id AND d.version=c.document_version
			JOIN kb_collections col ON col.collection_id=d.collection_id
			WHERE col.scope_id=? AND d.index_state='ready' AND kb_chunk_fts MATCH ?
			LIMIT ?`, scopeID, q, limit)
	}
	if err != nil {
		// Trigram MATCH can reject short/operator queries; fall back to LIKE.
		rows, err = t.tx.QueryContext(t.ctx, `SELECT `+cols+` FROM kb_chunks c
			JOIN kb_documents d ON d.document_id=c.document_id AND d.version=c.document_version
			JOIN kb_collections col ON col.collection_id=d.collection_id
			WHERE col.scope_id=? AND d.index_state='ready' AND c.body LIKE '%' || ? || '%'
			LIMIT ?`, scopeID, q, limit)
		if err != nil {
			return nil, t.fail(err)
		}
	}
	defer rows.Close()
	var out []m8app.KBSearchHit
	for rows.Next() {
		var hit m8app.KBSearchHit
		err := rows.Scan(
			&hit.Chunk.ChunkID, &hit.Chunk.DocumentID, &hit.Chunk.DocumentVersion, &hit.Chunk.Ordinal,
			&hit.Chunk.ContentDigest, &hit.Chunk.LocatorJSON, &hit.Chunk.CreatedAt, &hit.Chunk.Body,
			&hit.Document.DocumentID, &hit.Document.CollectionID, &hit.Document.Version, &hit.Document.MediaType,
			&hit.Document.ContentRef, &hit.Document.SHA256, &hit.Document.SourceLocator, &hit.Document.IndexState,
			&hit.Document.CreatedAt,
		)
		if err != nil {
			return nil, t.fail(err)
		}
		hit.Score = 1
		out = append(out, hit)
	}
	return out, t.fail(rows.Err())
}

func (t *agentRuntimeTx) CountKBStats(collectionID string) (docs, ready, chunks int, err error) {
	row := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN index_state='ready' THEN 1 ELSE 0 END),0)
		FROM kb_documents WHERE collection_id=?`, collectionID)
	if err = row.Scan(&docs, &ready); err != nil {
		return 0, 0, 0, t.fail(err)
	}
	row = t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM kb_chunks c
		JOIN kb_documents d ON d.document_id=c.document_id AND d.version=c.document_version
		WHERE d.collection_id=?`, collectionID)
	if err = row.Scan(&chunks); err != nil {
		return 0, 0, 0, t.fail(err)
	}
	return docs, ready, chunks, nil
}

func (t *agentRuntimeTx) GetGrowthPath(expertID string) (m8app.GrowthPath, bool, error) {
	row := t.tx.QueryRowContext(t.ctx, `SELECT expert_id,mission_snapshot,ladder_json,coverage_json,updated_at
		FROM expert_growth_paths WHERE expert_id=?`, expertID)
	var p m8app.GrowthPath
	err := row.Scan(&p.ExpertID, &p.MissionSnapshot, &p.LadderJSON, &p.CoverageJSON, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return m8app.GrowthPath{}, false, nil
	}
	if err != nil {
		return m8app.GrowthPath{}, false, t.fail(err)
	}
	return p, true, nil
}

func (t *agentRuntimeTx) PutGrowthPath(p m8app.GrowthPath) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO expert_growth_paths
		(expert_id,mission_snapshot,ladder_json,coverage_json,updated_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(expert_id) DO UPDATE SET
			mission_snapshot=excluded.mission_snapshot,
			ladder_json=excluded.ladder_json,
			coverage_json=excluded.coverage_json,
			updated_at=excluded.updated_at`,
		p.ExpertID, p.MissionSnapshot, p.LadderJSON, p.CoverageJSON, p.UpdatedAt)
	return t.fail(err)
}

var _ m8app.KBTx = (*agentRuntimeTx)(nil)
