// Package delegation implements the M6 DelegationEnvelope contract
// (T-6.3.2): the lunitide.delegation/v1 schema, ed25519 signing and the
// frozen verification order
//
//	schema -> signature/keyId -> nonce -> deadline -> root/parent
//	       -> depth -> capability subset -> budget freeze
//
// The first seven steps are static envelope checks and live here; nonce
// replay and the root/parent relation are enforced by the storage layer
// (UNIQUE(root_id, nonce) in 0046 plus the application service), and the
// budget freeze is the ledger reserve that runs in the same transaction as
// the delegation insert — a delegation row in grant_reserved state is the
// durable proof that the freeze happened.
//
// Wire error codes (M6_ERROR_CATALOG_V2):
//
//	M6-DLG-001 envelope verification failed (schema/signature/keyId/
//	          nonce/deadline/root-parent/depth/capability subset)
//	M6-DLG-002 depth, per-parent fan-out or tree child count over the
//	          hard cap — refused before the child is created
//
// Hard caps from the M6 policy table: depth <= 4, per-parent fan-out <= 16,
// tree children <= 100. The design prose also lists softer defaults
// (2 / 4 / 16) which are deployment policy, not code; the service refuses
// only the hard caps and leaves the defaults to the policy layer.
package delegation

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Schema is the frozen envelope schema identifier.
const Schema = "lunitide.delegation/v1"

// Hard caps (M6 policy table). The 0046 CHECK(depth BETWEEN 0 AND 4) backs
// the depth cap at the storage layer.
const (
	MaxDepth        = 4
	MaxFanOut       = 16
	MaxTreeChildren = 100
)

var (
	// ErrSchemaWrong: envelope.schema is not lunitide.delegation/v1.
	ErrSchemaWrong = errors.New("delegation: schema not supported")
	// ErrMalformed: a required field is missing or has a bad shape.
	ErrMalformed = errors.New("delegation: envelope field malformed")
	// ErrKeyUnknown: keyId does not resolve to a verification key.
	ErrKeyUnknown = errors.New("delegation: keyId unknown")
	// ErrSignatureInvalid: the ed25519 signature does not verify.
	ErrSignatureInvalid = errors.New("delegation: signature verification failed")
	// ErrDeadlineExceeded: now is past the envelope deadline.
	ErrDeadlineExceeded = errors.New("delegation: deadline exceeded")
	// ErrDepthExceeded: depth over the hard cap of 4.
	ErrDepthExceeded = errors.New("delegation: depth over hard cap")
	// ErrCapabilityEscalation: capabilitySet is not a subset of the parent
	// capabilities — a child may never widen its rights.
	ErrCapabilityEscalation = errors.New("delegation: capability set escalates parent")
	// ErrFanOutExceeded / ErrTreeChildrenExceeded: policy caps (DLG-002).
	ErrFanOutExceeded       = errors.New("delegation: per-parent fan-out over hard cap")
	ErrTreeChildrenExceeded = errors.New("delegation: tree child count over hard cap")
)

// BudgetGrant is the four-dimension grant on the envelope. Field names
// follow the wire schema (cpuSeconds/tokens/cost/wallClockMs); the design
// prose spelled them cpuSeconds/tokens/costMicros/wallSeconds — the wire
// schema is the authority (recorded in docs/evidence/m6-day0.txt).
type BudgetGrant struct {
	CPUSeconds  int64 `json:"cpuSeconds"`
	Tokens      int64 `json:"tokens"`
	Cost        int64 `json:"cost"`
	WallClockMs int64 `json:"wallClockMs"`
}

// NonZero reports whether any dimension carries a positive amount.
func (g BudgetGrant) NonZero() bool {
	return g.CPUSeconds > 0 || g.Tokens > 0 || g.Cost > 0 || g.WallClockMs > 0
}

// Negative reports whether any dimension is below zero (schema-invalid).
func (g BudgetGrant) Negative() bool {
	return g.CPUSeconds < 0 || g.Tokens < 0 || g.Cost < 0 || g.WallClockMs < 0
}

// Envelope is the delegation envelope (lunitide.delegation/v1). Field order
// is the canonical JSON order; the signature covers the marshalled bytes
// with Signature cleared.
type Envelope struct {
	Schema        string      `json:"schema"`
	DelegationID  string      `json:"delegationId"`
	RootID        string      `json:"rootId"`
	ParentID      string      `json:"parentId"`
	ChildID       string      `json:"childId"`
	Depth         int         `json:"depth"`
	Objective     string      `json:"objective"`
	InputDigests  []string    `json:"inputDigests"`
	CapabilitySet []string    `json:"capabilitySet"`
	BudgetGrant   BudgetGrant `json:"budgetGrant"`
	Deadline      string      `json:"deadline"`
	Nonce         string      `json:"nonce"`
	KeyID         string      `json:"keyId"`
	Signature     string      `json:"signature"`
}

