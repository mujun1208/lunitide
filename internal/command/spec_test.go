package command

import (
	"bytes"
	"crypto/ed25519"
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func testSpecs() []CommandSpec {
	return []CommandSpec{
		{
			ID: "spec-gofmt", Name: "gofmt", Description: "format go files",
			ArgvTemplate: []string{"gofmt", "-w", "{file}"},
			ParamSchema:  map[string]ParamSpec{"file": {Type: ParamPath, Required: true}},
			EnvAllowlist: []string{"PATH"}, CwdPolicy: "workspace",
			TimeoutMsUpper: 30000, Version: "1",
		},
		{
			ID: "spec-gotest", Name: "gotest", Description: "run package tests",
			ArgvTemplate: []string{"go", "test", "-count=1", "{pkg}"},
			ParamSchema:  map[string]ParamSpec{"pkg": {Type: ParamString, Required: true, MaxLen: 256}},
			EnvAllowlist: []string{"PATH", "GOPATH"}, CwdPolicy: "workspace",
			TimeoutMsUpper: 600000, Version: "1",
		},
	}
}

// signFixture builds a signed manifest JSON for the given revocation list.
func signFixture(t *testing.T, priv ed25519.PrivateKey, now time.Time, revoked []string) []byte {
	t.Helper()
	m := Manifest{
		Specs: testSpecs(), SignedAt: now.Add(-time.Hour),
		ExpiresAt: now.Add(24 * time.Hour), Revoked: revoked,
	}
	if err := SignManifest(&m, priv); err != nil {
		t.Fatalf("SignManifest: %v", err)
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return data
}

func TestSpecSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(crand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	data := signFixture(t, priv, now, nil)

	// Positive: a properly signed, unexpired manifest loads fully.
	got, err := LoadManifest(data, pub, now)
	if err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	if len(got) != 2 || got[0].ID != "spec-gofmt" || got[1].ID != "spec-gotest" {
		t.Fatalf("unexpected specs: %+v", got)
	}

	// Negative: one flipped byte in the manifest body changes the
	// canonical bytes and must break verification.
	tampered := bytes.Replace(data, []byte(`"gofmt"`), []byte(`"gofmtx"`), 1)
	if bytes.Equal(tampered, data) {
		t.Fatal("tamper fixture did not modify the manifest")
	}
	if _, err := LoadManifest(tampered, pub, now); !errors.Is(err, ErrSpecSignature) {
		t.Fatalf("body tamper want ErrSpecSignature, got %v", err)
	}

	// Negative: one flipped byte inside the signature itself.
	badSig := bytes.Replace(data, []byte(`"signature":"`), []byte(`"signature":"A`), 1)
	if bytes.Equal(badSig, data) {
		t.Fatal("signature tamper fixture did not modify the manifest")
	}
	if _, err := LoadManifest(badSig, pub, now); !errors.Is(err, ErrSpecSignature) {
		t.Fatalf("signature tamper want ErrSpecSignature, got %v", err)
	}

	// Negative: truncated JSON is a damaged manifest, still CMD-001.
	if _, err := LoadManifest(data[:len(data)/2], pub, now); !errors.Is(err, ErrSpecSignature) {
		t.Fatalf("damaged JSON want ErrSpecSignature, got %v", err)
	}

	// Negative: wrong root key must not verify.
	otherPub, _, err := ed25519.GenerateKey(crand.Reader)
	if err != nil {
		t.Fatalf("generate other key: %v", err)
	}
	if _, err := LoadManifest(data, otherPub, now); !errors.Is(err, ErrSpecSignature) {
		t.Fatalf("wrong key want ErrSpecSignature, got %v", err)
	}

	// Negative: past ExpiresAt the manifest is refused as expired.
	if _, err := LoadManifest(data, pub, now.Add(25*time.Hour)); !errors.Is(err, ErrSpecExpired) {
		t.Fatalf("expired want ErrSpecExpired, got %v", err)
	}

	// Negative: a revoked spec digest drops that spec and aggregates
	// ErrSpecRevoked while the sibling spec still loads.
	digest, err := SpecDigest(testSpecs()[0])
	if err != nil {
		t.Fatalf("SpecDigest: %v", err)
	}
	revokedData := signFixture(t, priv, now, []string{digest})
	got, err = LoadManifest(revokedData, pub, now)
	if !errors.Is(err, ErrSpecRevoked) {
		t.Fatalf("revoked want ErrSpecRevoked, got %v", err)
	}
	if len(got) != 1 || got[0].ID != "spec-gotest" {
		t.Fatalf("revoked spec must be skipped, got %+v", got)
	}
}
