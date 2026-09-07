package ipc

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/oklog/ulid/v2"
)

type postAckEventHandler struct {
	synchronousStreamingHandler
	start <-chan struct{}
	errs  chan<- [2]error
}

func (h postAckEventHandler) HandleStreaming(ctx context.Context, r bridge.Request, emit func(bridge.Event) error) bridge.Response {
	id := ulid.Make().String()
	go func() {
		select {
		case <-h.start:
		case <-ctx.Done():
			return
		}
		first := emit(bridge.Event{Version: bridge.Version, Kind: "event", ID: ulid.Make().String(), StreamID: id, Sequence: 1, Type: bridge.EventDelta, Delta: &bridge.DeltaEvent{Text: "partial"}})
		second := emit(bridge.Event{Version: bridge.Version, Kind: "event", ID: ulid.Make().String(), StreamID: id, Sequence: 2, Type: bridge.EventCompleted})
		h.errs <- [2]error{first, second}
	}()
	return r.Ok(map[string]string{"streamId": id})
}

func TestPartialPostAckEventWriteClosesConnectionBeforeNextFrame(t *testing.T) {
	start := make(chan struct{})
	errs := make(chan [2]error, 1)
	var eventWrites atomic.Int32
	injected := errors.New("injected partial event write timeout")
	writer := func(w io.Writer, raw []byte) error {
		var header struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			return err
		}
		if header.Kind != "event" || eventWrites.Add(1) > 1 {
			return WriteFrame(w, raw)
		}
		// The peer has received the declared length and only part of its body.
		var prefix [4]byte
		binary.BigEndian.PutUint32(prefix[:], uint32(len(raw)))
		if err := writeFull(w, append(prefix[:], raw[:8]...)); err != nil {
			return err
		}
		return injected
	}
	client := startFramingRegressionSession(t, postAckEventHandler{start: start, errs: errs}, writer)
	raw, _ := json.Marshal(framingRegressionRequest())
	if err := WriteFrame(client, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFrame(client); err != nil {
		t.Fatal(err)
	}
	// Emit only after the response has fully crossed the connection: the
	// pre-response queue has a separate existing failure cleanup path.
	close(start)
	if _, err := ReadFrame(client); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated frame was not terminated immediately: %v", err)
	}
	select {
	case got := <-errs:
		if !errors.Is(got[0], injected) || got[1] == nil || eventWrites.Load() != 1 {
			t.Fatalf("damaged connection accepted another frame: errors=%v writes=%d", got, eventWrites.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("failed event writes did not return promptly")
	}
}
