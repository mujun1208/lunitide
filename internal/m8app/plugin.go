// M8 FR-18 application service (T-8.9.x): plugin.install / list / toggle /
// upgrade / uninstall / dev.create.
//
// The install verification chain runs in order - signature/package hash
// (M8-035, quarantine with zero registration), manifest schema (M8-036),
// permission whitelist comparison against the user grant (M8-037), sandbox
// probe (M8-038, retryable) - then capabilities hot-register into the
// EXISTING registries through the registrar hook and the binding rows
// commit in the same transaction as the m6_outbox audit event. Any chain
// failure registers zero capabilities. Upgrades are versioned replacements:
// a permission expansion (M8-039) parks the install at quarantined pending
// review, never auto-enabling. Uninstall revokes every binding and writes
// the recursive-cleanup tombstone in one transaction (M8-041 all-or-
// nothing). Every capability call path pre-checks an active binding
// (M8-040) via CheckBinding.
package m8app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m8core"
)

// M8 FR-18 error family (04 错误矩阵 M8-035~041).
var (
	// ErrPluginSignatureInvalid: signature/package hash invalid -
	// quarantined with zero registration (M8-035).
	ErrPluginSignatureInvalid = errors.New("m8app: plugin signature or package hash invalid")
	// ErrPluginManifestInvalid: the manifest failed schema validation
	// (M8-036, 422).
	ErrPluginManifestInvalid = errors.New("m8app: plugin manifest invalid")
	// ErrPluginPermissionDenied: requested permissions exceed the user
	// grant whitelist (M8-037, 403).
	ErrPluginPermissionDenied = errors.New("m8app: plugin permission beyond grant")
	// ErrPluginProbeFailed: the sandbox probe failed (M8-038, 502
	// retryable).
	ErrPluginProbeFailed = errors.New("m8app: plugin probe failed")
	// ErrPluginPermissionExpansion: the upgrade requests permissions
	// outside the standing grant - quarantined pending review (M8-039,
	// 409).
	ErrPluginPermissionExpansion = errors.New("m8app: plugin upgrade permission expansion")
	// ErrBindingInactive: the capability call found no active binding -
	// refused with zero side effects (M8-040, 403).
	ErrBindingInactive = errors.New("m8app: plugin capability binding not active")
	// ErrPluginUninstallConflict: one step of the uninstall chain failed -
	// the whole transaction rolls back, no half-uninstalled state
	// (M8-041, 409).
	ErrPluginUninstallConflict = errors.New("m8app: plugin uninstall chain failed")
	// ErrInstallNotFound: the install row does not exist.
	ErrInstallNotFound = errors.New("m8app: plugin install not found")
	// ErrInstallStateInvalid: the transition is not on the allowed path
	// (e.g. quarantined refusing anything but uninstall).
	ErrInstallStateInvalid = errors.New("m8app: plugin install state transition invalid")
)

// PackageSource is the resolved package description feeding the
// verification chain. SignatureStatus is one of verified/unverified/
// invalid.
type PackageSource struct {
	PluginID        string
	Semver          string
	Publisher       string
	Kind            string
	ManifestRef     string
	Entrypoint      string
	Capabilities    string // JSON array document
	Permissions     string // canonical JSON permission document
	Requires        string // JSON object document
	PackageHash     string // full-package SHA-256
	SignatureStatus string
}

// BindingSpec is one hot-registration target produced by the registrar.
type BindingSpec struct {
	TargetType       string
	TargetID         string
	CapabilityDigest string
}

// SourceResolver resolves one (origin, source) ref into the package
// description (may perform IO: market fetch, local path read, dev
// workspace).
type SourceResolver func(ctx context.Context, origin, source string) (PackageSource, error)

// Prober is the sandbox liveness probe (kind=mcp reuses the mcp.add
// probe). Returning an error fails the chain at M8-038.
type Prober func(ctx context.Context, pkg PackageSource) error

// Registrar performs the hot registration into the EXISTING registry for
// the package kind and answers the binding specs. The binding rows commit
// in the caller's transaction; an error fails the chain with zero
// registration.
type Registrar func(ctx context.Context, pkg PackageSource) ([]BindingSpec, error)

// Revoker reverses one hot registration in the target registry (disable /
// tombstone / manifest removal). An error fails the operation wholesale.
type Revoker func(ctx context.Context, spec BindingSpec) error

