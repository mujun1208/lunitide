package tts

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockEngine records concurrency and call counts for the queue tests.
type mockEngine struct {
	mu sync.Mutex

	inFlight  atomic.Int32
	maxFlight atomic.Int32
	calls     atomic.Int32

	block   chan struct{} // non-nil => each call waits until closed
	voices  []Voice
	voiceEr error
	synEr   error
}

func (m *mockEngine) Voices() ([]Voice, error) {
	m.calls.Add(1)
	if m.voiceEr != nil {
		return nil, m.voiceEr
	}
	return m.voices, nil
}

func (m *mockEngine) Synthesize(in SynthesizeInput) (SynthesizeResult, bool, error) {
	cur := m.inFlight.Add(1)
	for {
		max := m.maxFlight.Load()
		if cur <= max || m.maxFlight.CompareAndSwap(max, cur) {
			break
		}
	}
	time.Sleep(2 * time.Millisecond) // widen the race window
	if m.block != nil {
		<-m.block
	}
	m.inFlight.Add(-1)
	m.calls.Add(1)
	if m.synEr != nil {
		return SynthesizeResult{}, false, m.synEr
	}
	return SynthesizeResult{WavBase64: "wav:" + in.Text, DurationHint: 1.5}, false, nil
}

func TestServiceSynthesizeSingleFlight(t *testing.T) {
	mock := &mockEngine{}
	svc := NewService(mock)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := svc.Synthesize(SynthesizeInput{Text: "段"})
			if err != nil || out.Result.WavBase64 == "" {
				t.Error("unexpected synthesize failure")
			}
		}()
	}
	wg.Wait()
	if mock.maxFlight.Load() != 1 {
		t.Fatalf("engine saw %d concurrent flights, want 1", mock.maxFlight.Load())
	}
}

func TestServiceCancelDiscardsQueuedSegments(t *testing.T) {
	mock := &mockEngine{block: make(chan struct{})}
	svc := NewService(mock)

	first := make(chan struct{})
	go func() {
		out, err := svc.Synthesize(SynthesizeInput{Text: "第一段"})
		if err != nil {
			t.Error(err)
		}
		if !out.Discarded {
			t.Error("in-flight segment must be discarded after cancel")
		}
		close(first)
	}()

	time.Sleep(5 * time.Millisecond) // let the first call occupy the engine

	second := make(chan struct{})
	go func() {
		out, err := svc.Synthesize(SynthesizeInput{Text: "第二段"})
		if err != nil {
			t.Error(err)
		}
		if !out.Discarded {
			t.Error("queued segment after cancel must be discarded")
		}
		close(second)
	}()
	time.Sleep(5 * time.Millisecond) // let the queued call enter before the cancel
	svc.Cancel()

	close(mock.block) // release the in-flight engine call
	<-first
	<-second

	if got := mock.calls.Load(); got != 1 {
		t.Fatalf("engine calls = %d, want 1 (queued segment never reached the engine)", got)
	}

	// A request issued after the cancel starts a fresh epoch and runs.
	fresh, err := svc.Synthesize(SynthesizeInput{Text: "新回复"})
	if err != nil || fresh.Discarded || fresh.Result.WavBase64 != "wav:新回复" {
		t.Fatalf("post-cancel fresh request = %+v, %v", fresh, err)
	}
	if got := mock.calls.Load(); got != 2 {
		t.Fatalf("engine calls after fresh request = %d, want 2", got)
	}
}

func TestServiceDeduplicatesRepeatedSegments(t *testing.T) {
	mock := &mockEngine{}
	svc := NewService(mock)
	in := SynthesizeInput{Text: "重复段", VoiceID: "v1", Rate: 0, Volume: 80}
	if _, err := svc.Synthesize(in); err != nil {
		t.Fatal(err)
	}
	out, err := svc.Synthesize(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Result.WavBase64 != "wav:重复段" {
		t.Fatalf("cache miss: %q", out.Result.WavBase64)
	}
	if got := mock.calls.Load(); got != 1 {
		t.Fatalf("engine calls = %d, want 1 (dedup)", got)
	}
}

func TestServiceErrorPropagation(t *testing.T) {
	svc := NewService(&mockEngine{voiceEr: ErrEngineUnavailable})
	if _, err := svc.Voices(); !errors.Is(err, ErrEngineUnavailable) {
		t.Fatalf("voices error = %v, want ErrEngineUnavailable", err)
	}
	svc2 := NewService(&mockEngine{synEr: ErrSynthesisFailed})
	if _, err := svc2.Synthesize(SynthesizeInput{Text: "x"}); !errors.Is(err, ErrSynthesisFailed) {
		t.Fatalf("synthesize error = %v, want ErrSynthesisFailed", err)
	}
}

func TestServiceCancelIdleIsNoOp(t *testing.T) {
	svc := NewService(&mockEngine{})
	svc.Cancel()
	svc.Cancel()
	out, err := svc.Synthesize(SynthesizeInput{Text: "ok"})
	if err != nil || out.Discarded || out.Result.WavBase64 == "" {
		t.Fatalf("post-idle-cancel synthesize = %+v, %v", out, err)
	}
}
