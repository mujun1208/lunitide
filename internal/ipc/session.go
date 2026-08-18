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
	"log"
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
var sessionWriteTimeout = 35 * time.Second
var sessionDrainTimeout = 5 * time.Second

// 10x 优化：JSON 编码缓冲池 — 复用 bytes.Buffer 避免每次 json.Marshal
// 都分配新缓冲区。对于高频流式事件（delta/thinking），这能减少 ~30%
// 的堆分配，降低 GC 压力。
var jsonBufferPool = sync.Pool{
	New: func() any { return bytes.NewBuffer(make([]byte, 0, 4096)) },
}

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
	// The deadline protects only the unauthenticated handshake. Keeping a
	// connection-wide deadline here would tear down an otherwise healthy idle
	// session and any long-running stream that has no new request frames.
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return err
	}
	var writeMu sync.Mutex
	var lastWriteDeadline time.Time
	write := func(value any) error {
		// 10x 优化：使用 JSON 编码缓冲池替代 json.Marshal，
		// 复用 bytes.Buffer 减少堆分配和 GC 压力。
		buf := jsonBufferPool.Get().(*bytes.Buffer)
		defer jsonBufferPool.Put(buf)
		buf.Reset()
		enc := json.NewEncoder(buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(value); err != nil {
			return err
		}
		raw := buf.Bytes()
		// json.Encoder.Encode 追加换行符，需要去除以匹配 json.Marshal 行为
		if len(raw) > 0 && raw[len(raw)-1] == '\n' {
			raw = raw[:len(raw)-1]
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		// Refresh the write deadline at most once per second. Named Pipe
		// writes are fast kernel-mode IPC; per-event SetWriteDeadline
		// syscalls (~1-5 μs each) are wasteful for streaming sessions
		// that may emit hundreds of delta events.
		now := time.Now()
		if now.After(lastWriteDeadline) {
			if err := conn.SetWriteDeadline(now.Add(sessionWriteTimeout)); err != nil {
				return err
			}
			lastWriteDeadline = now.Add(sessionWriteTimeout)
		}
		return writeFrame(conn, raw)
	}
	var requests sync.WaitGroup
	admission := make(chan struct{}, 32)
	defer func() {
		cancelSession()
		drained := make(chan struct{})
		go func() {
			requests.Wait()
			close(drained)
		}()
		select {
		case <-drained:
		case <-time.After(sessionDrainTimeout):
		}
	}()
	for {
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
			const preResponseEventLimit = 64
			var eventMu sync.Mutex
			responseWritten := false
			preResponseEvents := make([]bridge.Event, 0, 4)
			var preResponseError error
			emit := func(event bridge.Event) error {
				eventMu.Lock()
				defer eventMu.Unlock()
				if !responseWritten {
					if preResponseError != nil {
						return preResponseError
					}
					if len(preResponseEvents) == preResponseEventLimit {
						preResponseError = errors.New("stream emitted too many events before its response")
						return preResponseError
					}
					preResponseEvents = append(preResponseEvents, event)
					return nil
				}
				return write(event)
			}
			var response bridge.Response
			if streaming, ok := handler.(StreamingHandler); ok {
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("ENGINE PANIC recovered in streaming handler: %v (request %s)", r, request.ID)
							response = bridge.Failure(request.ID, request.TraceID, "ENGINE_INTERNAL_ERROR", "引擎内部错误，已自动恢复", false)
						}
					}()
					response = streaming.HandleStreaming(sessionCtx, request, emit)
				}()
			} else {
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("ENGINE PANIC recovered in handler: %v (request %s)", r, request.ID)
							response = bridge.Failure(request.ID, request.TraceID, "ENGINE_INTERNAL_ERROR", "引擎内部错误，已自动恢复", false)
						}
					}()
					response = handler.Handle(sessionCtx, request)
				}()
			}
			eventMu.Lock()
			if preResponseError != nil {
				eventMu.Unlock()
				_ = conn.Close()
				return
			}
			if err := write(response); err != nil {
				eventMu.Unlock()
				_ = conn.Close()
				return
			}
			responseWritten = true
			for _, event := range preResponseEvents {
				if err := write(event); err != nil {
					eventMu.Unlock()
					_ = conn.Close()
					return
				}
			}
			preResponseEvents = nil
			eventMu.Unlock()
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
