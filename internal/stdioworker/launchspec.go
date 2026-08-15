// Package stdioworker implements the M6 slice-5B stdio CONTROLLED
// implementation: the production strongly-isolated stdio worker runtime.
//
// It is a real implementation, not a POC artifact: signed launch specs
// (Ed25519 over a canonical digest, keyId rotation, nonce + deadline), a
// session-bound frame protocol (strict sequence numbers, 4 MiB cap),
// supply-chain verification of the worker executable digest, an fsync'd
// recovery journal, revocation that freezes late results, Job Object
// resource/process-tree enforcement on Windows, and stdio.* audit events.
//
// THE FEATURE IS DISABLED BY DEFAULT. Gate keeps the runtime closed until
// the 5C production escape acceptance passes AND a Security Owner signs
// off; the mcp6 registry keeps answering M6-MCP-004 at the transport gate
// regardless. Nothing in this repository opens the gate.
package stdioworker

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Wire error codes this package maps onto (M6_ERROR_CATALOG_V2).
const (
	// CodeSpecInvalid maps onto M6-DLG-001 envelope semantics: signature,
	// keyId, nonce or deadline failure refuses the launch.
	CodeSpecInvalid = "M6-DLG-001"
	// CodeQuotaExhausted maps onto M6-SBX-003: resource quota exhaustion.
	CodeQuotaExhausted = "M6-SBX-003"
	// CodeFenced maps onto M6-SBX-004: late result from a lost/revoked run.
	CodeFenced = "M6-SBX-004"
)

var (
	// ErrSpecSignature is M6-DLG-001: the launch spec signature, keyId,
	// nonce or deadline failed verification.
	ErrSpecSignature = errors.New("stdioworker: launch spec signature/nonce/deadline verification failed (M6-DLG-001)")
	// ErrSupplyChain: executable digest does not match the pinned digest.
	ErrSupplyChain = errors.New("stdioworker: worker executable digest does not match the signed launch spec")
	// ErrGateClosed: the stdio runtime gate is closed (default).
	ErrGateClosed = errors.New("stdioworker: runtime gate is closed (5C sign-off missing)")
	// ErrRevoked: the run was revoked; late results are frozen.
	ErrRevoked = errors.New("stdioworker: run revoked, result frozen (M6-SBX-004)")
)

var keyIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// LaunchSpec is the signed contract between the control plane and the
// isolated worker runtime. Every field is covered by the signature: the
// canonical JSON digest is what gets signed and re-verified at launch.
type LaunchSpec struct {
	// SpecID is a fresh ULID per launch; the nonce below forbids replay.
	SpecID string `json:"specId"`
	// EndpointID is the m6_mcp_endpoint this worker serves (stdio transport).
	EndpointID string `json:"endpointId"`
	// Command is the absolute worker executable path; Args are fixed argv.
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	// ExeDigest is the hex sha256 of the executable file (supply-chain pin).
	ExeDigest string `json:"exeDigest"`
	// CapabilitySet is the closed capability list the worker may use.
	CapabilitySet []string `json:"capabilitySet"`
	// Quotas bound the process tree.
	Quotas Quotas `json:"quotas"`
	// WorkingDir is the isolated sandbox root the child runs in.
	WorkingDir string `json:"workingDir"`

	// Nonce is a single-use random string; the runtime rejects replays.
	Nonce string `json:"nonce"`
	// NotBefore/ExpiresAt bound the spec validity window (clock skew of
	// ±90s is tolerated).
	NotBefore time.Time `json:"notBefore"`
	ExpiresAt time.Time `json:"expiresAt"`

	// ConfigDigest binds the launch to the frozen runtime policy digest so
	// 5A/5B/5C evidence can be tied to one build/config identity.
	ConfigDigest string `json:"configDigest"`

	// KeyID identifies the signing key (rotation support); the signature
	// itself travels beside the spec, not inside it.
	KeyID string `json:"keyId"`
}

// Quotas are the hard resource ceilings enforced by the OS (Job Object on
// Windows) plus the protocol watchdogs.
type Quotas struct {
	MaxProcs        uint32 `json:"maxProcs"`        // tree-wide process count
	MemoryCapBytes  uint64 `json:"memoryCapBytes"`  // tree-wide commit cap
	DeadlineMS      int64  `json:"deadlineMS"`      // whole-run wall clock
	HeartbeatMS     int64  `json:"heartbeatMS"`     // heartbeat interval
	MaxMissedBeats  int    `json:"maxMissedBeats"`  // beats before Reaper
}

// SignedSpec is a LaunchSpec plus its detached Ed25519 signature.
type SignedSpec struct {
	Spec       LaunchSpec `json:"spec"`
	Signature  string     `json:"signature"` // hex ed25519 over CanonicalDigest
}

// CanonicalJSON marshals v with Go's deterministic struct-field order.
func CanonicalJSON(v any) ([]byte, error) { return json.Marshal(v) }

