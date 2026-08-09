//go:build windows

// Package secretlease implements the private Host-to-Engine credential broker.
// It is deliberately not wired to Bridge dispatch or any Renderer API.
package secretlease

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/lunitide/lunitide/internal/secret"
)

const (
	MaxFrame             = 64 << 10
	MaxTTL               = 5 * time.Second
	ChatMaxTTL           = 10 * time.Minute
	MaxActiveConnections = 32
	MaxNonceCacheEntries = 4096
)

// Operation is authenticated and auditable request metadata. It is not an
// enforceable capability boundary inside Engine after a secret is delivered.
type Operation string

const (
	OperationModelDiscover Operation = "model.discover"
	OperationProviderTest  Operation = "provider.test"
	OperationChat          Operation = "chat"
)

func (o Operation) valid() bool {
	return o == OperationModelDiscover || o == OperationProviderTest || o == OperationChat
}

func (o Operation) maxTTL() time.Duration {
	if o == OperationChat {
		return ChatMaxTTL
	}
	return MaxTTL
}

type Request struct {
	ProviderID, CredentialRef, Origin, Protocol string
	Operation                                   Operation
	Deadline                                    time.Time
	Nonce                                       [32]byte
}

func DeriveKey(bootstrap []byte) []byte {
	mac := hmac.New(sha256.New, bootstrap)
	mac.Write([]byte("lunitide/secret-broker/v1"))
	return mac.Sum(nil)
}

type Server struct {
	listener    net.Listener
	service     secret.Service
	expectedPID int
	key         []byte
	mu          sync.Mutex
	used        map[[32]byte]time.Time
	active      map[net.Conn]struct{}
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	closeOnce   sync.Once
	closed      bool
}

