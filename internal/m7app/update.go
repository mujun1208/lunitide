// M7 slice 5 application service (T-7.5.1): the AppUpdate split-track.
// The app-update domain is physically isolated from the project release
// domain (02-技术设计 §05): UpdateTx never touches release_* tables, the
// appUpdate.* namespace rejects every project release ID, and package
// manifests are JCS-canonical signed documents whose nonce is consumed
// exactly once. Normal updates never downgrade: the target version must be
// >= the device's last succeeded version and >= the manifest min_version.
// Install failures auto-rollback through append-only attempts (RBK-002
// semantics); a failed rollback freezes the install (RBK-001 semantics).
package m7app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

var (
	// ErrUpdateNotFound: referenced update package missing.
	ErrUpdateNotFound = errors.New("m7app: update package not found")
	// ErrUpdateChannelInvalid: unknown or retired channel name.
	ErrUpdateChannelInvalid = errors.New("m7app: update channel invalid")
	// ErrUpdatePackageExists: the channel already carries that version.
	ErrUpdatePackageExists = errors.New("m7app: update package already exists")
	// ErrUpdateNotPublished: only published packages install.
	ErrUpdateNotPublished = errors.New("m7app: update package not published")
	// ErrUpdateWindowClosed: trusted now is outside [not_before, expires_at].
	ErrUpdateWindowClosed = errors.New("m7app: update window closed")
	// ErrUpdateSignature: digest mismatch or manifest signature verification
	// failed (M7-UPD-001 - install is forbidden).
	ErrUpdateSignature = errors.New("m7app: update signature or digest invalid")
	// ErrUpdateDowngrade: target version is below the device version or the
	// manifest min_version (normal updates never downgrade).
	ErrUpdateDowngrade = errors.New("m7app: update would downgrade")
	// ErrNonceReplayed: the manifest nonce was already consumed.
	ErrNonceReplayed = errors.New("m7app: update nonce already consumed")
	// ErrUpdateInstallFailed: the installer adapter failed a step.
	ErrUpdateInstallFailed = errors.New("m7app: update install failed")
	// ErrUpdateRollbackFailed: the auto-rollback failed - the installation
	// stays failed and requires manual disposal (RBK-001 semantics).
	ErrUpdateRollbackFailed = errors.New("m7app: update rollback failed")
	// ErrIllegalInstallationTransition: transition outside the canonical
	// installation state machine.
	ErrIllegalInstallationTransition = errors.New("m7app: illegal installation transition")
)

// UpdateTx is the slice-5 single-writer transaction. It only addresses the
// app-update split-track tables plus the shared audit ledger - never the
// project release domain.
type UpdateTx interface {
	GetChannelByName(name string) (m7flow.UpdateChannel, error)
	PutUpdatePackage(m7flow.UpdatePackage) error
	GetUpdatePackage(id string) (m7flow.UpdatePackage, error)
	FindPackageByChannelVersion(channelID, appVersion string) (m7flow.UpdatePackage, error)
	FindLatestPublishedPackage(channelID string, now time.Time) (m7flow.UpdatePackage, error)
	PutUpdateInstallation(m7flow.UpdateInstallation) error
	GetUpdateInstallation(id string) (m7flow.UpdateInstallation, error)
	FindInstallationByDevicePackage(deviceID, packageID string) (m7flow.UpdateInstallation, error)
	FindLastSucceededVersion(deviceID string) (string, error)
	UpdateInstallationState(id, from, to string, completedAt *time.Time) error
	PutUpdateReceipt(m7flow.UpdateReceipt) error
	PutUpdateRollbackAttempt(m7flow.UpdateRollbackAttempt) error
	UpdateRollbackAttemptState(id, from, to, resultJSON string, completedAt *time.Time) error
	ConsumeNonce(nonce string, consumedAt time.Time) (replayed bool, err error)
	AppendAuditEvent(e audit.Event) (audit.Event, error)
	LastAuditEvent() (audit.Event, error)
	ListAuditEvents() ([]audit.Event, error)
}

