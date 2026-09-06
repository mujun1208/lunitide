package people

import (
	"errors"
	"net"
	"testing"
)

func TestPeerGrantStopsPayloadAfterRevocation(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	revoked := false
	guarded := peerGrantConn{Conn: a, check: func() error {
		if revoked {
			return ErrNotTrusted
		}
		return nil
	}}
	read := make(chan int, 1)
	go func() { buf := make([]byte, 4); n, _ := b.Read(buf); read <- n }()
	if n, err := guarded.Write([]byte("safe")); n != 4 || err != nil {
		t.Fatalf("initial grant %d %v", n, err)
	}
	if <-read != 4 {
		t.Fatal("initial frame lost")
	}
	revoked = true
	if n, err := guarded.Write([]byte("private")); n != 0 || !errors.Is(err, ErrNotTrusted) {
		t.Fatalf("revoked payload escaped %d %v", n, err)
	}
}
