package ipc

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

type shortWriter struct{ bytes.Buffer }

func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return w.Buffer.Write(p)
}

type stalledWriter struct{}

func (stalledWriter) Write([]byte) (int, error) { return 0, nil }

func TestFrameRoundTrip(t *testing.T) {
	var buffer bytes.Buffer
	want := []byte(`{"method":"provider.list"}`)
	if err := WriteFrame(&buffer, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(&buffer)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestReadFrameRejectsOversizedPayloadBeforeAllocation(t *testing.T) {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], MaxFrameSize+1)
	if _, err := ReadFrame(bytes.NewReader(header[:])); err == nil {
		t.Fatal("expected oversized frame rejection")
	}
}

func TestWriteFrameHandlesShortWrites(t *testing.T) {
	w := &shortWriter{}
	if err := WriteFrame(w, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(&w.Buffer)
	if err != nil || string(got) != "payload" {
		t.Fatalf("got %q, %v", got, err)
	}
	if err := WriteFrame(stalledWriter{}, []byte("x")); err != io.ErrShortWrite {
		t.Fatalf("error = %v", err)
	}
}
