// identity.go - the enterprise identity service (T-9.1.2, ADR-012):
// principal lifecycle, time-limited role bindings with clock-fresh expiry
// (M9-005, no grace cache), revocation-watermark ticket invalidation and
// the org-admin mutation surface. Writes re-derive the org scope from the
// verified context through the Gate - never from payload fields.
package org

import (
	"context"
	"regexp"
	"time"

	"github.com/oklog/ulid/v2"
)

var namePattern = regexp.MustCompile(`^[\p{L}\p{N}_][\p{L}\p{N}\-_ ]{0,127}$`)

// Service is the identity + org-admin mutation service.
type Service struct {
	gate  *Gate
	clock func() time.Time
	newID func() string
}

func NewService(gate *Gate, clock func() time.Time) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{gate: gate, clock: clock, newID: func() string { return ulid.Make().String() }}
}

func (s *Service) Gate() *Gate { return s.gate }

func (s *Service) now() string { return Now(s.clock()) }

// liveOrg loads the caller's org and enforces the ADR-011 write rules:
// suspended/closed refuse new writes with M9-002 (suspended keeps
// Hold/audit; closed seals read-only).
func (s *Service) liveOrg(ctx context.Context) (Organization, error) {
	o, err := s.boundOrg(ctx)
	if err != nil {
		return Organization{}, err
	}
	switch o.State {
	case OrgSuspended, OrgClosed:
		return Organization{}, ErrOrgSuspended
	}
	return o, nil
}

// boundOrg loads the caller's own org without the write-state refusal -
// the state machine methods (ActivateOrg/SuspendOrg) still verify every
// transition against AllowedOrgTransition.
func (s *Service) boundOrg(ctx context.Context) (Organization, error) {
	scope, err := s.gate.Scope(ctx)
	if err != nil {
		return Organization{}, err
	}
	o, err := s.gate.RawStore().OrgByID(ctx, scope)
	if err != nil {
		return Organization{}, ErrOrgNotFound
	}
	return o, nil
}

// CreateOrg is the bootstrap path used by the platform operator (no
// verified org context exists yet); after bootstrap every other mutation
// requires the verified context.
func (s *Service) CreateOrg(ctx context.Context, name string) (Organization, error) {
	if !namePattern.MatchString(name) {
		return Organization{}, ErrOrgNotFound
	}
	o := Organization{
		OrgID: s.newID(), Name: name, State: OrgDraft,
		RetentionDays: 730, CreatedAt: s.now(), UpdatedAt: s.now(),
	}
	if err := s.gate.RawStore().CreateOrg(ctx, o); err != nil {
		return Organization{}, err
	}
	return o, nil
}

// ActivateOrg moves the caller's org along the ADR-011 state machine
// (draft -> active, and suspended -> active resume; Hold/audit survive).
func (s *Service) ActivateOrg(ctx context.Context) (Organization, error) {
	o, err := s.boundOrg(ctx)
	if err != nil {
		return Organization{}, err
	}
	if !AllowedOrgTransition(o.State, OrgActive) {
		return Organization{}, ErrOrgSuspended
	}
	if err := s.gate.RawStore().UpdateOrgState(ctx, o.OrgID, OrgActive, s.now()); err != nil {
		return Organization{}, err
	}
	o.State = OrgActive
	return o, nil
}

// SuspendOrg moves the caller's org to suspended (ADR-011: suspended keeps
// Hold/audit alive; resume goes back through ActivateOrg; closed is the
// terminal seal and is not reachable from this surface).
func (s *Service) SuspendOrg(ctx context.Context) (Organization, error) {
	o, err := s.liveOrg(ctx)
	if err != nil {
		return Organization{}, err
	}
	if !AllowedOrgTransition(o.State, OrgSuspended) {
		return Organization{}, ErrOrgSuspended
	}
	if err := s.gate.RawStore().UpdateOrgState(ctx, o.OrgID, OrgSuspended, s.now()); err != nil {
		return Organization{}, err
	}
	o.State = OrgSuspended
	return o, nil
}

