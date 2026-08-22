//go:build windows

// sapi_windows.go encapsulates the Windows SAPI SpVoice/SpFileStream COM
// surface (M9.5 T-9.5.1.1): offline in-process WAV synthesis at
// 16kHz mono 16-bit, voice enumeration with id/name/gender/lang, and
// rate [-10,10] / volume [0,100] passthrough.
//
// Performance: a dedicated worker goroutine owns a long-lived SpVoice
// COM object (STA, pinned to one OS thread). Synthesis requests are
// serialised through a channel so the per-call overhead of
// CoCreateInstance + token walks + DISPID resolution disappears —
// roughly 50–100 ms saved per segment, which translates to 3–10×
// faster conversational TTS for the Moon Companion.
//
// The OneCore natural voices (Huihui/Kangkang/Yaoyao…, the neural
// pool under HKLM\SOFTWARE\Microsoft\Speech_OneCore) are hidden from
// classic SAPI enumeration. Raw ISpObjectToken/ISpVoice vtable calls
// proved fragile (object lifetime crashes in the OneCore engine), so
// the supported route is used instead: on enumeration the OneCore
// token registry keys are mirrored (once, idempotently) into
// HKCU\SOFTWARE\Microsoft\Speech\Voices\Tokens, which makes them
// ordinary SAPI tokens — enumerated via GetVoices and selected via
// the standard Voice property, no raw COM, no admin rights.
package tts

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"

	"github.com/zzl/go-com/com"
	"github.com/zzl/go-com/ole"
	"github.com/zzl/go-win32api/v2/win32"
)

