//go:build windows

package engineclient

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/oklog/ulid/v2"
)

type Client struct {
	conn            net.Conn
	writeMu         sync.Mutex
	stateMu         sync.Mutex
	closeOnce       sync.Once
	closeErr        error
	broken          error
	pending         map[string]chan callResult
	events          chan bridge.Event
	eventsOnce      sync.Once
	done            chan struct{}
	doneOnce        sync.Once
	tombstones      map[string]time.Time
	streams         map[string]streamProgress
	streamTerminals map[string]time.Time
}

const (
	requestTombstoneLifetime = time.Minute
	streamTombstoneLifetime  = 11 * time.Minute
	clientWriteTimeout       = 35 * time.Second
)

type streamProgress struct {
	nextSequence uint64
}

type callResult struct {
	response bridge.Response
	err      error
}

type handshakeAck struct {
	Accepted bool `json:"accepted"`
	RPCMajor int  `json:"rpcMajor"`
	RPCMinor int  `json:"rpcMinor"`
}

func Connect(ctx context.Context, pipe string, expectedEnginePID int, sessionNonce string) (*Client, error) {
	conn, err := dialExpectedEngine(ctx, pipe, expectedEnginePID)
	if err != nil {
		return nil, err
	}
	client := &Client{conn: conn, pending: make(map[string]chan callResult), events: make(chan bridge.Event, 1024), done: make(chan struct{}), tombstones: make(map[string]time.Time), streams: make(map[string]streamProgress), streamTerminals: make(map[string]time.Time)}
	if err := client.handshake(ctx, sessionNonce); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("clear Engine RPC handshake deadline: %w", err)
	}
	go client.readPump()
	return client, nil
}

func (c *Client) Close() error {
	_ = c.poison(errors.New("Engine RPC client is closed"))
	return c.closeErr
}

func (c *Client) Call(ctx context.Context, request bridge.Request) (bridge.Response, error) {
	if err := c.brokenError(); err != nil {
		return bridge.Response{}, err
	}
	if err := ctx.Err(); err != nil {
		return bridge.Response{}, err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return bridge.Response{}, err
	}
	responseCh := make(chan callResult, 1)
	c.stateMu.Lock()
	if c.broken != nil {
		err := c.broken
		c.stateMu.Unlock()
		return bridge.Response{}, err
	}
	if c.pending == nil {
		c.pending = make(map[string]chan callResult)
	}
	if _, exists := c.pending[request.ID]; exists {
		c.stateMu.Unlock()
		return bridge.Response{}, errors.New("duplicate pending Engine request ID")
	}
	c.pruneTombstonesLocked(time.Now())
	if _, cancelled := c.tombstones[request.ID]; cancelled {
		c.stateMu.Unlock()
		return bridge.Response{}, errors.New("Engine request ID was already cancelled")
	}
	c.pending[request.ID] = responseCh
	c.stateMu.Unlock()
	c.writeMu.Lock()
	if ctx.Err() != nil {
		c.writeMu.Unlock()
		c.cancelPending(request.ID, responseCh)
		return bridge.Response{}, ctx.Err()
	}
	stopCancelWatch := interruptConnectionOnCancel(ctx, func() { _ = c.conn.SetWriteDeadline(time.Now()) })
	deadline := time.Now().Add(clientWriteTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	err = c.conn.SetWriteDeadline(deadline)
	if err == nil {
		err = ipc.WriteFrame(c.conn, body)
	}
	stopCancelWatch()
	if err == nil {
		err = c.conn.SetWriteDeadline(time.Time{})
	}
	c.writeMu.Unlock()
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		c.poison(err)
		result := <-responseCh
		return result.response, result.err
	}
	select {
	case result := <-responseCh:
		return result.response, result.err
	case <-ctx.Done():
		c.stateMu.Lock()
		if pending, exists := c.pending[request.ID]; exists && pending == responseCh {
			delete(c.pending, request.ID)
			c.addTombstoneLocked(request.ID)
			c.stateMu.Unlock()
			return bridge.Response{}, ctx.Err()
		}
		c.stateMu.Unlock()
		// The read pump already claimed the request, so its response is the
		// terminal result even if cancellation became ready at the same instant.
		result := <-responseCh
		return result.response, result.err
	}
}