// CreateSpace creates a team space inside the verified org.
func (s *Service) CreateSpace(ctx context.Context, name string) (TeamSpace, error) {
	if _, err := s.liveOrg(ctx); err != nil {
		return TeamSpace{}, err
	}
	if !namePattern.MatchString(name) {
		return TeamSpace{}, ErrSpaceNotFound
	}
	scope, _ := s.gate.Scope(ctx)
	space := TeamSpace{
		SpaceID: s.newID(), OrgID: scope, Name: name, State: "active",
		CreatedAt: s.now(), UpdatedAt: s.now(),
	}
	if err := s.gate.RawStore().CreateSpace(ctx, space); err != nil {
		return TeamSpace{}, err
	}
	return space, nil
}

// InvitePrincipal creates a principal (optionally bridged to an external
// IdP assertion) inside the verified org.
func (s *Service) InvitePrincipal(ctx context.Context, displayName, externalID, idpIssuer, expiresAt string) (Principal, error) {
	if _, err := s.liveOrg(ctx); err != nil {
		return Principal{}, err
	}
	if displayName == "" || len(displayName) > 128 {
		return Principal{}, ErrRoleBindingDenied
	}
	if (externalID == "") != (idpIssuer == "") {
		return Principal{}, ErrRoleBindingDenied // bridged identity needs both halves
	}
	if expiresAt != "" {
		if _, err := time.Parse(time.RFC3339, expiresAt); err != nil {
			return Principal{}, ErrRoleBindingDenied
		}
	}
	scope, _ := s.gate.Scope(ctx)
	p := Principal{
		PrincipalID: s.newID(), OrgID: scope, DisplayName: displayName,
		ExternalID: externalID, IdpIssuer: idpIssuer, State: PrincipalActive,
		BindingVersion: 1, ExpiresAt: expiresAt,
		CreatedAt: s.now(), UpdatedAt: s.now(),
	}
	ev := IdentityEvent{
		EventID: s.newID(), OrgID: scope, PrincipalID: p.PrincipalID,
		Kind: "created", BindingVersion: 1, CreatedAt: s.now(),
	}
	if err := s.gate.RawStore().CreatePrincipal(ctx, p, ev); err != nil {
		return Principal{}, err
	}
	return p, nil
}

// BindRole grants a time-limited role (ADR-012). Negative paths:
// M9-003 foreign principal/space, M9-004 unknown in-org space,
// M9-006 non-positive duration or duplicate active grant.
func (s *Service) BindRole(ctx context.Context, principalID, spaceID, role, expiresAt string) (RoleBinding, error) {
	if _, err := s.liveOrg(ctx); err != nil {
		return RoleBinding{}, err
	}
	scope, _ := s.gate.Scope(ctx)
	p, err := s.gate.RawStore().PrincipalByID(ctx, scope, principalID)
	if err != nil {
		return RoleBinding{}, ErrCrossOrgAccess
	}
	if !livePrincipal(p, s.clock) {
		return RoleBinding{}, ErrPrincipalExpired
	}
	scopeKey := "org"
	if spaceID != "" {
		sp, err := s.gate.RawStore().SpaceByID(ctx, scope, spaceID)
		if err != nil || sp.State != "active" {
			return RoleBinding{}, ErrSpaceNotFound
		}
		scopeKey = spaceID
	}
	if !validRole(role) {
		return RoleBinding{}, ErrRoleBindingDenied
	}
	exp, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil || !exp.After(s.clock()) {
		return RoleBinding{}, ErrRoleBindingDenied // time-limited roles must have a future expiry
	}
	for _, b := range mustList(s.gate.RawStore().ListBindings(ctx, scope, principalID)) {
		if b.ScopeKey == scopeKey && b.Role == role && b.State == "active" && liveBinding(b, s.clock) {
			return RoleBinding{}, ErrRoleBindingDenied
		}
	}
	binding := RoleBinding{
		BindingID: s.newID(), OrgID: scope, PrincipalID: principalID,
		ScopeKey: scopeKey, Role: role, ExpiresAt: expiresAt, State: "active",
		CreatedAt: s.now(), UpdatedAt: s.now(),
	}
	if err := s.gate.RawStore().CreateBinding(ctx, binding); err != nil {
		return RoleBinding{}, err
	}
	return binding, nil
}