// UpdateUnitOfWork is the slice-5 single-writer boundary.
type UpdateUnitOfWork interface {
	TransactUpdate(ctx context.Context, fn func(UpdateTx) error) error
}

// UpdateInstaller is the install engine port (never a bridge handler): the
// steps run inside the installation state machine and must be idempotent on
// installationID + package digest.
type UpdateInstaller interface {
	Download(ctx context.Context, installationID, packageID, digest string) error
	Install(ctx context.Context, installationID, packageID, digest string) error
	Verify(ctx context.Context, installationID, digest string) error
	Rollback(ctx context.Context, installationID string) error
}

// LocalUpdateInstaller is the deterministic in-process installer: every step
// is a recorded no-op that succeeds. Real deployments inject the desktop
// updater implementation.
type LocalUpdateInstaller struct{}

func (LocalUpdateInstaller) Download(context.Context, string, string, string) error { return nil }
func (LocalUpdateInstaller) Install(context.Context, string, string, string) error { return nil }
func (LocalUpdateInstaller) Verify(context.Context, string, string) error         { return nil }
func (LocalUpdateInstaller) Rollback(context.Context, string) error               { return nil }

// ── service ─────────────────────────────────────────────────────────────────

// UpdateService implements appUpdate.check / appUpdate.install plus the
// internal publish path that feeds the channel (bridge-visible methods stay
// limited to the two read/install verbs per the wire contract).
type UpdateService struct {
	uow       UpdateUnitOfWork
	clock     Clock
	signer    ReleaseSigner
	installer UpdateInstaller
}

func NewUpdateService(uow UpdateUnitOfWork) *UpdateService {
	return &UpdateService{uow: uow, clock: systemClock{}, signer: NewLocalMACSigner(), installer: LocalUpdateInstaller{}}
}

func (s *UpdateService) SetClock(c Clock) { s.clock = c }

// SetSigner substitutes the manifest signer (tests / future signing service).
func (s *UpdateService) SetSigner(sig ReleaseSigner) { s.signer = sig }

// SetInstaller substitutes the install engine port (tests, real updater).
func (s *UpdateService) SetInstaller(i UpdateInstaller) { s.installer = i }

// PublishInput is the internal publish command (management plane, not a
// bridge method).
type PublishInput struct {
	Channel       string
	AppVersion    string
	MinVersion    string
	PackageDigest string
	PackageBody   string
	NotBefore     time.Time
	ExpiresAt     time.Time
	KeyID         string
	Actor         string
}

