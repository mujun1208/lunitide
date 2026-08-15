package stdiopoc

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func frameBytes(payload []byte) []byte {
	var head [4]byte
	binary.BigEndian.PutUint32(head[:], uint32(len(payload)))
	return append(head[:], payload...)
}

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte(`{"type":"ready","probe":"secret","nonce":"0123456789abcdef0123456789abcdef"}`)
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
}

func TestReadFrameOversizeDeclared(t *testing.T) {
	var head [4]byte
	binary.BigEndian.PutUint32(head[:], MaxFrameBytes+(1<<20))
	r := bytes.NewReader(append(head[:], 'x'))
	_, err := ReadFrame(r)
	if !errors.Is(err, ErrOversizeFrame) {
		t.Fatalf("want ErrOversizeFrame, got %v", err)
	}
}

func TestReadFrameOversizeReal(t *testing.T) {
	// A real >4MiB byte stream: the reader must reject on the header
	// without trying to buffer it.
	var head [4]byte
	binary.BigEndian.PutUint32(head[:], MaxFrameBytes+1)
	r := bytes.NewReader(append(head[:], bytes.Repeat([]byte{0}, MaxFrameBytes+1)...))
	_, err := ReadFrame(r)
	if !errors.Is(err, ErrOversizeFrame) {
		t.Fatalf("want ErrOversizeFrame, got %v", err)
	}
}

func TestReadFrameZeroLength(t *testing.T) {
	r := bytes.NewReader(make([]byte, 4))
	_, err := ReadFrame(r)
	if !errors.Is(err, ErrMalformedFrame) {
		t.Fatalf("want ErrMalformedFrame, got %v", err)
	}
}

func TestReadFrameTruncatedPayload(t *testing.T) {
	frame := frameBytes(bytes.Repeat([]byte{'a'}, 100))
	r := bytes.NewReader(frame[:54]) // header + 50 of 100 bytes
	_, err := ReadFrame(r)
	if !errors.Is(err, ErrMalformedFrame) {
		t.Fatalf("want ErrMalformedFrame, got %v", err)
	}
}

func TestReadFrameShortHeader(t *testing.T) {
	_, err := ReadFrame(bytes.NewReader([]byte{1, 2, 3}))
	if !errors.Is(err, ErrMalformedFrame) {
		t.Fatalf("want ErrMalformedFrame, got %v", err)
	}
}

func TestWriteFrameRefuses(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, nil); !errors.Is(err, ErrMalformedFrame) {
		t.Fatalf("empty: want ErrMalformedFrame, got %v", err)
	}
	if err := WriteFrame(&buf, bytes.Repeat([]byte{'x'}, MaxFrameBytes+1)); !errors.Is(err, ErrOversizeFrame) {
		t.Fatalf("oversize: want ErrOversizeFrame, got %v", err)
	}
}

// --- envelope validator ------------------------------------------------------

func validEnvPayload(mutate func(*FrameEnvelope)) []byte {
	env := FrameEnvelope{
		Type:  EnvelopeTypeReport,
		Nonce: "0123456789abcdef0123456789abcdef",
		Probe: AssumptionSecret,
	}
	if mutate != nil {
		mutate(&env)
	}
	raw, _ := json.Marshal(env)
	return raw
}

func TestParseEnvelopeValid(t *testing.T) {
	env, err := ParseEnvelope(validEnvPayload(nil), AssumptionSecret)
	if err != nil {
		t.Fatal(err)
	}
	if env.Type != EnvelopeTypeReport || env.Probe != AssumptionSecret {
		t.Fatalf("bad parse: %+v", env)
	}
}

func TestParseEnvelopeForgedType(t *testing.T) {
	payload := validEnvPayload(func(e *FrameEnvelope) { e.Type = "admin" })
	_, err := ParseEnvelope(payload, "")
	if !errors.Is(err, ErrForgedFrame) {
		t.Fatalf("want ErrForgedFrame, got %v", err)
	}
}

func TestParseEnvelopeBadNonce(t *testing.T) {
	payload := validEnvPayload(func(e *FrameEnvelope) { e.Nonce = "NOT-HEX-NONCE-000000000000000" })
	_, err := ParseEnvelope(payload, "")
	if !errors.Is(err, ErrForgedFrame) {
		t.Fatalf("want ErrForgedFrame, got %v", err)
	}
}

