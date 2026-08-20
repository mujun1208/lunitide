// Package m6app implements the M6 slice-1 application services: the
// third-party extension supply chain (extension.search / extension.install /
// extension.lifecycle, T-6.1.2/T-6.1.3) and the MCP endpoint persistence
// adapter (T-6.1.4). Mutations share the agent-runtime single-writer
// transaction so the install row, its audit record and the idempotency
// record commit atomically; quarantined verdicts are persisted too — a
// failed verification is a durable fact, not a transient error.
package m6app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/extension"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

var (
	ErrServiceUnavailable = errors.New("m6app: unit of work unavailable")
	ErrArtifactNotFound   = errors.New("m6app: artifact not found")
	ErrVersionMismatch    = errors.New("m6app: artifact version mismatch")
	ErrArtifactUnverified = errors.New("m6app: artifact signature state is not verified")
	ErrInstallNotFound    = errors.New("m6app: install not found")
	ErrBadLifecycleOp     = errors.New("m6app: lifecycle op invalid for current state")
	ErrTargetRequired     = errors.New("m6app: targetVersion required")
	ErrSubjectRequired    = errors.New("m6app: subject required")
)

// Tx is the agent-runtime transaction extended with the M6 supply-chain
// tables. sqlite.agentRuntimeTx satisfies it; TransactM6Supply enforces the
// assertion at open time.
type Tx interface {
	Idempotency(op, key string, now time.Time) (providerapp.Record, bool, error)
	PutIdempotency(providerapp.Record) error
	PutAudit(providerapp.Audit) error

	PutM6Artifact(m6supply.Artifact) error
	GetM6Artifact(id string) (m6supply.Artifact, error)
	SearchM6Artifacts(query, publisher string, maxRiskRank int, limit int) ([]m6supply.Artifact, error)
	FindM6Artifact(publisher, name, version string) (m6supply.Artifact, error)

	PutM6Install(m6supply.Install) error
	GetM6Install(id string) (m6supply.Install, error)
	FindM6Install(subject, artifactID string) (m6supply.Install, error)
	ListM6Installs(subject string) ([]m6supply.Install, error)
	TransitionM6Install(id string, expectedVersion int64, to m6supply.InstallState, at time.Time) (m6supply.Install, error)
	RepointM6Install(id string, expectedVersion int64, artifactID string, at time.Time) (m6supply.Install, error)

	PutM6Endpoint(m6supply.Endpoint) error
	GetM6Endpoint(id string) (m6supply.Endpoint, error)
	UpdateM6EndpointState(id string, state string, at time.Time) error
	ListM6Endpoints() ([]m6supply.Endpoint, error)

	// Slice-2 (T-6.2.x): connector metadata snapshots + cloud task rows.
	MaxM6ConnectorSnapshotVersion(connectorID string) (int64, error)
	PutM6ConnectorSnapshot(m6supply.ConnectorSnapshot) error
	GetM6CloudTaskByIdempotencyKey(key string) (m6supply.CloudTask, error)
	PutM6CloudTask(m6supply.CloudTask) error

	// Slice-3 (T-6.3.x): delegation envelopes, budget ledger, join barriers.
	GetM6BudgetAccount(rootID, dimension string) (m6supply.BudgetAccount, error)
	PutM6BudgetAccount(m6supply.BudgetAccount) error
	UpdateM6BudgetAccountBalances(id string, expectedVersion int64, granted, reserved, consumed, refundable int64, at time.Time) (m6supply.BudgetAccount, error)
	ListM6BudgetAccounts(rootID string) ([]m6supply.BudgetAccount, error)
	GetM6Delegation(id string) (m6supply.Delegation, error)
	PutM6Delegation(m6supply.Delegation) error
	CountActiveM6DelegationsByParent(rootID, parentID string) (int64, error)
	CountActiveM6DelegationsByRoot(rootID string) (int64, error)
	UpdateM6DelegationState(id string, expectedVersion int64, to string, at time.Time, settledAt *time.Time) (m6supply.Delegation, error)
	GetM6Barrier(id string) (m6supply.Barrier, error)
	FindOpenM6BarrierByRoot(rootID string) (m6supply.Barrier, error)
	PutM6Barrier(m6supply.Barrier) error
	CloseM6Barrier(id string, expectedVersion int64, reason string, at time.Time) (m6supply.Barrier, error)
	PutM6BarrierArrival(m6supply.BarrierArrival) error
	GetM6BarrierArrival(barrierID, childID string) (m6supply.BarrierArrival, error)
	ListM6BarrierArrivals(barrierID string) ([]m6supply.BarrierArrival, error)

	// Slice-4 (T-6.4.x): root-writer merge intents + transactional outbox.
	GetM6MergeIntent(id string) (m6supply.MergeIntent, error)
	GetM6MergeIntentBySequence(rootID string, sequence int64) (m6supply.MergeIntent, error)
	PutM6MergeIntent(m6supply.MergeIntent) error
	UpdateM6MergeIntentState(id string, expectedVersion int64, to string, currentHead *string, at time.Time) (m6supply.MergeIntent, error)
	UpdateM6MergeIntentRebased(id string, expectedVersion int64, newExpectedHead string, at time.Time) (m6supply.MergeIntent, error)
	ListM6MergeIntentsByRoot(rootID string) ([]m6supply.MergeIntent, error)
	AppendM6Outbox(m6supply.OutboxEvent) error
	ListUnpublishedM6Outbox(limit int) ([]m6supply.OutboxEvent, error)
	CountUnpublishedM6Outbox() (int64, error)
	MarkM6OutboxPublished(ids []string, at time.Time) error
	PruneM6Outbox(publishedBefore time.Time) (int64, error)

	// Legacy S5 governance (0053): credential refs, integrations, api
	// operations, field mappings.
	PutM6CredentialRef(m6supply.CredentialRef) error
	GetM6CredentialRef(id string) (m6supply.CredentialRef, error)
	RevokeM6CredentialRef(id string, expectedVersion int64, at time.Time) (m6supply.CredentialRef, error)

	PutM6Integration(m6supply.Integration) error
	GetM6Integration(id string) (m6supply.Integration, error)
	FindM6IntegrationByName(name, specVersion string) (m6supply.Integration, error)
	TransitionM6Integration(id string, expectedVersion int64, to string, at time.Time) (m6supply.Integration, error)
	ListM6Integrations() ([]m6supply.Integration, error)

	PutM6ApiOperation(m6supply.ApiOperation) error
	GetM6ApiOperation(id string) (m6supply.ApiOperation, error)
	FindM6ApiOperationByOperationID(integrationID, operationID string) (m6supply.ApiOperation, error)
	SetM6ApiOperationEnabled(id string, expectedVersion int64, enabled bool, at time.Time) (m6supply.ApiOperation, error)
	ListM6ApiOperations(integrationID string) ([]m6supply.ApiOperation, error)

	PutM6FieldMapping(m6supply.FieldMapping) error
	FindM6FieldMapping(operationRowID, source, target, direction string) (m6supply.FieldMapping, error)
	ListM6FieldMappings(operationRowID string) ([]m6supply.FieldMapping, error)

	// Health samples & call logs (0053, append-only via triggers).
	GetM6CallLog(id string) (m6supply.CallLog, error)
	ListM6CallLogs(integrationID string, limit int) ([]m6supply.CallLog, error)
	ListM6HealthSamples(integrationID string, since time.Time, limit int) ([]m6supply.HealthSample, error)
	PutM6CallLog(c m6supply.CallLog) error
	PutM6HealthSample(s m6supply.HealthSample) error

	// Skill entity chain & import pipeline (0053).
	FindM6ImportCandidate(sourceURL, immutableCommit string) (m6supply.ImportCandidate, error)
	FindM6SkillByName(name string) (m6supply.Skill, error)
	FindM6SkillInstall(skillVersionID, workspaceID string) (m6supply.SkillInstall, error)
	FindM6SkillVersion(skillID, semver string) (m6supply.SkillVersion, error)
	GetM6ImportCandidate(id string) (m6supply.ImportCandidate, error)
	GetM6Skill(id string) (m6supply.Skill, error)
	GetM6SkillVersion(id string) (m6supply.SkillVersion, error)
	ListM6SkillDependencies(skillVersionID string) ([]m6supply.SkillDependency, error)
	PutM6ImportCandidate(c m6supply.ImportCandidate) error
	PutM6Skill(sk m6supply.Skill) error
	PutM6SkillDependency(d m6supply.SkillDependency) error
	PutM6SkillInstall(i m6supply.SkillInstall) error
	PutM6SkillTrigger(tr m6supply.SkillTrigger) error
	PutM6SkillVersion(v m6supply.SkillVersion) error
	SetM6SkillCurrentVersion(id, currentVersionID string, at time.Time) error
	SetM6SkillInstallStatus(id string, expectedVersion int64, status string, at time.Time) (m6supply.SkillInstall, error)
	TransitionM6ImportCandidate(id string, expectedVersion int64, to string, evidence m6supply.ImportEvidence, at time.Time) (m6supply.ImportCandidate, error)
	TransitionM6SkillState(id string, to string, at time.Time) (m6supply.Skill, error)

	// Prompt bundle entity chain (0084).
	FindM6PromptBundleByName(name string) (m6supply.PromptBundle, error)
	FindM6PromptBundleVersion(bundleID, semver string) (m6supply.PromptBundleVersion, error)
	GetM6PromptBundleVersion(id string) (m6supply.PromptBundleVersion, error)
	PutM6PromptBundle(pb m6supply.PromptBundle) error
	PutM6PromptBundleVersion(v m6supply.PromptBundleVersion) error
	SetM6PromptBundleCurrentVersion(id, currentVersionID string, at time.Time) error

	// Complexity routing, child manifests, bundles, synthesis (0053).
	FindM6ComplexityDecision(inputDigest, routerVersion string) (m6supply.ComplexityDecision, error)
	GetM6ChildManifestByDelegation(delegationID string) (m6supply.ChildContextManifest, error)
	ListM6ComplexityDecisions(sessionID string, limit int) ([]m6supply.ComplexityDecision, error)
	ListM6ResultBundles(delegationID string) ([]m6supply.ResultBundle, error)
	ListM6SynthesisRecords(rootID string) ([]m6supply.SynthesisRecord, error)
	PutM6ChildManifest(m m6supply.ChildContextManifest) error
	PutM6ComplexityDecision(d m6supply.ComplexityDecision) error
	PutM6ResultBundle(b m6supply.ResultBundle) error
	PutM6SynthesisRecord(r m6supply.SynthesisRecord) error

	// Cloud runners, region policy, leases, receipts, reconcile (0053).
	ExpireM6WorkerLeases(now time.Time) (int64, error)
	FindM6CloudRunnerByIdentity(workloadIdentity string) (m6supply.CloudRunner, error)
	GetM6CloudRunner(id string) (m6supply.CloudRunner, error)
	GetM6RemoteReceipt(id string) (m6supply.RemoteReceipt, error)
	GetM6WorkerLeaseByID(id string) (m6supply.WorkerLease, error)
	GetM6WorkerLeaseByTask(taskID string) (m6supply.WorkerLease, error)
	LatestM6RegionPolicy() (m6supply.RegionPolicy, error)
	ListM6CloudRunnersByState(state string) ([]m6supply.CloudRunner, error)
	ListM6PendingRemoteReceipts(limit int) ([]m6supply.RemoteReceipt, error)
	ListM6ReconcileDecisionsByTask(taskID string) ([]m6supply.ReconcileDecision, error)
	ListM6RemoteReceiptsByTask(taskID string) ([]m6supply.RemoteReceipt, error)
	MaxM6RegionPolicyVersion() (int64, error)
	PutM6CloudRunner(r m6supply.CloudRunner) error
	PutM6ReconcileDecision(d m6supply.ReconcileDecision) error
	PutM6RegionPolicy(p m6supply.RegionPolicy) error
	PutM6RemoteReceipt(r m6supply.RemoteReceipt) error
	PutM6WorkerLease(l m6supply.WorkerLease) error
	RenewM6WorkerLease(id string, newEpoch int64, runnerID string, expiresAt, at time.Time) (m6supply.WorkerLease, error)
	SetM6ReceiptReconcileState(id string, expectedVersion int64, state string, at time.Time) error
	TransitionM6CloudRunner(id string, expectedVersion int64, to string, at time.Time) (m6supply.CloudRunner, error)
	TransitionM6WorkerLease(id string, expectedEpoch int64, to string, at time.Time) (m6supply.WorkerLease, error)
}

