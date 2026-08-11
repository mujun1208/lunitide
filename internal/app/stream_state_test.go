package app

import (
	"context"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/secretlease"
)

type streamTestLease struct{}

func (streamTestLease) WithLease(_ context.Context, _ secretlease.Request, fn func([]byte) error) error {
	return fn([]byte("test-credential"))
}

type streamTestAdapter struct{ usageCallbacks int }

func (a streamTestAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (a streamTestAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (a streamTestAdapter) Stream(_ context.Context, _ []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	usage := gateway.Usage{InputTokens: 2, OutputTokens: 3, TotalTokens: 5}
	for range a.usageCallbacks {
		if err := emit(gateway.Delta{Usage: &usage}); err != nil {
			return gateway.Response{}, err
		}
	}
	return gateway.Response{Usage: usage}, nil
}

func TestStreamCancellationTerminalArbitration(t *testing.T) {
	e := NewEngine(nil, "test")
	ctx, cancel := context.WithCancel(context.Background())
	id := "stream"
	state := &streamState{cancel: cancel, state: streamRunning}
	e.streams[id] = state
	if !e.cancelStream(id) {
		t.Fatal("running cancellation lost")
	}
	<-ctx.Done()
	if e.cancelStream(id) {
		t.Fatal("second cancellation succeeded")
	}
	if got := e.selectTerminal(id, state, errors.New("upstream")); got != bridge.EventCancelled {
		t.Fatalf("terminal=%s", got)
	}
	e.finishTerminal(id, state)
	if e.cancelStream(id) {
		t.Fatal("post-terminal cancellation succeeded")
	}
	if _, ok := e.streams[id]; ok {
		t.Fatal("terminal stream retained")
	}
}

func TestStreamCompletionWinsBeforeCancellation(t *testing.T) {
	e := NewEngine(nil, "test")
	_, cancel := context.WithCancel(context.Background())
	id := "stream"
	state := &streamState{cancel: cancel, state: streamRunning}
	e.streams[id] = state
	if got := e.selectTerminal(id, state, nil); got != bridge.EventCompleted {
		t.Fatalf("terminal=%s", got)
	}
	e.finishTerminal(id, state)
	if e.cancelStream(id) {
		t.Fatal("cancellation changed selected terminal")
	}
}

func TestFailedTerminalEmissionReleasesStreamState(t *testing.T) {
	e := NewEngine(nil, "test")
	_, cancel := context.WithCancel(context.Background())
	state := &streamState{cancel: cancel, state: streamRunning}
	e.streams["stream"] = state
	if got := e.selectTerminal("stream", state, errors.New("upstream")); got != bridge.EventFailed {
		t.Fatalf("terminal=%s", got)
	}
	e.finishTerminal("stream", state)
	if _, ok := e.streams["stream"]; ok {
		t.Fatal("failed terminal emission retained stream capacity")
	}
}

func TestRunStreamEmitsUsageExactlyOnce(t *testing.T) {
	for _, usageCallbacks := range []int{0, 1, 2} {
		e := NewEngineWithGateway(nil, "test", streamTestLease{})
		e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
			return streamTestAdapter{usageCallbacks: usageCallbacks}, nil
		})
		_, cancel := context.WithCancel(context.Background())
		state := &streamState{cancel: cancel, state: streamRunning}
		id := "stream"
		e.streams[id] = state
		var events []bridge.Event
		e.runStream(context.Background(), id, state, provider.Provider{
			ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible,
			BaseURL: "https://api.example.com", CredentialRef: "credential-ref",
		}, gateway.Request{}, func(event bridge.Event) error {
			events = append(events, event)
			return nil
		}, "")
		usageCount := 0
		for _, event := range events {
			if event.Type == bridge.EventUsage {
				usageCount++
			}
		}
		if usageCount != 1 {
			t.Fatalf("usageCallbacks=%d usage events=%d events=%#v", usageCallbacks, usageCount, events)
		}
		if events[len(events)-1].Type != bridge.EventCompleted {
			t.Fatalf("usageCallbacks=%d terminal=%s", usageCallbacks, events[len(events)-1].Type)
		}
	}
}
