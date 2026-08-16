// Package execution implements the M9 slice-3 Runner dispatch Broker
// (T-9.3.2, ADR-015 / D-4): runner attestation, residency routing and the
// read-only M6 execution projection.
//
// Reuse contract (ADR-015 decision 4, threat model m9-05 S7): the broker
// never builds a second state machine or kernel. RECEIVED → POLICY_LOCKED →
// WAITING_APPROVAL → BUDGET_RESERVED → ROUTED are M9's own read-only
// routing stages and end at ROUTED; DISPATCHED / CHECKPOINTED / SUCCEEDED /
// UNKNOWN / COMPENSATING are verbatim projections of M6 canonical
// CloudTask states and events (internal/domain/m6supply constants), and any
// M6 fact without a defined projection fails closed instead of being
// guessed.
//
// Routing (ADR-015 decision 2, threat model mitigation 2): the routing
// input carries the eight required elements — data classification,
// residency regions, capabilities, trust requirement, network egress,
// secret tier, budget (bound on the capability ticket) and availability
// (evaluated live from heartbeats). Any required constraint without a
// matching runner blocks dispatch (M9-019/020/021/022); restricted data is
// never degraded to a cloud runner (「数据禁止出域；当仅云 Runner 可用；
// 则阻断而不外发」, T-13).
//
// Attestation (ADR-015 decision 1): managed/cloud runners register with an
// ed25519-signed, time-limited proof over
// {runner_kind, trust_tier, residency_regions, key_region, capabilities,
// network_egress, issued_at, nonce}, verified through the M6 delegation
// envelope's keyID-versioned KeyResolver mechanism. Local runners default
// to trust_tier=trusted and residency=in-org. A heartbeat is liveness
// only — it never substitutes for, or extends, a proof (S3), and a proof
// that fails verification quarantines the runner (mitigation 7).
//
// UNKNOWN semantics (ADR-015 decision 3, T-14): a lost runner or an
// outcome_unknown receipt moves the dispatch to UNKNOWN; UNKNOWN tasks are
// never re-dispatched automatically, the implicated runner takes no new
// side-effect work, and recovery happens only through governance
// reconciliation against M6 facts — a conflicting fact fails closed.
//
// Concept error taxonomy (04 错误目录, wire-alias only):
//
//	M9-019 RUNNER_ATTESTATION_INVALID
//	M9-020 RUNNER_OFFLINE
//	M9-021 RUNNER_RESIDENCY_MISMATCH
//	M9-022 SAFE_RUNNER_UNAVAILABLE
package execution

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/delegation"
	"github.com/lunitide/lunitide/internal/domain/m6supply"
)

// ConceptError is an M9 concept-taxonomy error (code + name).
type ConceptError struct {
	Code string
	Name string
}

func (e *ConceptError) Error() string { return e.Code + " " + e.Name }

func (e *ConceptError) Is(target error) bool {
	var other *ConceptError
	if errors.As(target, &other) {
		return other.Code == e.Code
	}
	return false
}

var (
	ErrAttestationInvalid    = &ConceptError{"M9-019", "RUNNER_ATTESTATION_INVALID"}
	ErrRunnerOffline         = &ConceptError{"M9-020", "RUNNER_OFFLINE"}
	ErrResidencyMismatch     = &ConceptError{"M9-021", "RUNNER_RESIDENCY_MISMATCH"}
	ErrSafeRunnerUnavailable = &ConceptError{"M9-022", "SAFE_RUNNER_UNAVAILABLE"}
)