// PluginTx is the FR-18 single-writer transaction (plugin tables plus the
// shared audit ledger and the recursive-cleanup tombstone).
type PluginTx interface {
	GetPluginBundle(bundleID string) (m8core.PluginBundle, error)
	GetPluginBundleBySemver(pluginID, semver string) (m8core.PluginBundle, error)
	PutPluginBundle(m8core.PluginBundle) error
	GetInstall(installID string) (m8core.PluginInstall, error)
	GetInstallBySubjectPlugin(subjectID, pluginID string) (m8core.PluginInstall, bool, error)
	PutInstall(m8core.PluginInstall) error
	PutBinding(m8core.PluginCapabilityBinding) error
	ListBindings(installID string) ([]m8core.PluginCapabilityBinding, error)
	RevokeBindings(installID string, now string) (int, error)
	ListInstalls(kind, state string) ([]PluginListItem, error)
	PutTombstone(m8core.Tombstone) error
	AppendAuditEvent(audit.Event) (audit.Event, error)
}

// PluginUnitOfWork is the FR-18 single-writer boundary.
type PluginUnitOfWork interface {
	TransactPlugin(ctx context.Context, fn func(PluginTx) error) error
}

// PluginService implements the FR-18 use cases.
type PluginService struct {
	uow      PluginUnitOfWork
	clock    Clock
	subject  string
	resolve  SourceResolver
	probe    Prober
	register Registrar
	revoke   Revoker
}

// NewPluginService wires the FR-18 service.
func NewPluginService(uow PluginUnitOfWork, localSubject string) *PluginService {
	return &PluginService{uow: uow, clock: systemClock{}, subject: localSubject}
}

// SetClock substitutes the clock (tests).
func (s *PluginService) SetClock(c Clock) { s.clock = c }

// SetSourceResolver substitutes the (origin, source) resolver (tests and
// production IO adapters).
func (s *PluginService) SetSourceResolver(f SourceResolver) { s.resolve = f }

// SetProber substitutes the sandbox probe.
func (s *PluginService) SetProber(f Prober) { s.probe = f }

// SetRegistrar substitutes the hot-registration hook.
func (s *PluginService) SetRegistrar(f Registrar) { s.register = f }

// SetRevoker substitutes the revocation hook.
func (s *PluginService) SetRevoker(f Revoker) { s.revoke = f }

// resolveSource answers the package for one source ref: the injected
// resolver when present, otherwise source names an already-registered
// bundle row (local/dev flow where dev.create staged the bundle).
func (s *PluginService) resolveSource(ctx context.Context, tx PluginTx, origin, source string) (PackageSource, error) {
	if s.resolve != nil {
		return s.resolve(ctx, origin, source)
	}
	b, err := tx.GetPluginBundle(source)
	if err != nil {
		return PackageSource{}, err
	}
	return PackageSource{
		PluginID: b.PluginID, Semver: b.Semver, Publisher: b.Publisher,
		Kind: b.Kind, ManifestRef: b.ManifestRef, Entrypoint: b.Entrypoint,
		Capabilities: b.CapabilitiesJSON, Permissions: b.PermissionsJSON,
		Requires: b.RequiresJSON, PackageHash: b.PackageHash,
		SignatureStatus: b.SignatureStatus,
	}, nil
}

func decodePermissionDoc(doc string) (m8core.PermissionDoc, error) {
	if doc == "" {
		return m8core.PermissionDoc{}, nil
	}
	var p m8core.PermissionDoc
	if err := json.Unmarshal([]byte(doc), &p); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPluginManifestInvalid, err)
	}
	return p, nil
}

// verifyChain enacts the ordered chain M8-035 -> M8-036 -> M8-037 ->
// M8-038. It answers the permission grant digest on success.
func (s *PluginService) verifyChain(ctx context.Context, pkg PackageSource, grant m8core.PermissionDoc) error {
	if !m8core.ValidHexDigest(pkg.PackageHash) {
		return fmt.Errorf("%w: package hash", ErrPluginSignatureInvalid)
	}
	if pkg.SignatureStatus == m8core.SignatureInvalid {
		return ErrPluginSignatureInvalid
	}
	if err := m8core.ValidateManifest(pkg.PluginID, pkg.Semver, pkg.Publisher, pkg.Kind, pkg.Capabilities); err != nil {
		return fmt.Errorf("%w: %v", ErrPluginManifestInvalid, err)
	}
	req, err := decodePermissionDoc(pkg.Permissions)
	if err != nil {
		return err
	}
	if !m8core.PermissionsWithin(req, grant) {
		return ErrPluginPermissionDenied
	}
	if s.probe != nil {
		if err := s.probe(ctx, pkg); err != nil {
			return fmt.Errorf("%w: %v", ErrPluginProbeFailed, err)
		}
	}
	return nil
}

