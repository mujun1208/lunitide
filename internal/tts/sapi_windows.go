//go:build windows

// sapi_windows.go encapsulates the Windows SAPI SpVoice/SpFileStream COM
// surface (M9.5 T-9.5.1.1): offline in-process WAV synthesis at
// 16kHz mono 16-bit, voice enumeration with id/name/gender/lang, and
// rate [-10,10] / volume [0,100] passthrough. Every COM object is
// created and released within one call; no state crosses calls, so the
// single-flight Service mutex plus a locked OS thread per call is
// sufficient apartment safety.
package tts

import (
	"encoding/base64"
	"fmt"
	"math"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"

	"github.com/zzl/go-com/com"
	"github.com/zzl/go-com/ole"
	"github.com/zzl/go-win32api/v2/win32"
)

const (
	progIDSpVoice        = "SAPI.SpVoice"
	progIDSpFileStream   = "SAPI.SpFileStream"
	progIDSpObjectToken  = "SAPI.SpObjectToken"
	oneCoreVoiceRootPath = `HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Speech_OneCore\Voices\Tokens\`
	oneCoreVoiceRegPath  = `SOFTWARE\Microsoft\Speech_OneCore\Voices\Tokens`

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

// OneCore voices are the natural neural pool shipped with Windows 10/11
// (Huihui/Kangkang/Yaoyao…). They are hidden from classic SAPI
// enumeration and can only be driven through ISpObjectToken, whose
// minimal IDispatch exposes just SetId/Id — so SetId and SetVoice go
// through raw vtable calls (apartment-safe: same locked STA thread as
// the IDispatch calls around them).
//
// Vtable indices (from sapi.idl): ISpObjectToken = IUnknown(3) +
// ISpDataKey(12) + SetId → 15; ISpVoice = IUnknown(3) +
// ISpNotifySource(7) + ISpEventSource(3) + SetVoice → 18.
// IID_ISpObjectToken {14056589-E16C-11D2-BB90-00C04F8EE6C0}
var iidISpObjectToken = syscall.GUID{
	Data1: 0x14056589, Data2: 0xE16C, Data3: 0x11D2,
	Data4: [8]byte{0xBB, 0x90, 0x00, 0xC0, 0x4F, 0x8E, 0xE6, 0xC0},
}

// IID_ISpVoice {6C44DF74-72B9-4992-A1EC-EF996E0422D4}
var iidISpVoice = syscall.GUID{
	Data1: 0x6C44DF74, Data2: 0x72B9, Data3: 0x4992,
	Data4: [8]byte{0xA1, 0xEC, 0xEF, 0x99, 0x6E, 0x04, 0x22, 0xD4},
}

const (
	vtblSetId     = 15
	vtblSetVoice  = 18
	vtblRelease   = 2
	vtblQueryFace = 0
)

func vtblEntry(self unsafe.Pointer, idx int) uintptr {
	vtbl := *(**[64]uintptr)(self)
	return vtbl[idx]
}

func queryInterface(self unsafe.Pointer, iid *syscall.GUID) (unsafe.Pointer, int32) {
	var out unsafe.Pointer
	hr, _, _ := syscall.SyscallN(vtblEntry(self, vtblQueryFace),
		uintptr(self), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	return out, int32(hr)
}

func comRelease(self unsafe.Pointer) {
	_, _, _ = syscall.SyscallN(vtblEntry(self, vtblRelease), uintptr(self))
}

// isOneCoreVoice reports whether voiceId is an OneCore registry token path.
func isOneCoreVoice(voiceID string) bool {
	return strings.HasPrefix(voiceID, oneCoreVoiceRootPath)
}

// sapiEngine is a stateless Engine; each call builds its own COM objects.
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

// oneCoreVoices reads the natural neural voice pool from the OneCore
// token registry. VoiceID doubles as the SpObjectToken SetId path used
// at synthesis time, so enumeration and synthesis share one identity.
func oneCoreVoices() []Voice {
	root, err := registry.OpenKey(registry.LOCAL_MACHINE, oneCoreVoiceRegPath,
		registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer root.Close()
	names, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	voices := make([]Voice, 0, len(names))
	for _, name := range names {
		attr, err := registry.OpenKey(root, name+`\Attributes`, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		display, _, verr := attr.GetStringValue("Name")
		gender, _, _ := attr.GetStringValue("Gender")
		lang, _, _ := attr.GetStringValue("Language")
		attr.Close()
		if verr != nil || strings.TrimSpace(display) == "" {
			continue
		}
		v := Voice{
			VoiceID:     oneCoreVoiceRootPath + name,
			DisplayName: strings.TrimSpace(display) + "（自然语音）",
			Lang:        normalizeLang(strings.TrimSpace(lang)),
		}
		switch strings.ToLower(strings.TrimSpace(gender)) {
		case "female":
			v.Gender = "female"
		case "male":
			v.Gender = "male"
		default:
			v.Gender = "neutral"
		}
		voices = append(voices, v)
	}
	return voices
}

// setOneCoreVoice binds voice to the OneCore token identified by its
// registry path via raw ISpObjectToken/ISpVoice vtable calls.
func setOneCoreVoice(voice *ole.OleClient, tokenPath string) error {
	token, err := createDispatch(progIDSpObjectToken)
	if err != nil {
		return err
	}
	defer token.Release()

	tokIface, hr := queryInterface(unsafe.Pointer(token.IDispatch), &iidISpObjectToken)
	if win32.FAILED(hr) {
		return fmt.Errorf("QI ISpObjectToken: 0x%08X", uint32(hr))
	}
	defer comRelease(tokIface)

	pathPtr, err := syscall.UTF16PtrFromString(tokenPath)
	if err != nil {
		return err
	}
	// SetId(categoryId=NULL, tokenId, fCreateIfNotExist=FALSE).
	rc, _, _ := syscall.SyscallN(vtblEntry(tokIface, vtblSetId),
		uintptr(tokIface), 0, uintptr(unsafe.Pointer(pathPtr)), 0)
	if win32.FAILED(int32(rc)) {
		return fmt.Errorf("SetId: 0x%08X", uint32(rc))
	}

	voiceIface, hr2 := queryInterface(unsafe.Pointer(voice.IDispatch), &iidISpVoice)
	if win32.FAILED(hr2) {
		return fmt.Errorf("QI ISpVoice: 0x%08X", uint32(hr2))
	}
	defer comRelease(voiceIface)

	rc, _, _ = syscall.SyscallN(vtblEntry(voiceIface, vtblSetVoice),
		uintptr(voiceIface), uintptr(tokIface))
	if win32.FAILED(int32(rc)) {
		return fmt.Errorf("SetVoice: 0x%08X", uint32(rc))
	}
	return nil
}

// Voices enumerates the OneCore natural pool plus the classic desktop
// SAPI voices (M95-001 when nothing usable).
func (sapiEngine) Voices() ([]Voice, error) {
	voices := oneCoreVoices()
	desktop, err := desktopVoices()
	if err == nil {
		voices = append(voices, desktop...)
	}
	if len(voices) == 0 {
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
		}
		return nil, fmt.Errorf("%w: no voices enumerated", ErrEngineUnavailable)
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

// Synthesize renders one segment to a base64 WAV via SpFileStream.
// The returned bool reports that the requested voice was missing and
// the default voice was used (M95-004 notice semantics).
func (sapiEngine) Synthesize(in SynthesizeInput) (SynthesizeResult, bool, error) {
	var zero SynthesizeResult
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	uninit, err := comInit()
	if err != nil {
		return zero, false, fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	defer uninit()

	voice, err := createDispatch(progIDSpVoice)
	if err != nil {
		return zero, false, fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	defer voice.Release()

	voiceFallback := false
	switch {
	case isOneCoreVoice(in.VoiceID):
		// OneCore natural voice: invisible to classic GetVoices, so it is
		// bound through the ISpObjectToken vtable bridge.
		if oerr := setOneCoreVoice(voice, in.VoiceID); oerr != nil {
			voiceFallback = true // M95-004: fall back to the default voice
		}
	case in.VoiceID != "":
		tokVar, terr := findTokenByID(voice, in.VoiceID)
		if terr != nil || tokVar == nil {
			voiceFallback = true // M95-004: fall back to the default voice
		} else {
			defer tokVar.Clear()
			if id, derr := dispID(voice, "Voice"); derr == nil {
				_ = voice.PropPutRef(id, []interface{}{tokVar.IDispatch()})
			}
		}
	default:
		// "默认音色" keeps the natural engines natural: prefer an OneCore
		// voice when the pool exists, otherwise the SAPI default stays.
		if in.Engine == EngineNatural || in.Engine == EngineEdge {
			if ones := oneCoreVoices(); len(ones) > 0 {
				_ = setOneCoreVoice(voice, ones[0].VoiceID)
			}
		}
	}

	if id, derr := dispID(voice, "Rate"); derr == nil {
		_ = voice.PropPut(id, []interface{}{int32(clamp(in.Rate, -10, 10))})
	}
	if id, derr := dispID(voice, "Volume"); derr == nil {
		_ = voice.PropPut(id, []interface{}{int32(clamp(in.Volume, 0, 100))})
	}

	tmp, err := os.CreateTemp("", "lunitide-tts-*.wav")
	if err != nil {
		return zero, voiceFallback, fmt.Errorf("%w: %v", ErrSynthesisFailed, err)
	}
	tmpPath := tmp.Name()
	tmp.Close()
	os.Remove(tmpPath)
	defer os.Remove(tmpPath)

	stream, err := createDispatch(progIDSpFileStream)
	if err != nil {
		return zero, voiceFallback, fmt.Errorf("%w: %v", ErrSynthesisFailed, err)
	}
	defer stream.Release()

	// Stream format: 16kHz mono 16-bit WAV.
	if fmtVar, ferr := propGetByName(stream, "Format"); ferr == nil {
		if fmtObj := fmtVar.IDispatch(); fmtObj != nil {
			format := &ole.OleClient{IDispatch: fmtObj}
			if id, derr := dispID(format, "Type"); derr == nil {
				_ = format.PropPut(id, []interface{}{int32(saft16k16BitMono)})
			}
		}
		fmtVar.Clear()
	}

	closeStream := func() {
		if id, derr := dispID(stream, "Close"); derr == nil {
			_, _ = stream.Call(id, nil)
		}
	}

	openID, oerr := dispID(stream, "Open")
	if oerr != nil {
		return zero, voiceFallback, fmt.Errorf("%w: %v", ErrSynthesisFailed, oerr)
	}
	if _, err := stream.Call(openID, []interface{}{tmpPath, int32(ssfmCreateForOverwrite), false}); err != nil {
		return zero, voiceFallback, fmt.Errorf("%w: %v", ErrSynthesisFailed, err)
	}

	if id, derr := dispID(voice, "AudioOutputStream"); derr == nil {
		if perr := voice.PropPutRef(id, []interface{}{stream.IDispatch}); perr != nil {
			closeStream()
			return zero, voiceFallback, fmt.Errorf("%w: %v", ErrSynthesisFailed, perr)
		}
	}

	speakID, serr := dispID(voice, "Speak")
	if serr != nil {
		closeStream()
		return zero, voiceFallback, fmt.Errorf("%w: %v", ErrSynthesisFailed, serr)
	}
	if _, err := voice.Call(speakID, []interface{}{in.Text, int32(svsfDefault)}); err != nil {
		closeStream()
		return zero, voiceFallback, fmt.Errorf("%w: %v", ErrSynthesisFailed, err)
	}
	closeStream()

	data, err := os.ReadFile(tmpPath)
	if err != nil || len(data) <= wavHeaderBytes || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return zero, voiceFallback, fmt.Errorf("%w: invalid wav output", ErrSynthesisFailed)
	}
	seconds := float64(len(data)-wavHeaderBytes) / float64(wavSamplesPerSecond*wavBytesPerSample)
	return SynthesizeResult{
		WavBase64:    base64.StdEncoding.EncodeToString(data),
		DurationHint: math.Round(seconds*100) / 100,
	}, voiceFallback, nil
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
