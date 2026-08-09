//go:build windows

package secretlease

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/ipc"
)

func validRequest(now time.Time) Request {
	var nonce [32]byte
	_, _ = rand.Read(nonce[:])
	return Request{ProviderID: "provider", CredentialRef: "credential", Origin: "https://example.com", Protocol: "openai_compatible", Operation: OperationChat, Deadline: now.Add(time.Second), Nonce: nonce}
}

func TestLeaseAuthenticationBindingAndSize(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	r := validRequest(time.Now())
	raw, err := encodeRequest(r, key)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRequest(raw, key)
	if err != nil || decoded.ProviderID != r.ProviderID || decoded.Operation != r.Operation {
		t.Fatalf("binding lost: %#v %v", decoded, err)
	}
	raw[50] ^= 1
	if _, err := decodeRequest(raw, key); err == nil {
		t.Fatal("modified binding authenticated")
	}
	if err := writeLimited(&bytes.Buffer{}, make([]byte, MaxFrame+1)); err == nil {
		t.Fatal("oversized frame accepted")
	}
}

func TestLeaseExpiryReplayAndConcurrentOnce(t *testing.T) {
	now := time.Now()
	s := &Server{used: make(map[[32]byte]time.Time)}
	expired := validRequest(now)
	expired.Deadline = now.Add(-time.Millisecond)
	if s.consume(expired, now) == nil {
		t.Fatal("expired lease accepted")
	}
	r := validRequest(now)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.consume(r, now) == nil {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("lease consumed %d times", accepted.Load())
	}
	if s.consume(r, now) == nil {
		t.Fatal("replay accepted")
	}
	tooLong := validRequest(now)
	tooLong.Operation = OperationProviderTest
	tooLong.Deadline = now.Add(MaxTTL + time.Millisecond)
	if s.consume(tooLong, now) == nil {
		t.Fatal("excessive TTL accepted")
	}
	unknown := validRequest(now)
	unknown.Operation = Operation("arbitrary")
	if s.consume(unknown, now) == nil {
		t.Fatal("unknown operation accepted")
	}
	full := &Server{used: make(map[[32]byte]time.Time)}
	for i := 0; i < MaxNonceCacheEntries; i++ {
		var nonce [32]byte
		binary.BigEndian.PutUint64(nonce[:], uint64(i))
		full.used[nonce] = now.Add(time.Second)
	}
	if full.consume(validRequest(now), now) == nil {
		t.Fatal("nonce cache overflow accepted")
	}
}

func TestBrokerKernelPIDRejectsWrongEngine(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	if ipc.VerifyClientProcess(server, 1) == nil {
		t.Fatal("non-kernel/wrong-PID connection accepted")
	}
}

func TestOperationSpecificTTL(t *testing.T) {
	now := time.Now()
	s := &Server{used: make(map[[32]byte]time.Time)}
	nonChat := validRequest(now)
	nonChat.Operation = OperationModelDiscover
	nonChat.Deadline = now.Add(6 * time.Second)
	if s.consume(nonChat, now) == nil {
		t.Fatal("non-chat lease over five seconds accepted")
	}
	chat := validRequest(now)
	chat.Deadline = now.Add(9 * time.Minute)
	if err := s.consume(chat, now); err != nil {
		t.Fatalf("bounded chat lease rejected: %v", err)
	}
	tooLong := validRequest(now)
	tooLong.Deadline = now.Add(ChatMaxTTL + time.Millisecond)
	if s.consume(tooLong, now) == nil {
		t.Fatal("chat lease over maximum accepted")
	}
}
