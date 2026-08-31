package ipc

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/oklog/ulid/v2"
)

type blockingStreamingHandler struct{ started, cancelled chan struct{} }

type staticHandler struct{ payload any }

func (h staticHandler) Handle(_ context.Context, request bridge.Request) bridge.Response {
	return bridge.Success(request.ID, h.payload)
}

type synchronousStreamingHandler struct{}

func (synchronousStreamingHandler) Handle(context.Context, bridge.Request) bridge.Response {
	panic("unexpected")
}

func (synchronousStreamingHandler) HandleStreaming(_ context.Context, request bridge.Request, emit func(bridge.Event) error) bridge.Response {
	_ = emit(bridge.Event{Version: bridge.Version, Kind: "event", ID: ulid.Make().String(), StreamID: request.ID, Type: bridge.EventDelta})
	return bridge.Success(request.ID, map[string]bool{"accepted": true})
}

type failDeadlineClearConn struct {
	net.Conn
	setDeadlineCalls int
}

func (c *failDeadlineClearConn) SetDeadline(deadline time.Time) error {
	c.setDeadlineCalls++
	if c.setDeadlineCalls == 2 && deadline.IsZero() {
		return errors.New("forced deadline clear failure")
	}
	return c.Conn.SetDeadline(deadline)
}

func (h *blockingStreamingHandler) Handle(context.Context, bridge.Request) bridge.Response {
	panic("unexpected")
}
func (h *blockingStreamingHandler) HandleStreaming(ctx context.Context, r bridge.Request, _ func(bridge.Event) error) bridge.Response {
	close(h.started)
	<-ctx.Done()
	close(h.cancelled)
	return bridge.Failure(r.ID, r.TraceID, "CANCELLED", "cancelled", false)
}

func TestDecodeStrictRejectsUnknownAndTrailingFields(t *testing.T) {
	var hello Handshake
	if err := decodeStrict([]byte(`{"rpcMajor":1,"rpcMinor":0,"clientPid":42,"sessionNonce":"value","extra":true}`), &hello); err == nil {
		t.Fatal("expected unknown field rejection")
	}
	if err := decodeStrict([]byte(`{"rpcMajor":1,"rpcMinor":0,"clientPid":42,"sessionNonce":"value"} {}`), &hello); err == nil {
		t.Fatal("expected trailing JSON rejection")
	}
}

func TestReadFrameLimitUsesHandshakeLimit(t *testing.T) {
	var buffer bytes.Buffer
	if err := WriteFrame(&buffer, bytes.Repeat([]byte{'x'}, 4097)); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFrameLimit(&buffer, 4096); err == nil {
		t.Fatal("expected handshake frame limit rejection")
	}
}

func TestDisconnectCancelsStreamingHandlerBeforeWait(t *testing.T) {
	server, client := net.Pipe()
	secret := bytes.Repeat([]byte{7}, sessionSecretSize)
	auth := NewSessionAuthenticator(append([]byte(nil), secret...))
	h := &blockingStreamingHandler{started: make(chan struct{}), cancelled: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- serveSession(context.Background(), server, 42, auth, h, nil, func(net.Conn) (uint32, error) { return 42, nil }, WriteFrame)
	}()
	hello, _ := json.Marshal(Handshake{RPCMajor: RPCMajor, RPCMinor: RPCMinor, ClientPID: 42, SessionNonce: hex.EncodeToString(secret)})
	if err := WriteFrame(client, hello); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFrameLimit(client, 4096); err != nil {
		t.Fatal(err)
	}
	req, _ := json.Marshal(bridge.Request{Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(), Method: "system.health", SentAt: time.Now(), Payload: json.RawMessage(`{}`), DeadlineMS: 3000})
	if err := WriteFrame(client, req); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	_ = client.Close()
	select {
	case <-h.cancelled:
	case <-time.After(time.Second):
		t.Fatal("disconnect did not cancel handler")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ServeSession waited before cancellation")
	}
}

func TestDeadlineClearFailureStillSignalsAuthenticatedSession(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	secret := bytes.Repeat([]byte{8}, sessionSecretSize)
	authenticated := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- serveSession(context.Background(), &failDeadlineClearConn{Conn: server}, 42,
			NewSessionAuthenticator(append([]byte(nil), secret...)), nil, func() { close(authenticated) },
			func(net.Conn) (uint32, error) { return 42, nil }, WriteFrame)
	}()
	hello, _ := json.Marshal(Handshake{RPCMajor: RPCMajor, RPCMinor: RPCMinor, ClientPID: 42, SessionNonce: hex.EncodeToString(secret)})
	if err := WriteFrame(client, hello); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFrameLimit(client, 4096); err != nil {
		t.Fatal(err)
	}
	select {
	case <-authenticated:
	case <-time.After(time.Second):
		t.Fatal("post-ACK failure did not signal authenticated session")
	}
	select {
	case err := <-done:
		if err == nil || err.Error() != "forced deadline clear failure" {
			t.Fatalf("error = %v, want forced deadline clear failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ServeSession did not return after deadline clear failure")
	}
}

