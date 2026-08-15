package stdioworker

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Production frame protocol (5B): 4-byte big-endian length prefix + payload,
// hard cap 4 MiB per frame. Frames carry session-bound envelopes — every
// envelope must quote the sessionID assigned at launch and a strictly
// increasing sequence number starting at 0. Anything else is protocol
// cheating and terminates the session (negative tests bind this behavior
// to the same build/config digest as the rest of 5B).

// MaxFrameBytes is the hard per-frame payload cap.
const MaxFrameBytes = 4 << 20

// Envelope types of the 5B contract.
const (
	EnvHello     = "hello"     // child → host, once, binds spec digest
	EnvHeartbeat = "heartbeat" // child → host, periodic
	EnvResult    = "result"    // child → host, final payload
	EnvJob       = "job"       // host → child, work item
	EnvAck       = "ack"       // child → host, job accepted
)

var envelopeTypes = map[string]bool{
	EnvHello: true, EnvHeartbeat: true, EnvResult: true, EnvJob: true, EnvAck: true,
}

// Envelope is the payload of one frame.
type Envelope struct {
	SessionID string          `json:"sessionId"`
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// Validate checks one inbound envelope against the session contract.
func (e *Envelope) Validate(sessionID string, nextSeq int64) error {
	if e.SessionID != sessionID {
		return fmt.Errorf("stdioworker: forged session id %q (want %q)", e.SessionID, sessionID)
	}
	if e.Seq != nextSeq {
		return fmt.Errorf("stdioworker: sequence gap: got %d want %d", e.Seq, nextSeq)
	}
	if !envelopeTypes[e.Type] {
		return fmt.Errorf("stdioworker: unknown envelope type %q", e.Type)
	}
	return nil
}

// SessionValidator is the per-stream state machine: it enforces session
// binding and monotonic sequence on the child→host stream.
type SessionValidator struct {
	sessionID string
	next      int64
}

// NewSessionValidator builds the validator for one child stream.
func NewSessionValidator(sessionID string) *SessionValidator {
	return &SessionValidator{sessionID: sessionID}
}

// Validate advances the sequence when the envelope is legit.
func (v *SessionValidator) Validate(e *Envelope) error {
	if err := e.Validate(v.sessionID, v.next); err != nil {
		return err
	}
	v.next++
	return nil
}

// Next returns the next expected sequence (diagnostics).
func (v *SessionValidator) Next() int64 { return v.next }

// --- wire format -----------------------------------------------------------

var (
	errOversizeFrame = fmt.Errorf("stdioworker: frame declares more than %d bytes", MaxFrameBytes)
	errZeroFrame     = fmt.Errorf("stdioworker: zero-length frame")
	errShortHeader   = fmt.Errorf("stdioworker: short frame header")
	errTruncated     = fmt.Errorf("stdioworker: truncated frame payload")
)

// ReadFrame reads one length-prefixed frame from r.
func ReadFrame(r io.Reader) ([]byte, error) {
	var head [4]byte
	n, err := io.ReadFull(r, head[:])
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errShortHeader, err)
	}
	_ = n
	size := binary.BigEndian.Uint32(head[:])
	if size == 0 {
		return nil, errZeroFrame
	}
	if size > MaxFrameBytes {
		return nil, fmt.Errorf("%w: declared %d", errOversizeFrame, size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("%w: %v", errTruncated, err)
	}
	return payload, nil
}

// WriteFrame writes one length-prefixed frame to w.
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) == 0 {
		return errZeroFrame
	}
	if len(payload) > MaxFrameBytes {
		return errOversizeFrame
	}
	var head [4]byte
	binary.BigEndian.PutUint32(head[:], uint32(len(payload)))
	if _, err := w.Write(head[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

// ReadEnvelope reads and decodes one envelope frame.
func ReadEnvelope(r io.Reader) (*Envelope, error) {
	payload, err := ReadFrame(r)
	if err != nil {
		return nil, err
	}
	var e Envelope
	if err := json.Unmarshal(payload, &e); err != nil {
		return nil, fmt.Errorf("stdioworker: malformed envelope: %w", err)
	}
	return &e, nil
}

// WriteEnvelope encodes and writes one envelope frame.
func WriteEnvelope(w io.Writer, e *Envelope) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return WriteFrame(w, raw)
}

// --- supply chain ------------------------------------------------------------

// FileDigest streams sha256 over path (handles big binaries without
// loading them; also used by tests to pin the reference worker).
func FileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, bufio.NewReader(f)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyExecutable pins the on-disk binary to the digest inside the signed
// spec. This is the supply-chain check of 5B: a swapped binary (or a
// tampered cache) never launches even with a stolen-but-valid spec.
func VerifyExecutable(exePath, wantDigest string) error {
	got, err := FileDigest(exePath)
	if err != nil {
		return fmt.Errorf("stdioworker: digest worker executable: %w", err)
	}
	if !strings.EqualFold(got, wantDigest) {
		return fmt.Errorf("%w: got %s want %s", ErrSupplyChain, got, wantDigest)
	}
	return nil
}