// Code extracts the M9 concept code when err carries one.
func Code(err error) string {
	var ce *ConceptError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// Runner kinds (threat model: local/managed/cloud hybrid pool).
const (
	RunnerKindLocal   = "local"
	RunnerKindManaged = "managed"
	RunnerKindCloud   = "cloud"
)

// Trust tiers carried on the proof.
const (
	TrustTrusted   = "trusted"
	TrustUntrusted = "untrusted"
)

// Network egress levels, ordered none < policy < open.
const (
	EgressNone   = "none"
	EgressPolicy = "policy"
	EgressOpen   = "open"
)

// Data classifications. Restricted data must never leave the organization
// domain (T-13).
const (
	DataClassRestricted = "restricted"
	DataClassInternal   = "internal"
	DataClassPublic     = "public"
)

// Secret tiers. In-org secrets are a hard match against open-egress
// runners (threat model S8: 出网 Runner 不得拿内网 Secret).
const (
	SecretTierNone  = "none"
	SecretTierInOrg = "in_org"
)

// ResidencyInOrg is the local-runner default residency label.
const ResidencyInOrg = "in-org"

// M9 read-only routing stages (ADR-015 decision 4). The broker's own
// routing stage ends at ROUTED; everything after is an M6 projection.
const (
	StageReceived        = "RECEIVED"
	StagePolicyLocked    = "POLICY_LOCKED"
	StageWaitingApproval = "WAITING_APPROVAL"
	StageBudgetReserved  = "BUDGET_RESERVED"
	StageRouted          = "ROUTED"
)

// Dispatch-phase projections of M6 canonical states/events.
const (
	ProjDispatched   = "DISPATCHED"
	ProjCheckpointed = "CHECKPOINTED"
	ProjSucceeded    = "SUCCEEDED"
	ProjUnknown      = "UNKNOWN"
	ProjCompensating = "COMPENSATING"
)

// Proof and heartbeat validity windows. Proofs are short-lived tickets:
// a heartbeat never extends ProofTTL (S3).
const (
	ProofTTL      = time.Hour
	HeartbeatTTL  = 30 * time.Second
	SkewTolerance = 2 * time.Minute
)

// Proof is the runner attestation (ADR-015 decision 1): the exact field
// set {runner_kind, trust_tier, residency_regions, key_region,
// capabilities, network_egress, issued_at, nonce}, ed25519-signed over the
// canonical JSON bytes with the signature cleared (delegation-envelope
// mechanism, keyID-versioned).
type Proof struct {
	RunnerKind       string   `json:"runner_kind"`
	TrustTier        string   `json:"trust_tier"`
	ResidencyRegions []string `json:"residency_regions"`
	KeyRegion        string   `json:"key_region"`
	Capabilities     []string `json:"capabilities"`
	NetworkEgress    string   `json:"network_egress"`
	IssuedAt         string   `json:"issued_at"` // RFC3339
	Nonce            string   `json:"nonce"`
	KeyID            string   `json:"key_id"`
	Signature        string   `json:"signature"` // hex(ed25519)
}

// CanonicalProofBytes marshals the proof with the signature cleared — the
// exact bytes the signature covers. json.Marshal on this struct is
// deterministic (fixed field order, no maps).
func CanonicalProofBytes(p Proof) []byte {
	cleared := p
	cleared.Signature = ""
	raw, err := json.Marshal(cleared)
	if err != nil {
		return nil
	}
	return raw
}

// ProofDigest is the SHA-256 of the canonical proof bytes (audit trail).
func ProofDigest(p Proof) string {
	sum := sha256.Sum256(CanonicalProofBytes(p))
	return hex.EncodeToString(sum[:])
}

// SignProof signs the proof with a keyID-versioned ed25519 key (Runner
// console / registration CLI helper).
func SignProof(keyID string, priv ed25519.PrivateKey, p *Proof) {
	p.KeyID = keyID
	p.Signature = ""
	p.Signature = hex.EncodeToString(ed25519.Sign(priv, CanonicalProofBytes(*p)))
}

// Runner is one registered execution runner.
type Runner struct {
	ID       string
	OrgID    string // "" = shared pool (proven managed/cloud); local runners are org-scoped
	Kind     string // local | managed | cloud
	TrustTier string

	// Attested placement and capability labels (from the proof, or the
	// local defaults trusted / in-org).
	ResidencyRegions []string
	KeyRegion        string
	Capabilities     []string
	NetworkEgress    string

	// ProofExpiresAt is nil for local runners (default trust, no proof);
	// otherwise the attestation deadline re-checked at every route.
	ProofExpiresAt *time.Time

	// Liveness (heartbeat) — never a trust substitute (S3).
	Online         bool
	LastHeartbeat  time.Time

	// Quarantined runners are excluded from all routing until ops removes
	// them (proof failure, mitigation 7).
	Quarantined bool
}

// Ticket is the short-lived capability ticket authorizing one dispatch
// (threat model S4): bound to org, policy version, package digest and
// budget reservation. The task organization is taken from the ticket, not
// from the request.
type Ticket struct {
	TicketID            string
	OrgID               string
	PolicyDigest        string
	PackageDigest       string
	BudgetReservationID string
	ExpiresAt           time.Time
}

// RouteRequest is the eight-element routing input (ADR-015 decision 2):
// data classification, residency, capability, trust, network, secret and
// budget (on the ticket) plus availability (evaluated live).
type RouteRequest struct {
	DataClass             string   // restricted | internal | public
	RequiredRegions       []string // processing must happen inside one of these
	RequiredCapabilities  []string // runner must be a superset
	RequireTrustedProof   bool     // trust element: runner must be trusted-tier with valid proof
	MaxEgress             string   // network element: runner egress ceiling (none|policy|open)
	SecretTier            string   // none | in_org (hard match vs egress, S8)
	SideEffects           bool     // side-effect steps get UNKNOWN no-re-run semantics (T-14)
}

// M6Fact is an M6 canonical fact (CloudTask state plus observed events)
// observed on the cloud task backing one dispatch.
type M6Fact struct {
	CloudTaskState    string // m6supply.CloudTask* constants
	CheckpointReceipt bool   // M6 checkpoint event observed
	Compensating      bool   // M6 compensation activity observed
}

// Dispatch is one routed task and its read-only state view.
type Dispatch struct {
	TaskID       string
	TicketID     string
	OrgID        string
	RunnerID     string
	SideEffects  bool
	State        string // StageRouted | ProjDispatched | ProjCheckpointed | ProjSucceeded | ProjUnknown | ProjCompensating
	UnknownSince *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Broker routes tasks to trusted runners under residency constraints and
// keeps the read-only M6 projection. All mutation goes through its own
// mutex; the broker performs no M6 writes of any kind (no second kernel).
type Broker struct {
	mu         sync.Mutex
	keys       delegation.KeyResolver
	runners    map[string]*Runner
	nonces     map[string]bool
	dispatches map[string]*Dispatch
}

// NewBroker builds a broker verifying proof keyIDs against the resolver
// (the M6 delegation envelope KeyResolver contract).
func NewBroker(keys delegation.KeyResolver) *Broker {
	return &Broker{
		keys:       keys,
		runners:    make(map[string]*Runner),
		nonces:     make(map[string]bool),
		dispatches: make(map[string]*Dispatch),
	}
}

// RegisterLocal registers an org-scoped local runner. Per ADR-015 the
// local default is trust_tier=trusted, residency=in-org, no proof.
func (b *Broker) RegisterLocal(orgID, runnerID string, capabilities []string, egress string) (*Runner, error) {
	if orgID == "" || runnerID == "" {
		return nil, errors.New("execution: local runner requires org and runner id")
	}
	if len(capabilities) == 0 {
		return nil, errors.New("execution: local runner requires capabilities")
	}
	if !validEgress(egress) {
		return nil, fmt.Errorf("execution: invalid egress %q", egress)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, dup := b.runners[runnerID]; dup {
		return nil, fmt.Errorf("execution: runner %q already registered", runnerID)
	}
	r := &Runner{
		ID: runnerID, OrgID: orgID, Kind: RunnerKindLocal, TrustTier: TrustTrusted,
		ResidencyRegions: []string{ResidencyInOrg}, KeyRegion: ResidencyInOrg,
		Capabilities: append([]string(nil), capabilities...), NetworkEgress: egress,
	}
	b.runners[runnerID] = r
	return r, nil
}

// RegisterProven registers a managed/cloud runner against its signed
// attestation. Every attestation defect — wrong kind, malformed fields,
// future-issued, expired, replayed nonce, unknown key, bad signature —
// refuses registration with M9-019.
func (b *Broker) RegisterProven(orgID, runnerID string, proof Proof, now time.Time) (*Runner, error) {
	if runnerID == "" {
		return nil, errors.New("execution: runner id required")
	}
	attest := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrAttestationInvalid, fmt.Sprintf(format, args...))
	}
	if proof.RunnerKind != RunnerKindManaged && proof.RunnerKind != RunnerKindCloud {
		return nil, attest("runner_kind %q invalid (local runners register without proof)", proof.RunnerKind)
	}
	if proof.TrustTier != TrustTrusted && proof.TrustTier != TrustUntrusted {
		return nil, attest("trust_tier %q invalid", proof.TrustTier)
	}
	if len(proof.ResidencyRegions) == 0 || proof.KeyRegion == "" {
		return nil, attest("residency_regions and key_region are required")
	}
	for _, rr := range proof.ResidencyRegions {
		if rr == "" {
			return nil, attest("residency_regions contains an empty region")
		}
	}
	if len(proof.Capabilities) == 0 {
		return nil, attest("capabilities are required")
	}
	if !validEgress(proof.NetworkEgress) {
		return nil, attest("network_egress %q invalid", proof.NetworkEgress)
	}
	if proof.Nonce == "" || proof.KeyID == "" || proof.Signature == "" {
		return nil, attest("nonce, key_id and signature are required")
	}
	issued, err := time.Parse(time.RFC3339, proof.IssuedAt)
	if err != nil {
		return nil, attest("issued_at %q is not RFC3339", proof.IssuedAt)
	}
	if issued.After(now.Add(SkewTolerance)) {
		return nil, attest("proof issued in the future (%s)", proof.IssuedAt)
	}
	if now.Sub(issued) > ProofTTL {
		return nil, attest("proof expired (issued %s, ttl %s)", proof.IssuedAt, ProofTTL)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if _, dup := b.runners[runnerID]; dup {
		return nil, fmt.Errorf("execution: runner %q already registered", runnerID)
	}
	if b.nonces[proof.Nonce] {
		return nil, attest("proof nonce replayed")
	}
	key, ok := b.keys(proof.KeyID)
	if !ok {
		return nil, attest("signing key %q unknown", proof.KeyID)
	}
	sig, err := hex.DecodeString(proof.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, attest("signature is not %d-byte hex", ed25519.SignatureSize)
	}
	if !ed25519.Verify(key, CanonicalProofBytes(proof), sig) {
		return nil, attest("signature verification failed for proof %s", ProofDigest(proof))
	}
	expires := issued.Add(ProofTTL)
	r := &Runner{
		ID: runnerID, OrgID: orgID, Kind: proof.RunnerKind, TrustTier: proof.TrustTier,
		ResidencyRegions: append([]string(nil), proof.ResidencyRegions...),
		KeyRegion:        proof.KeyRegion,
		Capabilities:     append([]string(nil), proof.Capabilities...),
		NetworkEgress:    proof.NetworkEgress,
		ProofExpiresAt:   &expires,
	}
	b.runners[runnerID] = r
	b.nonces[proof.Nonce] = true
	return r, nil
}

// Heartbeat records liveness. It never restores a quarantined runner and
// never extends a proof (S3: heartbeat is not a trust substitute).
func (b *Broker) Heartbeat(runnerID string, at time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.runners[runnerID]
	if !ok {
		return fmt.Errorf("execution: runner %q unknown", runnerID)
	}
	if r.Quarantined {
		return fmt.Errorf("%w: runner %q is quarantined; heartbeat cannot restore trust", ErrAttestationInvalid, runnerID)
	}
	r.Online = true
	r.LastHeartbeat = at
	return nil
}

// Unregister removes a runner (ops action, e.g. quarantined cleanup).
func (b *Broker) Unregister(runnerID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.runners[runnerID]; !ok {
		return fmt.Errorf("execution: runner %q unknown", runnerID)
	}
	delete(b.runners, runnerID)
	return nil
}

// Route is the dispatch gate. The task organization comes from the ticket
// (S4); every required constraint without a matching runner blocks the
// dispatch — the broker never degrades outward (T-13). Repeated routing
// of the same task is idempotent and returns the existing dispatch (no
// double dispatch, threat model S6), except tasks in UNKNOWN, which are
// refused pending governance reconciliation (T-14).
func (b *Broker) Route(taskID string, ticket Ticket, req RouteRequest, now time.Time) (*Dispatch, error) {
	if taskID == "" {
		return nil, errors.New("execution: task id required")
	}
	if err := validateTicket(ticket, now); err != nil {
		return nil, err
	}
	if err := validateRequest(req); err != nil {
		return nil, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if d, ok := b.dispatches[taskID]; ok {
		if d.State == ProjUnknown {
			return nil, fmt.Errorf("%w: task %s is UNKNOWN pending governance reconciliation; re-dispatch blocked so side effects cannot repeat", ErrRunnerOffline, taskID)
		}
		return d, nil // idempotent replay: same task, same runner
	}

	// Pool: non-quarantined runners visible to the ticket organization
	// (local runners are org-scoped; proven runners may be shared).
	var pool []*Runner
	for _, r := range b.runners {
		if r.Quarantined {
			continue
		}
		if r.OrgID != "" && r.OrgID != ticket.OrgID {
			continue
		}
		pool = append(pool, r)
	}

	// Attestation freshness re-checked at route time (S3: heartbeat online
	// but proof expired must not route). Stale proofs quarantine.
	var fresh []*Runner
	for _, r := range pool {
		if r.ProofExpiresAt != nil && now.After(*r.ProofExpiresAt) {
			r.Quarantined = true
			continue
		}
		fresh = append(fresh, r)
	}
	if len(pool) > 0 && len(fresh) == 0 {
		return nil, fmt.Errorf("%w: every candidate runner attestation expired; stale runners quarantined", ErrAttestationInvalid)
	}

	// Residency: processing must happen inside a required region, and
	// restricted data never routes to cloud runners nor untrusted tiers
	// (T-13 / S2 — block, do not degrade outward).
	var inRegion []*Runner
	for _, r := range fresh {
		if residencyOK(r, req) {
			inRegion = append(inRegion, r)
		}
	}
	if len(fresh) > 0 && len(inRegion) == 0 {
		return nil, fmt.Errorf("%w: no runner processes %s data within %v (cross-domain fallback refused)", ErrResidencyMismatch, req.DataClass, req.RequiredRegions)
	}

	// Hard constraints: capability superset, trust tier, egress ceiling,
	// secret/egress match (S8).
	var capable []*Runner
	for _, r := range inRegion {
		if constraintsOK(r, req) {
			capable = append(capable, r)
		}
	}
	if len(inRegion) > 0 && len(capable) == 0 {
		return nil, fmt.Errorf("%w: no runner satisfies capabilities %v / trust=%v / egress<=%s / secret=%s", ErrSafeRunnerUnavailable, req.RequiredCapabilities, req.RequireTrustedProof, req.MaxEgress, req.SecretTier)
	}

	// Availability (live): fresh heartbeat, and for side-effect tasks the
	// runner must not hold an unresolved UNKNOWN side-effect dispatch
	// (S5/T-14).
	var online []*Runner
	for _, r := range capable {
		if !b.available(r, req, now) {
			continue
		}
		online = append(online, r)
	}
	if len(capable) > 0 && len(online) == 0 {
		return nil, fmt.Errorf("%w: every capable runner is offline or UNKNOWN-locked", ErrRunnerOffline)
	}
	if len(online) == 0 {
		return nil, fmt.Errorf("%w: no runner registered for the routing input", ErrSafeRunnerUnavailable)
	}

	sort.Slice(online, func(i, j int) bool { return online[i].ID < online[j].ID })
	pick := online[0]
	d := &Dispatch{
		TaskID: taskID, TicketID: ticket.TicketID, OrgID: ticket.OrgID,
		RunnerID: pick.ID, SideEffects: req.SideEffects,
		State: StageRouted, CreatedAt: now, UpdatedAt: now,
	}
	b.dispatches[taskID] = d
	return d, nil
}

// ProjectM6 applies an M6 canonical fact to the dispatch's read-only
// projection. Unknown or unmappable facts fail closed (S7 — M9 invents no
// facts); created/queued facts leave the ROUTED view unchanged. Dispatches
// in UNKNOWN move only through Reconcile.
func (b *Broker) ProjectM6(taskID string, fact M6Fact, now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.dispatches[taskID]
	if !ok {
		return fmt.Errorf("execution: task %q not dispatched", taskID)
	}
	if d.State == ProjUnknown {
		return fmt.Errorf("execution: task %s is UNKNOWN; projection moves only via governance reconciliation", taskID)
	}
	next, err := project(fact)
	if err != nil {
		return err
	}
	if next == "" { // pre-dispatch M6 fact (created/queued): no projection change
		return nil
	}
	d.State = next
	d.UpdatedAt = now
	return nil
}

// MarkUnknown moves a dispatch to UNKNOWN — runner lost contact or an
// outcome_unknown receipt. No side-effect step re-runs automatically and
// the implicated runner takes no new side-effect work until reconciliation
// resolves the dispatch (T-14).
func (b *Broker) MarkUnknown(taskID string, now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.dispatches[taskID]
	if !ok {
		return fmt.Errorf("execution: task %q not dispatched", taskID)
	}
	if d.State == ProjUnknown {
		return nil // already unknown
	}
	d.State = ProjUnknown
	d.UnknownSince = &now
	d.UpdatedAt = now
	return nil
}

// Reconcile recovers an UNKNOWN dispatch through governance reconciliation
// against M6 facts (ADR-015 decision 3): it requires independent approval,
// adopts only projectable M6 facts, and fails closed on conflicts (the
// dispatch stays UNKNOWN).
func (b *Broker) Reconcile(taskID string, fact M6Fact, governanceApproved bool, now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.dispatches[taskID]
	if !ok {
		return fmt.Errorf("execution: task %q not dispatched", taskID)
	}
	if d.State != ProjUnknown {
		return fmt.Errorf("execution: task %s is %s, not UNKNOWN; reconciliation does not apply", taskID, d.State)
	}
	if !governanceApproved {
		return errors.New("execution: UNKNOWN recovery requires governance approval (diagnose, terminate or independently approved recovery only)")
	}
	next, err := project(fact)
	if err != nil {
		return fmt.Errorf("execution: reconciliation of task %s failed closed: %w", taskID, err)
	}
	if next == "" {
		return fmt.Errorf("execution: reconciliation of task %s failed closed: M6 fact %s is not a recovery verdict", taskID, fact.CloudTaskState)
	}
	d.State = next
	d.UnknownSince = nil
	d.UpdatedAt = now
	return nil
}

// GetDispatch returns a live dispatch view (Runner console).
func (b *Broker) GetDispatch(taskID string) (*Dispatch, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	d, ok := b.dispatches[taskID]
	return d, ok
}

// GetRunner returns a live runner view (Runner console).
func (b *Broker) GetRunner(runnerID string) (*Runner, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.runners[runnerID]
	return r, ok
}

// available is the live availability check: fresh heartbeat, and for
// side-effect requests the runner must hold no unresolved UNKNOWN
// side-effect dispatch.
func (b *Broker) available(r *Runner, req RouteRequest, now time.Time) bool {
	if !r.Online || now.Sub(r.LastHeartbeat) > HeartbeatTTL {
		return false
	}
	if req.SideEffects && b.hasUnknownSideEffect(r.ID) {
		return false
	}
	return true
}

func (b *Broker) hasUnknownSideEffect(runnerID string) bool {
	for _, d := range b.dispatches {
		if d.RunnerID == runnerID && d.SideEffects && d.State == ProjUnknown {
			return true
		}
	}
	return false
}

// project maps an M6 canonical fact onto the M9 read-only projection.
// The empty string means "no projection change" (pre-dispatch facts);
// anything without a defined projection is an error (fail closed, S7).
func project(fact M6Fact) (string, error) {
	switch fact.CloudTaskState {
	case m6supply.CloudTaskLeased, m6supply.CloudTaskRunning:
		if fact.CheckpointReceipt {
			return ProjCheckpointed, nil
		}
		return ProjDispatched, nil
	case m6supply.CloudTaskJoining:
		return ProjCheckpointed, nil
	case m6supply.CloudTaskSucceeded:
		return ProjSucceeded, nil
	case m6supply.CloudTaskFailed:
		if fact.Compensating {
			return ProjCompensating, nil
		}
		return "", fmt.Errorf("execution: M6 fact %s without compensation has no M9 projection (fail closed)", fact.CloudTaskState)
	case m6supply.CloudTaskCreated, m6supply.CloudTaskQueued:
		return "", nil // pre-dispatch: the ROUTED view stands
	default:
		return "", fmt.Errorf("execution: unknown M6 canonical state %q (fail closed)", fact.CloudTaskState)
	}
}

// residencyOK: processing region within the required set; restricted data
// additionally never routes to cloud runners or untrusted tiers (T-13).
func residencyOK(r *Runner, req RouteRequest) bool {
	if req.DataClass == DataClassRestricted {
		if r.Kind == RunnerKindCloud {
			return false
		}
		if r.TrustTier != TrustTrusted {
			return false
		}
	}
	for _, have := range r.ResidencyRegions {
		for _, want := range req.RequiredRegions {
			if have == want {
				return true
			}
		}
	}
	return false
}

// constraintsOK: capability superset, trust tier, egress ceiling and the
// secret/egress hard match (S8).
func constraintsOK(r *Runner, req RouteRequest) bool {
	have := make(map[string]bool, len(r.Capabilities))
	for _, c := range r.Capabilities {
		have[c] = true
	}
	for _, c := range req.RequiredCapabilities {
		if !have[c] {
			return false
		}
	}
	if egressRank(r.NetworkEgress) > egressRank(req.MaxEgress) {
		return false
	}
	if req.SecretTier == SecretTierInOrg && r.NetworkEgress != EgressNone {
		return false
	}
	if req.RequireTrustedProof && r.TrustTier != TrustTrusted {
		return false
	}
	return true
}

func validateTicket(t Ticket, now time.Time) error {
	if t.TicketID == "" || t.OrgID == "" || t.PolicyDigest == "" || t.PackageDigest == "" || t.BudgetReservationID == "" {
		return errors.New("execution: capability ticket must bind org, policy version, package digest and budget reservation")
	}
	if len(t.PolicyDigest) != 64 || !isLowerHex(t.PolicyDigest) || len(t.PackageDigest) != 64 || !isLowerHex(t.PackageDigest) {
		return errors.New("execution: ticket digests must be 64-char lowercase hex SHA-256")
	}
	if t.ExpiresAt.IsZero() || !now.Before(t.ExpiresAt) {
		return fmt.Errorf("execution: capability ticket %s expired or unbounded", t.TicketID)
	}
	return nil
}

func validateRequest(req RouteRequest) error {
	switch req.DataClass {
	case DataClassRestricted, DataClassInternal, DataClassPublic:
	default:
		return fmt.Errorf("execution: invalid data class %q", req.DataClass)
	}
	if len(req.RequiredRegions) == 0 {
		return errors.New("execution: routing input must declare residency regions")
	}
	if !validEgress(req.MaxEgress) {
		return fmt.Errorf("execution: invalid egress ceiling %q", req.MaxEgress)
	}
	if req.SecretTier != SecretTierNone && req.SecretTier != SecretTierInOrg {
		return fmt.Errorf("execution: invalid secret tier %q", req.SecretTier)
	}
	return nil
}

func validEgress(e string) bool {
	return e == EgressNone || e == EgressPolicy || e == EgressOpen
}

func egressRank(e string) int {
	switch e {
	case EgressNone:
		return 0
	case EgressPolicy:
		return 1
	default:
		return 2
	}
}

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
