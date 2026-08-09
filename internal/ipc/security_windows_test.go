//go:build windows

package ipc

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionAuthenticatorRejectsReplayAndClearsInput(t *testing.T) {
	secret := make([]byte, sessionSecretSize)
	for index := range secret {
		secret[index] = byte(index + 1)
	}
	nonce := hex.EncodeToString(secret)
	authenticator := NewSessionAuthenticator(secret)
	for _, value := range secret {
		if value != 0 {
			t.Fatal("bootstrap secret was not cleared")
		}
	}
	if !authenticator.Consume(nonce) {
		t.Fatal("first authentication was rejected")
	}
	if authenticator.Consume(nonce) {
		t.Fatal("replayed nonce was accepted")
	}
}

func TestSessionGateEnforcesConnectionLimit(t *testing.T) {
	gate := NewSessionGate(2)
	leaveFirst, ok := gate.TryEnter()
	if !ok {
		t.Fatal("first session rejected")
	}
	leaveSecond, ok := gate.TryEnter()
	if !ok {
		t.Fatal("second session rejected")
	}
	if _, ok := gate.TryEnter(); ok {
		t.Fatal("session above limit accepted")
	}
	leaveFirst()
	leaveFirst()
	leaveThird, ok := gate.TryEnter()
	if !ok {
		t.Fatal("slot was not released")
	}
	leaveSecond()
	leaveThird()
}

func TestServeSessionRejectsSlowHandshake(t *testing.T) {
	previousTimeout := handshakeTimeout
	handshakeTimeout = 50 * time.Millisecond
	defer func() { handshakeTimeout = previousTimeout }()
	server, client := net.Pipe()
	defer client.Close()
	secret := make([]byte, sessionSecretSize)
	authenticator := NewSessionAuthenticator(secret)
	started := time.Now()
	err := ServeSession(context.Background(), server, 1, authenticator, nil, nil)
	if err == nil {
		t.Fatal("slow handshake was accepted")
	}
	elapsed := time.Since(started)
	if elapsed < 40*time.Millisecond || elapsed > time.Second {
		t.Fatalf("handshake timeout was unstable: %v", elapsed)
	}
}

func TestSessionAuthenticatorConcurrentReserveExactlyOnce(t *testing.T) {
	secret := make([]byte, sessionSecretSize)
	for i := range secret {
		secret[i] = byte(i + 1)
	}
	nonce := hex.EncodeToString(secret)
	authenticator := NewSessionAuthenticator(secret)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if reservation, ok := authenticator.Reserve(nonce); ok {
				successes.Add(1)
				reservation.Commit()
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful reservations = %d, want 1", successes.Load())
	}
}

func TestSessionAuthenticatorConcurrentConsumeExactlyOnce(t *testing.T) {
	secret := make([]byte, sessionSecretSize)
	nonce := hex.EncodeToString(secret)
	authenticator := NewSessionAuthenticator(secret)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if authenticator.Consume(nonce) {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful consumes = %d, want 1", successes.Load())
	}
}

func TestServeSessionACKFailureLeavesCredentialReserved(t *testing.T) {
	server, client := net.Pipe()
	secret := make([]byte, sessionSecretSize)
	nonce := hex.EncodeToString(secret)
	authenticator := NewSessionAuthenticator(secret)
	done := make(chan error, 1)
	go func() {
		done <- serveSession(context.Background(), server, os.Getpid(), authenticator, nil, nil,
			func(net.Conn) (uint32, error) { return uint32(os.Getpid()), nil },
			func(io.Writer, []byte) error { return errors.New("forced ACK failure") })
	}()
	hello := fmt.Sprintf(`{"rpcMajor":1,"rpcMinor":0,"clientPid":%d,"sessionNonce":%q}`, os.Getpid(), nonce)
	if err := WriteFrame(client, []byte(hello)); err != nil {
		t.Fatal(err)
	}
	err := <-done
	_ = client.Close()
	if !errors.Is(err, ErrHandshakeACK) {
		t.Fatalf("error = %v, want ErrHandshakeACK", err)
	}
	if _, ok := authenticator.Reserve(nonce); ok {
		t.Fatal("ACK failure reopened reserved credential")
	}
}

func TestNamedPipeSquattingAndKernelPIDChecks(t *testing.T) {
	pipe := fmt.Sprintf(`\\.\pipe\lunitide-ipc-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	listener, err := ListenCurrentUser(pipe)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if squatter, err := ListenCurrentUser(pipe); err == nil {
		squatter.Close()
		t.Fatal("second server squatted an existing pipe name")
	}
	_ = listener.Close()

	pipe = fmt.Sprintf(`\\.\pipe\lunitide-ipc-pid-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	listener, err = ListenCurrentUser(pipe)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		server, err := listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- server
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := Dial(ctx, pipe)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var server net.Conn
	select {
	case server = <-accepted:
	case err := <-acceptErr:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defer server.Close()
	if err := VerifyClientProcess(server, os.Getpid()); err != nil {
		t.Fatalf("kernel rejected actual client PID: %v", err)
	}
	gate := NewSessionGate(1)
	if leave, ok := AdmitClient(server, os.Getpid()+1, gate); ok {
		leave()
		t.Fatal("spoofed expected PID was admitted")
	}
	leave, ok := gate.TryEnter()
	if !ok {
		t.Fatal("rejected PID occupied the session gate")
	}
	leave()
}