// UnitOfWork is the M6 single-writer boundary.
type UnitOfWork interface {
	TransactM6(ctx context.Context, fn func(Tx) error) error
}

type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// SearchItem is one extension.search result row.
type SearchItem struct {
	ArtifactID  string   `json:"artifactId"`
	Publisher   string   `json:"publisher"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Digest      string   `json:"digest"`
	Permissions []string `json:"permissions"`
	Risk        string   `json:"risk"`
	SBOMRef     string   `json:"sbomRef"`
}

// InstallResult is the extension.install / extension.lifecycle result.
type InstallResult struct {
	InstallID    string
	State        string
	AuditEventID string
	// QuarantineCode carries the M6 wire code (e.g. M6-EXT-001) when the
	// install landed in quarantined; empty otherwise.
	QuarantineCode string
}

// ExtensionService implements the extension supply-chain use cases.
type ExtensionService struct {
	uow      UnitOfWork
	verifier *extension.Verifier
	clock    Clock
}

func NewExtensionService(uow UnitOfWork, verifier *extension.Verifier) *ExtensionService {
	return &ExtensionService{uow: uow, verifier: verifier, clock: systemClock{}}
}

// SetClock substitutes the wall clock (tests).
func (s *ExtensionService) SetClock(c Clock) { s.clock = c }

func (s *ExtensionService) available() error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	return nil
}

// Search scans the verified artifact catalog. query matches name or
// publisher (case-insensitive substring); maxRisk is "low" or "medium" —
// high-risk artifacts are never surfaced (schema enum).
func (s *ExtensionService) Search(ctx context.Context, query, publisher, maxRisk string) ([]SearchItem, error) {
	if err := s.available(); err != nil {
		return nil, err
	}
	rank := m6supply.RiskRank(maxRisk)
	if maxRisk != m6supply.RiskLow && maxRisk != m6supply.RiskMedium {
		rank = m6supply.RiskRank(m6supply.RiskMedium)
	}
	var items []SearchItem
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		rows, err := tx.SearchM6Artifacts(strings.ToLower(query), strings.ToLower(publisher), rank, 100)
		if err != nil {
			return err
		}
		items = make([]SearchItem, 0, len(rows))
		for _, a := range rows {
			item := SearchItem{
				ArtifactID: a.ID, Publisher: a.Publisher, Name: a.Name, Version: a.Version,
				Digest: a.Digest, Risk: a.Risk, SBOMRef: a.SBOMRef, Permissions: []string{},
			}
			var m extension.Manifest
			if err := json.Unmarshal([]byte(a.ManifestJSON), &m); err == nil {
				item.Permissions = m.Permissions
			}
			items = append(items, item)
		}
		return nil
	})
	return items, err
}

type permissionGrantRecord struct {
	Granted []string `json:"granted"`
}

// Install verifies and installs one artifact for a subject. The four-step
// verdict (digest/signature/SBOM/permission delta) runs first; a quarantined
// verdict still persists the install row in quarantined plus the audit
// record so the failure is durable evidence.
func (s *ExtensionService) Install(ctx context.Context, key, actor, subject, scope, artifactID, version string, grant extension.GrantDecision) (InstallResult, error) {
	if err := s.available(); err != nil {
		return InstallResult{}, err
	}
	if subject == "" {
		return InstallResult{}, ErrSubjectRequired
	}
	granted := append([]string(nil), grant.Granted...)
	sort.Strings(granted)
	digestIn := requestDigestOf(artifactID, version, subject, grant.ConfirmedDeltaDigest, strings.Join(granted, "\n"))
	var result InstallResult
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		if record, found, err := tx.Idempotency("extension.install", key, now); err != nil {
			return err
		} else if found {
			if record.Digest != digestIn {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal(record.Response, &result)
		}
		artifact, err := tx.GetM6Artifact(artifactID)
		if errors.Is(err, m6supply.ErrNotFound) {
			return ErrArtifactNotFound
		}
		if err != nil {
			return err
		}
		if artifact.Version != version {
			return ErrVersionMismatch
		}
		if artifact.SignatureState != m6supply.SignatureVerified {
			return ErrArtifactUnverified
		}

		candidate := s.candidate(artifact)
		var previouslyGranted []string
		if prev, err := tx.FindM6Install(subject, artifact.ID); err == nil {
			var rec permissionGrantRecord
			if json.Unmarshal([]byte(prev.PermissionGrantJSON), &rec) == nil {
				previouslyGranted = rec.Granted
			}
		}

		verdict := s.verifier.Verify(candidate, grant, previouslyGranted)
		audit := providerapp.Audit{
			ID: ulid.Make().String(), Action: "extension.installed", AggregateID: artifact.ID,
			Actor: actor, CreatedAt: now,
		}
		installState := m6supply.InstallInstalled
		if verdict.Quarantined {
			installState = m6supply.InstallQuarantined
		}
		install := m6supply.Install{
			ID: ulid.Make().String(), ArtifactID: artifact.ID, Subject: subject, Scope: scope,
			State: installState, Version: 1, InstalledAt: now, UpdatedAt: now,
			PermissionGrantJSON: marshalGrant(grant),
		}
		if err := tx.PutM6Install(install); err != nil {
			return err
		}
		audit.Metadata = auditMetadata(install.ID, installState, verdict.Code)
		if err := tx.PutAudit(audit); err != nil {
			return err
		}
		result = InstallResult{InstallID: install.ID, State: string(install.State), AuditEventID: audit.ID, QuarantineCode: verdict.Code}
		return tx.PutIdempotency(providerapp.Record{
			Operation: "extension.install", Key: key, Digest: digestIn,
			Response: marshalJSON(result), CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
		})
	})
	return result, err
}

// candidate adapts the durable artifact row into the verifier's candidate
// (digest + parsed manifest).
func (s *ExtensionService) candidate(a m6supply.Artifact) extension.Artifact {
	var m extension.Manifest
	_ = json.Unmarshal([]byte(a.ManifestJSON), &m)
	return extension.Artifact{Publisher: a.Publisher, Name: a.Name, Version: a.Version, Digest: a.Digest, Manifest: m}
}

// LifecycleOp applies enable/disable/pause/upgrade/rollback/uninstall.
// upgrade/rollback repoint the install at another artifact of the same
// publisher+name pair; the actual re-verification runs on the next install
// call for that version — lifecycle itself never escalates permissions.
func (s *ExtensionService) Lifecycle(ctx context.Context, key, actor, installID, op, targetVersion string) (InstallResult, error) {
	if err := s.available(); err != nil {
		return InstallResult{}, err
	}
	if !m6supply.ValidLifecycleOp(op) {
		return InstallResult{}, ErrBadLifecycleOp
	}
	digestIn := requestDigestOf(installID, op, targetVersion)
	var result InstallResult
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		if record, found, err := tx.Idempotency("extension.lifecycle", key, now); err != nil {
			return err
		} else if found {
			if record.Digest != digestIn {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal(record.Response, &result)
		}
		install, err := tx.GetM6Install(installID)
		if errors.Is(err, m6supply.ErrNotFound) {
			return ErrInstallNotFound
		}
		if err != nil {
			return err
		}

		var target m6supply.InstallState
		switch op {
		case "enable":
			target = m6supply.InstallEnabled
		case "disable":
			target = m6supply.InstallInstalled
		case "pause":
			target = m6supply.InstallPaused
		case "uninstall":
			target = m6supply.InstallUninstalled
		case "upgrade", "rollback":
			target = m6supply.InstallInstalled
		}
		if !lifecycleAllowed(install.State, op) {
			return ErrBadLifecycleOp
		}
		if op == "upgrade" || op == "rollback" {
			if targetVersion == "" {
				return ErrTargetRequired
			}
			artifact, aerr := tx.GetM6Artifact(install.ArtifactID)
			if aerr != nil {
				return aerr
			}
			next, ferr := tx.FindM6Artifact(artifact.Publisher, artifact.Name, targetVersion)
			if errors.Is(ferr, m6supply.ErrNotFound) {
				return ErrArtifactNotFound
			}
			if ferr != nil {
				return ferr
			}
			install, err = tx.RepointM6Install(install.ID, install.Version, next.ID, now)
			if err != nil {
				return err
			}
		} else {
			install, err = tx.TransitionM6Install(install.ID, install.Version, target, now)
			if err != nil {
				return err
			}
		}

		audit := providerapp.Audit{
			ID: ulid.Make().String(), Action: lifecycleAction(op), AggregateID: install.ID,
			Actor: actor, CreatedAt: now, Metadata: auditMetadata(install.ID, install.State, ""),
		}
		if err := tx.PutAudit(audit); err != nil {
			return err
		}
		result = InstallResult{InstallID: install.ID, State: string(install.State), AuditEventID: audit.ID}
		return tx.PutIdempotency(providerapp.Record{
			Operation: "extension.lifecycle", Key: key, Digest: digestIn,
			Response: marshalJSON(result), CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
		})
	})
	return result, err
}

// lifecycleAllowed encodes the frozen transition table. quarantined and
// blocked installs are read-only except uninstall; terminal uninstalled
// rejects everything.
func lifecycleAllowed(from m6supply.InstallState, op string) bool {
	if from.Terminal() {
		return false
	}
	switch op {
	case "enable":
		return from == m6supply.InstallInstalled || from == m6supply.InstallPaused
	case "disable", "pause":
		return from == m6supply.InstallEnabled
	case "upgrade", "rollback":
		return from == m6supply.InstallInstalled || from == m6supply.InstallEnabled || from == m6supply.InstallPaused
	case "uninstall":
		return true
	}
	return false
}

// ErrIdempotencyConflict is the public sentinel for handler mapping.
var ErrIdempotencyConflict = errors.New("m6app: idempotency key replayed with a different request")

func requestDigestOf(parts ...string) string {
	return extension.DeltaDigest(parts)
}

func marshalGrant(g extension.GrantDecision) string {
	rec := permissionGrantRecord{Granted: g.Granted}
	if rec.Granted == nil {
		rec.Granted = []string{}
	}
	return string(marshalJSON(rec))
}

func marshalJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	return b
}

func auditMetadata(installID string, state m6supply.InstallState, code string) []byte {
	type meta struct {
		InstallID string `json:"installId"`
		State     string `json:"state"`
		Code      string `json:"code,omitempty"`
	}
	return marshalJSON(meta{InstallID: installID, State: string(state), Code: code})
}

// lifecycleAction maps a wire op onto its past-tense audit action (the
// 0047 audit_events action CHECK set).
func lifecycleAction(op string) string {
	switch op {
	case "enable":
		return "extension.enabled"
	case "disable":
		return "extension.disabled"
	case "pause":
		return "extension.paused"
	case "upgrade":
		return "extension.upgraded"
	case "rollback":
		return "extension.rolled_back"
	case "uninstall":
		return "extension.uninstalled"
	}
	return "extension." + op
}
