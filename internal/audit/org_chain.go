// Organization-dimension audit chains and export re-authorization
// (T-9.4.2, threat model m9-06 S4/S5). This file deliberately reuses the M7
// hash-chain kernel (Link / Seal / ComputeHash / VerifyChain) — the org
// ledger only partitions events per organization and adds the export gate;
// it must NOT grow a second hashing or chaining kernel (M6/M8 kernel-reuse
// acceptance rule).
//
// Partitioning: every org owns one independent chain (seq restarts at 1,
// genesis prev per partition), so one tenant's volume or tampering never
// blocks another tenant's verification (按组织摘要链与投递分区).
//
// Export (M9-027 AUDIT_EXPORT_DENIED): exporting an org's audit trail
// requires a fresh ExportGrant scoped to the same org AND actor, and the
// export itself is appended to the chain as an audit.export event — an
// export that leaves no trace is a broken export. The returned Export
// bundle is plain data; any third party can independently re-verify it via
// VerifyChain(events) and compare HeadHash (导出可独立复算且再授权).
//
// Signed checkpoints (mitigation 5: 跨区用签名检查点): Checkpoint signs the
// partition head (org|seq|head_hash) with an ed25519 key so cross-region
// copies can be validated without shipping the whole chain.
//
// Concept error taxonomy (04 错误目录, wire-alias only):
//
//	M9-026 AUDIT_CHAIN_INVALID   审计链不完整 (删除/乱序/篡改)
//	M9-027 AUDIT_EXPORT_DENIED   审计导出拒绝 (无授权/过期/范围或身份不符)
package audit

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
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
	ErrChainInvalid = &ConceptError{"M9-026", "AUDIT_CHAIN_INVALID"}
	ErrExportDenied = &ConceptError{"M9-027", "AUDIT_EXPORT_DENIED"}
)

// M9Code extracts the M9 concept code when err carries one.
func M9Code(err error) string {
	var ce *ConceptError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// ExportGrant is the time-boxed, org- and actor-scoped export authority.
// Grants are single-purpose: they authorize exactly one Export call, which
// consumes them (再授权 — every export needs a fresh grant).
type ExportGrant struct {
	GrantID   string
	OrgID     string
	Actor     string
	Reason    string
	ExpiresAt time.Time
	consumed  bool
}

// Checkpoint is an ed25519 signature over one partition head.
type Checkpoint struct {
	OrgID    string
	Seq      int64
	HeadHash string
	KeyID    string
	SigHex   string
	At       time.Time
}

// Export is the independently verifiable audit bundle.
type Export struct {
	OrgID     string
	Events    []Event
	HeadHash  string
	GrantID   string
	Actor     string
	VerifiedAt time.Time
}

// OrgLedger is the org-partitioned append-only audit ledger. All mutations
// hold one mutex; Append both links and seals atomically (领域事务与审计同锁,
// S5 同提交 semantics at the in-process prototype level).
type OrgLedger struct {
	mu          sync.Mutex
	partitions  map[string][]Event
	ids         map[string]string // event id → owning org (globally unique)
	grants      map[string]*ExportGrant
	checkpoints map[string][]Checkpoint
}

// NewOrgLedger builds an empty org-partitioned ledger.
func NewOrgLedger() *OrgLedger {
	return &OrgLedger{
		partitions:  make(map[string][]Event),
		ids:         make(map[string]string),
		grants:      make(map[string]*ExportGrant),
		checkpoints: make(map[string][]Checkpoint),
	}
}

// Append links one event onto its organization partition and seals it. The
// event id is globally unique across partitions; CreatedAt, when empty, is
// stamped as now (UTC RFC3339).
func (l *OrgLedger) Append(orgID string, e Event, now time.Time) (*Event, error) {
	if orgID == "" {
		return nil, errors.New("audit: org id required")
	}
	if e.ID == "" || e.Action == "" || e.Actor == "" {
		return nil, errors.New("audit: event id, action and actor are required")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if owner, dup := l.ids[e.ID]; dup {
		return nil, fmt.Errorf("audit: event id %q already exists in org %s", e.ID, owner)
	}
	if e.CreatedAt == "" {
		e.CreatedAt = now.UTC().Format(time.RFC3339)
	}
	chain := l.partitions[orgID]
	var head *Event
	if n := len(chain); n > 0 {
		head = &chain[n-1]
	}
	sealed := Link(head, e) // M7 kernel reuse: seq + prev_hash + hash
	l.partitions[orgID] = append(chain, sealed)
	l.ids[e.ID] = orgID
	return &sealed, nil
}

// Partition returns a copy of one org's chain (storage order).
func (l *OrgLedger) Partition(orgID string) []Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Event, len(l.partitions[orgID]))
	copy(out, l.partitions[orgID])
	return out
}

