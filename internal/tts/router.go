// router.go routes SynthesizeInput onto the engine chosen by the
// companion settings: natural (local OneCore neural voices, the default
// that legacy "edge" payloads alias), sapi (classic desktop voices) or
// ref (local reference-timbre cloning). natural/edge/sapi all run on the
// platform SAPI engine — natural simply prefers OneCore tokens; the
// ref engine stays behind the router for bridge-level callers.
package tts

import "fmt"

// RouterEngine fan-out over the engines.
type RouterEngine struct {
	sapi Engine
	ref  Engine
}

// NewRouterEngine wraps the platform (SAPI) engine with the reference
// engine behind one Engine implementation.
func NewRouterEngine(platform Engine) *RouterEngine {
	return NewRouterEngineWithEngines(platform, NewRefEngine())
}

// NewRouterEngineWithEngines wires explicit engine implementations
// (tests and embedders). A nil engine fails with M95-001 semantics
// instead of panicking.
func NewRouterEngineWithEngines(sapi, ref Engine) *RouterEngine {
	return &RouterEngine{sapi: sapi, ref: ref}
}

// Voices enumerates the platform engine catalogue (OneCore natural
// voices first, then classic desktop voices).
func (r *RouterEngine) Voices() ([]Voice, error) {
	if r.sapi == nil {
		return nil, fmt.Errorf("%w: SAPI 引擎未装配", ErrEngineUnavailable)
	}
	return r.sapi.Voices()
}

// Synthesize dispatches by in.Engine; "" and legacy "edge" behave like
// EngineNatural so pre-existing payloads keep working.
func (r *RouterEngine) Synthesize(in SynthesizeInput) (SynthesizeResult, bool, error) {
	switch in.Engine {
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