// RevokePrincipal suspends a principal and bumps the binding version; the
// new version is the revocation watermark - every ticket stamped with an
// older version is rejected from this instant (ADR-012).
func (s *Service) RevokePrincipal(ctx context.Context, principalID string) error {
	if _, err := s.liveOrg(ctx); err != nil {
		return err
	}
	scope, _ := s.gate.Scope(ctx)
	p, err := s.gate.RawStore().PrincipalByID(ctx, scope, principalID)
	if err != nil {
		return ErrCrossOrgAccess
	}
	ev := IdentityEvent{
		EventID: s.newID(), OrgID: scope, PrincipalID: principalID,
		Kind: "revoked", BindingVersion: p.BindingVersion + 1, CreatedAt: s.now(),
	}
	return s.gate.RawStore().UpdatePrincipalState(ctx, scope, principalID, PrincipalRevoked, p.BindingVersion+1, ev)
}

// RevokeBinding revokes one role binding inside the verified org.
func (s *Service) RevokeBinding(ctx context.Context, bindingID string) error {
	if _, err := s.liveOrg(ctx); err != nil {
		return err
	}
	scope, _ := s.gate.Scope(ctx)
	if _, err := s.gate.RawStore().BindingByID(ctx, scope, bindingID); err != nil {
		return ErrCrossOrgAccess
	}
	return s.gate.RawStore().RevokeBinding(ctx, scope, bindingID, s.now())
}

// CheckRole is the authorization decision point (clock-fresh, no cache):
//
//	M9-003 missing scope / foreign principal
//	M9-002 suspended org refuses new runs
//	M9-005 expired / revoked principal or external identity
//	M9-006 no live binding for the requested role+scope
func (s *Service) CheckRole(ctx context.Context, principalID, spaceID, role string) error {
	scope, err := s.gate.Scope(ctx)
	if err != nil {
		return err
	}
	o, err := s.gate.RawStore().OrgByID(ctx, scope)
	if err != nil {
		return ErrOrgNotFound
	}
	if o.State != OrgActive {
		return ErrOrgSuspended
	}
	p, err := s.gate.RawStore().PrincipalByID(ctx, scope, principalID)
	if err != nil {
		return ErrCrossOrgAccess
	}
	if !livePrincipal(p, s.clock) {
		return ErrPrincipalExpired
	}
	scopeKey := "org"
	if spaceID != "" {
		if _, err := s.gate.RawStore().SpaceByID(ctx, scope, spaceID); err != nil {
			return ErrCrossOrgAccess
		}
		scopeKey = spaceID
	}
	for _, b := range mustList(s.gate.RawStore().ListBindings(ctx, scope, principalID)) {
		if b.Role == role && b.ScopeKey == scopeKey && liveBinding(b, s.clock) {
			return nil
		}
	}
	return ErrRoleBindingDenied
}

// ValidateTicket enforces the revocation watermark: a capability ticket
// issued at bindingVersion is dead once the principal's current version is
// higher (M9-005 semantics for already-issued tickets).
func (s *Service) ValidateTicket(ctx context.Context, principalID string, ticketVersion int) error {
	scope, err := s.gate.Scope(ctx)
	if err != nil {
		return err
	}
	p, err := s.gate.RawStore().PrincipalByID(ctx, scope, principalID)
	if err != nil {
		return ErrCrossOrgAccess
	}
	if !livePrincipal(p, s.clock) {
		return ErrPrincipalExpired
	}
	if ticketVersion < 1 || ticketVersion < p.BindingVersion {
		return ErrPrincipalExpired
	}
	return nil
}

func validRole(role string) bool {
	switch role {
	case RoleOrgAdmin, RoleSpaceAdmin, RoleOperator, RoleApprover, RoleAuditor, RoleLegalOfficer, RoleMember:
		return true
	}
	return false
}

func livePrincipal(p Principal, clock func() time.Time) bool {
	switch p.State {
	case PrincipalSuspended, PrincipalExpired, PrincipalRevoked:
		return false
	}
	if p.ExpiresAt != "" {
		if exp, err := time.Parse(time.RFC3339, p.ExpiresAt); err == nil && !exp.After(clock()) {
			return false
		}
	}
	return true
}

func liveBinding(b RoleBinding, clock func() time.Time) bool {
	if b.State != "active" {
		return false
	}
	exp, err := time.Parse(time.RFC3339, b.ExpiresAt)
	return err == nil && exp.After(clock())
}

func mustList(bindings []RoleBinding, err error) []RoleBinding {
	if err != nil {
		return nil
	}
	return bindings
}