// defaultRegistrar derives one binding at the routed target for the
// package kind (the identity hot registration used when no registrar is
// injected: target id = package hash pin).
func defaultRegistrar(ctx context.Context, pkg PackageSource) ([]BindingSpec, error) {
	target := m8core.RouteTarget(pkg.Kind)
	if target == "" {
		return nil, fmt.Errorf("%w: kind %s", ErrPluginManifestInvalid, pkg.Kind)
	}
	return []BindingSpec{{
		TargetType:       target,
		TargetID:         pkg.PluginID + "@" + pkg.Semver,
		CapabilityDigest: pkg.PackageHash,
	}}, nil
}

// InstallInput is the plugin.install command.
type InstallInput struct {
	Origin          string // market | local | dev
	Source          string
	PermissionGrant json.RawMessage
	RequestID       string
	Actor           string
}

// InstallResult is the plugin.install outcome.
type InstallResult struct {
	InstallID string               `json:"installId"`
	State     string               `json:"state"`
	Bindings  []InstallBindingView `json:"bindings"`
}

// InstallBindingView mirrors the x-result binding item.
type InstallBindingView struct {
	TargetType       string `json:"targetType"`
	TargetID         string `json:"targetId"`
	CapabilityDigest string `json:"capabilityDigest"`
}

// Install enacts the verification chain then the hot registration; any
// failure registers zero capabilities. A signature/package-hash failure
// (M8-035) persists a quarantined install row for forensics - the package
// body is never physically deleted.
func (s *PluginService) Install(ctx context.Context, in InstallInput) (InstallResult, error) {
	if s == nil || s.uow == nil {
		return InstallResult{}, ErrServiceUnavailable
	}
	if in.Origin != "market" && in.Origin != "local" && in.Origin != "dev" {
		return InstallResult{}, ErrPayloadInvalid
	}
	if len(in.Source) < 1 || len(in.Source) > 512 || len(in.RequestID) < 1 || len(in.RequestID) > 128 {
		return InstallResult{}, ErrPayloadInvalid
	}
	var grant m8core.PermissionDoc
	if len(in.PermissionGrant) > 0 {
		if err := json.Unmarshal(in.PermissionGrant, &grant); err != nil {
			return InstallResult{}, ErrPayloadInvalid
		}
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	var out InstallResult
	err := s.uow.TransactPlugin(ctx, func(tx PluginTx) error {
		pkg, err := s.resolveSource(ctx, tx, in.Origin, in.Source)
		if errors.Is(err, m8core.ErrNotFound) || errors.Is(err, ErrBundleNotFound) {
			return ErrBundleNotFound
		}
		if err != nil {
			return err
		}
		installID := ulid.Make().String()
		install := m8core.PluginInstall{
			InstallID: installID, PluginID: pkg.PluginID, SubjectID: s.subject,
			Origin: in.Origin, State: m8core.InstallInstalled,
			PermissionGrantDigest: m8core.CanonicalGrantDigest(grant),
			InstalledAt:           now, UpdatedAt: now,
		}
		// M8-035: signature/package-hash failure quarantines with zero
		// registration; the quarantined row survives for forensics. A
		// malformed hash cannot satisfy the bundle CHECK, so that case
		// refuses without writing anything.
		if pkg.SignatureStatus == m8core.SignatureInvalid && m8core.ValidHexDigest(pkg.PackageHash) {
			bundleID, berr := s.ensureBundle(tx, pkg, now)
			if berr != nil {
				return berr
			}
			install.BundleID, install.State = bundleID, m8core.InstallQuarantined
			if err := tx.PutInstall(install); err != nil {
				return err
			}
			out = InstallResult{InstallID: installID, State: m8core.InstallQuarantined, Bindings: []InstallBindingView{}}
			// The quarantined forensics row commits; the chain failure is
			// answered outside the transaction.
			return nil
		}
		if !m8core.ValidHexDigest(pkg.PackageHash) {
			return ErrPluginSignatureInvalid
		}
		if err := s.verifyChain(ctx, pkg, grant); err != nil {
			return err
		}
		bundleID, err := s.ensureBundle(tx, pkg, now)
		if err != nil {
			return err
		}
		install.BundleID = bundleID
		specs, err := s.hotRegister(ctx, pkg)
		if err != nil {
			return err
		}
		if err := tx.PutInstall(install); err != nil {
			return err
		}
		out.InstallID, out.State = installID, m8core.InstallInstalled
		out.Bindings = make([]InstallBindingView, 0, len(specs))
		for _, spec := range specs {
			b := m8core.PluginCapabilityBinding{
				BindingID: ulid.Make().String(), InstallID: installID,
				TargetType: spec.TargetType, TargetID: spec.TargetID,
				CapabilityDigest: spec.CapabilityDigest,
				State:            m8core.BindingActive, CreatedAt: now,
			}
			if err := tx.PutBinding(b); err != nil {
				return err
			}
			out.Bindings = append(out.Bindings, InstallBindingView{
				TargetType: b.TargetType, TargetID: b.TargetID, CapabilityDigest: b.CapabilityDigest,
			})
		}
		_, err = tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "plugin.install",
			ResourceType: "plugin_install", ResourceID: installID,
			Actor: s.actor(in.Actor), CorrelationID: in.RequestID,
			AfterDigest: pkg.PackageHash, CreatedAt: now,
		})
		return err
	})
	if err != nil {
		return out, err
	}
	// The committed quarantine verdict answers M8-035 to the caller.
	if out.State == m8core.InstallQuarantined {
		return out, ErrPluginSignatureInvalid
	}
	return out, nil
}