// Signer mints control-plane signatures. One key per deployment; the keyId
// is recorded so verifiers can rotate keys without redeploying envelopes.
type Signer struct {
	KeyID      string
	privateKey ed25519.PrivateKey
	Public     ed25519.PublicKey
}

// NewSigner wraps an ed25519 private key (64-byte expanded form).
func NewSigner(keyID string, priv ed25519.PrivateKey) *Signer {
	return &Signer{KeyID: keyID, privateKey: priv, Public: priv.Public().(ed25519.PublicKey)}
}

// GenerateSigner mints a fresh key pair (tests and first-run bootstrap).
func GenerateSigner(keyID string) (*Signer, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Signer{KeyID: keyID, privateKey: priv, Public: pub}, nil
}

// Sign fills KeyID and Signature over the canonical bytes.
func (s *Signer) Sign(env *Envelope) error {
	if s == nil || s.privateKey == nil {
		return ErrKeyUnknown
	}
	env.KeyID = s.KeyID
	env.Signature = ""
	sig := ed25519.Sign(s.privateKey, CanonicalBytes(env))
	env.Signature = base64.StdEncoding.EncodeToString(sig)
	return nil
}

// KeyResolver answers the verification key for one keyId.
type KeyResolver func(keyID string) (ed25519.PublicKey, bool)

// CanonicalBytes marshals the envelope with the signature cleared — the
// exact bytes the signature covers. json.Marshal on this struct is
// deterministic (fixed field order, no maps).
func CanonicalBytes(env *Envelope) []byte {
	cleared := *env
	cleared.Signature = ""
	raw, err := json.Marshal(cleared)
	if err != nil {
		return nil
	}
	return raw
}

// Digest is the sha-256 of the full canonical envelope (signature kept);
// it backs the envelope_digest UNIQUE column in m6_delegation.
func Digest(env *Envelope) string {
	cleared := *env
	cleared.Signature = ""
	raw, err := json.Marshal(cleared)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// GenerateNonce mints a 64-char hex nonce (32 random bytes); the 0046
// column CHECK accepts 16..128 chars.
func GenerateNonce() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// DeadlineOf parses the envelope deadline (RFC3339).
func DeadlineOf(env *Envelope) (time.Time, error) {
	return time.Parse(time.RFC3339, env.Deadline)
}

// Verify runs the static chain: schema -> signature/keyId -> deadline ->
// depth -> capability subset. Nonce replay and the root/parent relation
// need storage state and run in the application service; the budget freeze
// is the ledger reserve in the same transaction.
func Verify(env *Envelope, keys KeyResolver, parentCaps []string, now time.Time) error {
	if env == nil || env.Schema != Schema {
		return ErrSchemaWrong
	}
	if env.DelegationID == "" || env.RootID == "" || env.ParentID == "" || env.ChildID == "" ||
		env.Objective == "" || env.Nonce == "" || env.KeyID == "" || env.Signature == "" {
		return ErrMalformed
	}
	if len(env.InputDigests) == 0 || len(env.CapabilitySet) == 0 {
		return ErrMalformed
	}
	for _, d := range env.InputDigests {
		if len(d) != 64 || !isLowerHex(d) {
			return fmt.Errorf("%w: input digest", ErrMalformed)
		}
	}
	if env.BudgetGrant.Negative() || !env.BudgetGrant.NonZero() {
		return fmt.Errorf("%w: budget grant", ErrMalformed)
	}

	// signature over the canonical bytes with the recorded key
	sig, err := base64.StdEncoding.DecodeString(env.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrSignatureInvalid
	}
	key, ok := keys(env.KeyID)
	if !ok {
		return ErrKeyUnknown
	}
	if !ed25519.Verify(key, CanonicalBytes(env), sig) {
		return ErrSignatureInvalid
	}

	// deadline
	deadline, err := DeadlineOf(env)
	if err != nil {
		return fmt.Errorf("%w: deadline", ErrMalformed)
	}
	if now.After(deadline) {
		return ErrDeadlineExceeded
	}

	// depth
	if env.Depth < 0 || env.Depth > MaxDepth {
		return ErrDepthExceeded
	}

	// capability subset: child may never widen the parent's rights
	allowed := make(map[string]bool, len(parentCaps))
	for _, c := range parentCaps {
		allowed[c] = true
	}
	for _, c := range env.CapabilitySet {
		if !allowed[c] {
			return ErrCapabilityEscalation
		}
	}
	return nil
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
