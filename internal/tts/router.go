// router.go routes SynthesizeInput onto the engine chosen by the
// companion settings: sapi (offline default), edge (online natural
// voices) or ref (local reference-timbre cloning). Voices() keeps the
// SAPI enumeration for the legacy no-engine payloads; engine-scoped
// catalogues (EdgeVoices/RefVoices) are served by the bridge handlers.
package tts

import "fmt"

// RouterEngine fan-out over the three engines.
type RouterEngine struct {
	sapi Engine
	edge Engine
	ref  Engine
}

// NewRouterEngine wraps the platform (SAPI) engine with the edge and
// reference engines behind one Engine implementation.
func NewRouterEngine(platform Engine) *RouterEngine {
	return NewRouterEngineWithEngines(platform, edgeEngine{}, NewRefEngine())
}

// NewRouterEngineWithEngines wires explicit engine implementations
// (tests and embedders). A nil engine fails with M95-001 semantics
// instead of panicking.
func NewRouterEngineWithEngines(sapi, edge, ref Engine) *RouterEngine {
	return &RouterEngine{sapi: sapi, edge: edge, ref: ref}
}

// Voices keeps legacy behavior: enumerate the platform engine.
func (r *RouterEngine) Voices() ([]Voice, error) {
	if r.sapi == nil {
		return nil, fmt.Errorf("%w: SAPI 引擎未装配", ErrEngineUnavailable)
	}
	return r.sapi.Voices()
}

// Synthesize dispatches by in.Engine; "" behaves like EngineSapi so
// pre-engine payloads keep working.
func (r *RouterEngine) Synthesize(in SynthesizeInput) (SynthesizeResult, bool, error) {
	switch in.Engine {
	case EngineEdge:
		if r.edge == nil {
			return SynthesizeResult{}, false, fmt.Errorf("%w: edge 引擎未装配", ErrEngineUnavailable)
		}
		return r.edge.Synthesize(in)
	case EngineRef:
		if r.ref == nil {
			return SynthesizeResult{}, false, fmt.Errorf("%w: 参考音色引擎未装配", ErrEngineUnavailable)
		}
		return r.ref.Synthesize(in)
	default:
		if r.sapi == nil {
			return SynthesizeResult{}, false, fmt.Errorf("%w: SAPI 引擎未装配", ErrEngineUnavailable)
		}
		return r.sapi.Synthesize(in)
	}
}