// hotRegister invokes the registrar hook (or the identity default) - the
// binding rows commit in the caller's transaction.
func (s *PluginService) hotRegister(ctx context.Context, pkg PackageSource) ([]BindingSpec, error) {
	if s.register != nil {
		return s.register(ctx, pkg)
	}
	return defaultRegistrar(ctx, pkg)
}

// ensureBundle upserts the (plugin_id, semver) bundle row with the chain
// verdict state.
func (s *PluginService) ensureBundle(tx PluginTx, pkg PackageSource, now string) (string, error) {
	b := m8core.PluginBundle{
		BundleID: ulid.Make().String(), PluginID: pkg.PluginID, Semver: pkg.Semver,
		Publisher: pkg.Publisher, Kind: pkg.Kind, ManifestRef: pkg.ManifestRef,
		Entrypoint: pkg.Entrypoint, CapabilitiesJSON: pkg.Capabilities,
		PermissionsJSON: pkg.Permissions, RequiresJSON: pkg.Requires,
		PackageHash: pkg.PackageHash, SignatureStatus: pkg.SignatureStatus,
		State: m8core.PluginVerified, CreatedAt: now,
	}
	if pkg.SignatureStatus == m8core.SignatureInvalid {
		b.State = m8core.PluginQuarantined
	}
	// Reuse the existing append-only row when this version already landed.
	if prior, err := tx.GetPluginBundleBySemver(pkg.PluginID, pkg.Semver); err == nil {
		return prior.BundleID, nil
	} else if !errors.Is(err, m8core.ErrNotFound) && !errors.Is(err, ErrBundleNotFound) {
		return "", err
	}
	if err := tx.PutPluginBundle(b); err != nil {
		return "", err
	}
	return b.BundleID, nil
}

func (s *PluginService) actor(in string) string {
	if in != "" {
		return in
	}
	return s.subject
}

// PluginListItem is one plugin.list row (install joined with bundle).
type PluginListItem struct {
	InstallID    string
	PluginID     string
	Semver       string
	Publisher    string
	Kind         string
	Origin       string
	State        string
	BindingCount int
	InstalledAt  string
}

// ListResult is the plugin.list outcome.
type ListResult struct {
	Plugins []PluginListItem `json:"plugins"`
}

// List answers the per-subject install projections with optional
// kind/state filters.
func (s *PluginService) List(ctx context.Context, kind, state string) (ListResult, error) {
	if s == nil || s.uow == nil {
		return ListResult{}, ErrServiceUnavailable
	}
	if kind != "" && !m8core.ValidPluginKind(kind) {
		return ListResult{}, ErrPayloadInvalid
	}
	var out ListResult
	err := s.uow.TransactPlugin(ctx, func(tx PluginTx) error {
		items, err := tx.ListInstalls(kind, state)
		if err != nil {
			return err
		}
		out.Plugins = items
		return nil
	})
	return out, err
}

