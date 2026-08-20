// router.go routes SynthesizeInput onto the engine chosen by the
// companion settings: natural/sapi (local OneCore / desktop SAPI),
// edge (free Microsoft cloud neural voices) or ref (local GPT-SoVITS).
package tts

import "fmt"

// RouterEngine fan-out over the engines.
type RouterEngine struct {
	sapi Engine
	ref  Engine
	edge Engine
}

// NewRouterEngine wraps the platform (SAPI) engine with the cloud Edge
// engine and the local reference-timbre engine.
func NewRouterEngine(platform Engine) *RouterEngine {
	return &RouterEngine{sapi: platform, ref: NewRefEngine(), edge: NewEdgeEngine()}
}

// NewRouterEngineWithEngines wires explicit SAPI and ref implementations
// (tests). Edge stays nil so unit tests never dial the public cloud.
func NewRouterEngineWithEngines(sapi, ref Engine) *RouterEngine {
	return &RouterEngine{sapi: sapi, ref: ref}
}

// NewRouterEngineWithAll wires every engine, including a fake Edge
// catalogue for handler tests.
func NewRouterEngineWithAll(sapi, ref, edge Engine) *RouterEngine {
	return &RouterEngine{sapi: sapi, ref: ref, edge: edge}
}

// Voices enumerates the platform engine catalogue (OneCore natural
// voices first, then classic desktop voices).
func (r *RouterEngine) Voices() ([]Voice, error) {
	return r.VoicesFor(EngineSapi)
}

// VoicesFor returns the catalogue for one engine selector.
func (r *RouterEngine) VoicesFor(engine string) ([]Voice, error) {
	switch engine {
	case EngineEdge:
		if r.edge == nil {
			return nil, fmt.Errorf("%w: 云端语音引擎未装配", ErrEngineUnavailable)
		}
		return r.edge.Voices()
	case EngineRef:
		if r.ref == nil {
			return nil, fmt.Errorf("%w: 参考音色引擎未装配", ErrEngineUnavailable)
		}
		return r.ref.Voices()
	default:
		if r.sapi == nil {
			return nil, fmt.Errorf("%w: SAPI 引擎未装配", ErrEngineUnavailable)
		}
		return r.sapi.Voices()
	}
}

// Synthesize dispatches by in.Engine. Empty engine keeps the local
// natural/SAPI path so old payloads stay valid.
func (r *RouterEngine) Synthesize(in SynthesizeInput) (SynthesizeResult, bool, error) {
	switch in.Engine {
	case EngineRef:
		if r.ref == nil {
			return SynthesizeResult{}, false, fmt.Errorf("%w: 参考音色引擎未装配", ErrEngineUnavailable)
		}
		return r.ref.Synthesize(in)
	case EngineEdge:
		if r.edge == nil {
			return SynthesizeResult{}, false, fmt.Errorf("%w: 云端语音引擎未装配", ErrEngineUnavailable)
		}
		res, fb, err := r.edge.Synthesize(in)
		if err == nil {
			return res, fb, nil
		}
		if r.sapi == nil {
			return res, fb, err
		}
		local := in
		local.Engine = EngineNatural
		local.VoiceID = edgeVoiceToLocal(in.VoiceID)
		res2, fb2, err2 := r.sapi.Synthesize(local)
		if err2 != nil {
			return res, fb, err
		}
		return res2, fb2 || true, nil
	default:
		if r.sapi == nil {
			return SynthesizeResult{}, false, fmt.Errorf("%w: SAPI 引擎未装配", ErrEngineUnavailable)
		}
		return r.sapi.Synthesize(in)
	}
}
