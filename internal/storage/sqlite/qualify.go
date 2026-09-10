package sqlite

import (
	"context"
	"time"

	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/oklog/ulid/v2"
)

func (s *Store) GetModelFitQualification(ctx context.Context, ownerScope, family, modelID, codecVersion string) (modelfit.Qualification, error) {
	var q modelfit.Qualification
	var status, evidence string
	err := s.db.QueryRowContext(ctx, `SELECT family, model_id, codec_version, status, evidence FROM model_fit_qualification
		WHERE owner_scope=? AND family=? AND model_id=? AND codec_version=?`,
		ownerScope, family, modelID, codecVersion).Scan(&q.Family, &q.ModelID, &q.CodecVersion, &status, &evidence)
	if err != nil {
		def := modelfit.DefaultQualification(family, modelID, codecVersion)
		return def, nil
	}
	q.Status = status
	q.Evidence = evidence
	q.Adopted = false
	return q, nil
}

func (s *Store) PutModelFitQualification(ctx context.Context, ownerScope string, q modelfit.Qualification) error {
	if q.Status == "" {
		q.Status = modelfit.QualifyUntested
	}
	if q.Status != modelfit.QualifyUntested && q.Status != modelfit.QualifyFixturePass && q.Status != modelfit.QualifyBlocked {
		q.Status = modelfit.QualifyUntested
	}
	now := formatTime(time.Now().UTC())
	_, err := s.db.ExecContext(ctx, `INSERT INTO model_fit_qualification(id,owner_scope,family,codec_version,model_id,status,evidence,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(owner_scope, family, model_id, codec_version) DO UPDATE SET
			status=excluded.status, evidence=excluded.evidence, updated_at=excluded.updated_at`,
		ulid.Make().String(), ownerScope, string(q.Family), q.CodecVersion, q.ModelID, q.Status, q.Evidence, now, now)
	return mapWriteError(err)
}
