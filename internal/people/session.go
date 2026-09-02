package people

import (
	"context"

	"github.com/oklog/ulid/v2"
)

func (s *Service) ThreadSession(ctx context.Context, threadID string) (string, bool, error) {
	if s == nil || s.store == nil {
		return "", false, ErrUnavailable
	}
	if !canonicalPeopleULID(threadID) {
		return "", false, ErrInvalid
	}
	return s.store.ThreadSession(ctx, threadID)
}

func (s *Service) BindThreadSession(ctx context.Context, threadID, sessionID string) error {
	if s == nil || s.store == nil {
		return ErrUnavailable
	}
	if !canonicalPeopleULID(threadID) || !canonicalPeopleULID(sessionID) {
		return ErrInvalid
	}
	return s.store.BindThreadSession(ctx, threadID, sessionID, nowRFC3339())
}

func (s *Service) ClearThreadSession(ctx context.Context, threadID string) error {
	if s == nil || s.store == nil {
		return ErrUnavailable
	}
	if !canonicalPeopleULID(threadID) {
		return ErrInvalid
	}
	return s.store.ClearThreadSession(ctx, threadID)
}

func (s *Service) ListBoundSessionIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.store == nil {
		return nil, ErrUnavailable
	}
	return s.store.ListBoundSessionIDs(ctx)
}

func canonicalPeopleULID(v string) bool {
	u, err := ulid.ParseStrict(v)
	return err == nil && u.String() == v && v[0] <= '7'
}
