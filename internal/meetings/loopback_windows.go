//go:build windows

package meetings

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32Loop             = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstanceL = ole32Loop.NewProc("CoCreateInstance")
	procCoInitializeExL   = ole32Loop.NewProc("CoInitializeEx")
	procCoUninitializeL   = ole32Loop.NewProc("CoUninitialize")
	procCoTaskMemFreeL    = ole32Loop.NewProc("CoTaskMemFree")
	loopbackKernel32      = windows.NewLazySystemDLL("kernel32.dll")
	procLoopbackQPC       = loopbackKernel32.NewProc("QueryPerformanceCounter")
	procLoopbackQPF       = loopbackKernel32.NewProc("QueryPerformanceFrequency")
)

var (
	clsidMMDeviceEnumerator = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioClient         = windows.GUID{Data1: 0x1CB9AD4C, Data2: 0xDBFA, Data3: 0x4C32, Data4: [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioCaptureClient  = windows.GUID{Data1: 0xC8ADBD64, Data2: 0xE71E, Data3: 0x48A0, Data4: [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
)

const (
	eRender                          = 0
	eConsole                         = 0
	deviceStateActive                = 1
	clsctxAll                        = 0x17
	coinitMultithreaded              = 0
	audclntSharemodeShared           = 0
	audclntStreamflagsLoopback       = 0x00020000
	audclntBufferflagsSilent         = 0x2
	audclntBufferflagsTimestampError = 0x4
	waveFormatIEEEFloat              = 3
	waveFormatExtensible             = 0xFFFE
	refTime1s                        = 10_000_000
	rpcEChangedMode                  = uint32(0x80010106)
)

func openPlatformLoopback() (loopbackSource, error) {
	return startWASAPILoopback()
}

type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	ExtraSize      uint16
}

type wasapiPump struct {
	stop      chan struct{}
	done      chan struct{}
	chunks    chan []byte
	closeOnce sync.Once
	active    atomic.Bool
}

func startWASAPILoopback() (*wasapiPump, error) {
	pump := &wasapiPump{
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
		chunks: make(chan []byte, 8),
	}
	ready := make(chan error, 1)
	go pump.loop(ready)
	if err := <-ready; err != nil {
		<-pump.done
		return nil, err
	}
	return pump, nil
}

func (w *wasapiPump) loop(ready chan<- error) {
	// COM initialization, capture and release must use the same OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(w.done)
	enumerator, cleanup, err := openLoopbackEnumerator()
	if err != nil {
		ready <- err
		return
	}
	defer cleanup()
	clock, err := newLoopbackClock()
	if err != nil {
		ready <- err
		return
	}
	endpoints := newLoopbackEndpoints(clock(), func() ([]string, error) {
		return activeRenderEndpointIDs(enumerator)
	}, func(id string) (endpointCapture, error) {
		return openWASAPIEndpoint(enumerator, id, clock)
	})
	defer endpoints.close()
	if err = endpoints.refresh(time.Now()); err != nil {
		ready <- err
		return
	}
	w.active.Store(len(endpoints.streams) > 0)
	defer w.active.Store(false)
	ready <- nil
	ticker := time.NewTicker(8 * time.Millisecond)
	defer ticker.Stop()
	nextRefresh := time.Now().Add(endpointRefresh)
	for {
		select {
		case <-w.stop:
			return
		case now := <-ticker.C:
			if !now.Before(nextRefresh) {
				_ = endpoints.refresh(now)
				nextRefresh = now.Add(endpointRefresh)
			}
			endpoints.read(now)
			w.active.Store(len(endpoints.streams) > 0)
		}
		pcm := endpoints.mixer.render(clock())
		if len(pcm) > 0 {
			select {
			case w.chunks <- pcm:
			default:
			}
		}
	}
}

func (w *wasapiPump) Active() bool { return w.active.Load() }

func (w *wasapiPump) ReadPCM() ([]byte, error) {
	select {
	case <-w.stop:
		return nil, errors.New("loopback closed")
	case pcm := <-w.chunks:
		return pcm, nil
	case <-w.done:
		return nil, errLoopbackUnavailable
	case <-time.After(8 * time.Millisecond):
		return nil, nil
	}
}

func (w *wasapiPump) Close() error {
	w.closeOnce.Do(func() { close(w.stop) })
	<-w.done
	return nil
}

func openLoopbackEnumerator() (uintptr, func(), error) {
	hr, _, _ := procCoInitializeExL.Call(0, coinitMultithreaded)
	code := uint32(hr)
	comOwned := code == 0 || code == 1 // S_OK and S_FALSE both require CoUninitialize.
	if failedHR(hr) && code != rpcEChangedMode {
		return 0, nil, errors.New("com init failed")
	}
	var enumerator uintptr
	cleanup := func() {
		comRelease(enumerator)
		if comOwned {
			_, _, _ = procCoUninitializeL.Call()
		}
	}
	hr, _, _ = procCoCreateInstanceL.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)),
		0,
		clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)),
		uintptr(unsafe.Pointer(&enumerator)),
	)
	if failedHR(hr) || enumerator == 0 {
		cleanup()
		return 0, nil, errors.New("mmdevice enumerator unavailable")
	}
	return enumerator, cleanup, nil
}

