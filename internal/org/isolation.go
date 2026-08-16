// isolation.go - the fail-closed organization context gate (T-9.1.1).
//
// The verified org id travels exclusively inside context.Context; it is
// exported from a verified session or a signed capability ticket and can
// never be supplied through a payload, query parameter or Runner
// self-report (ADR-011). Every gate method re-derives the scope from the
// context and answers M9-003 uniformly when it is absent or when the target
// resolves outside the caller's organization.
package org

import (
	"context"
)

type ctxKey struct{}

// WithVerifiedOrg stamps the verified organization id into ctx. Callers are
// the session layer or the capability-ticket validator - nothing else.
func WithVerifiedOrg(ctx context.Context, orgID string) context.Context {
	return context.WithValue(ctx, ctxKey{}, orgID)
}

// VerifiedOrg extracts the verified organization id; absent -> fail closed.
func VerifiedOrg(ctx context.Context) (string, error) {
	orgID, _ := ctx.Value(ctxKey{}).(string)
	if orgID == "" {
		return "", ErrCrossOrgAccess
	}
	return orgID, nil
}

// Gate is the isolation middleware every M9 control-plane read/write goes
// through. It wraps Store; payload ids are only ever resolved inside the
// org scope taken from the context.
type Gate struct {
	store Store
}

func NewGate(store Store) *Gate { return &Gate{store: store} }

func (g *Gate) Store() Store { return g.store }

// orgScope resolves the context org or fails closed with the uniform
// M9-003 answer.
func orgScope(ctx context.Context) (string, error) { return VerifiedOrg(ctx) }

// Org returns the caller's own organization (M9-001 only within the
// caller's own scope - a foreign org id answers M9-003 uniformly).
func (g *Gate) Org(ctx context.Context, orgID string) (Organization, error) {
	scope, err := orgScope(ctx)
	if err != nil {
		return Organization{}, err
	}
	if orgID != scope {
		return Organization{}, ErrCrossOrgAccess
	}
	o, err := g.store.OrgByID(ctx, orgID)
	if err != nil {
		return Organization{}, ErrCrossOrgAccess
	}
	return o, nil
}

// Space resolves a space strictly inside the verified org; foreign and
// unknown ids are indistinguishable (M9-003, threat T-01).
func (g *Gate) Space(ctx context.Context, spaceID string) (TeamSpace, error) {
	scope, err := orgScope(ctx)
	if err != nil {
		return TeamSpace{}, err
	}
	s, err := g.store.SpaceByID(ctx, scope, spaceID)
	if err != nil {
		return TeamSpace{}, ErrCrossOrgAccess
	}
	return s, nil
}

// Principal resolves a principal strictly inside the verified org.
func (g *Gate) Principal(ctx context.Context, principalID string) (Principal, error) {
	scope, err := orgScope(ctx)
	if err != nil {
		return Principal{}, err
	}
	p, err := g.store.PrincipalByID(ctx, scope, principalID)
	if err != nil {
		return Principal{}, ErrCrossOrgAccess
	}
	return p, nil
}

// ListSpaces lists the caller-org spaces (org-scoped predicate in store).
func (g *Gate) ListSpaces(ctx context.Context) ([]TeamSpace, error) {
	scope, err := orgScope(ctx)
	if err != nil {
		return nil, err
	}
	return g.store.ListSpaces(ctx, scope)
}

// ListPrincipals lists the caller-org principals.
func (g *Gate) ListPrincipals(ctx context.Context) ([]Principal, error) {
	scope, err := orgScope(ctx)
	if err != nil {
		return nil, err
	}
	return g.store.ListPrincipals(ctx, scope)
}

// ListBindings lists a principal's bindings inside the verified org.
func (g *Gate) ListBindings(ctx context.Context, principalID string) ([]RoleBinding, error) {
	scope, err := orgScope(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := g.store.PrincipalByID(ctx, scope, principalID); err != nil {
		return nil, ErrCrossOrgAccess
	}
	return g.store.ListBindings(ctx, scope, principalID)
}

// Scope exposes the verified org id for the mutation service (fail closed).
func (g *Gate) Scope(ctx context.Context) (string, error) { return orgScope(ctx) }

// Store exposes the underlying store for the mutation service; all service
// writes must re-derive the org scope via Scope first.
func (g *Gate) RawStore() Store { return g.store }