// ToggleInput is the plugin.toggle command.
type ToggleInput struct {
	InstallID string
	Enabled   bool
	Actor     string
}

// ToggleBindingView mirrors the x-result binding item.
type ToggleBindingView struct {
	TargetID string `json:"targetId"`
	State    string `json:"state"`
}

// ToggleResult is the plugin.toggle outcome.
type ToggleResult struct {
	InstallID string               `json:"installId"`
	State     string               `json:"state"`
	Bindings  []ToggleBindingView  `json:"bindings"`
}

// Toggle enacts enabled<->disabled: disabling revokes every binding
// immediately (zero-latency effect); re-enabling re-runs the hot
// registration.
func (s *PluginService) Toggle(ctx context.Context, in ToggleInput) (ToggleResult, error) {
	if s == nil || s.uow == nil {
		return ToggleResult{}, ErrServiceUnavailable
	}
	if len(in.InstallID) != 26 {
		return ToggleResult{}, ErrPayloadInvalid
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	var out ToggleResult
	err := s.uow.TransactPlugin(ctx, func(tx PluginTx) error {
		inst, err := tx.GetInstall(in.InstallID)
		if errors.Is(err, m8core.ErrNotFound) {
			return ErrInstallNotFound
		}
		if err != nil {
			return err
		}
		target := m8core.InstallDisabled
		if in.Enabled {
			target = m8core.InstallEnabled
		}
		if err := m8core.InstallTransition(inst.State, target); err != nil {
			return ErrInstallStateInvalid
		}
		if !in.Enabled {
			if _, err := tx.RevokeBindings(inst.InstallID, now); err != nil {
				return err
			}
		} else {
			// Re-enable re-runs the hot registration so the bindings sit
			// active again (M8-040 pre-checks depend on them).
			b, err := tx.GetPluginBundle(inst.BundleID)
			if err != nil {
				return err
			}
			pkg := PackageSource{
				PluginID: b.PluginID, Semver: b.Semver, Publisher: b.Publisher,
				Kind: b.Kind, ManifestRef: b.ManifestRef, Entrypoint: b.Entrypoint,
				Capabilities: b.CapabilitiesJSON, Permissions: b.PermissionsJSON,
				Requires: b.RequiresJSON, PackageHash: b.PackageHash,
				SignatureStatus: b.SignatureStatus,
			}
			specs, err := s.hotRegister(ctx, pkg)
			if err != nil {
				return err
			}
			for _, spec := range specs {
				if err := tx.PutBinding(m8core.PluginCapabilityBinding{
					BindingID: ulid.Make().String(), InstallID: inst.InstallID,
					TargetType: spec.TargetType, TargetID: spec.TargetID,
					CapabilityDigest: spec.CapabilityDigest,
					State:            m8core.BindingActive, CreatedAt: now,
				}); err != nil {
					return err
				}
			}
		}
		inst.State, inst.UpdatedAt = target, now
		if err := tx.PutInstall(inst); err != nil {
			return err
		}
		out.InstallID, out.State = inst.InstallID, target
		out.Bindings = []ToggleBindingView{}
		bindings, err := tx.ListBindings(inst.InstallID)
		if err != nil {
			return err
		}
		for _, b := range bindings {
			out.Bindings = append(out.Bindings, ToggleBindingView{TargetID: b.TargetID, State: b.State})
		}
		_, err = tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "plugin.toggle",
			ResourceType: "plugin_install", ResourceID: inst.InstallID,
			Actor: s.actor(in.Actor), CreatedAt: now,
		})
		return err
	})
	return out, err
}

// UpgradeInput is the plugin.upgrade command.
type UpgradeInput struct {
	InstallID       string
	TargetSemver    string
	PermissionGrant json.RawMessage
	Actor           string
}

// UpgradeResult is the plugin.upgrade outcome.
type UpgradeResult struct {
	InstallID          string `json:"installId"`
	FromSemver         string `json:"fromSemver"`
	ToSemver           string `json:"toSemver"`
	State              string `json:"state"`
	PermissionExpansion bool  `json:"permissionExpansion"`
}

