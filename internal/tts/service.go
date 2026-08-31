// service.go is the M9.5 TTS orchestration core (T-9.5.1.2): a
// single-flight synthesis queue (only one segment may occupy the
// engine at a time), epoch-based session cancellation (segments that
// start after a cancel never touch the engine; a segment finishing
// after a cancel is discarded), and small de-duplication of repeated
// segments inside one reply.
package tts

import (
	"strconv"
	"sync"
	"sync/atomic"
)

// Service serializes engine access and owns the cancel epoch.
type Service struct {
	engine Engine

	mu    sync.Mutex
	epoch atomic.Uint64

	cache map[string]SynthesizeResult
	order []string
}

const cacheCapacity = 4

// NewService wires the platform engine into the single-flight shell.
func NewService(engine Engine) *Service {
	return &Service{engine: engine, cache: make(map[string]SynthesizeResult)}
}

// Voices forwards the enumeration under the engine lock.
func (s *Service) Voices() ([]Voice, error) {
	return s.VoicesFor("")
}

// VoicesFor returns the catalogue for one engine selector (edge hits
// the cloud list; everything else stays on the platform engine).
func (s *Service) VoicesFor(engine string) ([]Voice, error) {
	// Edge catalogue is an HTTPS fetch — do not hold the single-flight
	// lock across the network, or a settings probe would stall speech.
	if engine == EngineEdge || engine == EngineVolc {
		s.mu.Lock()
		eng := s.engine
		s.mu.Unlock()
		return voicesFor(eng, engine)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return voicesFor(s.engine, engine)
}

func voicesFor(eng Engine, engine string) ([]Voice, error) {
	if r, ok := eng.(*RouterEngine); ok {
		return r.VoicesFor(engine)
	}
	return eng.Voices()
}

// Cancel bumps the epoch: every synthesize call that has not produced
// a result yet is discarded. Idempotent (a no-op when idle).
func (s *Service) Cancel() {
	s.epoch.Add(1)
}

func cacheKey(in SynthesizeInput) string {
	// The engine and reference parameters join the key: the same text at
	// the same rate is a different audio file per engine/reference.
	return in.Text + "\x00" + in.Engine + "\x00" + in.VoiceID + "\x00" +
		in.RefEndpoint + "\x00" + in.RefWavPath + "\x00" +
		strconv.Itoa(in.Rate) + "\x00" + strconv.Itoa(in.Volume)
}

// SynthesizeResultOut is what the bridge handler needs: the WAV body,
// whether the result was discarded by a racing cancel, and whether the
// requested voice fell back to the default (M95-004).
type SynthesizeResultOut struct {
	Result        SynthesizeResult
	Discarded     bool
	VoiceFallback bool
}

// Synthesize runs one segment under the single-flight lock. The epoch
// snapshot is taken when the request enters (before queueing), so
// calls that were in flight or queued when a cancel landed never reach
// the engine; requests issued after the cancel start a fresh epoch and
// synthesize normally.
func (s *Service) Synthesize(in SynthesizeInput) (SynthesizeResultOut, error) {
	key := cacheKey(in)
	startEpoch := s.epoch.Load()

	s.mu.Lock()
	defer s.mu.Unlock()

	// A cancel landed while this call was queued: drop it before it
	// ever occupies the engine (zero zombie synthesis after cancel).
	if s.epoch.Load() != startEpoch {
		return SynthesizeResultOut{Discarded: true}, nil
	}
	if hit, ok := s.cache[key]; ok {
		return SynthesizeResultOut{Result: hit}, nil
	}

	res, voiceFallback, err := s.engine.Synthesize(in)
	if err != nil {
		// A segment queued behind a cancel may surface as an engine
		// error; report it as discarded so the caller stays quiet.
		if s.epoch.Load() != startEpoch {
			return SynthesizeResultOut{Discarded: true}, nil
		}
		return SynthesizeResultOut{}, err
	}
	if s.epoch.Load() != startEpoch {
		return SynthesizeResultOut{Discarded: true}, nil
	}

	s.cache[key] = res
	s.order = append(s.order, key)
	for len(s.order) > cacheCapacity {
		delete(s.cache, s.order[0])
		s.order = s.order[1:]
	}
	return SynthesizeResultOut{Result: res, VoiceFallback: voiceFallback}, nil
}
