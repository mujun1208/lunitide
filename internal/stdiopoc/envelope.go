package stdiopoc

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// FrameEnvelope is the typed payload every POC frame carries. The validator
// is intentionally strict: unknown types, malformed nonces or probe ids that
// do not match the negotiated probe are forged frames and must be rejected
// even though the framing itself is syntactically valid.
type FrameEnvelope struct {
	Type  string          `json:"type"`  // "report" | "attack" | "ready"
	Nonce string          `json:"nonce"` // 16-byte hex anti-replay tag
	Probe string          `json:"probe"` // emitting probe id (AssumptionID)
	Data  json.RawMessage `json:"data,omitempty"`
}

// Envelope types.
const (
	EnvelopeTypeReport = "report"
	EnvelopeTypeAttack = "attack"
	EnvelopeTypeReady  = "ready"
)

// NewNonce builds a hex nonce from 16 bytes.
func NewNonce(b [16]byte) string { return hex.EncodeToString(b[:]) }

// validNonce: 32 lowercase hex chars.
func validNonce(n string) bool {
	if len(n) != 32 {
		return false
	}
	for _, c := range n {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// ParseEnvelope decodes one frame payload and validates it against the
// expected probe id. Errors wrap ErrForgedFrame (bad envelope) or
// ErrMalformedFrame (not JSON at all).
func ParseEnvelope(payload []byte, expectedProbe string) (*FrameEnvelope, error) {
	var env FrameEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, fmt.Errorf("%w: payload is not an envelope: %v", ErrMalformedFrame, err)
	}
	switch env.Type {
	case EnvelopeTypeReport, EnvelopeTypeAttack, EnvelopeTypeReady:
	default:
		return nil, fmt.Errorf("%w: unknown envelope type %q", ErrForgedFrame, env.Type)
	}
	if !validNonce(env.Nonce) {
		return nil, fmt.Errorf("%w: bad nonce %q", ErrForgedFrame, env.Nonce)
	}
	if !validAssumptionID(env.Probe) {
		return nil, fmt.Errorf("%w: unknown probe id %q", ErrForgedFrame, env.Probe)
	}
	if expectedProbe != "" && env.Probe != expectedProbe {
		return nil, fmt.Errorf("%w: probe mismatch %q != %q", ErrForgedFrame, env.Probe, expectedProbe)
	}
	return &env, nil
}

// validAssumptionID reports whether id is one of the six POC assumptions.
func validAssumptionID(id string) bool {
	_, ok := assumptionByID(id)
	return ok
}

// String implements a compact one-line rendering for logs/reports.
func (e *FrameEnvelope) String() string {
	if e == nil {
		return "<nil envelope>"
	}
	return fmt.Sprintf("frame(%s probe=%s nonce=%s bytes=%d)",
		e.Type, e.Probe, e.Nonce[:8], len(e.Data))
}

// trimForDetail keeps error details bounded in evidence bundles.
func trimForDetail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 512 {
		return s[:512] + "...(truncated)"
	}
	return s
}