// Upgrade enacts the versioned replacement: the new bundle re-runs the
// full chain; a permission expansion (new permissions outside the standing
// grant, M8-039) parks the install at quarantined pending review - never
// auto-enabled. Rollback is re-enabling the old bundle.
func (s *PluginService) Upgrade(ctx context.Context, in UpgradeInput) (UpgradeResult, error) {
	if s == nil || s.uow == nil {
		return UpgradeResult{}, ErrServiceUnavailable
	}
	if len(in.InstallID) != 26 || (in.TargetSemver != "" && len(in.TargetSemver) > 32) {
		return UpgradeResult{}, ErrPayloadInvalid
	}
	var grantOverride m8core.PermissionDoc
	if len(in.PermissionGrant) > 0 {
		if err := json.Unmarshal(in.PermissionGrant, &grantOverride); err != nil {
			return UpgradeResult{}, ErrPayloadInvalid
		}
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	var out UpgradeResult
	err := s.uow.TransactPlugin(ctx, func(tx PluginTx) error {
		inst, err := tx.GetInstall(in.InstallID)
		if errors.Is(err, m8core.ErrNotFound) {
			return ErrInstallNotFound
		}
		if err != nil {
			return err
		}
		if inst.State != m8core.InstallEnabled && inst.State != m8core.InstallDisabled {
			return ErrInstallStateInvalid
		}
		cur, err := tx.GetPluginBundle(inst.BundleID)
		if err != nil {
			return err
		}
		out.FromSemver = cur.Semver
		nextSemver := in.TargetSemver
		if nextSemver == "" {
			nextSemver = cur.Semver
		}
		next, err := tx.GetPluginBundleBySemver(inst.PluginID, nextSemver)
		if errors.Is(err, m8core.ErrNotFound) {
			return ErrBundleNotFound
		}
		if err != nil {
			return err
		}
		out.ToSemver = next.Semver
		if next.State != m8core.PluginVerified {
			inst.State, inst.UpdatedAt = m8core.InstallQuarantined, now
			if err := tx.PutInstall(inst); err != nil {
				return err
			}
			out.InstallID, out.State, out.PermissionExpansion = inst.InstallID, m8core.InstallQuarantined, false
			// The quarantined verdict commits; M8-035 answers outside.
			return nil
		}
		nextPerms, err := decodePermissionDoc(next.PermissionsJSON)
		if err != nil {
			return err
		}
		// Standing grant (fail-closed): an explicit grant on this call
		// re-authorizes; without one, only the identical original grant
		// digest passes - anything else is an expansion (M8-039).
		subset := false
		if grantOverride != nil {
			subset = m8core.PermissionsSubset(nextPerms, grantOverride)
		} else {
			subset = m8core.CanonicalGrantDigest(nextPerms) == inst.PermissionGrantDigest
		}
		if !subset {
			// M8-039: expansion quarantines pending review.
			if _, err := tx.RevokeBindings(inst.InstallID, now); err != nil {
				return err
			}
			inst.State, inst.BundleID, inst.UpdatedAt = m8core.InstallQuarantined, next.BundleID, now
			if err := tx.PutInstall(inst); err != nil {
				return err
			}
			out.InstallID, out.State, out.PermissionExpansion = inst.InstallID, m8core.InstallQuarantined, true
			_, aerr := tx.AppendAuditEvent(audit.Event{
				ID: ulid.Make().String(), Action: "plugin.upgrade.quarantined",
				ResourceType: "plugin_install", ResourceID: inst.InstallID,
				Actor: s.actor(in.Actor), AfterDigest: next.PackageHash, CreatedAt: now,
			})
			if aerr != nil {
				return aerr
			}
			// The quarantined verdict commits; M8-039 answers outside.
			return nil
		}
		// Clean replacement: revoke the old bindings, switch the install
		// to the new bundle at installed (never auto-enabled).
		if _, err := tx.RevokeBindings(inst.InstallID, now); err != nil {
			return err
		}
		inst.BundleID, inst.State, inst.UpdatedAt = next.BundleID, m8core.InstallInstalled, now
		if err := tx.PutInstall(inst); err != nil {
			return err
		}
		out.InstallID, out.State, out.PermissionExpansion = inst.InstallID, m8core.InstallInstalled, false
		_, err = tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "plugin.upgrade",
			ResourceType: "plugin_install", ResourceID: inst.InstallID,
			Actor: s.actor(in.Actor), AfterDigest: next.PackageHash, CreatedAt: now,
		})
		return err
	})
	if err != nil {
		return out, err
	}
	// Committed quarantine verdicts answer their chain codes to the caller.
	switch {
	case out.State == m8core.InstallQuarantined && out.PermissionExpansion:
		return out, ErrPluginPermissionExpansion
	case out.State == m8core.InstallQuarantined:
		return out, ErrPluginSignatureInvalid
	}
	return out, nil
}