// Publish seals and publishes one update package on a channel. The manifest
// signature covers the JCS-canonical document; the nonce is minted here and
// consumed exactly once by the first install.
func (s *UpdateService) Publish(ctx context.Context, in PublishInput) (m7flow.UpdatePackage, error) {
	if s == nil || s.uow == nil {
		return m7flow.UpdatePackage{}, ErrServiceUnavailable
	}
	if in.Channel != m7flow.ChannelStable && in.Channel != m7flow.ChannelBeta {
		return m7flow.UpdatePackage{}, fmt.Errorf("%w: %q", ErrUpdateChannelInvalid, in.Channel)
	}
	if _, ok := m7flow.ParseVersion(in.AppVersion); !ok {
		return m7flow.UpdatePackage{}, fmt.Errorf("%w: appVersion %q not numeric", ErrUpdateDowngrade, in.AppVersion)
	}
	if _, ok := m7flow.ParseVersion(in.MinVersion); !ok {
		return m7flow.UpdatePackage{}, fmt.Errorf("%w: minVersion %q not numeric", ErrUpdateDowngrade, in.MinVersion)
	}
	// The digest must be the SHA-256 of the shipped body (self-verifying).
	if got := m7flow.SHA256Hex([]byte(in.PackageBody)); got != in.PackageDigest {
		return m7flow.UpdatePackage{}, fmt.Errorf("%w: packageDigest does not match body", ErrUpdateSignature)
	}
	if !in.NotBefore.Before(in.ExpiresAt) {
		return m7flow.UpdatePackage{}, fmt.Errorf("%w: notBefore must precede expiresAt", ErrUpdateWindowClosed)
	}
	keyID := in.KeyID
	if keyID == "" {
		keyID = s.signer.KeyID()
	}
	var out m7flow.UpdatePackage
	err := s.uow.TransactUpdate(ctx, func(tx UpdateTx) error {
		ch, err := tx.GetChannelByName(in.Channel)
		if err != nil {
			return fmt.Errorf("%w: %q", ErrUpdateChannelInvalid, in.Channel)
		}
		if existing, err := tx.FindPackageByChannelVersion(ch.ID, in.AppVersion); err == nil {
			return fmt.Errorf("%w: package %s", ErrUpdatePackageExists, existing.ID)
		} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
			return err
		}
		now := s.clock.Now().UTC()
		nonceBytes := make([]byte, 16)
		if _, err := rand.Read(nonceBytes); err != nil {
			return err
		}
		pkg := m7flow.UpdatePackage{
			ID: ulid.Make().String(), ChannelID: ch.ID,
			AppVersion: in.AppVersion, MinVersion: in.MinVersion,
			PackageDigest: in.PackageDigest, Nonce: hex.EncodeToString(nonceBytes),
			NotBefore: in.NotBefore.UTC(), ExpiresAt: in.ExpiresAt.UTC(),
			KeyID: keyID, State: m7flow.UpdBuilding, CreatedAt: now,
		}
		// Seal: sign the canonical manifest, then flip to published.
		pkg.Signature = s.signer.Sign(m7flow.ManifestOf(pkg).Canonical())
		pkg.State = m7flow.UpdPublished
		if err := tx.PutUpdatePackage(pkg); err != nil {
			return err
		}
		if _, err := s.recordAudit(tx, audit.Event{
			ID: ulid.Make().String(), Action: "app_update.published",
			ResourceType: "update_package", ResourceID: pkg.ID,
			Actor: in.Actor, AfterDigest: pkg.PackageDigest,
			CorrelationID: pkg.ID, CreatedAt: m7RFC3339(now),
		}); err != nil {
			return err
		}
		out = pkg
		return nil
	})
	if err != nil {
		return m7flow.UpdatePackage{}, err
	}
	return out, nil
}

// m7RFC3339 renders a UTC RFC3339 timestamp for ledger rows.
func m7RFC3339(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// recordAudit seals and appends one ledger event inside the caller tx.
func (s *UpdateService) recordAudit(tx UpdateTx, e audit.Event) (audit.Event, error) {
	return tx.AppendAuditEvent(e)
}

// CheckResult is the appUpdate.check projection.
type CheckResult struct {
	UpdateID  string
	Version   string
	Digest    string
	Mandatory bool
}

// Check answers the newest applicable update of one channel for a device on
// currentVersion. An empty UpdateID means "up to date".
func (s *UpdateService) Check(ctx context.Context, channel, currentVersion string) (CheckResult, error) {
	if s == nil || s.uow == nil {
		return CheckResult{}, ErrServiceUnavailable
	}
	if channel != m7flow.ChannelStable && channel != m7flow.ChannelBeta {
		return CheckResult{}, fmt.Errorf("%w: %q", ErrUpdateChannelInvalid, channel)
	}
	if currentVersion != "" {
		if _, ok := m7flow.ParseVersion(currentVersion); !ok {
			return CheckResult{}, fmt.Errorf("%w: currentVersion %q not numeric", ErrUpdateDowngrade, currentVersion)
		}
	}
	var out CheckResult
	err := s.uow.TransactUpdate(ctx, func(tx UpdateTx) error {
		ch, err := tx.GetChannelByName(channel)
		if err != nil {
			return fmt.Errorf("%w: %q", ErrUpdateChannelInvalid, channel)
		}
		if ch.State != m7flow.ChActive {
			return fmt.Errorf("%w: channel retired", ErrUpdateChannelInvalid)
		}
		pkg, err := tx.FindLatestPublishedPackage(ch.ID, s.clock.Now().UTC())
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, m7flow.ErrNotFound) {
			return nil // no in-window package: up to date
		}
		if err != nil {
			return err
		}
		// A version at or below the device version is not an update.
		if currentVersion != "" && m7flow.CompareVersions(pkg.AppVersion, currentVersion) <= 0 {
			return nil
		}
		out = CheckResult{UpdateID: pkg.ID, Version: pkg.AppVersion, Digest: pkg.PackageDigest}
		// Mandatory when the device fell below the supported floor.
		if currentVersion != "" && m7flow.CompareVersions(currentVersion, pkg.MinVersion) < 0 {
			out.Mandatory = true
		}
		return nil
	})
	if err != nil {
		return CheckResult{}, err
	}
	return out, nil
}