func newLoopbackClock() (func() int64, error) {
	var frequency int64
	if ok, _, _ := procLoopbackQPF.Call(uintptr(unsafe.Pointer(&frequency))); ok == 0 || frequency <= 0 {
		return nil, errors.New("performance clock unavailable")
	}
	return func() int64 {
		var ticks int64
		procLoopbackQPC.Call(uintptr(unsafe.Pointer(&ticks)))
		return ticks/frequency*loopbackClockRate + ticks%frequency*loopbackClockRate/frequency
	}, nil
}

func endpointID(device uintptr) (string, error) {
	var id *uint16
	if hr := comCall(device, 5, uintptr(unsafe.Pointer(&id))); failedHR(hr) || id == nil {
		return "", errors.New("endpoint ID unavailable")
	}
	defer procCoTaskMemFreeL.Call(uintptr(unsafe.Pointer(id)))
	return windows.UTF16PtrToString(id), nil
}

func activeRenderEndpointIDs(enumerator uintptr) ([]string, error) {
	var collection uintptr
	if hr := comCall(enumerator, 3, eRender, deviceStateActive, uintptr(unsafe.Pointer(&collection))); failedHR(hr) || collection == 0 {
		return nil, errors.New("render endpoints unavailable")
	}
	defer comRelease(collection)
	var count uint32
	if hr := comCall(collection, 3, uintptr(unsafe.Pointer(&count))); failedHR(hr) {
		return nil, errors.New("render endpoint count unavailable")
	}
	ids := make([]string, 0, count)
	for i := uint32(0); i < count; i++ {
		var device uintptr
		if hr := comCall(collection, 4, uintptr(i), uintptr(unsafe.Pointer(&device))); failedHR(hr) || device == 0 {
			return nil, errors.New("render endpoint unavailable")
		}
		id, err := endpointID(device)
		comRelease(device)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

type wasapiEndpoint struct {
	client, capture uintptr
	format          pcmFormat
	clock           func() int64
}

func (e *wasapiEndpoint) close() {
	if e.client != 0 {
		_ = comCall(e.client, 11)
	}
	comRelease(e.capture)
	comRelease(e.client)
	e.client, e.capture = 0, 0
}

func openWASAPIEndpoint(enumerator uintptr, id string, clock func() int64) (*wasapiEndpoint, error) {
	idPtr, err := windows.UTF16PtrFromString(id)
	if err != nil {
		return nil, err
	}

	var device uintptr
	if hr := comCall(enumerator, 5, uintptr(unsafe.Pointer(idPtr)), uintptr(unsafe.Pointer(&device))); failedHR(hr) || device == 0 {
		return nil, errors.New("render device unavailable")
	}
	defer comRelease(device)
	e := &wasapiEndpoint{clock: clock}
	fail := func(err error) (*wasapiEndpoint, error) { e.close(); return nil, err }
	if hr := comCall(device, 3, uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&e.client))); failedHR(hr) || e.client == 0 {
		return fail(errors.New("audio client unavailable"))
	}

	var mixFmt *waveFormatEx
	if hr := comCall(e.client, 8, uintptr(unsafe.Pointer(&mixFmt))); failedHR(hr) || mixFmt == nil {
		return fail(errors.New("mix format unavailable"))
	}
	defer procCoTaskMemFreeL.Call(uintptr(unsafe.Pointer(mixFmt)))
	e.format = pcmFormatFromWave(mixFmt)
	if !validLoopbackFormat(e.format) {
		return fail(errors.New("unsupported loopback format"))
	}

	hr := comCall(e.client, 3,
		uintptr(audclntSharemodeShared),
		uintptr(audclntStreamflagsLoopback),
		uintptr(uint64(refTime1s)),
		0,
		uintptr(unsafe.Pointer(mixFmt)),
		0,
	)
	if failedHR(hr) {
		return fail(errors.New("loopback initialize failed"))
	}

	if hr = comCall(e.client, 14, uintptr(unsafe.Pointer(&iidIAudioCaptureClient)), uintptr(unsafe.Pointer(&e.capture))); failedHR(hr) || e.capture == 0 {
		return fail(errors.New("capture client unavailable"))
	}
	if hr = comCall(e.client, 10); failedHR(hr) {
		return fail(errors.New("loopback start failed"))
	}
	return e, nil
}

