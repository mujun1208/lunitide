package ipc

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/oklog/ulid/v2"
)

type malformedEquipHandler struct{ synchronousStreamingHandler }

func (malformedEquipHandler) HandleStreaming(_ context.Context, r bridge.Request, emit func(bridge.Event) error) bridge.Response {
	if err := emit(bridge.Event{Type: bridge.EventEquip, Equip: &bridge.EquipEvent{Experts: []string{"技能专家"}}}); err == nil {
		return r.Fail("TEST_INVALID_EVENT_ACCEPTED", "invalid event was accepted", false)
	}
	return r.Ok(map[string]bool{"ready": true})
}

// A malformed producer must not write kind:"" onto the shared connection.
// Subsequent requests remain usable without restarting the user's engine.
func TestMalformedEquipDoesNotPoisonSharedConnection(t *testing.T) {
	client := startFramingRegressionSession(t, malformedEquipHandler{}, WriteFrame)
	for i := 0; i < 2; i++ {
		request := framingRegressionRequest()
		raw, _ := json.Marshal(request)
		if err := WriteFrame(client, raw); err != nil {
			t.Fatal(err)
		}
		raw, err := ReadFrame(client)
		if err != nil {
			t.Fatal(err)
		}
		var response bridge.Response
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		if response.Kind != "response" || !response.OK || response.RequestID != request.ID {
			t.Fatalf("bad response: %#v", response)
		}
	}
}

func framingRegressionRequest() bridge.Request {
	return bridge.Request{Version: bridge.Version, Kind: "request", ID: ulid.Make().String(), TraceID: ulid.Make().String(), Method: "system.health", SentAt: time.Now(), Payload: json.RawMessage(`{}`), DeadlineMS: 3000}
}

func startFramingRegressionSession(t *testing.T, h Handler, writer func(io.Writer, []byte) error) net.Conn {
	t.Helper()
	server, client := net.Pipe()
	secret := bytes.Repeat([]byte{12}, sessionSecretSize)
	done := make(chan error, 1)
	go func() {
		done <- serveSession(context.Background(), server, 42, NewSessionAuthenticator(append([]byte(nil), secret...)), h, nil, func(net.Conn) (uint32, error) { return 42, nil }, writer)
	}()
	t.Cleanup(func() {
		_ = client.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("session did not stop")
		}
	})
	hello, _ := json.Marshal(Handshake{RPCMajor: RPCMajor, RPCMinor: RPCMinor, ClientPID: 42, SessionNonce: hex.EncodeToString(secret)})
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := WriteFrame(client, hello); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFrame(client); err != nil {
		t.Fatal(err)
	}
	return client
}

type checkedWriteDeadlineConn struct {
	net.Conn
	deadline time.Time
	writes   int
}

func (c *checkedWriteDeadlineConn) SetWriteDeadline(d time.Time) error {
	c.deadline = d
	return c.Conn.SetWriteDeadline(d)
}

func TestEveryAuthenticatedFrameRenewsWriteBudget(t *testing.T) {
	server, client := net.Pipe()
	checked := &checkedWriteDeadlineConn{Conn: server}
	secret := bytes.Repeat([]byte{13}, sessionSecretSize)
	done := make(chan error, 1)
	writer := func(conn io.Writer, raw []byte) error {
		checked.writes++
		if checked.writes > 1 && time.Until(checked.deadline) < sessionWriteTimeout-time.Second {
			return errors.New("new frame inherited exhausted write budget")
		}
		if err := WriteFrame(conn, raw); err != nil {
			return err
		}
		// Emulate the previous frame finishing with almost no budget left.
		return checked.SetWriteDeadline(time.Now().Add(time.Second))
	}
	go func() {
		done <- serveSession(context.Background(), checked, 42, NewSessionAuthenticator(append([]byte(nil), secret...)), staticHandler{payload: true}, nil, func(net.Conn) (uint32, error) { return 42, nil }, writer)
	}()
	t.Cleanup(func() {
		_ = client.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("session did not stop")
		}
	})
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	hello, _ := json.Marshal(Handshake{RPCMajor: RPCMajor, RPCMinor: RPCMinor, ClientPID: 42, SessionNonce: hex.EncodeToString(secret)})
	if err := WriteFrame(client, hello); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFrame(client); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		raw, _ := json.Marshal(framingRegressionRequest())
		if err := WriteFrame(client, raw); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadFrame(client); err != nil {
			t.Fatal(err)
		}
	}
}
