// Package skill implements the M5 T-5.3.5 builtin skill registry. Builtin
// manifests are embedded in the product binary and are accepted only with a
// valid ed25519 signature from the product root key. The embed FS is the
// single source of manifests: no function in this package accepts a path,
// directory or caller-supplied filesystem, so third-party skills have no
// loading entry at the code level — this is the physical guarantee behind
// the "product signature only" rule (wire error family SKL-001).
package skill

import (
	"crypto/ed25519"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"
)

//go:embed manifests/*.json
var builtinFS embed.FS

var (
	// ErrSkillRejected aggregates per-manifest refusals: bad signature,
	// tampered digest, expiry or malformed structure (SKL-001).
	ErrSkillRejected = errors.New("skill: manifest rejected")
	// ErrSkillRegistryEmpty answers a registry with no loadable manifest
	// (including a missing/nil root key, which is a wiring bug).
	ErrSkillRegistryEmpty = errors.New("skill: builtin registry is empty")
)

// Step is one manifest step: a named action with its parameter schema.
type Step struct {
	Name        string         `json:"name"`
	Action      string         `json:"action"`
	ParamSchema map[string]any `json:"paramSchema,omitempty"`
}

// SkillManifest is a signed builtin skill manifest. Digest is the hex
// sha256 of the canonical JSON (every field except Digest and Signature,
// object keys sorted); Signature is an ed25519 signature over those same
// canonical bytes.
type SkillManifest struct {
	ID           string    `json:"id"`
	Version      string    `json:"version"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Capabilities []string  `json:"capabilities"`
	Steps        []Step    `json:"steps"`
	Digest       string    `json:"digest"`
	Signature    []byte    `json:"signature"`
	SignedAt     time.Time `json:"signedAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

// LoadBuiltins loads the embedded builtin manifests and verifies every one
// against the product root public key. A nil key is a wiring bug and
// answers ErrSkillRegistryEmpty; any rejected manifest fails the whole load
// with the per-manifest SKL-001 details joined onto ErrSkillRejected.
func LoadBuiltins(rootPub ed25519.PublicKey, now time.Time) ([]SkillManifest, error) {
	return loadFromFS(builtinFS, rootPub, now)
}

// loadFromFS is the testable core: LoadBuiltins passes the embed FS. Tests
// inject fstest.MapFS to exercise the verification matrix; it stays
// unexported so production callers cannot reach it with a foreign FS.
func loadFromFS(fsys fs.FS, rootPub ed25519.PublicKey, now time.Time) ([]SkillManifest, error) {
	if len(rootPub) != ed25519.PublicKeySize {
		return nil, ErrSkillRegistryEmpty
	}
	entries, err := fs.ReadDir(fsys, "manifests")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSkillRegistryEmpty, err)
	}
	var manifests []SkillManifest
	var rejected []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := "manifests/" + entry.Name()
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			rejected = append(rejected, fmt.Errorf("%w: %s: %v", ErrSkillRejected, path, err))
			continue
		}
		m, err := verifyManifest(data, rootPub, now)
		if err != nil {
			rejected = append(rejected, fmt.Errorf("%w: %s: %v", ErrSkillRejected, manifestID(data), err))
			continue
		}
		manifests = append(manifests, m)
	}
	if len(rejected) > 0 {
		return nil, errors.Join(rejected...)
	}
	if len(manifests) == 0 {
		return nil, ErrSkillRegistryEmpty
	}
	return manifests, nil
}

// verifyManifest runs one manifest through the full acceptance chain:
// structure, digest, signature, expiry.
func verifyManifest(data []byte, rootPub ed25519.PublicKey, now time.Time) (SkillManifest, error) {
	var m SkillManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if m.ID == "" || m.Name == "" || !validVersion(m.Version) {
		return m, errors.New("id, name and X.Y.Z version are required")
	}
	canonical, err := canonicalManifestBytes(m)
	if err != nil {
		return m, err
	}
	sum := sha256.Sum256(canonical)
	if m.Digest != hex.EncodeToString(sum[:]) {
		return m, errors.New("digest does not match canonical content")
	}
	if len(m.Signature) != ed25519.SignatureSize {
		return m, errors.New("signature must be 64 bytes")
	}
	if !ed25519.Verify(rootPub, canonical, m.Signature) {
		return m, errors.New("signature verification failed")
	}
	if !m.ExpiresAt.IsZero() && now.After(m.ExpiresAt) {
		return m, errors.New("manifest expired")
	}
	return m, nil
}

// canonicalManifestBytes serializes the manifest with the Digest and
// Signature fields removed and every object key sorted (Go marshals maps in
// key order, and the JSON round-trip normalizes times and numbers so both
// the signer and the verifier derive identical bytes).
func canonicalManifestBytes(m SkillManifest) ([]byte, error) {
	payload := map[string]any{
		"id":           m.ID,
		"version":      m.Version,
		"name":         m.Name,
		"description":  m.Description,
		"capabilities": m.Capabilities,
		"steps":        m.Steps,
		"signedAt":     m.SignedAt,
		"expiresAt":    m.ExpiresAt,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var normalized map[string]any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

// validVersion enforces the frozen X.Y.Z numeric shape.
func validVersion(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 8 {
			return false
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// manifestID best-effort extracts the id for rejection messages.
func manifestID(data []byte) string {
	var probe struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &probe); err != nil || probe.ID == "" {
		return "unknown"
	}
	return probe.ID
}