func (e *wasapiEndpoint) readPacket() (endpointPacket, error) {
	var packet uint32
	if hr := comCall(e.capture, 5, uintptr(unsafe.Pointer(&packet))); failedHR(hr) {
		return endpointPacket{}, fmt.Errorf("loopback packet unavailable: HRESULT %#x", uint32(hr))
	}
	if packet == 0 {
		return endpointPacket{}, nil
	}
	var (
		data   uintptr
		frames uint32
		flags  uint32
		devPos uint64
		qpcPos uint64
	)
	hr := comCall(e.capture, 3,
		uintptr(unsafe.Pointer(&data)),
		uintptr(unsafe.Pointer(&frames)),
		uintptr(unsafe.Pointer(&flags)),
		uintptr(unsafe.Pointer(&devPos)),
		uintptr(unsafe.Pointer(&qpcPos)),
	)
	if failedHR(hr) {
		return endpointPacket{}, fmt.Errorf("loopback buffer unavailable: HRESULT %#x", uint32(hr))
	}
	if frames == 0 {
		return endpointPacket{}, nil
	}
	if frames > uint32(e.format.rate) {
		_ = comCall(e.capture, 4, uintptr(frames))
		return endpointPacket{}, errors.New("oversized loopback packet")
	}
	n := int(frames) * e.format.blockAlign
	raw := make([]byte, n)
	if flags&audclntBufferflagsSilent == 0 && data != 0 && n > 0 {
		copy(raw, unsafe.Slice((*byte)(ptrFromUintptr(data)), n))
	}
	if hr := comCall(e.capture, 4, uintptr(frames)); failedHR(hr) {
		return endpointPacket{}, fmt.Errorf("loopback release failed: HRESULT %#x", uint32(hr))
	}
	position := int64(qpcPos)
	if flags&audclntBufferflagsTimestampError != 0 || position <= 0 {
		position = e.clock() - int64(frames)*loopbackClockRate/int64(e.format.rate)
	}
	return endpointPacket{raw: raw, format: e.format, position: position}, nil
}

func pcmFormatFromWave(fmt *waveFormatEx) pcmFormat {
	if fmt == nil {
		return pcmFormat{}
	}
	float := fmt.FormatTag == waveFormatIEEEFloat
	if fmt.FormatTag == waveFormatExtensible && fmt.ExtraSize >= 22 {
		sub := *(*windows.GUID)(unsafe.Add(unsafe.Pointer(fmt), 24))
		if sub.Data1 == 3 {
			float = true
		}
	}
	align := int(fmt.BlockAlign)
	if align < 1 {
		width := int(fmt.BitsPerSample) / 8
		if width < 1 {
			width = 4
		}
		align = int(fmt.Channels) * width
	}
	return pcmFormat{
		channels:   int(fmt.Channels),
		rate:       int(fmt.SamplesPerSec),
		bits:       int(fmt.BitsPerSample),
		blockAlign: align,
		float:      float,
	}
}

func failedHR(hr uintptr) bool {
	return int32(uint32(hr)) < 0
}

func ptrFromUintptr(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

func comCall(this uintptr, slot uintptr, args ...uintptr) uintptr {
	if this == 0 {
		return 0x80004003
	}
	vtbl := *(*unsafe.Pointer)(ptrFromUintptr(this))
	fn := *(*uintptr)(unsafe.Add(vtbl, unsafe.Sizeof(uintptr(0))*slot))
	all := make([]uintptr, 0, 1+len(args))
	all = append(all, this)
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(fn, all...)
	return r
}

func comRelease(this uintptr) {
	if this == 0 {
		return
	}
	_ = comCall(this, 2)
}