func (c *Client) cancelPending(id string, responseCh chan callResult) {
	c.stateMu.Lock()
	if pending, exists := c.pending[id]; exists && pending == responseCh {
		delete(c.pending, id)
		c.addTombstoneLocked(id)
	}
	c.stateMu.Unlock()
}

func (c *Client) Events() <-chan bridge.Event { return c.events }
func (c *Client) addTombstoneLocked(id string) {
	if c.tombstones == nil {
		c.tombstones = make(map[string]time.Time)
	}
	now := time.Now()
	c.pruneTombstonesLocked(now)
	c.tombstones[id] = now.Add(requestTombstoneLifetime)
}
func (c *Client) pruneTombstonesLocked(now time.Time) {
	for id, expires := range c.tombstones {
		if !expires.After(now) {
			delete(c.tombstones, id)
		}
	}
	for id, expires := range c.streamTerminals {
		if !expires.After(now) {
			delete(c.streamTerminals, id)
		}
	}
}
func (c *Client) readPump() {
	defer c.eventsOnce.Do(func() { close(c.events) })
	for {
		raw, err := ipc.ReadFrame(c.conn)
		if err != nil {
			c.poison(err)
			return
		}
		var header struct {
			Kind string `json:"kind"`
		}
		if decodeStrict(raw, &header) != nil { // header decode cannot be strict
			var loose map[string]json.RawMessage
			if json.Unmarshal(raw, &loose) != nil || json.Unmarshal(loose["kind"], &header.Kind) != nil {
				c.poison(errors.New("invalid Engine frame"))
				return
			}
		}
		switch header.Kind {
		case "response":
			var response bridge.Response
			if decodeStrict(raw, &response) != nil {
				c.poison(errors.New("invalid Engine response"))
				return
			}
			if validateResponse(response, response.RequestID) != nil {
				c.poison(errors.New("invalid Engine response"))
				return
			}
			c.stateMu.Lock()
			c.pruneTombstonesLocked(time.Now())
			ch, ok := c.pending[response.RequestID]
			_, late := c.tombstones[response.RequestID]
			if ok {
				delete(c.pending, response.RequestID)
			}
			c.stateMu.Unlock()
			if !ok && !late {
				c.poison(errors.New("unknown Engine response"))
				return
			}
			if late {
				continue
			}
			ch <- callResult{response: response}
		case "event":
			var event bridge.Event
			if decodeStrict(raw, &event) != nil || validateEvent(event) != nil {
				c.poison(errors.New("invalid Engine event"))
				return
			}
			if err := c.acceptEvent(event); err != nil {
				c.poison(err)
				return
			}
			select {
			case c.events <- event:
			case <-c.done:
				return
			case <-time.After(30 * time.Second):
				c.poison(errors.New("Engine event consumer stalled"))
				return
			}
		default:
			c.poison(errors.New("unknown Engine frame"))
			return
		}
	}
}

func (c *Client) acceptEvent(event bridge.Event) error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.streams == nil {
		c.streams = make(map[string]streamProgress)
	}
	if c.streamTerminals == nil {
		c.streamTerminals = make(map[string]time.Time)
	}
	c.pruneTombstonesLocked(time.Now())
	if _, terminal := c.streamTerminals[event.StreamID]; terminal {
		return errors.New("Engine event received after stream terminal")
	}
	progress, exists := c.streams[event.StreamID]
	if !exists {
		progress.nextSequence = 1
	}
	if event.Sequence != progress.nextSequence {
		return errors.New("Engine event sequence mismatch")
	}
	progress.nextSequence++
	if terminalEventType(event.Type) {
		delete(c.streams, event.StreamID)
		c.streamTerminals[event.StreamID] = time.Now().Add(streamTombstoneLifetime)
	} else {
		c.streams[event.StreamID] = progress
	}
	return nil
}

