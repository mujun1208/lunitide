package sqlite

import (
	"database/sql"
	"errors"
	"github.com/lunitide/lunitide/internal/m8app"
)

const kbSourceColumns = `source_id,collection_id,path_key,path,media_type,source_locator,sha256,state,version,revision,checked_at,error,created_at`

// KBSourceCurrentPredicate uses alias d for kb_documents. Callers must also
// check the document's latest ready version and their collection ownership.
const KBSourceCurrentPredicate = `(NOT EXISTS (SELECT 1 FROM kb_source_documents sd WHERE sd.document_id=d.document_id) OR EXISTS (SELECT 1 FROM kb_source_documents sd JOIN kb_sources s ON s.source_id=sd.source_id WHERE sd.document_id=d.document_id AND sd.document_version=d.version AND sd.source_version=s.version AND s.state='fresh'))`

func scanKBSource(row interface{ Scan(...any) error }) (m8app.KBSource, error) {
	var s m8app.KBSource
	err := row.Scan(&s.SourceID, &s.CollectionID, &s.PathKey, &s.Path, &s.MediaType, &s.SourceLocator, &s.SHA256, &s.State, &s.Version, &s.Revision, &s.CheckedAt, &s.Error, &s.CreatedAt)
	s.Versions = []m8app.KBSourceVersion{}
	return s, err
}
func (t *agentRuntimeTx) KBCollectionOwned(id, subject string) (bool, error) {
	var exists bool
	err := t.tx.QueryRowContext(t.ctx, `SELECT EXISTS(SELECT 1 FROM kb_collections WHERE collection_id=? AND subject_id=?)`, id, subject).Scan(&exists)
	return exists, t.fail(err)
}
func (t *agentRuntimeTx) GetKBSource(collectionID, key string) (m8app.KBSource, bool, error) {
	s, err := scanKBSource(t.tx.QueryRowContext(t.ctx, `SELECT `+kbSourceColumns+` FROM kb_sources WHERE collection_id=? AND path_key=?`, collectionID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return s, false, nil
	}
	return s, err == nil, t.fail(err)
}
func (t *agentRuntimeTx) GetKBSourceForDocument(id string) (m8app.KBSource, bool, error) {
	s, err := scanKBSource(t.tx.QueryRowContext(t.ctx, `SELECT `+kbSourceColumns+` FROM kb_sources WHERE source_id=(SELECT source_id FROM kb_source_documents WHERE document_id=? LIMIT 1)`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return s, false, nil
	}
	return s, err == nil, t.fail(err)
}
func (t *agentRuntimeTx) ListKBSources(collectionID, after string, limit int) ([]m8app.KBSource, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+kbSourceColumns+` FROM kb_sources WHERE collection_id=? AND source_id>? ORDER BY source_id LIMIT ?`, collectionID, after, limit)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	out := []m8app.KBSource{}
	for rows.Next() {
		s, e := scanKBSource(rows)
		if e != nil {
			return nil, t.fail(e)
		}
		out = append(out, s)
	}
	return out, t.fail(rows.Err())
}
func (t *agentRuntimeTx) PutKBSource(s m8app.KBSource, expected int64) error {
	var result sql.Result
	var err error
	if expected == 0 {
		result, err = t.tx.ExecContext(t.ctx, `INSERT INTO kb_sources (`+kbSourceColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, s.SourceID, s.CollectionID, s.PathKey, s.Path, s.MediaType, s.SourceLocator, s.SHA256, s.State, s.Version, s.Revision, s.CheckedAt, s.Error, s.CreatedAt)
	} else {
		result, err = t.tx.ExecContext(t.ctx, `UPDATE kb_sources SET media_type=?,source_locator=?,sha256=?,state=?,version=?,revision=?,checked_at=?,error=? WHERE source_id=? AND collection_id=? AND path_key=? AND revision=?`, s.MediaType, s.SourceLocator, s.SHA256, s.State, s.Version, s.Revision, s.CheckedAt, s.Error, s.SourceID, s.CollectionID, s.PathKey, expected)
	}
	if err != nil {
		return t.fail(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return t.fail(err)
	}
	if n != 1 {
		return m8app.ErrKBVersionConflict
	}
	return nil
}
func (t *agentRuntimeTx) PutKBSourceVersion(s m8app.KBSourceVersion) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO kb_source_versions(source_id,version,sha256,state,error,created_at) VALUES(?,?,?,?,?,?)`, s.SourceID, s.Version, s.SHA256, s.State, s.Error, s.CreatedAt)
	return t.fail(err)
}
func (t *agentRuntimeTx) ListKBSourceVersions(id string, before int64, limit int) ([]m8app.KBSourceVersion, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT source_id,version,sha256,state,error,created_at FROM kb_source_versions WHERE source_id=? AND (?=0 OR version<?) ORDER BY version DESC LIMIT ?`, id, before, before, limit)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	out := []m8app.KBSourceVersion{}
	for rows.Next() {
		var s m8app.KBSourceVersion
		if e := rows.Scan(&s.SourceID, &s.Version, &s.SHA256, &s.State, &s.Error, &s.CreatedAt); e != nil {
			return nil, t.fail(e)
		}
		out = append(out, s)
	}
	return out, t.fail(rows.Err())
}
func (t *agentRuntimeTx) PutKBSourceDocument(d m8app.KBSourceDocument) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO kb_source_documents(source_id,source_version,ordinal,document_id,document_version) VALUES(?,?,?,?,?)`, d.SourceID, d.SourceVersion, d.Ordinal, d.DocumentID, d.DocumentVersion)
	return t.fail(err)
}
func (t *agentRuntimeTx) ListKBSourceDocuments(id string, version int64) ([]m8app.KBSourceDocument, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT source_id,source_version,ordinal,document_id,document_version FROM kb_source_documents WHERE source_id=? AND source_version=? ORDER BY ordinal`, id, version)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	out := []m8app.KBSourceDocument{}
	for rows.Next() {
		var d m8app.KBSourceDocument
		if e := rows.Scan(&d.SourceID, &d.SourceVersion, &d.Ordinal, &d.DocumentID, &d.DocumentVersion); e != nil {
			return nil, t.fail(e)
		}
		out = append(out, d)
	}
	return out, t.fail(rows.Err())
}
func (t *agentRuntimeTx) KBSourceDocumentCurrent(id string, version int64) (bool, error) {
	var current bool
	err := t.tx.QueryRowContext(t.ctx, `SELECT EXISTS(SELECT 1 FROM kb_documents d WHERE d.document_id=? AND d.version=? AND d.index_state='ready' AND d.version=(SELECT MAX(current.version) FROM kb_documents current WHERE current.document_id=d.document_id) AND `+KBSourceCurrentPredicate+`)`, id, version).Scan(&current)
	return current, t.fail(err)
}

var _ m8app.KBSourceTx = (*agentRuntimeTx)(nil)

func (t *agentRuntimeTx) GetKBSourceByID(collectionID, id string) (m8app.KBSource, bool, error) {
	s, err := scanKBSource(t.tx.QueryRowContext(t.ctx, `SELECT `+kbSourceColumns+` FROM kb_sources WHERE collection_id=? AND source_id=?`, collectionID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return s, false, nil
	}
	return s, err == nil, t.fail(err)
}
