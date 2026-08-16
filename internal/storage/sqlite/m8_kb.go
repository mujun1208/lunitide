// M8 slice-2 storage (T-8.2.x): kb_collections / kb_documents / kb_chunks
// on the agent-runtime single-writer transaction.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

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
		if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO kb_chunks
			(chunk_id,document_id,document_version,ordinal,content_digest,locator_json,embedding,created_at)
			VALUES(?,?,?,?,?,?,NULL,?)`,
			c.ChunkID, c.DocumentID, c.DocumentVersion, c.Ordinal,
			c.ContentDigest, c.LocatorJSON, c.CreatedAt); err != nil {
			return t.fail(err)
		}
	}
	return nil
}