const (
	progIDSpVoice      = "SAPI.SpVoice"
	progIDSpFileStream = "SAPI.SpFileStream"

	oneCoreVoiceRegPath  = `SOFTWARE\Microsoft\Speech_OneCore\Voices\Tokens`
	oneCoreVoiceRootPath = `HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Speech_OneCore\Voices\Tokens\`
	// mirrorRootPath is where the mirrored OneCore tokens live so that
	// classic SAPI (SpVoice.GetVoices) sees them as ordinary tokens.
	mirrorRootPath = `SOFTWARE\Microsoft\Speech\Voices\Tokens`
	mirrorIDPrefix = `HKEY_CURRENT_USER\SOFTWARE\Microsoft\Speech\Voices\Tokens\`

	// SpeechAudioFormatType: SAFT16kHz16BitMono = 18 (16kHz, 16-bit, mono).
	saft16k16BitMono = 18
	// SpeechStreamFileMode: SSFMCreateForOverwrite = 3.
	ssfmCreateForOverwrite = 3
	// SpeechVoiceSpeakFlags: SVSFDefault = 0 (synchronous Speak).
	svsfDefault = 0

	wavSamplesPerSecond = 16000
	wavBytesPerSample   = 2
	wavHeaderBytes      = 44
)

// ---------------------------------------------------------------------------
// Voice worker pool — a long-lived goroutine that owns the SpVoice COM object
// on a pinned OS thread. Every synthesis request is serialised through a
// channel, eliminating per-call CoCreateInstance / token-enumeration /
// DISPID-resolution overhead (~50–100 ms per segment).
// ---------------------------------------------------------------------------

// voiceReq is one synthesis request sent to the worker goroutine.
type voiceReq struct {
	input  SynthesizeInput
	respCh chan voiceResp
}

// voiceResp is the result of one synthesis request.
type voiceResp struct {
	result   SynthesizeResult
	fallback bool
	err      error
}

// voiceWorker is the long-lived synthesis goroutine. It owns the SpVoice
// and SpFileStream COM objects, pre-resolved DISPIDs, a cached voice
// token, and a reusable temp directory.
type voiceWorker struct {
	reqCh chan voiceReq

	// COM objects (owned by the pinned OS thread).
	voice  *ole.OleClient // SAPI.SpVoice
	stream *ole.OleClient // SAPI.SpFileStream

	// Pre-resolved DISPIDs — resolving these once saves ~5 ms per call.
	rateID      win32.DISPID
	volID       win32.DISPID
	voicePropID win32.DISPID
	speakID     win32.DISPID
	audioOutID  win32.DISPID
	streamOpen  win32.DISPID
	streamClose win32.DISPID

	// Cached state so we only re-select the voice token when it changes.
	currentVoiceID string
	currentEngine  string

	// Reusable temp directory for WAV output.
	tmpDir string
}

var (
	workerOnce   sync.Once
	globalWorker *voiceWorker
)

// getWorker returns the singleton voice worker, starting it on first use.
func getWorker() *voiceWorker {
	workerOnce.Do(func() {
		globalWorker = startWorker()
	})
	return globalWorker
}

// startWorker creates the worker and launches its goroutine. The goroutine
// pins itself to an OS thread (STA) and initialises COM once.
func startWorker() *voiceWorker {
	w := &voiceWorker{reqCh: make(chan voiceReq)}
	go w.run()
	return w
}

// run is the worker loop. It locks the OS thread, initialises COM, creates
// the long-lived SpVoice + SpFileStream, pre-resolves all DISPIDs, and
// processes synthesis requests until the channel is closed.
func (w *voiceWorker) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	uninit, err := comInit()
	if err != nil {
		// If COM init fails the worker is dead; every request will get
		// ErrEngineUnavailable. The channel receive loop still drains
		// requests so callers don't block forever.
		for req := range w.reqCh {
			req.respCh <- voiceResp{err: fmt.Errorf("%w: %v", ErrEngineUnavailable, err)}
		}
		return
	}
	defer uninit()

	// Mirror OneCore tokens so natural voices are available.
	_ = mirrorOneCoreTokens()

	w.voice, err = createDispatch(progIDSpVoice)
	if err != nil {
		for req := range w.reqCh {
			req.respCh <- voiceResp{err: fmt.Errorf("%w: %v", ErrEngineUnavailable, err)}
		}
		return
	}
	defer w.voice.Release()

	w.stream, err = createDispatch(progIDSpFileStream)
	if err != nil {
		for req := range w.reqCh {
			req.respCh <- voiceResp{err: fmt.Errorf("%w: %v", ErrEngineUnavailable, err)}
		}
		return
	}
	defer w.stream.Release()

	// Set stream format once: 16kHz mono 16-bit.
	if fmtVar, ferr := propGetByName(w.stream, "Format"); ferr == nil {
		if fmtObj := fmtVar.IDispatch(); fmtObj != nil {
			format := &ole.OleClient{IDispatch: fmtObj}
			if id, derr := dispID(format, "Type"); derr == nil {
				_ = format.PropPut(id, []interface{}{int32(saft16k16BitMono)})
			}
		}
		fmtVar.Clear()
	}

	// Pre-resolve DISPIDs — saves ~1 ms per property access.
	w.rateID, _ = dispID(w.voice, "Rate")
	w.volID, _ = dispID(w.voice, "Volume")
	w.voicePropID, _ = dispID(w.voice, "Voice")
	w.speakID, _ = dispID(w.voice, "Speak")
	w.audioOutID, _ = dispID(w.voice, "AudioOutputStream")
	w.streamOpen, _ = dispID(w.stream, "Open")
	w.streamClose, _ = dispID(w.stream, "Close")

	// Pre-create temp directory for WAV output.
	w.tmpDir, _ = os.MkdirTemp("", "lunitide-tts")
	if w.tmpDir != "" {
		defer os.RemoveAll(w.tmpDir)
	}

	for req := range w.reqCh {
		result, fallback, err := w.synth(req.input)
		req.respCh <- voiceResp{result: result, fallback: fallback, err: err}
	}
}

// synth performs one synthesis using the cached COM objects. Voice
// selection is only re-done when the voice ID or engine changes.
func (w *voiceWorker) synth(in SynthesizeInput) (SynthesizeResult, bool, error) {
	var zero SynthesizeResult

	// Select voice only when it changed from the previous request.
	if in.VoiceID != w.currentVoiceID || in.Engine != w.currentEngine {
		w.selectVoice(in)
	}

	// Apply per-request rate and volume.
	if w.rateID != 0 {
		_ = w.voice.PropPut(w.rateID, []interface{}{int32(clamp(in.Rate, -10, 10))})
	}
	if w.volID != 0 {
		_ = w.voice.PropPut(w.volID, []interface{}{int32(clamp(in.Volume, 0, 100))})
	}

	// Synthesise to a temp WAV file in the pre-created directory.
	tmpPath := filepath.Join(w.tmpDir, "segment.wav")
	if w.streamOpen != 0 {
		if _, err := w.stream.Call(w.streamOpen, []interface{}{tmpPath, int32(ssfmCreateForOverwrite), false}); err != nil {
			return zero, false, fmt.Errorf("%w: %v", ErrSynthesisFailed, err)
		}
	}
	if w.audioOutID != 0 {
		_ = w.voice.PropPutRef(w.audioOutID, []interface{}{w.stream.IDispatch})
	}
	if w.speakID != 0 {
		if _, err := w.voice.Call(w.speakID, []interface{}{in.Text, int32(svsfDefault)}); err != nil {
			if w.streamClose != 0 {
				_, _ = w.stream.Call(w.streamClose, nil)
			}
			return zero, false, fmt.Errorf("%w: %v", ErrSynthesisFailed, err)
		}
	}
	if w.streamClose != 0 {
		_, _ = w.stream.Call(w.streamClose, nil)
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil || len(data) <= wavHeaderBytes || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return zero, false, fmt.Errorf("%w: invalid wav output", ErrSynthesisFailed)
	}
	seconds := float64(len(data)-wavHeaderBytes) / float64(wavSamplesPerSecond*wavBytesPerSample)
	return SynthesizeResult{
		WavBase64:    base64.StdEncoding.EncodeToString(data),
		DurationHint: math.Round(seconds*100) / 100,
	}, false, nil
}

// selectVoice updates the SpVoice's Voice property to match the requested
// voice ID or engine preference. It caches the current selection so
// subsequent requests with the same voice skip this step.
func (w *voiceWorker) selectVoice(in SynthesizeInput) {
	w.currentVoiceID = in.VoiceID
	w.currentEngine = in.Engine

	voiceID := in.VoiceID
	// Legacy OneCore ids → mirrored HKCU token.
	if strings.HasPrefix(voiceID, oneCoreVoiceRootPath) {
		voiceID = mirrorIDPrefix + strings.TrimPrefix(voiceID, oneCoreVoiceRootPath)
	}

	if voiceID != "" {
		tokVar, terr := findTokenByID(w.voice, voiceID)
		if terr == nil && tokVar != nil {
			defer tokVar.Clear()
			if w.voicePropID != 0 {
				_ = w.voice.PropPutRef(w.voicePropID, []interface{}{tokVar.IDispatch()})
			}
			return
		}
		// Fall through to default selection on lookup failure.
	}

	// "默认音色" or fallback: prefer the first OneCore natural voice.
	if in.Engine == EngineNatural || in.Engine == EngineEdge || in.Engine == "" {
		voices, verr := (sapiEngine{}).Voices()
		if verr == nil && len(voices) > 0 {
			tokVar, terr := findTokenByID(w.voice, voices[0].VoiceID)
			if terr == nil && tokVar != nil {
				defer tokVar.Clear()
				if w.voicePropID != 0 {
					_ = w.voice.PropPutRef(w.voicePropID, []interface{}{tokVar.IDispatch()})
				}
			}
		}
	}
}

// sapiEngine is a stateless Engine; the worker pool owns all COM state.
type sapiEngine struct{}

// NewPlatformEngine returns the Windows SAPI engine.
func NewPlatformEngine() Engine { return sapiEngine{} }

// langByHexLCID maps the SAPI Language attribute (hex LCID string) onto
// the BCP-47 tags the Moon Companion UI reasons about.
var langByHexLCID = map[string]string{
	"409": "en-US", "809": "en-GB", "1009": "en-CA", "c09": "en-AU",
	"804": "zh-CN", "404": "zh-TW", "c04": "zh-HK",
	"411": "ja-JP", "412": "ko-KR", "40c": "fr-FR", "407": "de-DE",
	"40a": "es-ES", "410": "it-IT", "416": "pt-BR", "419": "ru-RU",
}

func normalizeLang(hexLCID string) string {
	lower := strings.ToLower(hexLCID)
	if tag, ok := langByHexLCID[lower]; ok {
		return tag
	}
	return lower
}

// comInit initializes COM STA on the calling (locked) thread. When the
// thread is already apartment-initialized in a different mode the call
// keeps going: the objects stay usable through standard marshalling.
func comInit() (func(), error) {
	hr := win32.CoInitializeEx(nil, win32.COINIT_APARTMENTTHREADED)
	if win32.FAILED(hr) {
		if hr == win32.RPC_E_CHANGED_MODE {
			return func() {}, nil
		}
		return nil, com.NewError(hr)
	}
	return win32.CoUninitialize, nil
}

func clsidFromProgID(progID string) (*syscall.GUID, error) {
	var guid syscall.GUID
	pw := win32.StrToPwstr(progID)
	if hr := win32.CLSIDFromProgID(pw, &guid); win32.FAILED(hr) {
		return nil, com.NewError(hr)
	}
	return &guid, nil
}

// createDispatch CoCreates an in-process IDispatch object by ProgID.
func createDispatch(progID string) (*ole.OleClient, error) {
	clsid, err := clsidFromProgID(progID)
	if err != nil {
		return nil, err
	}
	var disp *win32.IDispatch
	hr := win32.CoCreateInstance(clsid, nil, win32.CLSCTX_INPROC_SERVER,
		&win32.IID_IDispatch, unsafe.Pointer(&disp))
	if win32.FAILED(hr) {
		return nil, com.NewError(hr)
	}
	return &ole.OleClient{IDispatch: disp}, nil
}

func dispID(c *ole.OleClient, name string) (win32.DISPID, error) {
	pw := win32.StrToPwstr(name)
	var id int32
	if hr := c.GetIDsOfNames(&win32.IID_NULL, &pw, 1, win32.LOCALE_INVARIANT, &id); win32.FAILED(hr) {
		return 0, com.NewError(hr)
	}
	return win32.DISPID(id), nil
}

func callByName(c *ole.OleClient, name string, reqArgs []interface{}, optArgs ...interface{}) (*ole.Variant, error) {
	id, err := dispID(c, name)
	if err != nil {
		return nil, err
	}
	return c.Call(id, reqArgs, optArgs...)
}

func propGetByName(c *ole.OleClient, name string, reqArgs ...interface{}) (*ole.Variant, error) {
	id, err := dispID(c, name)
	if err != nil {
		return nil, err
	}
	return c.PropGet(id, reqArgs)
}

// mirrorOneCoreTokens copies each OneCore token key (values plus the
// Attributes subtree) from HKLM\Speech_OneCore into HKCU\Speech\Tokens
// so classic SAPI enumerates them. Idempotent and best-effort: an
// already-mirrored token or a failed copy is skipped silently — worst
// case the natural voices stay hidden, exactly the status quo.
func mirrorOneCoreTokens() error {
	src, err := registry.OpenKey(registry.LOCAL_MACHINE, oneCoreVoiceRegPath, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return err
	}
	defer src.Close()
	names, err := src.ReadSubKeyNames(-1)
	if err != nil {
		return err
	}
	dstRoot, _, derr := registry.CreateKey(registry.CURRENT_USER, mirrorRootPath, registry.CREATE_SUB_KEY)
	if derr != nil {
		return derr
	}
	defer dstRoot.Close()
	for _, name := range names {
		dst, _, err := registry.CreateKey(dstRoot, name, registry.CREATE_SUB_KEY)
		if err != nil {
			continue
		}
		dst.Close()
		copyRegistryTree(src, dstRoot, name)
	}
	return nil
}

// copyRegistryTree recursively mirrors srcRoot\name into dstRoot\name.
func copyRegistryTree(srcRoot, dstRoot registry.Key, name string) {
	src, err := registry.OpenKey(srcRoot, name, registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return
	}
	defer src.Close()
	dst, _, err := registry.CreateKey(dstRoot, name, registry.CREATE_SUB_KEY)
	if err != nil {
		return
	}
	defer dst.Close()
	for _, vn := range valueNames(src) {
		setRegValue(src, dst, vn)
	}
	subNames, err := src.ReadSubKeyNames(-1)
	if err != nil {
		return
	}
	for _, sub := range subNames {
		copyRegistryTree(src, dst, sub)
	}
}

// setRegValue copies one value from src to dst with a typed setter —
// x/sys/windows/registry exposes no generic SetValue. Unsupported types
// are skipped: OneCore voice tokens only carry strings, dwords and the
// occasional binary blob.
func setRegValue(src, dst registry.Key, name string) {
	size, typ, err := src.GetValue(name, nil)
	if err != nil && err != syscall.ERROR_MORE_DATA {
		return
	}
	data := make([]byte, size)
	if _, _, err := src.GetValue(name, data); err != nil && err != syscall.ERROR_INSUFFICIENT_BUFFER {
		return
	}
	switch typ {
	case registry.SZ:
		_ = dst.SetStringValue(name, utf16ToString(data))
	case registry.EXPAND_SZ:
		_ = dst.SetExpandStringValue(name, utf16ToString(data))
	case registry.BINARY:
		_ = dst.SetBinaryValue(name, data)
	case registry.DWORD:
		if len(data) == 4 {
			_ = dst.SetDWordValue(name, binary.LittleEndian.Uint32(data))
		}
	case registry.QWORD:
		if len(data) == 8 {
			_ = dst.SetQWordValue(name, binary.LittleEndian.Uint64(data))
		}
	case registry.MULTI_SZ:
		if strs, _, err := src.GetStringsValue(name); err == nil {
			_ = dst.SetStringsValue(name, strs)
		}
	}
}

// utf16ToString decodes a NUL-terminated UTF-16LE registry byte blob.
func utf16ToString(data []byte) string {
	u16 := make([]uint16, len(data)/2)
	for i := range u16 {
		u16[i] = uint16(data[2*i]) | uint16(data[2*i+1])<<8
	}
	return syscall.UTF16ToString(u16)
}

func valueNames(key registry.Key) []string {
	names, err := key.ReadValueNames(-1)
	if err != nil {
		return nil
	}
	return names
}

// mirroredOneCoreNames reports the OneCore token tail names that exist
// in the HKLM pool, used to tag mirrored tokens as natural voices.
func mirroredOneCoreNames() map[string]bool {
	src, err := registry.OpenKey(registry.LOCAL_MACHINE, oneCoreVoiceRegPath, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer src.Close()
	names, err := src.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// oneCoreVoices lists only the mirrored OneCore natural voices (the
// neural pool engine=natural prefers); nil when the pool is missing.
func oneCoreVoices() []Voice {
	_ = mirrorOneCoreTokens()
	voices, err := desktopVoices()
	if err != nil {
		return nil
	}
	oneCore := mirroredOneCoreNames()
	out := make([]Voice, 0, len(voices))
	for _, v := range voices {
		tail := strings.TrimPrefix(v.VoiceID, mirrorIDPrefix)
		if strings.HasPrefix(v.VoiceID, mirrorIDPrefix) && oneCore[tail] {
			out = append(out, v)
		}
	}
	return out
}

// Voices enumerates the classic SAPI tokens (which now include the
// mirrored OneCore natural pool) with the natural voices first
// (M95-001 when nothing usable).
func (sapiEngine) Voices() ([]Voice, error) {
	_ = mirrorOneCoreTokens()
	voices, err := desktopVoices()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	oneCore := mirroredOneCoreNames()
	tagged := make([]Voice, 0, len(voices))
	naturals := make([]Voice, 0, len(voices))
	for _, v := range voices {
		tail := strings.TrimPrefix(v.VoiceID, mirrorIDPrefix)
		if strings.HasPrefix(v.VoiceID, mirrorIDPrefix) && oneCore[tail] {
			v.DisplayName = strings.TrimSpace(strings.TrimSuffix(v.DisplayName, "（自然语音）")) + "（自然语音）"
			naturals = append(naturals, v)
			continue
		}
		tagged = append(tagged, v)
	}
	voices = append(naturals, tagged...)
	if len(voices) == 0 {
		return nil, fmt.Errorf("%w: no voices enumerated", ErrEngineUnavailable)
	}
	return voices, nil
}

// NaturalVoices lists only mirrored OneCore neural voices (engine=natural).
func (sapiEngine) NaturalVoices() ([]Voice, error) {
	voices := oneCoreVoices()
	if len(voices) == 0 {
		return nil, fmt.Errorf("%w: no natural voices enumerated", ErrEngineUnavailable)
	}
	return voices, nil
}

// desktopVoices enumerates the voices classic SAPI exposes (the
// HKLM Speech\Tokens tree: Desktop 11.0 voices etc.).
func desktopVoices() ([]Voice, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	uninit, err := comInit()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	defer uninit()

	voice, err := createDispatch(progIDSpVoice)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	defer voice.Release()

	tokensVar, err := callByName(voice, "GetVoices", nil, "", "")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	defer tokensVar.Clear()
	tokens := &ole.OleClient{IDispatch: tokensVar.IDispatch()}

	countVar, err := propGetByName(tokens, "Count")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	count := int(countVar.Int())
	countVar.Clear()

	voices := make([]Voice, 0, count)
	for i := 0; i < count; i++ {
		tokVar, err := callByName(tokens, "Item", []interface{}{i})
		if err != nil {
			continue
		}
		v := tokenAttributes(&ole.OleClient{IDispatch: tokVar.IDispatch()})
		tokVar.Clear()
		if v.VoiceID != "" {
			voices = append(voices, v)
		}
	}
	return voices, nil
}

func tokenAttributes(tok *ole.OleClient) Voice {
	var v Voice
	if idVar, err := propGetByName(tok, "Id"); err == nil {
		v.VoiceID = idVar.String()
		idVar.Clear()
	}
	if nameVar, err := callByName(tok, "GetDescription", nil, -1); err == nil {
		v.DisplayName = nameVar.String()
		nameVar.Clear()
	}
	if gVar, err := callByName(tok, "GetAttribute", []interface{}{"Gender"}); err == nil {
		switch strings.TrimSpace(gVar.String()) {
		case "1":
			v.Gender = "male"
		case "2":
			v.Gender = "female"
		default:
			v.Gender = "neutral"
		}
		gVar.Clear()
	}
	if lVar, err := callByName(tok, "GetAttribute", []interface{}{"Language"}); err == nil {
		v.Lang = normalizeLang(strings.TrimSpace(lVar.String()))
		lVar.Clear()
	}
	return v
}

// findTokenByID walks the voice collection for an exact Id match.
func findTokenByID(voice *ole.OleClient, voiceID string) (*ole.Variant, error) {
	tokensVar, err := callByName(voice, "GetVoices", nil, "", "")
	if err != nil {
		return nil, err
	}
	tokens := &ole.OleClient{IDispatch: tokensVar.IDispatch()}
	countVar, cerr := propGetByName(tokens, "Count")
	if cerr != nil {
		tokensVar.Clear()
		return nil, cerr
	}
	count := int(countVar.Int())
	countVar.Clear()

	for i := 0; i < count; i++ {
		tokVar, err := callByName(tokens, "Item", []interface{}{i})
		if err != nil {
			continue
		}
		idVar, ierr := propGetByName(&ole.OleClient{IDispatch: tokVar.IDispatch()}, "Id")
		if ierr != nil {
			tokVar.Clear()
			continue
		}
		id := idVar.String()
		idVar.Clear()
		if id == voiceID {
			tokensVar.Clear()
			return tokVar, nil
		}
		tokVar.Clear()
	}
	tokensVar.Clear()
	return nil, nil
}

// Synthesize dispatches to the long-lived voice worker pool. The worker
// owns the SpVoice COM object on a pinned OS thread, so per-call overhead
// of CoCreateInstance / token enumeration / DISPID resolution is eliminated.
func (sapiEngine) Synthesize(in SynthesizeInput) (SynthesizeResult, bool, error) {
	w := getWorker()
	respCh := make(chan voiceResp, 1)
	w.reqCh <- voiceReq{input: in, respCh: respCh}
	resp := <-respCh
	return resp.result, resp.fallback, resp.err
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
