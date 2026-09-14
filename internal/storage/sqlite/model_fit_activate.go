package sqlite

import (
	"context"
	"database/sql"
	"sync"

	"github.com/lunitide/lunitide/internal/modelfit"
)

var modelFitLedgers sync.Map

func modelFitLedger(s *Store) *modelfit.BindingLedger {
	if v, ok := modelFitLedgers.Load(s); ok {
		return v.(*modelfit.BindingLedger)
	}
	created := modelfit.NewBindingLedger()
	actual, _ := modelFitLedgers.LoadOrStore(s, created)
	return actual.(*modelfit.BindingLedger)
}

func (s *Store) ActivateModelFit(ctx context.Context, in modelfit.ActivateInput) (modelfit.ActivateResult, error) {
	found, row, err := s.lookupModelFitQualification(ctx, in.OwnerScope, in.Family, in.ModelID, in.CodecVersion)
	if err != nil {
		return modelfit.ActivateResult{}, err
	}
	if found {
		in.FixtureRow = &row
	}
	return modelFitLedger(s).Activate(in)
}

func (s *Store) GetModelFitActiveBinding(ownerScope, providerID, modelID string) (modelfit.ActiveBinding, bool) {
	return modelFitLedger(s).Get(ownerScope, providerID, modelID)
}

func (s *Store) lookupModelFitQualification(ctx context.Context, ownerScope, family, modelID, codecVersion string) (bool, modelfit.Qualification, error) {
	var q modelfit.Qualification
	var status, evidence string
	err := s.db.QueryRowContext(ctx, `SELECT family, model_id, codec_version, status, evidence FROM model_fit_qualification
		WHERE owner_scope=? AND family=? AND model_id=? AND codec_version=?`,
		ownerScope, family, modelID, codecVersion).Scan(&q.Family, &q.ModelID, &q.CodecVersion, &status, &evidence)
	if err == sql.ErrNoRows {
		return false, modelfit.DefaultQualification(family, modelID, codecVersion), nil
	}
	if err != nil {
		return false, q, err
	}
	q.Status = status
	q.Evidence = evidence
	q.Adopted = false
	return true, q, nil
}
