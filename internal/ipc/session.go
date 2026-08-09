package ipc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
)

const (
	RPCMajor, RPCMinor = 1, 0
	sessionSecretSize  = 32
)

var handshakeTimeout = 5 * time.Second

type Handshake struct {
	RPCMajor     int    `json:"rpcMajor"`
	RPCMinor     int    `json:"rpcMinor"`
	ClientPID    int    `json:"clientPid"`
	SessionNonce string `json:"sessionNonce"`
}

type Handler interface {
	Handle(context.Context, bridge.Request) bridge.Response
}
type StreamingHandler interface {
	HandleStreaming(context.Context, bridge.Request, func(bridge.Event) error) bridge.Response
}

// SessionAuthenticator owns a single-use verifier. The bootstrap secret itself
// is hashed and erased at construction time.
type SessionAuthenticator struct {
	mu       sync.Mutex
	verifier [sha256.Size]byte
	state    credentialState
}

type credentialState uint8

const (
	credentialUnused credentialState = iota
	credentialReserved
	credentialCommitted
)

type CredentialReservation struct {
	authenticator *SessionAuthenticator
}

func NewSessionAuthenticator(secret []byte) *SessionAuthenticator {
	authenticator := &SessionAuthenticator{verifier: sha256.Sum256(secret)}
	zero(secret)
	return authenticator
}

func (authenticator *SessionAuthenticator) Consume(encodedNonce string) bool {
	reservation, ok := authenticator.Reserve(encodedNonce)
	if ok {
		reservation.Commit()
	}
	return ok
}

// Reserve atomically changes a matching credential from unused to reserved.
// A reservation is deliberately irreversible: if the handshake ACK cannot be
// delivered, the listener must shut down rather than make the nonce reusable.
func (authenticator *SessionAuthenticator) Reserve(encodedNonce string) (*CredentialReservation, bool) {
	candidate, err := hex.DecodeString(encodedNonce)
	if err != nil || len(candidate) != sessionSecretSize {
		zero(candidate)
		return nil, false
	}
	digest := sha256.Sum256(candidate)
	zero(candidate)
	authenticator.mu.Lock()
	defer authenticator.mu.Unlock()
	matched := authenticator.state == credentialUnused && subtle.ConstantTimeCompare(digest[:], authenticator.verifier[:]) == 1
	zero(digest[:])
	if matched {
		authenticator.state = credentialReserved
	}
	if !matched {
		return nil, false
	}
	return &CredentialReservation{authenticator: authenticator}, true
}

func (reservation *CredentialReservation) Commit() {
	if reservation == nil || reservation.authenticator == nil {
		return
	}
	authenticator := reservation.authenticator
	authenticator.mu.Lock()
	defer authenticator.mu.Unlock()
	if authenticator.state == credentialReserved {
		authenticator.state = credentialCommitted
		zero(authenticator.verifier[:])
	}
	reservation.authenticator = nil
}

var ErrHandshakeACK = errors.New("RPC handshake ACK failed after credential reservation")

func ServeSession(ctx context.Context, conn net.Conn, expectedPID int, authenticator *SessionAuthenticator, handler Handler, onAuthenticated func()) error {
	return serveSession(ctx, conn, expectedPID, authenticator, handler, onAuthenticated, ClientProcessID, WriteFrame)
}

func serveSession(ctx context.Context, conn net.Conn, expectedPID int, authenticator *SessionAuthenticator, handler Handler, onAuthenticated func(), peerProcessID func(net.Conn) (uint32, error), writeFrame func(io.Writer, []byte) error) error {
	defer conn.Close()
	sessionCtx, cancelSession := context.WithCancel(ctx)
	defer cancelSession()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	if err := conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return err
	}
	frame, err := ReadFrameLimit(conn, 4096)
	if err != nil {
		return err
	}
	var hello Handshake
	if err := decodeStrict(frame, &hello); err != nil {
		return errors.New("invalid RPC handshake")
	}
	peerPID, err := peerProcessID(conn)
	if err != nil {
		return errors.New("cannot authenticate RPC client process")
	}
	if hello.RPCMajor != RPCMajor || hello.RPCMinor < 0 || hello.RPCMinor > RPCMinor || hello.ClientPID != expectedPID || uint32(expectedPID) != peerPID {
		return errors.New("RPC handshake rejected")
	}
	reservation, ok := authenticator.Reserve(hello.SessionNonce)
	if !ok {
		return errors.New("RPC handshake rejected")
	}
	ack, _ := json.Marshal(map[string]any{"accepted": true, "rpcMajor": RPCMajor, "rpcMinor": min(hello.RPCMinor, RPCMinor)})
	if err := writeFrame(conn, ack); err != nil {
		return fmt.Errorf("%w: %v", ErrHandshakeACK, err)
	}
	reservation.Commit()
	if onAuthenticated != nil {
		onAuthenticated()
	}
	var writeMu sync.Mutex
	write := func(value any) error {
		raw, err := json.Marshal(value)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		return writeFrame(conn, raw)
	}
	var requests sync.WaitGroup
	admission := make(chan struct{}, 32)
	defer func() {
		cancelSession()
		requests.Wait()
	}()
	for {
		if err := conn.SetDeadline(time.Now().Add(35 * time.Second)); err != nil {
			return err
		}
		frame, err := ReadFrame(conn)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		var request bridge.Request
		if err := decodeStrict(frame, &request); err != nil {
			return errors.New("invalid bridge request")
		}
		select {
		case admission <- struct{}{}:
		default:
			if err := write(bridge.Failure(request.ID, request.TraceID, "ENGINE_BUSY", "核心引擎正忙，请稍后重试", true)); err != nil {
				return err
			}
			continue
		}
		requests.Add(1)
		go func(request bridge.Request) {
			defer requests.Done()
			defer func() { <-admission }()
			gate := make(chan struct{})
			var once sync.Once
			emit := func(event bridge.Event) error { <-gate; return write(event) }
			var response bridge.Response
			if streaming, ok := handler.(StreamingHandler); ok {
				response = streaming.HandleStreaming(sessionCtx, request, emit)
			} else {
				response = handler.Handle(sessionCtx, request)
			}
			if err := write(response); err != nil {
				_ = conn.Close()
			}
			once.Do(func() { close(gate) })
		}(request)
	}
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("multiple JSON values are not allowed")
	}
	return nil
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
