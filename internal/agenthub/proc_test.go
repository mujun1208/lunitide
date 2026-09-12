package agenthub

import (
	"os"
	"testing"
)

func TestWriteAndCloseReportsError(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = writeAndClose(f, []byte("x")); err == nil {
		t.Fatal("expected write on closed file to fail")
	}
}
