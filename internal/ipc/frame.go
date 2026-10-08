package ipc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MaxFrameSize bounds one request/response frame on the trusted local pipe.
// The product hub report (findings + full manual markdown + full manual HTML)
// measures ~4.3 MiB at 625 cards, so the old 4 MiB cap rejected the diagnostics
// response. 16 MiB leaves growth room; an oversize frame is still rejected, and
// the session layer degrades that one call to a structured failure instead of
// tearing the connection down.
const MaxFrameSize = 16 << 20

func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) == 0 || len(payload) > MaxFrameSize {
		return fmt.Errorf("invalid frame size: %d", len(payload))
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if err := writeFull(w, header[:]); err != nil {
		return err
	}
	return writeFull(w, payload)
}

func writeFull(w io.Writer, value []byte) error {
	for len(value) != 0 {
		n, err := w.Write(value)
		if n > 0 {
			value = value[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func ReadFrame(r io.Reader) ([]byte, error) {
	return ReadFrameLimit(r, MaxFrameSize)
}

func ReadFrameLimit(r io.Reader, limit uint32) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > limit || size > MaxFrameSize {
		return nil, errors.New("invalid or oversized RPC frame")
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// oversizeFrameError marks a frame rejected for size before a single byte was
// written. The transport is still healthy, so the session downgrades that one
// call to a structured failure instead of closing the connection.
type oversizeFrameError struct{ size int }

func (e oversizeFrameError) Error() string {
	return fmt.Sprintf("frame payload %d bytes exceeds the %d byte limit", e.size, MaxFrameSize)
}