func terminalEventType(t bridge.EventType) bool {
	return t == bridge.EventCompleted || t == bridge.EventCancelled || t == bridge.EventFailed || t == bridge.EventTerminalExit
}

func validateEvent(e bridge.Event) error {
	if e.Version != bridge.Version || e.Kind != "event" || !ulidValid(e.ID) || !ulidValid(e.StreamID) || e.Sequence < 1 {
		return errors.New("event envelope mismatch")
	}
	if e.Type != bridge.EventThinking && e.Thinking != nil {
		return errors.New("thinking payload on non-thinking event")
	}
	if e.Type != bridge.EventToolStarted && e.Type != bridge.EventToolCompleted && e.Type != bridge.EventApprovalRequired && e.Tool != nil {
		return errors.New("tool payload on non-tool event")
	}
	const maxText = 16 * 1024
	switch e.Type {
	case bridge.EventTerminalOutput:
		if e.Terminal == nil || e.Terminal.Data == "" || len(e.Terminal.Data) > maxText || e.Delta != nil || e.Usage != nil || e.Error != nil {
			return errors.New("invalid terminal output event")
		}
	case bridge.EventTerminalExit:
		if e.Terminal == nil || e.Terminal.Data != "" || e.Delta != nil || e.Usage != nil || e.Error != nil {
			return errors.New("invalid terminal exit event")
		}
	case bridge.EventDelta:
		if e.Delta == nil || e.Thinking != nil || e.Usage != nil || e.Completed != nil || e.Error != nil || e.Tool != nil || e.Terminal != nil || len(e.Delta.Text) == 0 || len(e.Delta.Text) > maxText {
			return errors.New("invalid delta event")
		}
	case bridge.EventThinking:
		if e.Thinking == nil || e.Delta != nil || e.Usage != nil || e.Completed != nil || e.Error != nil || e.Tool != nil || e.Terminal != nil || len(e.Thinking.Text) == 0 || len(e.Thinking.Text) > maxText {
			return errors.New("invalid thinking event")
		}
	case bridge.EventUsage:
		if e.Delta != nil || e.Thinking != nil || e.Usage == nil || e.Error != nil || e.Usage.InputTokens < 0 || e.Usage.OutputTokens < 0 || e.Usage.TotalTokens < 0 || e.Usage.TotalTokens != e.Usage.InputTokens+e.Usage.OutputTokens {
			return errors.New("invalid usage event")
		}
	case bridge.EventToolStarted, bridge.EventToolCompleted, bridge.EventApprovalRequired:
		if e.Tool == nil || e.Delta != nil || e.Thinking != nil || e.Usage != nil || e.Completed != nil || e.Error != nil || e.Terminal != nil {
			return errors.New("invalid tool event payload")
		}
		tool := e.Tool
		if len(tool.CallID) == 0 || len(tool.CallID) > 128 || len(tool.Name) == 0 || len(tool.Name) > 128 || len(tool.ArgsDigest) != 64 {
			return errors.New("invalid tool event fields")
		}
		if _, err := hex.DecodeString(tool.ArgsDigest); err != nil {
			return errors.New("invalid tool arguments digest")
		}
		if len(tool.Summary) > 4096 {
			return errors.New("tool summary too large")
		}
		switch e.Type {
		case bridge.EventToolStarted:
			if tool.Summary != "" || tool.Artifact != nil {
				return errors.New("invalid tool started event")
			}
		case bridge.EventApprovalRequired:
			if tool.Summary == "" || tool.Artifact != nil {
				return errors.New("invalid approval event")
			}
		case bridge.EventToolCompleted:
			if tool.Artifact != nil && (tool.Artifact.Kind != "html" || len(tool.Artifact.Path) == 0 || len(tool.Artifact.Path) > 4096 || len(tool.Artifact.Content) > 180<<10) {
				return errors.New("invalid tool artifact")
			}
		}
	case bridge.EventCompleted, bridge.EventCancelled:
		if e.Delta != nil || e.Thinking != nil || e.Usage != nil || e.Error != nil {
			return errors.New("invalid terminal event")
		}
	case bridge.EventFailed:
		if e.Delta != nil || e.Thinking != nil || e.Usage != nil || e.Error == nil || len(e.Error.Code) == 0 || len(e.Error.Code) > 128 || len(e.Error.Message) == 0 || len(e.Error.Message) > maxText {
			return errors.New("invalid failed event")
		}
	default:
		return errors.New("unknown event type")
	}
	return nil
}
func ulidValid(s string) bool { _, err := ulid.ParseStrict(s); return err == nil }

