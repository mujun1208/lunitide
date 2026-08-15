// Package stdiopoc implements the M6 slice-5A stdio strong-isolation POC:
// spawn a real child process under the OS-enforced isolation available on
// Windows today (explicit minimal environment, Job Object process-tree and
// memory quotas) and probe the six isolation assumptions the design demands
// before stdio MCP may even enter controlled implementation (5B):
//
//	host-file   ../ traversal, symlink and junction escapes blocked by the
//	            real-path guard (SBX-001); the OS-level boundary itself is
//	            5B AppContainer work — this POC proves the guard assumption.
//	network     cloud metadata / loopback / non-allowlist targets blocked by
//	            the dial guard (SBX-002), DNS-rebinding IPs included.
//	secret      the child inherits NO parent environment: marker variables
//	            read back empty (OS-enforced via explicit env block).
//	proctree    fork-bomb attempts hit the Job Object active-process quota
//	            and the whole tree is reaped on Kill (OS-enforced).
//	resource    memory-exhaustion attempts hit the Job Object commit quota
//	            (OS-enforced).
//	protocol    oversize (>4MiB), malformed and forged frames are rejected
//	            by the framed stdio protocol validator.
//
// The POC never enables stdio: M6-MCP-004 stays in force. Evidence bundles
// are written for independent security review; a PASS verdict only permits
// entering 5B development.
package stdiopoc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MaxFrameBytes is the hard per-frame cap: 4MiB payload, aligned with the
// gateway response ceiling in MCP_GATEWAY_POLICY_V1. A frame header that
// declares more is a protocol-cheat attempt, not a buffer to grow.
const MaxFrameBytes = 4 << 20

// Frame errors map onto the protocol-cheat assumption (one of the six).
var (
	// ErrOversizeFrame: declared payload length exceeds MaxFrameBytes.
	ErrOversizeFrame = errors.New("stdiopoc: frame exceeds 4MiB cap")
	// ErrMalformedFrame: zero-length frame, truncated payload or short header.
	ErrMalformedFrame = errors.New("stdiopoc: malformed frame")
	// ErrForgedFrame: syntactically valid frame whose envelope fails
	// validation (unknown type, bad nonce, wrong probe id).
	ErrForgedFrame = errors.New("stdiopoc: forged frame")
)

// ReadFrame reads one length-prefixed frame: a 4-byte big-endian payload
// length followed by exactly that many bytes. Declared lengths above the
// cap abort before allocating; short reads are malformed.
func ReadFrame(r io.Reader) ([]byte, error) {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, fmt.Errorf("%w: short header: %v", ErrMalformedFrame, err)
	}
	n := binary.BigEndian.Uint32(head[:])
	if n == 0 {
		return nil, fmt.Errorf("%w: zero-length frame", ErrMalformedFrame)
	}
	if n > MaxFrameBytes {
		return nil, fmt.Errorf("%w: declared %d bytes", ErrOversizeFrame, n)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("%w: truncated payload: %v", ErrMalformedFrame, err)
	}
	return payload, nil
}

// WriteFrame writes one length-prefixed frame. Writing more than the cap
// from our side is a programming error and is refused.
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("%w: refusing to write empty frame", ErrMalformedFrame)
	}
	if len(payload) > MaxFrameBytes {
		return fmt.Errorf("%w: payload %d bytes", ErrOversizeFrame, len(payload))
	}
	var head [4]byte
	binary.BigEndian.PutUint32(head[:], uint32(len(payload)))
	if _, err := w.Write(head[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}
