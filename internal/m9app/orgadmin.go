// Package m9app hosts the M9 slice-1 org-admin bridge service (T-9.1.3).
//
// OrgAdminService is the local-operator administration surface over the
// org foundation: it derives the verified org context exclusively from the
// persisted operator binding (the ADR-011 "verified session" boundary for
// the single-user desktop build) and never accepts an org id from a bridge
// payload except on org.switch, which re-binds and re-verifies the target.
package m9app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lunitide/lunitide/internal/org"
)

// BindingStore persists the local operator's bound organization id.
type BindingStore interface {
	// Load returns the bound org id, or "" when no binding exists yet.
	Load(ctx context.Context) (string, error)
	// Save re-binds the operator to orgID.
	Save(ctx context.Context, orgID string) error
}

// FileBindingStore is a compact JSON binding file under the engine data
// root. Writes are atomic (temp file + rename) and never fall back to a
// partial write.
type FileBindingStore struct{ path string }

func NewFileBindingStore(path string) *FileBindingStore { return &FileBindingStore{path: path} }

type bindingFile struct {
	OrgID string `json:"orgId"`
}

func (f *FileBindingStore) Load(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	raw, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var b bindingFile
	if err := json.Unmarshal(raw, &b); err != nil {
		return "", fmt.Errorf("org binding unreadable: %w", err)
	}
	return b.OrgID, nil
}

func (f *FileBindingStore) Save(ctx context.Context, orgID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(bindingFile{OrgID: orgID})
	if err != nil {
		return err
	}
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "binding-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, f.path)
}

// OrgAdminService is the org-admin mutation/read service behind the ten
// org.* bridge methods.
type OrgAdminService struct {
	svc     *org.Service
	binding BindingStore
}

func NewOrgAdminService(svc *org.Service, binding BindingStore) *OrgAdminService {
	return &OrgAdminService{svc: svc, binding: binding}
}

// verifiedCtx stamps the bound org id into the context; an absent binding
// fails closed with the uniform M9-003 answer (missing org context).
func (a *OrgAdminService) verifiedCtx(ctx context.Context) (context.Context, error) {
	orgID, err := a.binding.Load(ctx)
	if err != nil {
		return nil, err
	}
	if orgID == "" {
		return nil, org.ErrCrossOrgAccess
	}
	return org.WithVerifiedOrg(ctx, orgID), nil
}