// InstallInput is the appUpdate.install command.
type InstallInput struct {
	UpdateID       string
	ExpectedDigest string
	DeviceID       string
}

// Install drives one device installation: pending -> downloading ->
// installing -> succeeded, or failed -> rolled_back through the append-only
// auto-rollback path. The response state is the wire projection
// installed | rolled_back.
func (s *UpdateService) Install(ctx context.Context, in InstallInput) (string, error) {
	if s == nil || s.uow == nil {
		return "", ErrServiceUnavailable
	}
	device := in.DeviceID
	if device == "" {
		device = "local"
	}
	var wireState string
	var typedErr error // failure whose durable rows must still commit
	err := s.uow.TransactUpdate(ctx, func(tx UpdateTx) error {
		state, err := s.installTx(ctx, tx, in, device)
		wireState = state
		// A rolled-back install is a durable, wire-visible outcome - commit
		// the failed/rolled_back rows and still answer rolled_back; only a
		// failed rollback aborts to a typed error (also committed).
		if errors.Is(err, ErrUpdateInstallFailed) {
			typedErr = err
			return nil
		}
		return err
	})
	if err != nil {
		return "", err
	}
	return wireState, typedErr
}

func (s *UpdateService) installTx(ctx context.Context, tx UpdateTx, in InstallInput, device string) (string, error) {
	// Idempotent replay: a terminal installation of this device+package
	// answers its recorded outcome.
	if prior, err := tx.FindInstallationByDevicePackage(device, in.UpdateID); err == nil {
		switch prior.State {
		case m7flow.UpdSucceeded:
			return m7flow.WireInstalled, nil
		case m7flow.UpdRolledBack:
			return m7flow.UpdWireRolledBack, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
		return "", err
	}

	pkg, err := tx.GetUpdatePackage(in.UpdateID)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, m7flow.ErrNotFound) {
		return "", ErrUpdateNotFound
	} else if err != nil {
		return "", err
	}
	if pkg.State != m7flow.UpdPublished {
		return "", fmt.Errorf("%w: state %s", ErrUpdateNotPublished, pkg.State)
	}
	// M7-UPD-001: the digest the client pinned must equal the stored digest
	// and the canonical manifest must re-verify against the signature.
	if in.ExpectedDigest != pkg.PackageDigest {
		return "", fmt.Errorf("%w: expectedDigest mismatch", ErrUpdateSignature)
	}
	if !s.signer.Verify(m7flow.ManifestOf(pkg).Canonical(), pkg.Signature) {
		return "", fmt.Errorf("%w: manifest signature", ErrUpdateSignature)
	}
	now := s.clock.Now().UTC()
	if now.Before(pkg.NotBefore) || !now.Before(pkg.ExpiresAt) {
		return "", fmt.Errorf("%w: trusted now outside [notBefore, expiresAt]", ErrUpdateWindowClosed)
	}
	// Normal updates never downgrade: target >= device version and target
	// >= manifest min_version.
	if last, err := tx.FindLastSucceededVersion(device); err == nil && m7flow.CompareVersions(pkg.AppVersion, last) < 0 {
		return "", fmt.Errorf("%w: device at %s", ErrUpdateDowngrade, last)
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, m7flow.ErrNotFound) {
		return "", err
	}
	if m7flow.CompareVersions(pkg.AppVersion, pkg.MinVersion) < 0 {
		return "", fmt.Errorf("%w: below min_version %s", ErrUpdateDowngrade, pkg.MinVersion)
	}
	// Nonce single-consumption: replaying a consumed manifest is refused.
	if replayed, err := tx.ConsumeNonce(pkg.Nonce, now); err != nil {
		return "", err
	} else if replayed {
		return "", ErrNonceReplayed
	}

	inst := m7flow.UpdateInstallation{
		ID: ulid.Make().String(), PackageID: pkg.ID, DeviceID: device,
		State: m7flow.UpdPending, CreatedAt: now,
	}
	if err := tx.PutUpdateInstallation(inst); err != nil {
		return "", err
	}
	if _, err := s.recordAudit(tx, audit.Event{
		ID: ulid.Make().String(), Action: "app_update.install_started",
		ResourceType: "update_installation", ResourceID: inst.ID,
		Actor: device, AfterDigest: pkg.PackageDigest,
		CorrelationID: inst.ID, CreatedAt: m7RFC3339(now),
	}); err != nil {
		return "", err
	}

	step := func(to string, completed bool) error {
		if !m7flow.LegalInstallationTransition(inst.State, to) {
			return fmt.Errorf("%w: %s -> %s", ErrIllegalInstallationTransition, inst.State, to)
		}
		var done *time.Time
		at := s.clock.Now().UTC()
		if completed {
			done = &at
		}
		if err := tx.UpdateInstallationState(inst.ID, inst.State, to, done); err != nil {
			return err
		}
		inst.State = to
		return nil
	}
	fail := func(cause error) (string, error) {
		_ = step(m7flow.UpdFailed, true)
		_, _ = s.recordAudit(tx, audit.Event{
			ID: ulid.Make().String(), Action: "app_update.install_failed",
			ResourceType: "update_installation", ResourceID: inst.ID,
			Actor: device, AfterDigest: pkg.PackageDigest,
			CorrelationID: inst.ID, CreatedAt: m7RFC3339(s.clock.Now().UTC()),
		})
		wire, rbErr := s.autoRollback(ctx, tx, &inst, device, cause)
		if rbErr != nil {
			return "", rbErr // rollback failed: frozen (RBK-001 semantics)
		}
		return wire, fmt.Errorf("%w: %v", ErrUpdateInstallFailed, cause)
	}

	if err := step(m7flow.UpdDownloading, false); err != nil {
		return "", err
	}
	if err := s.installer.Download(ctx, inst.ID, pkg.ID, pkg.PackageDigest); err != nil {
		return fail(err)
	}
	if err := step(m7flow.UpdInstalling, false); err != nil {
		return "", err
	}
	if err := s.installer.Install(ctx, inst.ID, pkg.ID, pkg.PackageDigest); err != nil {
		return fail(err)
	}
	if err := s.installer.Verify(ctx, inst.ID, pkg.PackageDigest); err != nil {
		return fail(err)
	}
	if err := step(m7flow.UpdSucceeded, true); err != nil {
		return "", err
	}
	// Append-only success receipt.
	done := s.clock.Now().UTC()
	receiptJSON := mustJSON(map[string]string{
		"installationId": inst.ID, "packageId": pkg.ID, "version": pkg.AppVersion,
		"digest": pkg.PackageDigest, "installedAt": m7RFC3339(done),
	})
	sum := sha256.Sum256([]byte(receiptJSON))
	if err := tx.PutUpdateReceipt(m7flow.UpdateReceipt{
		ID: ulid.Make().String(), InstallationID: inst.ID,
		ReceiptJSON: receiptJSON, Digest: hex.EncodeToString(sum[:]), CreatedAt: done,
	}); err != nil {
		return "", err
	}
	if _, err := s.recordAudit(tx, audit.Event{
		ID: ulid.Make().String(), Action: "app_update.install_succeeded",
		ResourceType: "update_installation", ResourceID: inst.ID,
		Actor: device, AfterDigest: pkg.PackageDigest,
		CorrelationID: inst.ID, CreatedAt: m7RFC3339(done),
	}); err != nil {
		return "", err
	}
	return m7flow.WireInstalled, nil
}

