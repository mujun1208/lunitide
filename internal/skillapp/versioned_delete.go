package skillapp

import (
	"context"
	"errors"
)

// DeleteVersion deletes the revision the user reviewed, including published skills.
// Repositories lacking an atomic delete cannot satisfy this operation.
func (s *Service) DeleteVersion(ctx context.Context, id string, expectedRev int64) error {
	if s == nil || s.write == nil {
		return errors.New("skill writer unavailable")
	}
	sk, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if expectedRev < 0 || sk.Rev != expectedRev {
		return ErrSkillVersionConflict
	}
	w, ok := s.write.(interface {
		DeleteSkillVersion(context.Context, string, int64) error
	})
	if !ok {
		return errors.New("atomic skill delete unavailable")
	}
	return w.DeleteSkillVersion(ctx, id, expectedRev)
}
