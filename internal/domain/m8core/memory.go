// Package m8core holds the M8 slice-1 domain (T-8.1.x): the governed
// long-term memory core. MemoryCandidate -> MemoryFact promotion is
// explicit-only (FR-02/FR-11): a one-time confirmation token bound to
// candidate_id+payload_digest+subject+expiry is the sole promotion path.
// MemoryFact is an immutable version chain; SourceLeaf rows bind every fact
// version to leaf-level evidence with tamper-evident digests (FR-03);
// RecallTrace stores only the minimal explanation payload and never writes
// back into facts (FR-04).
package m8core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrNotFound answers any missing memory-core row.
var ErrNotFound = errors.New("m8core: not found")

// Candidate states (migration 0061 CHECK).
const (
	CandPending   = "pending"
	CandConfirmed = "confirmed"
	CandRejected  = "rejected"
	CandExpired   = "expired"
)

// Fact states.
const (
	FactActive     = "active"
	FactSuperseded = "superseded"
	FactTombstoned = "tombstoned"
)

// Sensitivity levels.
const (
	SensPublic   = "public"
	SensPrivate  = "private"
	SensSensitive = "sensitive"
)

// Trust levels.
const (
	TrustUntrusted        = "untrusted"
	TrustConfirmedSource  = "confirmed_source"
)

// Field limits mirroring migration 0061 CHECKs.
const (
	MaxSubjectID      = 128
	MaxScopeID        = 128
	MaxPayloadBytes   = 65536
	MaxJSONPointer    = 512
	MaxEvidenceRef    = 512
	DigestHexLen      = 64
	DefaultTokenTTL   = 168 * time.Hour
	RecallDefaultTopK = 10
	RecallMaxTopK     = 50
	// RecallScoreFloor is the keyword-coverage score under which a fact is
	// not adopted into hits (reported as a rule, never by identifier).
	RecallScoreFloor = 0.15
)

// CandTerminal reports whether a candidate state is final.
func CandTerminal(state string) bool {
	switch state {
	case CandConfirmed, CandRejected, CandExpired:
		return true
	}
	return false
}

// CandTransitionAllowed enacts pending -> {confirmed,rejected,expired}.
func CandTransitionAllowed(from, to string) bool {
	if from != CandPending {
		return false
	}
	switch to {
	case CandConfirmed, CandRejected, CandExpired:
		return true
	}
	return false
}

// SensitivityAllowed validates a sensitivity level.
func SensitivityAllowed(s string) bool {
	return s == SensPublic || s == SensPrivate || s == SensSensitive
}

// TrustAllowed validates a trust level.
func TrustAllowed(t string) bool {
	return t == TrustUntrusted || t == TrustConfirmedSource
}

// ValidHexDigest reports whether s is a 64-char lowercase hex digest.
func ValidHexDigest(s string) bool {
	if len(s) != DigestHexLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ValidJSONPointer enforces RFC 6901 shape (leaf evidence locators).
func ValidJSONPointer(p string) bool {
	if len(p) < 1 || len(p) > MaxJSONPointer || p[0] != '/' {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] < 0x20 || p[i] == 0x7f {
			return false
		}
	}
	return true
}

// SourceLeafClaim is one leaf-level evidence binding proposed with a
// candidate: where in the payload document the claim lives, which evidence
// artifact backs it and the tamper-evident digest of that evidence.
type SourceLeafClaim struct {
	JSONPointer string `json:"jsonPointer"`
	EvidenceRef string `json:"evidenceRef"`
	Digest      string `json:"digest"`
}

// PayloadDoc is the canonical candidate payload document. The SHA-256 over
// its canonical JSON (sorted keys, no whitespace) is the payload_digest that
// the confirmation token binds; scopeId and sensitivity therefore ride inside
// the digest.
type PayloadDoc struct {
	Content     string            `json:"content"`
	ScopeID     string            `json:"scopeId"`
	Sensitivity string            `json:"sensitivity"`
	Leaves      []SourceLeafClaim `json:"leaves"`
}

// CanonicalPayload marshals the payload document canonically.
func (p PayloadDoc) CanonicalPayload() ([]byte, error) {
	return json.Marshal(p) // struct fields keep fixed order; json.Marshal emits no whitespace
}

// PayloadDigest is the SHA-256 of the canonical payload document.
func (p PayloadDoc) PayloadDigest() (string, error) {
	b, err := p.CanonicalPayload()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Validate checks the payload document against the FR-02/FR-03 invariants:
// non-empty content/scope, valid sensitivity and 100% leaf coverage
// (at least one leaf, all pointers/digests well-formed).
func (p PayloadDoc) Validate() error {
	if len(p.Content) < 1 || len(p.ScopeID) < 1 || len(p.ScopeID) > MaxScopeID {
		return fmt.Errorf("m8core: payload content/scope invalid")
	}
	if !SensitivityAllowed(p.Sensitivity) {
		return fmt.Errorf("m8core: sensitivity %q invalid", p.Sensitivity)
	}
	if len(p.Leaves) < 1 {
		return fmt.Errorf("m8core: source leaf required")
	}
	for i, l := range p.Leaves {
		if !ValidJSONPointer(l.JSONPointer) || len(l.EvidenceRef) < 1 || len(l.EvidenceRef) > MaxEvidenceRef {
			return fmt.Errorf("m8core: leaf %d pointer/evidence invalid", i)
		}
		if !ValidHexDigest(l.Digest) {
			return fmt.Errorf("m8core: leaf %d digest invalid", i)
		}
	}
	return nil
}

// MemoryCandidate is one pending promotion proposal (migration 0061).
type MemoryCandidate struct {
	CandidateID   string
	SubjectID     string
	Payload       string // canonical JSON of PayloadDoc
	PayloadDigest string
	Inferred      bool
	Trust         string
	State         string
	ConfirmToken  string
	ExpiresAt     string // UTC RFC3339
	CreatedAt     string
	ConfirmedAt   string
}

// MemoryFact is one immutable version row of a governed fact.
type MemoryFact struct {
	FactID      string
	ScopeID     string
	Version     int64
	Sensitivity string
	State       string
	SupersededBy string
	CreatedAt   string
}

// SourceLeaf is one persisted leaf evidence binding (FR-03).
type SourceLeaf struct {
	ID          string
	FactID      string
	FactVersion int64
	JSONPointer string
	EvidenceRef string
	Digest      string
	CreatedAt   string
}

// RecallTrace is the minimal recall explanation record (FR-04); it never
// writes back into facts.
type RecallTrace struct {
	ID            string
	QueryDigest   string
	HitsJSON      string
	ReasonsJSON   string
	RedactionsJSON string
	CreatedAt     string
}

// tokenBindingDoc is the canonical document the confirmation token covers.
// The token thereby binds candidate_id + payload_digest (which embeds scope)
// + subject + expiry (FR-02 显式晋升).
type tokenBindingDoc struct {
	CandidateID   string `json:"candidate_id"`
	PayloadDigest string `json:"payload_digest"`
	SubjectID     string `json:"subject_id"`
	ExpiresAt     string `json:"expires_at"`
}

// DeriveConfirmToken computes the one-time confirmation token for a
// candidate. Deterministic on the stored row so the confirm path can
// re-verify without extra state.
func DeriveConfirmToken(candidateID, payloadDigest, subjectID, expiresAt string) string {
	b, _ := json.Marshal(tokenBindingDoc{
		CandidateID:   candidateID,
		PayloadDigest: payloadDigest,
		SubjectID:     subjectID,
		ExpiresAt:     expiresAt,
	})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// DigestOf is the shared lowercase-hex SHA-256 helper.
func DigestOf(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}
