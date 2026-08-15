package delegation

import (
	"crypto/ed25519"
	"encoding/json"
	"testing"
	"time"
)

func testEnvelope() (*Envelope, *Signer, KeyResolver) {
	signer, err := GenerateSigner("m6-control-1")
	if err != nil {
		panic(err)
	}
	env := &Envelope{
		Schema:        Schema,
		DelegationID:  "01ARZ3NDEKTSV4RRFFQ69G5FA9",
		RootID:        "01ARZ3NDEKTSV4RRFFQ69G5FA7",
		ParentID:      "node-12",
		ChildID:       "01ARZ3NDEKTSV4RRFFQ69G5FA8",
		Depth:         1,
		Objective:     "Refactor auth module tests",
		InputDigests:  []string{"e18fd15c7d72014d5b8d6cee758ec1a9f75618fab8ebfd2ebc3958c1d501924a"},
		CapabilitySet: []string{"fs.read", "command.run"},
		BudgetGrant:   BudgetGrant{CPUSeconds: 120, Tokens: 5000, Cost: 200, WallClockMs: 600000},
		Deadline:      time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
		Nonce:         "0123456789abcdef0123456789abcdef",
	}
	if err := signer.Sign(env); err != nil {
		panic(err)
	}
	keys := KeyResolver(func(keyID string) (ed25519.PublicKey, bool) {
		if keyID == signer.KeyID {
			return signer.Public, true
		}
		return nil, false
	})
	return env, signer, keys
}

func TestVerifyAcceptsSignedEnvelope(t *testing.T) {
	env, _, keys := testEnvelope()
	if err := Verify(env, keys, []string{"fs.read", "command.run", "fs.write"}, time.Now().UTC()); err != nil {
		t.Fatalf("valid envelope rejected: %v", err)
	}
}

func TestVerifyOrderSchemaFirst(t *testing.T) {
	env, _, keys := testEnvelope()
	env.Schema = "lunitide.delegation/v0"
	if err := Verify(env, keys, nil, time.Now().UTC()); err != ErrSchemaWrong {
		t.Fatalf("want ErrSchemaWrong, got %v", err)
	}
}

func TestVerifyRejectsBadSignature(t *testing.T) {
	env, _, keys := testEnvelope()
	env.Objective = "tampered objective"
	if err := Verify(env, keys, []string{"fs.read"}, time.Now().UTC()); err != ErrSignatureInvalid {
		t.Fatalf("want ErrSignatureInvalid, got %v", err)
	}
	unknown := KeyResolver(func(string) (ed25519.PublicKey, bool) { return nil, false })
	if err := Verify(env, unknown, nil, time.Now().UTC()); err != ErrKeyUnknown {
		t.Fatalf("want ErrKeyUnknown, got %v", err)
	}
}

func TestVerifyDeadlineAndDepth(t *testing.T) {
	env, signer, keys := testEnvelope()
	env.Deadline = time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if err := signer.Sign(env); err != nil {
		t.Fatal(err)
	}
	if err := Verify(env, keys, []string{"fs.read", "command.run"}, time.Now().UTC()); err != ErrDeadlineExceeded {
		t.Fatalf("want ErrDeadlineExceeded, got %v", err)
	}

	env2, signer2, keys2 := testEnvelope()
	env2.Depth = MaxDepth + 1
	if err := signer2.Sign(env2); err != nil {
		t.Fatal(err)
	}
	if err := Verify(env2, keys2, []string{"fs.read", "command.run"}, time.Now().UTC()); err != ErrDepthExceeded {
		t.Fatalf("want ErrDepthExceeded, got %v", err)
	}
}

func TestVerifyCapabilitySubset(t *testing.T) {
	env, signer, keys := testEnvelope()
	env.CapabilitySet = []string{"fs.read", "secret.reveal"}
	if err := signer.Sign(env); err != nil {
		t.Fatal(err)
	}
	if err := Verify(env, keys, []string{"fs.read", "command.run"}, time.Now().UTC()); err != ErrCapabilityEscalation {
		t.Fatalf("want ErrCapabilityEscalation, got %v", err)
	}
}

func TestVerifyMalformedFields(t *testing.T) {
	env, signer, keys := testEnvelope()
	env.InputDigests = []string{"nothex"}
	if err := signer.Sign(env); err != nil {
		t.Fatal(err)
	}
	if err := Verify(env, keys, nil, time.Now().UTC()); err == nil {
		t.Fatal("malformed input digest must be rejected")
	}

	env2, signer2, keys2 := testEnvelope()
	env2.BudgetGrant = BudgetGrant{}
	if err := signer2.Sign(env2); err != nil {
		t.Fatal(err)
	}
	if err := Verify(env2, keys2, nil, time.Now().UTC()); err == nil {
		t.Fatal("all-zero budget grant must be rejected")
	}
}

func TestDigestStableAndCanonicalDeterministic(t *testing.T) {
	env, _, _ := testEnvelope()
	d1 := Digest(env)
	if len(d1) != 64 {
		t.Fatalf("digest length: %d", len(d1))
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	var round Envelope
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatal(err)
	}
	if Digest(&round) != d1 {
		t.Fatal("digest must be stable across marshal round-trips")
	}
	env.Signature = "changed"
	if Digest(env) != d1 {
		t.Fatal("digest must ignore the signature field")
	}
}

func TestGenerateNonceShape(t *testing.T) {
	n, err := GenerateNonce()
	if err != nil {
		t.Fatal(err)
	}
	if len(n) != 64 {
		t.Fatalf("nonce length: %d", len(n))
	}
	m, _ := GenerateNonce()
	if n == m {
		t.Fatal("nonces must not repeat")
	}
}

func TestBudgetGrantGuards(t *testing.T) {
	if (BudgetGrant{}).NonZero() {
		t.Fatal("zero grant must not be non-zero")
	}
	if !(BudgetGrant{Tokens: 1}).NonZero() {
		t.Fatal("token grant must be non-zero")
	}
	if !(BudgetGrant{Tokens: -1}).Negative() {
		t.Fatal("negative grant must be flagged")
	}
}