func (c *Client) poison(err error) error {
	c.stateMu.Lock()
	if c.broken == nil {
		c.broken = fmt.Errorf("Engine RPC connection is unusable: %w", err)
	}
	broken := c.broken
	for id, ch := range c.pending {
		delete(c.pending, id)
		ch <- callResult{err: broken}
	}
	clear(c.streams)
	clear(c.streamTerminals)
	c.stateMu.Unlock()
	if c.done != nil {
		c.doneOnce.Do(func() { close(c.done) })
	}
	c.closeOnce.Do(func() { c.closeErr = c.conn.Close() })
	return broken
}

func (c *Client) brokenError() error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.broken
}

func (c *Client) handshake(ctx context.Context, sessionNonce string) error {
	if err := setContextDeadline(c.conn, ctx, 5*time.Second); err != nil {
		return err
	}
	stopCancelWatch := interruptConnectionOnCancel(ctx, func() { _ = c.conn.SetDeadline(time.Now()) })
	defer stopCancelWatch()
	hello, err := json.Marshal(ipc.Handshake{RPCMajor: ipc.RPCMajor, RPCMinor: ipc.RPCMinor, ClientPID: os.Getpid(), SessionNonce: sessionNonce})
	if err != nil {
		return err
	}
	if err := ipc.WriteFrame(c.conn, hello); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	raw, err := ipc.ReadFrameLimit(c.conn, 4096)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	var ack handshakeAck
	if err := decodeStrict(raw, &ack); err != nil || !ack.Accepted || ack.RPCMajor != ipc.RPCMajor || ack.RPCMinor < 0 || ack.RPCMinor > ipc.RPCMinor {
		return errors.New("Engine RPC handshake rejected")
	}
	return nil
}

func interruptConnectionOnCancel(ctx context.Context, interrupt func()) func() {
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			interrupt()
		case <-done:
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

func dialExpectedEngine(ctx context.Context, pipe string, expectedPID int) (net.Conn, error) {
	var lastErr error
	for ctx.Err() == nil {
		conn, err := ipc.Dial(ctx, pipe)
		if err == nil {
			pid, pidErr := ipc.ServerProcessID(conn)
			if pidErr == nil && pid == uint32(expectedPID) {
				return conn, nil
			}
			_ = conn.Close()
			if pidErr != nil {
				lastErr = pidErr
			} else {
				lastErr = errors.New("named pipe server PID mismatch")
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
		case <-time.After(100 * time.Millisecond):
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ctx.Err()
}

func setContextDeadline(conn net.Conn, ctx context.Context, fallback time.Duration) error {
	deadline := time.Now().Add(fallback)
	if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
		deadline = value
	}
	return conn.SetDeadline(deadline)
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

func validateResponse(response bridge.Response, requestID string) error {
	if response.Version != bridge.Version || response.Kind != "response" || response.RequestID != requestID {
		return errors.New("Engine response envelope mismatch")
	}
	if _, err := ulid.ParseStrict(response.ID); err != nil {
		return errors.New("Engine response ID is invalid")
	}
	if _, err := ulid.ParseStrict(response.RequestID); err != nil {
		return errors.New("Engine response request ID is invalid")
	}
	if response.OK == (response.Error != nil) || (!response.OK && response.Payload != nil) {
		return fmt.Errorf("Engine response success/error shape is invalid")
	}
	return nil
}