// UninstallInput is the plugin.uninstall command.
type UninstallInput struct {
	InstallID    string
	ConfirmToken string
	Actor        string
}

// UninstallResult is the plugin.uninstall outcome.
type UninstallResult struct {
	InstallID       string `json:"installId"`
	State           string `json:"state"`
	RevokedBindings int    `json:"revokedBindings"`
	TombstoneID     string `json:"tombstoneId"`
}

// Uninstall enacts the one-transaction uninstall: binding revocation, the
// recursive-cleanup tombstone and the audit event commit together - any
// failure rolls the whole chain back leaving no half-uninstalled state
// (M8-041).
func (s *PluginService) Uninstall(ctx context.Context, in UninstallInput) (UninstallResult, error) {
	if s == nil || s.uow == nil {
		return UninstallResult{}, ErrServiceUnavailable
	}
	if len(in.InstallID) != 26 || !m8core.ValidHexDigest(in.ConfirmToken) {
		return UninstallResult{}, ErrPayloadInvalid
	}
	now := s.clock.Now().UTC()
	var out UninstallResult
	err := s.uow.TransactPlugin(ctx, func(tx PluginTx) error {
		inst, err := tx.GetInstall(in.InstallID)
		if errors.Is(err, m8core.ErrNotFound) {
			return ErrInstallNotFound
		}
		if err != nil {
			return err
		}
		if inst.State == m8core.InstallUninstalled {
			// Idempotent replay answers the standing terminal state.
			bindings, err := tx.ListBindings(inst.InstallID)
			if err != nil {
				return err
			}
			revoked := 0
			for _, b := range bindings {
				if b.State == m8core.BindingRevoked {
					revoked++
				}
			}
			out = UninstallResult{InstallID: inst.InstallID, State: m8core.InstallUninstalled, RevokedBindings: revoked}
			return nil
		}
		if err := m8core.InstallTransition(inst.State, m8core.InstallUninstalled); err != nil {
			return ErrInstallStateInvalid
		}
		bindings, err := tx.ListBindings(inst.InstallID)
		if err != nil {
			return err
		}
		for _, b := range bindings {
			if b.State != m8core.BindingActive {
				continue
			}
			if s.revoke != nil {
				if err := s.revoke(ctx, BindingSpec{
					TargetType: b.TargetType, TargetID: b.TargetID,
					CapabilityDigest: b.CapabilityDigest,
				}); err != nil {
					return fmt.Errorf("%w: revoke %s: %v", ErrPluginUninstallConflict, b.TargetID, err)
				}
			}
		}
		revoked, err := tx.RevokeBindings(inst.InstallID, now.Format(time.RFC3339))
		if err != nil {
			return fmt.Errorf("%w: bindings: %v", ErrPluginUninstallConflict, err)
		}
		inst.State, inst.UpdatedAt = m8core.InstallUninstalled, now.Format(time.RFC3339)
		if err := tx.PutInstall(inst); err != nil {
			return fmt.Errorf("%w: install: %v", ErrPluginUninstallConflict, err)
		}
		tomb := m8core.Tombstone{
			ID:            ulid.Make().String(),
			RootRef:       "install:" + inst.InstallID,
			CascadeCursor: "{}", AckSet: "[]",
			State:         m8core.TombPropagating,
			CreatedAt:     now.Format(time.RFC3339),
		}
		if err := tx.PutTombstone(tomb); err != nil {
			return fmt.Errorf("%w: tombstone: %v", ErrPluginUninstallConflict, err)
		}
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "plugin.uninstall",
			ResourceType: "plugin_install", ResourceID: inst.InstallID,
			Actor: s.actor(in.Actor), AfterDigest: m8core.DigestOf(tomb.ID),
			CreatedAt: now.Format(time.RFC3339),
		}); err != nil {
			return fmt.Errorf("%w: audit: %v", ErrPluginUninstallConflict, err)
		}
		out = UninstallResult{
			InstallID: inst.InstallID, State: m8core.InstallUninstalled,
			RevokedBindings: revoked, TombstoneID: tomb.ID,
		}
		return nil
	})
	return out, err
}