func TestAuthenticatedResponseWriteHasDeadline(t *testing.T) {
	previousTimeout := sessionWriteTimeout
	sessionWriteTimeout = 50 * time.Millisecond
	defer func() { sessionWriteTimeout = previousTimeout }()
	server, client := net.Pipe()
	defer client.Close()
	secret := bytes.Repeat([]byte{9}, sessionSecretSize)
	done := make(chan error, 1)
	go func() {
		done <- serveSession(context.Background(), server, 42,
			NewSessionAuthenticator(append([]byte(nil), secret...)), staticHandler{payload: string(bytes.Repeat([]byte{'x'}, 1<<20))}, nil,
			func(net.Conn) (uint32, error) { return 42, nil }, WriteFrame)
	}()
	hello, _ := json.Marshal(Handshake{RPCMajor: RPCMajor, RPCMinor: RPCMinor, ClientPID: 42, SessionNonce: hex.EncodeToString(secret)})
	if err := WriteFrame(client, hello); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFrameLimit(client, 4096); err != nil {
		t.Fatal(err)
	}
	request, _ := json.Marshal(bridge.Request{Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(), Method: "system.health", SentAt: time.Now(), Payload: json.RawMessage(`{}`), DeadlineMS: 3000})
	if err := WriteFrame(client, request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("blocked authenticated response write exceeded its deadline")
	}
}

func TestSynchronousStreamEventFollowsInitialResponse(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	secret := bytes.Repeat([]byte{10}, sessionSecretSize)
	done := make(chan error, 1)
	go func() {
		done <- serveSession(context.Background(), server, 42,
			NewSessionAuthenticator(append([]byte(nil), secret...)), synchronousStreamingHandler{}, nil,
			func(net.Conn) (uint32, error) { return 42, nil }, WriteFrame)
	}()
	hello, _ := json.Marshal(Handshake{RPCMajor: RPCMajor, RPCMinor: RPCMinor, ClientPID: 42, SessionNonce: hex.EncodeToString(secret)})
	if err := WriteFrame(client, hello); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFrameLimit(client, 4096); err != nil {
		t.Fatal(err)
	}
	request := bridge.Request{Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(), Method: "chat.stream", SentAt: time.Now(), Payload: json.RawMessage(`{}`), DeadlineMS: 3000}
	raw, _ := json.Marshal(request)
	if err := WriteFrame(client, raw); err != nil {
		t.Fatal(err)
	}
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	first, err := ReadFrame(client)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ReadFrame(client)
	if err != nil {
		t.Fatal(err)
	}
	var firstHeader, secondHeader struct {
		Kind string `json:"kind"`
	}
	_ = json.Unmarshal(first, &firstHeader)
	_ = json.Unmarshal(second, &secondHeader)
	if firstHeader.Kind != "response" || secondHeader.Kind != "event" {
		t.Fatalf("frame order = %q then %q, want response then event", firstHeader.Kind, secondHeader.Kind)
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ServeSession did not close")
	}
}

type nonCooperativeHandler struct{ started chan struct{} }

func (h nonCooperativeHandler) Handle(context.Context, bridge.Request) bridge.Response {
	close(h.started)
	select {}
}

func TestDisconnectBoundsNonCooperativeHandlerDrain(t *testing.T) {
	previousTimeout := sessionDrainTimeout
	sessionDrainTimeout = 50 * time.Millisecond
	defer func() { sessionDrainTimeout = previousTimeout }()
	server, client := net.Pipe()
	secret := bytes.Repeat([]byte{11}, sessionSecretSize)
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- serveSession(context.Background(), server, 42,
			NewSessionAuthenticator(append([]byte(nil), secret...)), nonCooperativeHandler{started: started}, nil,
			func(net.Conn) (uint32, error) { return 42, nil }, WriteFrame)
	}()
	hello, _ := json.Marshal(Handshake{RPCMajor: RPCMajor, RPCMinor: RPCMinor, ClientPID: 42, SessionNonce: hex.EncodeToString(secret)})
	if err := WriteFrame(client, hello); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFrameLimit(client, 4096); err != nil {
		t.Fatal(err)
	}
	request, _ := json.Marshal(bridge.Request{Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(), Method: "system.health", SentAt: time.Now(), Payload: json.RawMessage(`{}`), DeadlineMS: 3000})
	if err := WriteFrame(client, request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ServeSession waited indefinitely for non-cooperative handler")
	}
}

