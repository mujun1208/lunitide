// router.go routes SynthesizeInput onto the engine chosen by the
// companion settings: natural/sapi (local OneCore / desktop SAPI),
// edge (free Microsoft cloud neural voices) or ref (local GPT-SoVITS).
package tts

import (
	"context"
	"encoding/base64"
	"fmt"
)

// RouterEngine fan-out over the engines.
type RouterEngine struct {
	sapi Engine
	ref  Engine
	edge Engine
	volc Engine
	onnx Engine
}

type naturalVoiceCatalog interface {
	NaturalVoices() ([]Voice, error)
}

// NewRouterEngine wraps the platform (SAPI) engine with the cloud Edge
// engine and the local reference-timbre engine.
func NewRouterEngine(platform Engine) *RouterEngine {
	return &RouterEngine{sapi: platform, ref: NewRefEngine(), edge: NewEdgeEngine(), volc: NewVolcEngine(), onnx: NewOnnxEngine()}
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

// NewRouterEngineWithVolc wires a fake or real Volc engine for tests.
func NewRouterEngineWithVolc(sapi, ref, edge, volc Engine) *RouterEngine {
	return &RouterEngine{sapi: sapi, ref: ref, edge: edge, volc: volc}
}

// NewRouterEngineWithOnnx wires a fake or real ONNX engine for tests.
func NewRouterEngineWithOnnx(sapi, ref, edge, volc, onnx Engine) *RouterEngine {
	return &RouterEngine{sapi: sapi, ref: ref, edge: edge, volc: volc, onnx: onnx}
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
	case EngineVolc:
		if r.volc == nil {
			return nil, fmt.Errorf("%w: 火山语音引擎未装配", ErrEngineUnavailable)
		}
		return r.volc.Voices()
	case EngineRef:
		if r.ref == nil {
			return nil, fmt.Errorf("%w: 参考音色引擎未装配", ErrEngineUnavailable)
		}
		return r.ref.Voices()
	case EngineOnnx:
		if r.onnx == nil {
			return nil, fmt.Errorf("%w: 本地语音引擎未装配", ErrEngineUnavailable)
		}
		return r.onnx.Voices()
	case EngineNatural:
		if r.sapi == nil {
			return nil, fmt.Errorf("%w: SAPI 引擎未装配", ErrEngineUnavailable)
		}
		if nat, ok := r.sapi.(naturalVoiceCatalog); ok {
			return nat.NaturalVoices()
		}
		return r.sapi.Voices()
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
	case EngineVolc:
		if r.volc == nil {
			return SynthesizeResult{}, false, fmt.Errorf("%w: 火山语音引擎未装配", ErrEngineUnavailable)
		}
		return r.volc.Synthesize(in)
	case EngineRef:
		if r.ref == nil {
			return SynthesizeResult{}, false, fmt.Errorf("%w: 参考音色引擎未装配", ErrEngineUnavailable)
		}
		return r.ref.Synthesize(in)
	case EngineOnnx:
		if r.onnx == nil {
			return SynthesizeResult{}, false, fmt.Errorf("%w: 本地语音引擎未装配", ErrEngineUnavailable)
		}
		return r.onnx.Synthesize(in)
	case EngineEdge:
		if r.edge == nil {
			return SynthesizeResult{}, false, fmt.Errorf("%w: 云端语音引擎未装配", ErrEngineUnavailable)
		}
		res, fb, err := r.edge.Synthesize(in)
		if err == nil {
			return res, fb, nil
		}
		// Edge neural voice IDs have no faithful local mapping; falling back
		// to SAPI made every voice sound identical (default OneCore pick).
		return res, fb, err
	default:
		if r.sapi == nil {
			return SynthesizeResult{}, false, fmt.Errorf("%w: SAPI 引擎未装配", ErrEngineUnavailable)
		}
		return r.sapi.Synthesize(in)
	}
}

func (r *RouterEngine) SynthesizeStream(ctx context.Context, in SynthesizeInput, emit func([]byte) error) (SynthesizeResult, bool, error) {
	res, fb, err := r.synthesizeStream(ctx, in, emit)
	if err != nil || emit == nil {
		return res, fb, err
	}
	return res, fb, nil
}

func (r *RouterEngine) synthesizeStream(ctx context.Context, in SynthesizeInput, emit func([]byte) error) (SynthesizeResult, bool, error) {
	switch in.Engine {
	case EngineVolc:
		if r.volc == nil {
			return SynthesizeResult{}, false, fmt.Errorf("%w: 火山语音引擎未装配", ErrEngineUnavailable)
		}
		if s, ok := r.volc.(ChunkStreamer); ok {
			return s.SynthesizeStream(ctx, in, emit)
		}
		return emitWhole(r.volc, in, emit)
	case EngineEdge:
		if r.edge == nil {
			return SynthesizeResult{}, false, fmt.Errorf("%w: 云端语音引擎未装配", ErrEngineUnavailable)
		}
		if s, ok := r.edge.(ChunkStreamer); ok {
			return s.SynthesizeStream(ctx, in, emit)
		}
		return emitWhole(r.edge, in, emit)
	default:
		eng := r.sapi
		if in.Engine == EngineRef {
			eng = r.ref
		}
		if in.Engine == EngineOnnx {
			eng = r.onnx
		}
		if eng == nil {
			return SynthesizeResult{}, false, fmt.Errorf("%w: 语音引擎未装配", ErrEngineUnavailable)
		}
		return emitWhole(eng, in, emit)
	}
}

func emitWhole(eng Engine, in SynthesizeInput, emit func([]byte) error) (SynthesizeResult, bool, error) {
	res, fb, err := eng.Synthesize(in)
	if err != nil || emit == nil || res.WavBase64 == "" {
		return res, fb, err
	}
	raw, decErr := base64.StdEncoding.DecodeString(res.WavBase64)
	if decErr != nil || len(raw) == 0 {
		return res, fb, err
	}
	return res, fb, emit(raw)
}
