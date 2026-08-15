package stdioworker

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func testKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func validSpec() LaunchSpec {
	return LaunchSpec{
		SpecID:        "spec-1",
		EndpointID:    "ep-1",
		Command:       `C:\bin\worker.exe`,
		ExeDigest:     strings.Repeat("ab", 32),
		CapabilitySet: []string{"mcp.tools.read"},
		Quotas: Quotas{
			MaxProcs: 8, MemoryCapBytes: 256 << 20,
			DeadlineMS: 60_000, HeartbeatMS: 1000, MaxMissedBeats: 3,
		},
		WorkingDir: `C:\sbx\w1`,
		Nonce:      NewNonce(),
		NotBefore:  time.Now().Add(-time.Minute),
		ExpiresAt:  time.Now().Add(10 * time.Minute),
		ConfigDigest: "",
		KeyID:      "k-2026-08",
	}
}

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv := testKeyPair(t)
	spec := validSpec()
	sp, err := Sign(spec, priv, "k-2026-08")
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.Verify(MapKeyStore{"k-2026-08": pub}, time.Now(), nil); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerifyRejectsTamperedSpec(t *testing.T) {
	pub, priv := testKeyPair(t)
	sp, err := Sign(validSpec(), priv, "k-2026-08")
	if err != nil {
		t.Fatal(err)
	}
	// Tamper: raise the process quota after signing.
	sp.Spec.Quotas.MaxProcs = 64
	if err := sp.Verify(MapKeyStore{"k-2026-08": pub}, time.Now(), nil); err == nil {
		t.Fatal("tampered spec must fail verification")
	}
}

func TestVerifyRejectsUnknownKeyID(t *testing.T) {
	_, priv := testKeyPair(t)
	sp, err := Sign(validSpec(), priv, "k-2026-08")
	if err != nil {
		t.Fatal(err)
	}
	otherPub, _ := testKeyPair(t)
	if err := sp.Verify(MapKeyStore{"k-other": otherPub}, time.Now(), nil); err == nil {
		t.Fatal("unknown keyId must fail")
	}
}

func TestVerifyRejectsExpiredAndReplay(t *testing.T) {
	pub, priv := testKeyPair(t)
	spec := validSpec()
	spec.NotBefore = time.Now().Add(-time.Hour)
	// beyond the ±90s clock-skew window so expiry is unambiguous
	spec.ExpiresAt = time.Now().Add(-2 * time.Minute)
	sp, err := Sign(spec, priv, "k-2026-08")
	if err != nil {
		t.Fatal(err)
	}
	if err := sp.Verify(MapKeyStore{"k-2026-08": pub}, time.Now(), nil); err == nil {
		t.Fatal("expired spec must fail")
	}

	sp2, err := Sign(validSpec(), priv, "k-2026-08")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	guard := func(endpointID, nonce string) bool {
		k := endpointID + "|" + nonce
		if seen[k] {
			return true
		}
		seen[k] = true
		return false
	}
	if err := sp2.Verify(MapKeyStore{"k-2026-08": pub}, time.Now(), guard); err != nil {
		t.Fatal(err)
	}
	if err := sp2.Verify(MapKeyStore{"k-2026-08": pub}, time.Now(), guard); err == nil {
		t.Fatal("nonce replay must fail")
	}
}

func TestValidateSchema(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*LaunchSpec)
	}{
		{"relative command", func(s *LaunchSpec) { s.Command = "worker.exe" }},
		{"bad digest", func(s *LaunchSpec) { s.ExeDigest = "xyz" }},
		{"no nonce", func(s *LaunchSpec) { s.Nonce = "" }},
		{"bad keyId", func(s *LaunchSpec) { s.KeyID = "UPPER" }},
		{"zero procs", func(s *LaunchSpec) { s.Quotas.MaxProcs = 0 }},
		{"no deadline", func(s *LaunchSpec) { s.Quotas.DeadlineMS = 0 }},
		{"inverted window", func(s *LaunchSpec) { s.ExpiresAt = s.NotBefore }},
		{"relative workdir", func(s *LaunchSpec) { s.WorkingDir = "w" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := validSpec()
			tc.mutate(&spec)
			if err := spec.Validate(); err == nil {
				t.Fatalf("%s: want validation error", tc.name)
			}
		})
	}
}