// Orgs lists the partition ids (audit coverage view).
func (l *OrgLedger) Orgs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	orgs := make([]string, 0, len(l.partitions))
	for id := range l.partitions {
		orgs = append(orgs, id)
	}
	sort.Strings(orgs)
	return orgs
}

// Verify re-computes one org partition via the M7 kernel; any deletion,
// reordering or field tampering answers M9-026 (T-17).
func (l *OrgLedger) Verify(orgID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	events, ok := l.partitions[orgID]
	if !ok {
		return fmt.Errorf("audit: unknown org partition %q", orgID)
	}
	if err := VerifyChain(events); err != nil {
		return fmt.Errorf("%w: org %s: %v", ErrChainInvalid, orgID, err)
	}
	return nil
}

// VerifyAll checks every partition; org partitioning means one tenant's
// broken chain is reported without muting the others.
func (l *OrgLedger) VerifyAll() []error {
	l.mu.Lock()
	orgs := make([]string, 0, len(l.partitions))
	for id := range l.partitions {
		orgs = append(orgs, id)
	}
	l.mu.Unlock()
	sort.Strings(orgs)
	var errs []error
	for _, id := range orgs {
		if err := l.Verify(id); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// IssueGrant registers one export grant. A grant whose expiry is not in
// the future is dead on arrival.
func (l *OrgLedger) IssueGrant(g ExportGrant, now time.Time) error {
	if g.GrantID == "" || g.OrgID == "" || g.Actor == "" {
		return errors.New("audit: grant id, org and actor are required")
	}
	if !g.ExpiresAt.After(now) {
		return fmt.Errorf("%w: grant %s already expired at issue", ErrExportDenied, g.GrantID)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, dup := l.grants[g.GrantID]; dup {
		return fmt.Errorf("audit: grant id %q already exists", g.GrantID)
	}
	l.grants[g.GrantID] = &g
	return nil
}

// Export releases one org's audit trail under a valid grant and records the
// export itself in the chain. Failures answer M9-027: missing/expired grant,
// org scope mismatch or actor mismatch. A broken chain (M9-026) also blocks
// the export — never ship an unverifiable bundle. Grants are single-use
// (consumed); the audit.export event embeds grant id and reason.
func (l *OrgLedger) Export(grantID, orgID, actor string, now time.Time) (*Export, error) {
	if grantID == "" {
		return nil, fmt.Errorf("%w: export requires a grant", ErrExportDenied)
	}
	l.mu.Lock()
	g, ok := l.grants[grantID]
	l.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%w: unknown grant %q", ErrExportDenied, grantID)
	}
	if now.After(g.ExpiresAt) {
		return nil, fmt.Errorf("%w: grant %s expired at %s", ErrExportDenied, grantID, g.ExpiresAt.Format(time.RFC3339))
	}
	if g.OrgID != orgID {
		return nil, fmt.Errorf("%w: grant %s scopes org %s, not %s", ErrExportDenied, grantID, g.OrgID, orgID)
	}
	if g.Actor != actor {
		return nil, fmt.Errorf("%w: grant %s belongs to actor %s, not %s", ErrExportDenied, grantID, g.Actor, actor)
	}
	if err := l.Verify(orgID); err != nil {
		return nil, fmt.Errorf("%w: export refused for an unverifiable chain: %v", ErrChainInvalid, err)
	}
	l.mu.Lock()
	if g.consumed {
		l.mu.Unlock()
		return nil, fmt.Errorf("%w: grant %s already consumed (every export needs re-authorization)", ErrExportDenied, grantID)
	}
	g.consumed = true
	l.mu.Unlock()

	// The export leaves its own trail: grant id + reason ride the event so
	// the authorization basis is auditable (导出留痕).
	ev, err := l.Append(orgID, Event{
		ID:           "audit-export-" + grantID,
		Action:       "audit.export",
		ResourceType: "audit_chain",
		ResourceID:   orgID,
		Actor:        actor,
		AfterDigest:  chainDigest(grantID, g.Reason),
		CorrelationID: grantID,
	}, now)
	if err != nil {
		return nil, err
	}
	events := l.Partition(orgID)
	return &Export{
		OrgID:      orgID,
		Events:     events,
		HeadHash:   ev.EventHash,
		GrantID:    grantID,
		Actor:      actor,
		VerifiedAt: now,
	}, nil
}

// Checkpoint signs the current partition head (org|seq|head_hash) with an
// ed25519 key so cross-region copies can be validated without the full
// chain (跨区签名检查点).
func (l *OrgLedger) Checkpoint(orgID, keyID string, priv ed25519.PrivateKey, now time.Time) (*Checkpoint, error) {
	if orgID == "" || keyID == "" || len(priv) != ed25519.PrivateKeySize {
		return nil, errors.New("audit: org id, key id and ed25519 private key are required")
	}
	l.mu.Lock()
	chain := l.partitions[orgID]
	if len(chain) == 0 {
		l.mu.Unlock()
		return nil, fmt.Errorf("audit: org %s has no events to checkpoint", orgID)
	}
	head := chain[len(chain)-1]
	l.mu.Unlock()
	cp := Checkpoint{
		OrgID: orgID, Seq: head.Seq, HeadHash: head.EventHash,
		KeyID: keyID, At: now,
	}
	sig := ed25519.Sign(priv, []byte(checkpointDoc(cp)))
	cp.SigHex = hex.EncodeToString(sig)
	l.mu.Lock()
	l.checkpoints[orgID] = append(l.checkpoints[orgID], cp)
	l.mu.Unlock()
	return &cp, nil
}

// Checkpoints returns one org's signed checkpoints (newest last).
func (l *OrgLedger) Checkpoints(orgID string) []Checkpoint {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Checkpoint, len(l.checkpoints[orgID]))
	copy(out, l.checkpoints[orgID])
	return out
}

// VerifyCheckpoint proves a checkpoint signature against its public key.
func VerifyCheckpoint(cp Checkpoint, pub ed25519.PublicKey) error {
	if len(pub) != ed25519.PublicKeySize {
		return errors.New("audit: ed25519 public key required")
	}
	sig, err := hex.DecodeString(cp.SigHex)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("audit: checkpoint signature for org %s is malformed", cp.OrgID)
	}
	if !ed25519.Verify(pub, []byte(checkpointDoc(cp)), sig) {
		return fmt.Errorf("%w: checkpoint for org %s seq %d fails signature verification", ErrChainInvalid, cp.OrgID, cp.Seq)
	}
	return nil
}

// checkpointDoc is the canonical checkpoint signing document.
func checkpointDoc(cp Checkpoint) string {
	return strings.Join([]string{cp.OrgID, fmt.Sprint(cp.Seq), cp.HeadHash, cp.KeyID}, "|")
}

// chainDigest folds export metadata into a hex digest for the trail event.
func chainDigest(grantID, reason string) string {
	sum := sha256.Sum256([]byte(grantID + "|" + reason))
	return hex.EncodeToString(sum[:])
}