func handshakeTestClient(t *testing.T, client net.Conn, secret []byte, pid int) {
	t.Helper()
	hello, _ := json.Marshal(Handshake{RPCMajor: RPCMajor, RPCMinor: RPCMinor, ClientPID: pid, SessionNonce: hex.EncodeToString(secret)})
	if err := WriteFrame(client, hello); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFrameLimit(client, 4096); err != nil {
		t.Fatal(err)
	}
}

func TestSecondSessionSurvivesFirstDisconnect(t *testing.T) {
	secret := bytes.Repeat([]byte{12}, sessionSecretSize)
	auth := NewSessionAuthenticator(append([]byte(nil), secret...))
	peer := func(net.Conn) (uint32, error) { return 42, nil }
	ctx := context.Background()

	s1, c1 := net.Pipe()
	done1 := make(chan error, 1)
	go func() {
		done1 <- serveSession(ctx, s1, 42, auth, staticHandler{payload: map[string]string{"who": "one"}}, nil, peer, WriteFrame)
	}()
	handshakeTestClient(t, c1, secret, 42)

	s2, c2 := net.Pipe()
	defer c2.Close()
	done2 := make(chan error, 1)
	go func() {
		done2 <- serveSession(ctx, s2, 42, auth, staticHandler{payload: map[string]string{"who": "two"}}, nil, peer, WriteFrame)
	}()
	handshakeTestClient(t, c2, secret, 42)
	select {
	case err := <-done2:
		t.Fatalf("second session ended before first disconnect: %v", err)
	default:
	}

	_ = c1.Close()
	select {
	case <-done1:
	case <-time.After(time.Second):
		t.Fatal("first session did not end")
	}
	select {
	case err := <-done2:
		t.Fatalf("second session died when first disconnected: %v", err)
	default:
	}

	req, _ := json.Marshal(bridge.Request{
		Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(),
		Method: "system.health", SentAt: time.Now(), Payload: json.RawMessage(`{}`), DeadlineMS: 3000,
	})
	if err := WriteFrame(c2, req); err != nil {
		t.Fatal(err)
	}
	if err := c2.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	frame, err := ReadFrame(c2)
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		OK      bool              `json:"ok"`
		Payload map[string]string `json:"payload"`
	}
	if err := json.Unmarshal(frame, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Payload["who"] != "two" {
		t.Fatalf("second session after first disconnect = %+v", resp)
	}
}

type chatStartHandler struct{}

func (chatStartHandler) Handle(_ context.Context, request bridge.Request) bridge.Response {
	if request.Method != "chat.start" && request.Method != "system.health" {
		return bridge.Failure(request.ID, request.TraceID, "UNKNOWN", "unexpected", false)
	}
	return bridge.Success(request.ID, map[string]any{"accepted": true, "method": string(request.Method)})
}

func (h chatStartHandler) HandleStreaming(ctx context.Context, request bridge.Request, _ func(bridge.Event) error) bridge.Response {
	return h.Handle(ctx, request)
}

func TestSecondSessionChatStartAfterFirstDisconnect(t *testing.T) {
	secret := bytes.Repeat([]byte{13}, sessionSecretSize)
	auth := NewSessionAuthenticator(append([]byte(nil), secret...))
	peer := func(net.Conn) (uint32, error) { return 42, nil }
	ctx := context.Background()
	handler := chatStartHandler{}

	s1, c1 := net.Pipe()
	done1 := make(chan error, 1)
	go func() {
		done1 <- serveSession(ctx, s1, 42, auth, handler, nil, peer, WriteFrame)
	}()
	handshakeTestClient(t, c1, secret, 42)

	s2, c2 := net.Pipe()
	defer c2.Close()
	done2 := make(chan error, 1)
	go func() {
		done2 <- serveSession(ctx, s2, 42, auth, handler, nil, peer, WriteFrame)
	}()
	handshakeTestClient(t, c2, secret, 42)
	_ = c1.Close()
	select {
	case <-done1:
	case <-time.After(time.Second):
		t.Fatal("first session did not end")
	}

	req, _ := json.Marshal(bridge.Request{
		Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(),
		Method: "chat.start", SentAt: time.Now(), Payload: json.RawMessage(`{}`), DeadlineMS: 3000,
	})
	if err := WriteFrame(c2, req); err != nil {
		t.Fatal(err)
	}
	if err := c2.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	frame, err := ReadFrame(c2)
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		OK      bool           `json:"ok"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(frame, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.Payload["method"] != "chat.start" {
		t.Fatalf("second session chat.start after first disconnect = %+v", resp)
	}
	select {
	case err := <-done2:
		t.Fatalf("second session died: %v", err)
	default:
	}
}