func NewServer(listener net.Listener, service secret.Service, expectedEnginePID int, key []byte) (*Server, error) {
	if listener == nil || service == nil || expectedEnginePID < 1 || len(key) != sha256.Size {
		return nil, errors.New("invalid secret broker configuration")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{listener: listener, service: service, expectedPID: expectedEnginePID, key: append([]byte(nil), key...), used: make(map[[32]byte]time.Time), active: make(map[net.Conn]struct{}), ctx: ctx, cancel: cancel}, nil
}

func (s *Server) Close() (result error) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.cancel()
		result = s.listener.Close()
		for conn := range s.active {
			_ = conn.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
		secret.Zero(s.key)
	})
	return result
}
func (s *Server) Serve(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || s.ctx.Err() != nil {
				return nil
			}
			return err
		}
		s.mu.Lock()
		if s.closed || len(s.active) >= MaxActiveConnections {
			s.mu.Unlock()
			_ = conn.Close()
			continue
		}
		s.active[conn] = struct{}{}
		s.wg.Add(1)
		s.mu.Unlock()
		if ipc.VerifyClientProcess(conn, s.expectedPID) != nil {
			s.finishConn(conn)
			continue
		}
		go s.serveConn(conn)
	}
}
func (s *Server) finishConn(conn net.Conn) {
	_ = conn.Close()
	s.mu.Lock()
	delete(s.active, conn)
	s.mu.Unlock()
	s.wg.Done()
}
func (s *Server) serveConn(conn net.Conn) {
	defer s.finishConn(conn)
	_ = conn.SetDeadline(time.Now().Add(ChatMaxTTL))
	raw, err := ipc.ReadFrameLimit(conn, MaxFrame)
	if err != nil {
		return
	}
	defer secret.Zero(raw)
	req, err := decodeRequest(raw, s.key)
	if err != nil || s.consume(req, time.Now()) != nil {
		_, _ = writeResponse(conn, nil, false)
		return
	}
	ref := secret.Ref{CredentialRef: req.CredentialRef, ProviderID: req.ProviderID, Origin: req.Origin, Protocol: req.Protocol}
	started := false
	err = s.service.WithSecret(s.ctx, ref, func(value []byte) error { var e error; started, e = writeResponse(conn, value, true); return e })
	if err != nil && !started {
		_, _ = writeResponse(conn, nil, false)
	}
}
func (s *Server) consume(r Request, now time.Time) error {
	ttl := r.Deadline.Sub(now)
	if !r.Operation.valid() || ttl <= 0 || ttl > r.Operation.maxTTL() {
		return errors.New("invalid lease")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for nonce, expiry := range s.used {
		if !expiry.After(now) {
			delete(s.used, nonce)
		}
	}
	if _, exists := s.used[r.Nonce]; exists {
		return errors.New("replayed lease")
	}
	if len(s.used) >= MaxNonceCacheEntries {
		return errors.New("nonce cache capacity exceeded")
	}
	s.used[r.Nonce] = now.Add(ttl)
	return nil
}

type Client struct {
	pipe    string
	hostPID int
	key     []byte
}

func NewClient(pipe string, hostPID int, key []byte) (*Client, error) {
	if pipe == "" || hostPID < 1 || len(key) != sha256.Size {
		return nil, errors.New("invalid broker client")
	}
	return &Client{pipe: pipe, hostPID: hostPID, key: append([]byte(nil), key...)}, nil
}
func (c *Client) Close() { secret.Zero(c.key) }
func (c *Client) WithLease(ctx context.Context, request Request, callback func([]byte) error) error {
	if callback == nil {
		return errors.New("lease callback required")
	}
	if !request.Operation.valid() {
		return errors.New("invalid lease operation")
	}
	maxTTL := request.Operation.maxTTL()
	if request.Deadline.IsZero() {
		request.Deadline = time.Now().Add(maxTTL)
	}
	if request.Deadline.After(time.Now().Add(maxTTL)) {
		return errors.New("lease TTL exceeds maximum")
	}
	if request.Nonce == ([32]byte{}) {
		if _, err := rand.Read(request.Nonce[:]); err != nil {
			return err
		}
	}
	conn, err := ipc.Dial(ctx, c.pipe)
	if err != nil {
		return err
	}
	defer conn.Close()
	pid, err := ipc.ServerProcessID(conn)
	if err != nil || pid != uint32(c.hostPID) {
		return errors.New("secret broker Host PID mismatch")
	}
	_ = conn.SetDeadline(request.Deadline)
	raw, err := encodeRequest(request, c.key)
	if err != nil {
		return err
	}
	defer secret.Zero(raw)
	if err = writeLimited(conn, raw); err != nil {
		return err
	}
	response, err := ipc.ReadFrameLimit(conn, MaxFrame)
	if err != nil {
		return err
	}
	defer secret.Zero(response)
	if len(response) < 1 || response[0] != 1 {
		return errors.New("secret lease rejected")
	}
	return callback(response[1:])
}

func encodeRequest(r Request, key []byte) ([]byte, error) {
	if r.Deadline.IsZero() {
		return nil, errors.New("deadline required")
	}
	fields := []string{r.ProviderID, r.CredentialRef, r.Origin, r.Protocol, string(r.Operation)}
	var b bytes.Buffer
	b.WriteByte(1)
	_ = binary.Write(&b, binary.BigEndian, r.Deadline.UnixMilli())
	b.Write(r.Nonce[:])
	for _, f := range fields {
		if len(f) == 0 || len(f) > 2048 {
			return nil, errors.New("invalid lease binding")
		}
		_ = binary.Write(&b, binary.BigEndian, uint16(len(f)))
		b.Write([]byte(f))
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(b.Bytes())
	b.Write(mac.Sum(nil))
	if b.Len() > MaxFrame {
		return nil, errors.New("oversized lease frame")
	}
	return b.Bytes(), nil
}
func decodeRequest(raw, key []byte) (Request, error) {
	var r Request
	if len(raw) < 1+8+32+10+sha256.Size {
		return r, errors.New("invalid lease frame")
	}
	body, tag := raw[:len(raw)-sha256.Size], raw[len(raw)-sha256.Size:]
	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	if !hmac.Equal(tag, mac.Sum(nil)) {
		return r, errors.New("broker authentication failed")
	}
	b := bytes.NewReader(body)
	version, _ := b.ReadByte()
	if version != 1 {
		return r, errors.New("invalid lease version")
	}
	var millis int64
	if binary.Read(b, binary.BigEndian, &millis) != nil {
		return r, io.ErrUnexpectedEOF
	}
	r.Deadline = time.UnixMilli(millis)
	io.ReadFull(b, r.Nonce[:])
	var operation string
	fields := []*string{&r.ProviderID, &r.CredentialRef, &r.Origin, &r.Protocol, &operation}
	for _, target := range fields {
		var n uint16
		if binary.Read(b, binary.BigEndian, &n) != nil || n == 0 || n > 2048 {
			return r, errors.New("invalid binding")
		}
		v := make([]byte, n)
		if _, err := io.ReadFull(b, v); err != nil {
			return r, err
		}
		*target = string(v)
		secret.Zero(v)
	}
	r.Operation = Operation(operation)
	if b.Len() != 0 {
		return r, errors.New("trailing lease data")
	}
	return r, nil
}
func writeLimited(w io.Writer, payload []byte) error {
	if len(payload) == 0 || len(payload) > MaxFrame {
		return errors.New("invalid broker frame size")
	}
	return ipc.WriteFrame(w, payload)
}
func writeResponse(w io.Writer, value []byte, ok bool) (bool, error) {
	payload := make([]byte, 1+len(value))
	if ok {
		payload[0] = 1
	}
	copy(payload[1:], value)
	defer secret.Zero(payload)
	if len(payload) > MaxFrame {
		return false, errors.New("invalid broker frame size")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if err := writeFull(w, header[:]); err != nil {
		return false, err
	}
	if err := writeFull(w, payload); err != nil {
		return true, err
	}
	return true, nil
}

func writeFull(w io.Writer, value []byte) error {
	for len(value) > 0 {
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
