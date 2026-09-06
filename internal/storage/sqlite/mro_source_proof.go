package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/mroapp"
)

type mroProofKey struct{}
type mroSourceProof struct {
	version int64
	digest  string
	valid   bool
}
type mroEvidenceProof struct {
	store     *Store
	org       string
	documents map[string]mroSourceProof
	checked   time.Time
	planning  bool
}

// File I/O runs before the writer transaction. Proofs are request-local and
// scoped; they cannot authorize a later document version or another store.
func (s *Store) PrepareMROEvidence(ctx context.Context, extra []string) (context.Context, error) {
	if proof, ok := ctx.Value(mroProofKey{}).(mroEvidenceProof); ok && proof.store == s && proof.org == mroapp.Scope(ctx) && time.Since(proof.checked) < time.Minute {
		complete := len(extra) > 0 || proof.planning
		for _, id := range extra {
			if _, ok := proof.documents[strings.TrimSpace(id)]; !ok {
				complete = false
			}
		}
		if complete {
			return ctx, nil
		}
	}
	if _, inside := ctx.Value(mroTxKey{}).(mroTxContext); inside {
		return ctx, mroapp.ErrConstraints
	}
	// Registration checks only the incoming documents. Planning checks the
	// controlled manuals its interval rules actually cite, not unrelated or
	// superseded references in the organization's catalog.
	query := `SELECT DISTINCT md.document_id FROM mro_manual_docs md JOIN mro_manuals m ON m.manual_id=md.manual_id AND m.org_id IS md.org_id WHERE md.org_id IS ? AND m.status='controlled' AND EXISTS(SELECT 1 FROM mro_interval_rules r WHERE r.org_id IS md.org_id AND (r.source_cite=m.manual_id OR r.source_cite='manual:'||m.manual_id)) LIMIT 10001`
	if len(extra) > 0 {
		query = `SELECT document_id FROM mro_manual_docs WHERE org_id IS ? AND 0`
	}
	rows, err := s.db.QueryContext(ctx, query, mroOrg(ctx))
	if err != nil {
		return ctx, err
	}
	ids := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids[id] = true
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return ctx, err
	}
	for _, id := range extra {
		ids[strings.TrimSpace(id)] = true
	}
	if len(ids) > 10000 {
		return ctx, mroapp.ErrCapacity
	}
	proof := mroEvidenceProof{store: s, org: mroapp.Scope(ctx), documents: map[string]mroSourceProof{}, checked: time.Now(), planning: len(extra) == 0}
	// Source groups can contain many document parts. Read each file once.
	files := map[string]string{}
	var total int64
	for id := range ids {
		if err = ctx.Err(); err != nil {
			return ctx, err
		}
		var item mroSourceProof
		var path string
		err = s.db.QueryRowContext(ctx, `SELECT version,sha256,content_ref FROM kb_documents WHERE document_id=? ORDER BY version DESC LIMIT 1`, id).Scan(&item.version, &item.digest, &path)
		if errors.Is(err, sql.ErrNoRows) {
			proof.documents[id] = item
			continue
		}
		if err != nil {
			return ctx, err
		}
		actual, exists := files[path]
		if !exists {
			raw, readErr := doctext.ReadSource(path)
			total += int64(len(raw))
			if total > 128<<20 || time.Since(proof.checked) > 15*time.Second {
				return ctx, mroapp.ErrCapacity
			}
			if readErr == nil {
				sum := sha256.Sum256(raw)
				actual = hex.EncodeToString(sum[:])
			}
			files[path] = actual
		}
		item.valid = actual != "" && actual == item.digest
		proof.documents[id] = item
	}
	return context.WithValue(ctx, mroProofKey{}, proof), nil
}

func (s *Store) mroProvedDocument(ctx context.Context, id string) (mroSourceProof, bool) {
	p, ok := ctx.Value(mroProofKey{}).(mroEvidenceProof)
	if !ok || p.store != s || p.org != mroapp.Scope(ctx) || time.Since(p.checked) >= time.Minute {
		return mroSourceProof{}, false
	}
	item, ok := p.documents[id]
	return item, ok && item.valid
}