func TestDigestDeterministic(t *testing.T) {
	a, err := validSpec().Digest()
	if err != nil {
		t.Fatal(err)
	}
	b, err := validSpec().Digest()
	if err != nil {
		t.Fatal(err)
	}
	// a and b differ in nonce only... validSpec() mints a fresh nonce every
	// call, so digests must differ; determinism is checked by re-digesting
	// the same value.
	s := validSpec()
	d1, _ := s.Digest()
	d2, _ := s.Digest()
	if d1 != d2 {
		t.Fatal("same spec must digest identically")
	}
	if a == b {
		t.Fatal("different nonces must produce different digests")
	}
	if _, err := hex.DecodeString(d1); err != nil {
		t.Fatalf("digest not hex: %v", err)
	}
}

// --- frame protocol ---------------------------------------------------------

func TestFrameRoundTripAndCaps(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("hello-frame")
	if err := WriteFrame(&buf, payload); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: %q", got)
	}
	if err := WriteFrame(&buf, []byte{}); err == nil {
		t.Fatal("empty frame must be refused")
	}
	big := make([]byte, MaxFrameBytes+1)
	if err := WriteFrame(&buf, big); err == nil {
		t.Fatal("oversize frame must be refused")
	}
	oversize := []byte{0x00, 0x41, 0x00, 0x00} // declares 4MiB+1
	if _, err := ReadFrame(bytes.NewReader(oversize)); err == nil {
		t.Fatal("oversize declared frame must be refused")
	}
}

func TestSessionValidatorBinding(t *testing.T) {
	v := NewSessionValidator("session-A")
	ok := &Envelope{SessionID: "session-A", Seq: 0, Type: EnvHello}
	if err := v.Validate(ok); err != nil {
		t.Fatal(err)
	}
	// forged session
	forged := &Envelope{SessionID: "session-B", Seq: 1, Type: EnvHeartbeat}
	if err := v.Validate(forged); err == nil {
		t.Fatal("forged session must fail")
	}
	// sequence gap / replay
	gap := &Envelope{SessionID: "session-A", Seq: 5, Type: EnvHeartbeat}
	if err := v.Validate(gap); err == nil {
		t.Fatal("sequence gap must fail")
	}
	replay := &Envelope{SessionID: "session-A", Seq: 0, Type: EnvHeartbeat}
	if err := v.Validate(replay); err == nil {
		t.Fatal("sequence replay must fail")
	}
	// unknown type
	weird := &Envelope{SessionID: "session-A", Seq: 1, Type: "admin"}
	if err := v.Validate(weird); err == nil {
		t.Fatal("unknown type must fail")
	}
	// correct next passes
	good := &Envelope{SessionID: "session-A", Seq: 1, Type: EnvHeartbeat}
	if err := v.Validate(good); err != nil {
		t.Fatal(err)
	}
}

// --- supply chain -----------------------------------------------------------

func TestVerifyExecutable(t *testing.T) {
	f := t.TempDir()
	path := f + "/worker.bin"
	if err := writeFile(path, []byte("binary-bytes")); err != nil {
		t.Fatal(err)
	}
	digest, err := FileDigest(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyExecutable(path, digest); err != nil {
		t.Fatal(err)
	}
	if err := VerifyExecutable(path, strings.Repeat("00", 32)); err == nil {
		t.Fatal("digest mismatch must fail")
	}
	if err := VerifyExecutable(path+"-missing", digest); err == nil {
		t.Fatal("missing file must fail")
	}
	// tamper after pinning
	if err := writeFile(path, []byte("tampered")); err != nil {
		t.Fatal(err)
	}
	if err := VerifyExecutable(path, digest); err == nil {
		t.Fatal("tampered binary must fail")
	}
}