func TestParseEnvelopeWrongProbe(t *testing.T) {
	payload := validEnvPayload(func(e *FrameEnvelope) { e.Probe = "root-escalation" })
	_, err := ParseEnvelope(payload, "")
	if !errors.Is(err, ErrForgedFrame) {
		t.Fatalf("want ErrForgedFrame, got %v", err)
	}
}

func TestParseEnvelopeProbeMismatch(t *testing.T) {
	payload := validEnvPayload(func(e *FrameEnvelope) { e.Probe = AssumptionNetwork })
	_, err := ParseEnvelope(payload, AssumptionSecret)
	if !errors.Is(err, ErrForgedFrame) {
		t.Fatalf("want ErrForgedFrame, got %v", err)
	}
}

func TestParseEnvelopeGarbage(t *testing.T) {
	_, err := ParseEnvelope([]byte("<<<not json>>>"), "")
	if !errors.Is(err, ErrMalformedFrame) {
		t.Fatalf("want ErrMalformedFrame, got %v", err)
	}
}

// TestWriteBadFrameAlignment replays the whole protocol attack script
// against a pipe-like buffer and asserts the exact classification plus
// stream alignment, mirroring what the harness does with a real child.
func TestWriteBadFrameAlignment(t *testing.T) {
	var buf bytes.Buffer
	for _, atk := range ProtocolAttacks {
		if err := writeEnvelope(&buf, EnvelopeTypeAttack, AssumptionProtocol, atk); err != nil {
			t.Fatal(err)
		}
		if err := writeBadFrame(&buf, atk.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeEnvelope(&buf, EnvelopeTypeReport, AssumptionProtocol, probeReport{Detail: "done"}); err != nil {
		t.Fatal(err)
	}
	for _, atk := range ProtocolAttacks {
		announce, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("announce %s: %v", atk.ID, err)
		}
		env, err := ParseEnvelope(announce, AssumptionProtocol)
		if err != nil || env.Type != EnvelopeTypeAttack {
			t.Fatalf("announce %s invalid: %v", atk.ID, err)
		}
		bad, err := ReadFrame(&buf)
		class := ""
		if err == nil {
			if _, perr := ParseEnvelope(bad, AssumptionProtocol); perr != nil {
				class = classifyFrameErr(perr)
			} else {
				class = "accepted"
			}
		} else {
			class = classifyFrameErr(err)
		}
		if class != atk.Expect {
			t.Fatalf("attack %s: classified %q, want %q", atk.ID, class, atk.Expect)
		}
	}
	// Trailing report must still parse: alignment survived the abuse.
	rep, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("trailing report: %v", err)
	}
	env, err := ParseEnvelope(rep, AssumptionProtocol)
	if err != nil || env.Type != EnvelopeTypeReport {
		t.Fatalf("trailing report invalid: %v", err)
	}
}

func TestMinimalEnv(t *testing.T) {
	parent := []string{
		"B=2", "A=1", "SECRET=leak", "SystemRoot=C:\\Windows",
	}
	got := MinimalEnv(parent, []string{"a", "systemroot"}, map[string]string{"K": "V"})
	want := []string{"A=1", "K=V", "SystemRoot=C:\\Windows"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("MinimalEnv = %v, want %v", got, want)
	}
}

func TestBuildBundleVerdict(t *testing.T) {
	pass := Assumption{ID: AssumptionSecret, Passed: true}
	fail := Assumption{ID: AssumptionProtocol, Passed: false}
	b, err := BuildBundle([]Assumption{pass}, dummyNow(), "windows")
	if err != nil {
		t.Fatal(err)
	}
	if b.Verdict != VerdictPass {
		t.Fatalf("want PASS, got %s", b.Verdict)
	}
	if len(b.Assumptions) != 1 || b.Assumptions[0].Digest == "" {
		t.Fatalf("digest chain missing: %+v", b.Assumptions)
	}
	b2, err := BuildBundle([]Assumption{pass, fail}, dummyNow(), "windows")
	if err != nil {
		t.Fatal(err)
	}
	if b2.Verdict != VerdictFail {
		t.Fatalf("want FAIL, got %s", b2.Verdict)
	}
	if b2.BundleDigest == b.BundleDigest {
		t.Fatal("bundle digest must change with content")
	}
}