// OrgView is the wire shape shared by summary/create/switch/activate/
// suspend results.
type OrgView struct {
	OrgID         string `json:"orgId"`
	Name          string `json:"name"`
	State         string `json:"state"`
	RetentionDays int    `json:"retentionDays"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

func orgView(o org.Organization) OrgView {
	return OrgView{OrgID: o.OrgID, Name: o.Name, State: o.State, RetentionDays: o.RetentionDays, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt}
}

// OrgListItem is the compact row in the summary orgs array.
type OrgListItem struct {
	OrgID string `json:"orgId"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// SummaryResult answers org.summary: the bound org detail plus the local
// operator's org directory (bootstrap surface - no verified context yet).
type SummaryResult struct {
	BoundOrgID string       `json:"boundOrgId"`
	Org        *OrgView     `json:"org,omitempty"`
	Orgs       []OrgListItem `json:"orgs"`
}

func (a *OrgAdminService) Summary(ctx context.Context) (SummaryResult, error) {
	orgs, err := a.svc.Gate().RawStore().ListOrgs(ctx)
	if err != nil {
		return SummaryResult{}, err
	}
	res := SummaryResult{Orgs: make([]OrgListItem, 0, len(orgs))}
	for _, o := range orgs {
		res.Orgs = append(res.Orgs, OrgListItem{OrgID: o.OrgID, Name: o.Name, State: o.State})
	}
	bound, err := a.binding.Load(ctx)
	if err != nil {
		return SummaryResult{}, err
	}
	res.BoundOrgID = bound
	if bound != "" {
		for _, o := range orgs {
			if o.OrgID == bound {
				view := orgView(o)
				res.Org = &view
				break
			}
		}
	}
	return res, nil
}

// CreateOrg is the platform-operator bootstrap path (no verified context).
func (a *OrgAdminService) CreateOrg(ctx context.Context, name string) (OrgView, error) {
	o, err := a.svc.CreateOrg(ctx, name)
	if err != nil {
		return OrgView{}, err
	}
	return orgView(o), nil
}

// Switch re-binds the operator to an existing org (re-verified on load).
func (a *OrgAdminService) Switch(ctx context.Context, orgID string) (OrgView, error) {
	o, err := a.svc.Gate().RawStore().OrgByID(ctx, orgID)
	if err != nil {
		return OrgView{}, org.ErrOrgNotFound
	}
	if err := a.binding.Save(ctx, orgID); err != nil {
		return OrgView{}, err
	}
	return orgView(o), nil
}

// Activate resumes/activates the bound org along the ADR-011 state machine.
func (a *OrgAdminService) Activate(ctx context.Context) (OrgView, error) {
	vctx, err := a.verifiedCtx(ctx)
	if err != nil {
		return OrgView{}, err
	}
	o, err := a.svc.ActivateOrg(vctx)
	if err != nil {
		return OrgView{}, err
	}
	return orgView(o), nil
}

// Suspend moves the bound org to suspended (Hold/audit stay available).
func (a *OrgAdminService) Suspend(ctx context.Context) (OrgView, error) {
	vctx, err := a.verifiedCtx(ctx)
	if err != nil {
		return OrgView{}, err
	}
	o, err := a.svc.SuspendOrg(vctx)
	if err != nil {
		return OrgView{}, err
	}
	return orgView(o), nil
}

// SpaceView is the wire shape for team spaces.
type SpaceView struct {
	SpaceID   string `json:"spaceId"`
	Name      string `json:"name"`
	State     string `json:"state"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func spaceView(s org.TeamSpace) SpaceView {
	return SpaceView{SpaceID: s.SpaceID, Name: s.Name, State: s.State, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
}

type SpaceListResult struct {
	Spaces []SpaceView `json:"spaces"`
}

func (a *OrgAdminService) ListSpaces(ctx context.Context) (SpaceListResult, error) {
	vctx, err := a.verifiedCtx(ctx)
	if err != nil {
		return SpaceListResult{}, err
	}
	spaces, err := a.svc.Gate().ListSpaces(vctx)
	if err != nil {
		return SpaceListResult{}, err
	}
	res := SpaceListResult{Spaces: make([]SpaceView, 0, len(spaces))}
	for _, s := range spaces {
		res.Spaces = append(res.Spaces, spaceView(s))
	}
	return res, nil
}

func (a *OrgAdminService) CreateSpace(ctx context.Context, name string) (SpaceView, error) {
	vctx, err := a.verifiedCtx(ctx)
	if err != nil {
		return SpaceView{}, err
	}
	s, err := a.svc.CreateSpace(vctx, name)
	if err != nil {
		return SpaceView{}, err
	}
	return spaceView(s), nil
}

// PrincipalView is the wire shape for principals.
type PrincipalView struct {
	PrincipalID    string `json:"principalId"`
	DisplayName    string `json:"displayName"`
	ExternalID     string `json:"externalId,omitempty"`
	IdpIssuer      string `json:"idpIssuer,omitempty"`
	State          string `json:"state"`
	BindingVersion int    `json:"bindingVersion"`
	ExpiresAt      string `json:"expiresAt,omitempty"`
	RevokedAt      string `json:"revokedAt,omitempty"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

func principalView(p org.Principal) PrincipalView {
	return PrincipalView{
		PrincipalID: p.PrincipalID, DisplayName: p.DisplayName, ExternalID: p.ExternalID,
		IdpIssuer: p.IdpIssuer, State: p.State, BindingVersion: p.BindingVersion,
		ExpiresAt: p.ExpiresAt, RevokedAt: p.RevokedAt, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

// BindingView is the wire shape for role bindings.
type BindingView struct {
	BindingID   string `json:"bindingId"`
	PrincipalID string `json:"principalId"`
	ScopeKey    string `json:"scopeKey"`
	Role        string `json:"role"`
	ExpiresAt   string `json:"expiresAt"`
	State       string `json:"state"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

// MemberView pairs a principal with its live+revoked bindings.
type MemberView struct {
	Principal PrincipalView `json:"principal"`
	Bindings  []BindingView `json:"bindings"`
}

type MemberListResult struct {
	Members []MemberView `json:"members"`
}

func (a *OrgAdminService) ListMembers(ctx context.Context) (MemberListResult, error) {
	vctx, err := a.verifiedCtx(ctx)
	if err != nil {
		return MemberListResult{}, err
	}
	gate := a.svc.Gate()
	principals, err := gate.ListPrincipals(vctx)
	if err != nil {
		return MemberListResult{}, err
	}
	res := MemberListResult{Members: make([]MemberView, 0, len(principals))}
	for _, p := range principals {
		member := MemberView{Principal: principalView(p), Bindings: []BindingView{}}
		bindings, err := gate.ListBindings(vctx, p.PrincipalID)
		if err != nil {
			return MemberListResult{}, err
		}
		for _, b := range bindings {
			member.Bindings = append(member.Bindings, BindingView{
				BindingID: b.BindingID, PrincipalID: b.PrincipalID, ScopeKey: b.ScopeKey,
				Role: b.Role, ExpiresAt: b.ExpiresAt, State: b.State,
				CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt,
			})
		}
		res.Members = append(res.Members, member)
	}
	return res, nil
}

func (a *OrgAdminService) Invite(ctx context.Context, displayName, externalID, idpIssuer, expiresAt string) (PrincipalView, error) {
	vctx, err := a.verifiedCtx(ctx)
	if err != nil {
		return PrincipalView{}, err
	}
	p, err := a.svc.InvitePrincipal(vctx, displayName, externalID, idpIssuer, expiresAt)
	if err != nil {
		return PrincipalView{}, err
	}
	return principalView(p), nil
}

// RevokeResult confirms the revocation watermark bump (M9-005 semantics:
// every ticket stamped with an older binding version dies instantly).
type RevokeResult struct {
	PrincipalID    string `json:"principalId"`
	State          string `json:"state"`
	BindingVersion int    `json:"bindingVersion"`
}

func (a *OrgAdminService) Revoke(ctx context.Context, principalID string) (RevokeResult, error) {
	vctx, err := a.verifiedCtx(ctx)
	if err != nil {
		return RevokeResult{}, err
	}
	if err := a.svc.RevokePrincipal(vctx, principalID); err != nil {
		return RevokeResult{}, err
	}
	p, err := a.svc.Gate().Principal(vctx, principalID)
	if err != nil {
		return RevokeResult{}, err
	}
	return RevokeResult{PrincipalID: p.PrincipalID, State: p.State, BindingVersion: p.BindingVersion}, nil
}
