package stdiopoc

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// writeBadFrame emits the raw bytes of one protocol-cheat vector. Every
// variant is stream-aligned: the host either rejects on the 4-byte header
// (consuming exactly the header) or reads the full declared payload before
// rejecting, so subsequent announce frames stay parseable.
func writeBadFrame(w io.Writer, id string) error {
	switch id {
	case "zero-length-frame":
		var head [4]byte // declares zero payload
		_, err := w.Write(head[:])
		return err
	case "garbage-payload":
		garbage := []byte("<<<this-is-not-json-garbage-payload>>>")
		var head [4]byte
		binary.BigEndian.PutUint32(head[:], uint32(len(garbage)))
		if _, err := w.Write(head[:]); err != nil {
			return err
		}
		_, err := w.Write(garbage)
		return err
	case "forged-type":
		return writeRawEnvelope(w, FrameEnvelope{Type: "admin", Nonce: nonceHex(), Probe: AssumptionProtocol})
	case "forged-nonce":
		return writeRawEnvelope(w, FrameEnvelope{Type: EnvelopeTypeReport, Nonce: "NOT-HEX-NONCE-000000000000000", Probe: AssumptionProtocol})
	case "forged-probe":
		return writeRawEnvelope(w, FrameEnvelope{Type: EnvelopeTypeReport, Nonce: nonceHex(), Probe: "root-escalation"})
	case "oversize-declared":
		// Header declares 5MiB but nothing follows: the reader must reject
		// on the header without allocating and without consuming anything
		// else, keeping the stream aligned.
		var head [4]byte
		binary.BigEndian.PutUint32(head[:], MaxFrameBytes+(1<<20))
		_, err := w.Write(head[:])
		return err
	default:
		return fmt.Errorf("stdiopoc: unknown bad frame %q", id)
	}
}

// writeRawEnvelope marshals an envelope exactly as-is (no validation), so
// forged fields reach the host reader verbatim.
func writeRawEnvelope(w io.Writer, env FrameEnvelope) error {
	payload, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return WriteFrame(w, payload)
}
