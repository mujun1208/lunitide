package ipc

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/oklog/ulid/v2"
)

type blockingStreamingHandler struct{ started, cancelled chan struct{} }

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
