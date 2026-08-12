package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

type reasoningStreamAdapter struct{}

type fragmentedReasoningAdapter struct {
	fragments int
	delay     time.Duration
}

func (reasoningStreamAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (reasoningStreamAdapter) TestConnection(context.Context, []byte, gateway.Request) error {
	return errors.New("not used")
}
func (reasoningStreamAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (reasoningStreamAdapter) Stream(_ context.Context, _ []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	if err := emit(gateway.Delta{Reasoning: strings.Repeat("思", 10000)}); err != nil {
		return gateway.Response{}, err
	}
	if err := emit(gateway.Delta{Text: "answer"}); err != nil {
		return gateway.Response{}, err
	}
	return gateway.Response{}, nil
}

func (fragmentedReasoningAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (fragmentedReasoningAdapter) TestConnection(context.Context, []byte, gateway.Request) error {
	return errors.New("not used")
}
func (fragmentedReasoningAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (a fragmentedReasoningAdapter) Stream(_ context.Context, _ []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	for i := 0; i < a.fragments; i++ {
		if a.delay > 0 && i == 1 {
			time.Sleep(a.delay)
		}
		if err := emit(gateway.Delta{Reasoning: "x"}); err != nil {
			return gateway.Response{}, err
		}
	}
	if err := emit(gateway.Delta{Text: "answer"}); err != nil {
		return gateway.Response{}, err
	}
	return gateway.Response{}, nil
}

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

func TestStreamFinalizationClaimRejectsCancellation(t *testing.T) {
	e := NewEngine(nil, "test")
	_, cancel := context.WithCancel(context.Background())
	id := "stream"
	state := &streamState{cancel: cancel, state: streamRunning}
	e.streams[id] = state
	if !e.claimStreamFinalization(state) {
		t.Fatal("finalization claim lost without cancellation")
	}
	if e.cancelStream(id) {
		t.Fatal("cancellation crossed finalization claim")
	}
	if got := e.selectTerminal(id, state, nil); got != bridge.EventCompleted {
		t.Fatalf("terminal=%s", got)
	}
	e.finishTerminal(id, state)
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

func TestRunStreamEmitsBoundedThinkingWithoutMixingAnswer(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return reasoningStreamAdapter{}, nil
	})
	_, cancel := context.WithCancel(context.Background())
	state := &streamState{cancel: cancel, state: streamRunning}
	e.streams["stream"] = state
	var events []bridge.Event
	e.runStream(context.Background(), "stream", state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, gateway.Request{}, func(event bridge.Event) error { events = append(events, event); return nil }, "")
	var thought, answer string
	for _, event := range events {
		if event.Type == bridge.EventThinking {
			if event.Thinking == nil || len(event.Thinking.Text) > 16*1024 || !utf8.ValidString(event.Thinking.Text) {
				t.Fatalf("invalid thinking chunk: %#v", event)
			}
			thought += event.Thinking.Text
		}
		if event.Type == bridge.EventDelta {
			answer += event.Delta.Text
		}
	}
	if answer != "answer" || thought != strings.Repeat("思", 10000) || strings.Contains(answer, "思") {
		t.Fatalf("answer=%q thinking bytes=%d", answer, len(thought))
	}
}

func TestRunStreamAggregatesFragmentedThinkingAndPreservesOrder(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return fragmentedReasoningAdapter{fragments: 5000}, nil
	})
	_, cancel := context.WithCancel(context.Background())
	state := &streamState{cancel: cancel, state: streamRunning}
	e.streams["stream"] = state
	var events []bridge.Event
	e.runStream(context.Background(), "stream", state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, gateway.Request{}, func(event bridge.Event) error {
		events = append(events, event)
		return nil
	}, "")
	thinkingEvents := 0
	var thought string
	answerIndex := -1
	for i, event := range events {
		switch event.Type {
		case bridge.EventThinking:
			if answerIndex >= 0 {
				t.Fatal("thinking emitted after answer")
			}
			thinkingEvents++
			thought += event.Thinking.Text
		case bridge.EventDelta:
			answerIndex = i
		}
	}
	if thought != strings.Repeat("x", 5000) || thinkingEvents > 2 {
		t.Fatalf("thinking bytes=%d events=%d", len(thought), thinkingEvents)
	}
	if answerIndex < 1 || events[answerIndex-1].Type != bridge.EventThinking || events[len(events)-1].Type != bridge.EventCompleted {
		t.Fatalf("unexpected ordering: %#v", events)
	}
}

func TestRunStreamFlushesSlowFragmentedThinkingPromptly(t *testing.T) {
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return fragmentedReasoningAdapter{fragments: 2, delay: 60 * time.Millisecond}, nil
	})
	_, cancel := context.WithCancel(context.Background())
	state := &streamState{cancel: cancel, state: streamRunning}
	e.streams["stream"] = state
	var events []bridge.Event
	e.runStream(context.Background(), "stream", state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, gateway.Request{}, func(event bridge.Event) error {
		events = append(events, event)
		return nil
	}, "")
	if len(events) < 3 || events[0].Type != bridge.EventThinking || events[0].Thinking.Text != "xx" || events[1].Type != bridge.EventDelta {
		t.Fatalf("slow fragments were not flushed before answer: %#v", events)
	}
}
