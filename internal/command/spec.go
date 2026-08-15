// Package command implements the M5 Slice 3 command gateway: a signed
// product manifest of CommandSpec templates (CMD-001), start-time
// argv/env/cwd validation (CMD-002) and Windows Job Object process trees
// with backgrounding, timeouts and orphan reaping (TASK-001).
package command

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Parameter types accepted in a ParamSpec.
const (
	ParamString = "string"
	ParamInt    = "int"
	ParamBool   = "bool"
	ParamPath   = "path"
)

// ParamSpec describes one command parameter.
type ParamSpec struct {
	Type     string `json:"type"` // string | int | bool | path
	Required bool   `json:"required"`
	MaxLen   int    `json:"maxLen,omitempty"` // string length ceiling
}

// CommandSpec is one allowlisted command template. ArgvTemplate entries
// carry {arg} placeholders that are substituted per-item; a rendered value
// is always a single literal argv entry and never a shell string.
type CommandSpec struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	Description    string               `json:"description"`
	ArgvTemplate   []string             `json:"argvTemplate"`
	ParamSchema    map[string]ParamSpec `json:"paramSchema"`
	EnvAllowlist   []string             `json:"envAllowlist"`
	CwdPolicy      string               `json:"cwdPolicy"` // "workspace"
	TimeoutMsUpper int64                `json:"timeoutMsUpper"`
	Version        string               `json:"version"`
}

// Manifest is the signed product catalogue shipped with the build.
type Manifest struct {
	Specs     []CommandSpec `json:"specs"`
	SignedAt  time.Time     `json:"signedAt"`
	ExpiresAt time.Time     `json:"expiresAt"`
	// Revoked lists sha256 hex digests of revoked specs (SpecDigest).
	Revoked []string `json:"revoked"`
	// Signature is an ed25519 signature over the canonical JSON of the
	// manifest with the Signature field removed.
	Signature []byte `json:"signature"`
}

// manifestPayload mirrors Manifest minus the signature so signing and
// verification share one canonical byte string.
type manifestPayload struct {
	Specs     []CommandSpec `json:"specs"`
	SignedAt  time.Time     `json:"signedAt"`
	ExpiresAt time.Time     `json:"expiresAt"`
	Revoked   []string      `json:"revoked"`
}

// canonicalJSON serialises v with every object key sorted (Go maps
// marshal in key order) so signatures and digests are byte-stable. Numbers
// round-trip via json.Number to avoid float64 re-encoding drift.
func canonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, err
	}
	return json.Marshal(generic)
}

// SpecDigest is the sha256 hex of a spec's canonical JSON; manifest
// identity and revocation both key off this digest.
func SpecDigest(spec CommandSpec) (string, error) {
	b, err := canonicalJSON(spec)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// SignManifest replaces m.Signature with an ed25519 signature over the
// canonical JSON of the manifest without the Signature field.
func SignManifest(m *Manifest, priv ed25519.PrivateKey) error {
	payload := manifestPayload{Specs: m.Specs, SignedAt: m.SignedAt, ExpiresAt: m.ExpiresAt, Revoked: m.Revoked}
	canonical, err := canonicalJSON(payload)
	if err != nil {
		return err
	}
	m.Signature = ed25519.Sign(priv, canonical)
	return nil
}

// LoadManifest verifies the manifest signature chain and returns the
// usable specs. Damaged JSON and failed verification answer ErrSpecSignature
// (CMD-001); a manifest past ExpiresAt answers ErrSpecExpired (CMD-001).
// Specs whose digest appears in Revoked are skipped and reported through an
// aggregated ErrSpecRevoked error; the surviving specs are still returned.
func LoadManifest(data []byte, rootPub ed25519.PublicKey, now time.Time) ([]CommandSpec, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: manifest JSON damaged: %v", ErrSpecSignature, err)
	}
	payload := manifestPayload{Specs: m.Specs, SignedAt: m.SignedAt, ExpiresAt: m.ExpiresAt, Revoked: m.Revoked}
	canonical, err := canonicalJSON(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: canonical encode failed: %v", ErrSpecSignature, err)
	}
	if len(m.Signature) == 0 || !ed25519.Verify(rootPub, canonical, m.Signature) {
		return nil, fmt.Errorf("%w: ed25519 verification failed", ErrSpecSignature)
	}
	if now.After(m.ExpiresAt) {
		return nil, fmt.Errorf("%w: expired at %s", ErrSpecExpired, m.ExpiresAt.UTC().Format(time.RFC3339))
	}
	revoked := make(map[string]bool, len(m.Revoked))
	for _, d := range m.Revoked {
		revoked[d] = true
	}
	kept := make([]CommandSpec, 0, len(m.Specs))
	var errs []error
	for _, s := range m.Specs {
		d, err := SpecDigest(s)
		if err != nil {
			return nil, fmt.Errorf("%w: digest of spec %s failed: %v", ErrSpecSignature, s.ID, err)
		}
		if revoked[d] {
			errs = append(errs, fmt.Errorf("%w: spec %s (%s) skipped", ErrSpecRevoked, s.ID, d))
			continue
		}
		kept = append(kept, s)
	}
	if len(errs) > 0 {
		return kept, errors.Join(errs...)
	}
	return kept, nil
}