// autoRollback drives the append-only rollback attempt of one failed
// installation to rolled_back (attempt state machine pending -> running ->
// succeeded | failed; rows are never deleted).
func (s *UpdateService) autoRollback(ctx context.Context, tx UpdateTx, inst *m7flow.UpdateInstallation, device string, cause error) (string, error) {
	now := s.clock.Now().UTC()
	attempt := m7flow.UpdateRollbackAttempt{
		ID: ulid.Make().String(), InstallationID: inst.ID,
		State: m7flow.UpdRbPending, OperatorID: device,
		ResultJSON: mustJSON(map[string]string{"cause": cause.Error()}), CreatedAt: now,
	}
	if err := tx.PutUpdateRollbackAttempt(attempt); err != nil {
		return "", err
	}
	transition := func(from, to, resultJSON string, done *time.Time) error {
		if !m7flow.LegalUpdateRollbackTransition(from, to) {
			return fmt.Errorf("%w: %s -> %s", ErrIllegalInstallationTransition, from, to)
		}
		return tx.UpdateRollbackAttemptState(attempt.ID, from, to, resultJSON, done)
	}
	if err := transition(attempt.State, m7flow.UpdRbRunning, attempt.ResultJSON, nil); err != nil {
		return "", err
	}
	attempt.State = m7flow.UpdRbRunning
	finishing := s.clock.Now().UTC()
	if err := s.installer.Rollback(ctx, inst.ID); err != nil {
		// The failed attempt is durable audit evidence (RBK-001 semantics);
		// surface the freeze after commit.
		_ = transition(attempt.State, m7flow.UpdRbFailed, mustJSON(map[string]string{"cause": cause.Error(), "error": err.Error()}), &finishing)
		_, _ = s.recordAudit(tx, audit.Event{
			ID: ulid.Make().String(), Action: "app_update.rollback_failed",
			ResourceType: "update_installation", ResourceID: inst.ID,
			Actor: device, CorrelationID: inst.ID, CreatedAt: m7RFC3339(finishing),
		})
		return "", fmt.Errorf("%w: %v", ErrUpdateRollbackFailed, err)
	}
	if err := transition(attempt.State, m7flow.UpdRbSucceeded, mustJSON(map[string]string{"operator": device}), &finishing); err != nil {
		return "", err
	}
	// installing|failed -> rolled_back is legal from both entry points.
	if !m7flow.LegalInstallationTransition(inst.State, m7flow.UpdRolledBack) {
		return "", fmt.Errorf("%w: %s -> rolled_back", ErrIllegalInstallationTransition, inst.State)
	}
	doneAt := finishing
	if err := tx.UpdateInstallationState(inst.ID, inst.State, m7flow.UpdRolledBack, &doneAt); err != nil {
		return "", err
	}
	inst.State = m7flow.UpdRolledBack
	if _, err := s.recordAudit(tx, audit.Event{
		ID: ulid.Make().String(), Action: "app_update.rollback_succeeded",
		ResourceType: "update_installation", ResourceID: inst.ID,
		Actor: device, CorrelationID: inst.ID, CreatedAt: m7RFC3339(finishing),
	}); err != nil {
		return "", err
	}
	return m7flow.UpdWireRolledBack, nil
}

// VerifyAuditChain proves the append-only ledger: seq continuity, prev-hash
// linkage and recomputed event hashes. A break answers audit.ErrChainBroken
// and the bridge layer freezes production promotions (M7-DR-001).
func (s *UpdateService) VerifyAuditChain(ctx context.Context) error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	var events []audit.Event
	err := s.uow.TransactUpdate(ctx, func(tx UpdateTx) error {
		var err error
		events, err = tx.ListAuditEvents()
		return err
	})
	if err != nil {
		return err
	}
	return audit.VerifyChain(events)
}