// DevCreateInput is the plugin.dev.create command.
type DevCreateInput struct {
	WorkspaceID string
	Manifest    map[string]any
	Entrypoint  string
}

// DevCreateResult is the plugin.dev.create outcome.
type DevCreateResult struct {
	BundleID string `json:"bundleId"`
	State    string `json:"state"`
}

// devManifest is the typed view of the caller-supplied manifest.
type devManifest struct {
	ID           string          `json:"id"`
	Semver       string          `json:"semver"`
	Version      string          `json:"version"`
	Publisher    string          `json:"publisher"`
	Kind         string          `json:"kind"`
	Capabilities json.RawMessage `json:"capabilities"`
	Permissions  json.RawMessage `json:"permissions"`
	Requires     json.RawMessage `json:"requires"`
}

// DevCreate stages a dev-workspace bundle. The source is unconfirmed, so
// the signature stays unverified and the bundle lands quarantined until
// review releases it - never auto-installed.
func (s *PluginService) DevCreate(ctx context.Context, in DevCreateInput) (DevCreateResult, error) {
	if s == nil || s.uow == nil {
		return DevCreateResult{}, ErrServiceUnavailable
	}
	if len(in.WorkspaceID) < 1 || len(in.WorkspaceID) > 128 ||
		len(in.Entrypoint) < 1 || len(in.Entrypoint) > 512 || in.Manifest == nil {
		return DevCreateResult{}, ErrPayloadInvalid
	}
	raw, err := json.Marshal(in.Manifest)
	if err != nil {
		return DevCreateResult{}, ErrPayloadInvalid
	}
	var m devManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return DevCreateResult{}, ErrPluginManifestInvalid
	}
	semver := m.Semver
	if semver == "" {
		semver = m.Version
	}
	if len(m.Capabilities) == 0 {
		m.Capabilities = json.RawMessage(`[]`)
	}
	if len(m.Permissions) == 0 {
		m.Permissions = json.RawMessage(`{}`)
	}
	if len(m.Requires) == 0 {
		m.Requires = json.RawMessage(`{}`)
	}
	if err := m8core.ValidateManifest(m.ID, semver, m.Publisher, m.Kind, string(m.Capabilities)); err != nil {
		return DevCreateResult{}, fmt.Errorf("%w: %v", ErrPluginManifestInvalid, err)
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	manifestRef := "devworkspace://" + in.WorkspaceID + "/" + m.ID
	pkg := PackageSource{
		PluginID: m.ID, Semver: semver, Publisher: m.Publisher, Kind: m.Kind,
		ManifestRef: manifestRef, Entrypoint: in.Entrypoint,
		Capabilities: string(m.Capabilities), Permissions: string(m.Permissions),
		Requires: string(m.Requires),
		PackageHash: m8core.DigestOf(string(raw) + "|" + in.Entrypoint),
		SignatureStatus: m8core.SignatureUnverified,
	}
	var out DevCreateResult
	err = s.uow.TransactPlugin(ctx, func(tx PluginTx) error {
		bundleID, err := s.ensureBundle(tx, pkg, now)
		if err != nil {
			return err
		}
		out = DevCreateResult{BundleID: bundleID, State: m8core.PluginQuarantined}
		_, err = tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "plugin.dev.create",
			ResourceType: "plugin_bundle", ResourceID: bundleID,
			Actor: s.subject, AfterDigest: pkg.PackageHash, CreatedAt: now,
		})
		return err
	})
	return out, err
}

// CheckBinding enacts the M8-040 capability-call pre-check: the call
// proceeds only against an active binding, otherwise refused with zero
// side effects.
func (s *PluginService) CheckBinding(ctx context.Context, installID, targetID string) error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	var found bool
	err := s.uow.TransactPlugin(ctx, func(tx PluginTx) error {
		bindings, err := tx.ListBindings(installID)
		if err != nil {
			return err
		}
		for _, b := range bindings {
			if b.TargetID == targetID && b.State == m8core.BindingActive {
				found = true
				return nil
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !found {
		return ErrBindingInactive
	}
	return nil
}
