package mroapp

import (
	"context"
	"errors"
	"strings"

	"github.com/oklog/ulid/v2"
)

type Publication struct {
	EvidenceDigest string
	Todos          []OpsTodo
	CreatedAt      string
}
type EvidenceStore interface {
	PrepareMROEvidence(context.Context, []string) (context.Context, error)
	MROManualSourcesCurrent(context.Context, []IntervalRule) (bool, error)
	MROEvidenceDigest(context.Context) (string, error)
	GetMROPublication(context.Context, string) (Publication, error)
	PutMROPublication(context.Context, string, Publication) error
}

// PrepareEvidence validates local source bytes before opening a write
// transaction. The store rechecks the captured document version inside it.
func (s *Service) PrepareEvidence(ctx context.Context, documentIDs []string) (context.Context, error) {
	if s == nil {
		return ctx, ErrServiceUnavailable
	}
	if store, ok := s.store.(EvidenceStore); ok {
		return store.PrepareMROEvidence(ctx, documentIDs)
	}
	return ctx, nil
}

func (s *Service) publicationInputs(ctx context.Context, packageID string) (WorkPackage, string, error) {
	store, ok := s.store.(EvidenceStore)
	if !ok {
		return WorkPackage{}, "", ErrServiceUnavailable
	}
	ops, err := s.ops()
	if err != nil {
		return WorkPackage{}, "", err
	}
	packages, err := ops.ListWorkPackages(ctx)
	if err != nil {
		return WorkPackage{}, "", err
	}
	var pkg WorkPackage
	for _, item := range packages {
		if item.ID == packageID {
			pkg = item
			break
		}
	}
	if pkg.ID == "" {
		return pkg, "", ErrNotFound
	}
	issues, err := s.CheckConstraints(ctx)
	if err != nil {
		return pkg, "", err
	}
	if len(issues) > 0 {
		return pkg, "", ErrConstraints
	}
	if len(pkg.SourceRefs) == 0 {
		return pkg, "", ErrConstraints
	}
	rules, err := ops.ListIntervalRules(ctx)
	if err != nil {
		return pkg, "", err
	}
	dues, err := ops.ListDueItems(ctx)
	if err != nil {
		return pkg, "", err
	}
	for _, ref := range pkg.SourceRefs {
		kind, id, ok := strings.Cut(ref, ":")
		if !ok {
			return pkg, "", ErrConstraints
		}
		found := false
		if kind == "card" {
			for _, rule := range rules {
				if rule.TaskKey == id {
					found = true
					break
				}
			}
		} else {
			for _, due := range dues {
				if due.ID == id && ((kind == "ad" && due.Kind == "AD") || (kind == "mel" && due.Kind == "MEL") || (kind == "open" && due.Kind == "CHECK")) {
					found = true
					break
				}
			}
		}
		if !found {
			return pkg, "", ErrConstraints
		}
	}
	digest, err := store.MROEvidenceDigest(ctx)
	return pkg, digest, err
}
func (s *Service) VerifyPublication(ctx context.Context, packageID string) error {
	var err error
	ctx, err = s.PrepareEvidence(ctx, nil)
	if err != nil {
		return err
	}
	return atomicError(s, ctx, func(ctx context.Context) error {
		_, digest, err := s.publicationInputs(ctx, packageID)
		if err != nil {
			return err
		}
		store := s.store.(EvidenceStore)
		p, err := store.GetMROPublication(ctx, packageID)
		if err != nil {
			return err
		}
		if digest != p.EvidenceDigest {
			return ErrConflict
		}
		return nil
	})
}
func (s *Service) publishVerified(ctx context.Context, packageID string) ([]OpsTodo, error) {
	pkg, digest, err := s.publicationInputs(ctx, packageID)
	if err != nil {
		return nil, err
	}
	store := s.store.(EvidenceStore)
	p, err := store.GetMROPublication(ctx, packageID)
	if err == nil {
		if p.EvidenceDigest != digest {
			return nil, ErrConflict
		}
		return p.Todos, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	todos := PublishScheduleTodos(pkg)
	now := s.clock.Now().UTC().Format(opsTimeLayout)
	for i := range todos {
		todos[i].ID = ulid.Make().String()
		todos[i].CreatedAt = now
	}
	ops, err := s.ops()
	if err != nil {
		return nil, err
	}
	if err = ops.InsertOpsTodos(ctx, todos); err != nil {
		return nil, err
	}
	if err = store.PutMROPublication(ctx, pkg.ID, Publication{EvidenceDigest: digest, Todos: todos, CreatedAt: now}); err != nil {
		return nil, err
	}
	return todos, nil
}