// Digest returns the hex sha256 of the canonical JSON encoding.
func (s LaunchSpec) Digest() (string, error) {
	raw, err := CanonicalJSON(s)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Validate performs the schema-level checks that run before crypto.
func (s *LaunchSpec) Validate() error {
	if s.SpecID == "" || s.EndpointID == "" {
		return fmt.Errorf("%w: specId/endpointId required", ErrSpecSignature)
	}
	if !filepathIsAbs(s.Command) {
		return fmt.Errorf("%w: command must be absolute", ErrSpecSignature)
	}
	digest, err := hex.DecodeString(s.ExeDigest)
	if err != nil || len(digest) != sha256.Size {
		return fmt.Errorf("%w: exeDigest must be hex sha256", ErrSpecSignature)
	}
	if s.Nonce == "" || len(s.Nonce) > 128 {
		return fmt.Errorf("%w: nonce required (max 128 chars)", ErrSpecSignature)
	}
	if !keyIDPattern.MatchString(s.KeyID) {
		return fmt.Errorf("%w: keyId must match %s", ErrSpecSignature, keyIDPattern.String())
	}
	if s.Quotas.MaxProcs == 0 || s.Quotas.MemoryCapBytes == 0 {
		return fmt.Errorf("%w: process/memory quotas required", ErrSpecSignature)
	}
	if s.Quotas.DeadlineMS <= 0 || s.Quotas.HeartbeatMS <= 0 || s.Quotas.MaxMissedBeats < 1 {
		return fmt.Errorf("%w: deadline/heartbeat quotas required", ErrSpecSignature)
	}
	if !s.ExpiresAt.After(s.NotBefore) {
		return fmt.Errorf("%w: expiresAt must be after notBefore", ErrSpecSignature)
	}
	if s.WorkingDir == "" || !filepathIsAbs(s.WorkingDir) {
		return fmt.Errorf("%w: workingDir must be absolute", ErrSpecSignature)
	}
	for _, c := range s.CapabilitySet {
		if c == "" || len(c) > 64 {
			return fmt.Errorf("%w: bad capability entry", ErrSpecSignature)
		}
	}
	return nil
}

// Sign produces a SignedSpec; the signature covers the canonical digest.
func Sign(s LaunchSpec, priv ed25519.PrivateKey, keyID string) (*SignedSpec, error) {
	s.KeyID = keyID
	if err := s.Validate(); err != nil {
		return nil, err
	}
	d, err := s.Digest()
	if err != nil {
		return nil, err
	}
	sig := ed25519.Sign(priv, []byte(d))
	return &SignedSpec{Spec: s, Signature: hex.EncodeToString(sig)}, nil
}

// KeyStore resolves keyId → verification key; rotation is a config change.
type KeyStore interface {
	VerifyKey(keyID string) (ed25519.PublicKey, bool)
}

// MapKeyStore is the static implementation used by tests and config load.
type MapKeyStore map[string]ed25519.PublicKey

func (m MapKeyStore) VerifyKey(keyID string) (ed25519.PublicKey, bool) {
	k, ok := m[keyID]
	return k, ok
}

// clockSkew is the tolerated clock drift on the validity window.
const clockSkew = 90 * time.Second

// Verify checks schema, keyId, signature, validity window and nonce
// freshness. seenNonce (when non-nil) is the replay guard: a nonce already
// consumed for the same endpoint is rejected.
func (sp *SignedSpec) Verify(keys KeyStore, now time.Time, seenNonce func(endpointID, nonce string) bool) error {
	s := sp.Spec
	if err := s.Validate(); err != nil {
		return err
	}
	pub, ok := keys.VerifyKey(s.KeyID)
	if !ok {
		return fmt.Errorf("%w: unknown keyId %q", ErrSpecSignature, s.KeyID)
	}
	d, err := s.Digest()
	if err != nil {
		return err
	}
	sig, err := hex.DecodeString(sp.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("%w: signature encoding", ErrSpecSignature)
	}
	if !ed25519.Verify(pub, []byte(d), sig) {
		return fmt.Errorf("%w: signature mismatch", ErrSpecSignature)
	}
	if now.Before(s.NotBefore.Add(-clockSkew)) || now.After(s.ExpiresAt.Add(clockSkew)) {
		return fmt.Errorf("%w: spec outside validity window", ErrSpecSignature)
	}
	if seenNonce != nil && seenNonce(s.EndpointID, s.Nonce) {
		return fmt.Errorf("%w: nonce replay", ErrSpecSignature)
	}
	return nil
}

// NewNonce returns a fresh 128-bit hex nonce.
func NewNonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// filepathIsAbs avoids importing path/filepath just for this (keeps the
// package importable by pure-protocol builds).
func filepathIsAbs(p string) bool {
	if p == "" {
		return false
	}
	if strings.HasPrefix(p, "/") {
		return true
	}
	// Windows drive letter form.
	if len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		return true
	}
	return false
}
