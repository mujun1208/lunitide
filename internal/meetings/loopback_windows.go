//go:build windows

package meetings

import (
	"errors"
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
)

var (
	clsidMMDeviceEnumerator = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioClient         = windows.GUID{Data1: 0x1CB9AD4C, Data2: 0xDBFA, Data3: 0x4C32, Data4: [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioCaptureClient  = windows.GUID{Data1: 0xC8ADBD64, Data2: 0xE71E, Data3: 0x48A0, Data4: [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
)

const (
	eRender                    = 0
	eConsole                   = 0
	clsctxAll                  = 0x17
	coinitMultithreaded        = 0
	audclntSharemodeShared     = 0
	audclntStreamflagsLoopback = 0x00020000
	audclntBufferflagsSilent   = 0x2
	waveFormatIEEEFloat        = 3
	waveFormatExtensible       = 0xFFFE
	refTime1s                  = 10_000_000
	rpcEChangedMode            = uint32(0x80010106)
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
	stop   chan struct{}
	done   chan struct{}
	chunks chan []byte
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
	defer close(w.done)
	client, capture, format, comOwned, err := openWASAPICapture()
	if err != nil {
		ready <- err
		return
	}
	ready <- nil
	defer func() {
		_ = comCall(client, 11)
		comRelease(capture)
		comRelease(client)
		if comOwned {
			_, _, _ = procCoUninitializeL.Call()
		}
	}()
	for {
		select {
		case <-w.stop:
			return
		default:
		}
		pcm, readErr := captureLoopbackPacket(capture, format)
		if len(pcm) > 0 {
			select {
			case w.chunks <- pcm:
			default:
			}
		}
		if readErr != nil {
			return
		}
		if len(pcm) == 0 {
			time.Sleep(8 * time.Millisecond)
		}
	}
}

func (w *wasapiPump) ReadPCM() ([]byte, error) {
	select {
	case <-w.stop:
		return nil, errors.New("loopback closed")
	case pcm := <-w.chunks:
		return pcm, nil
	case <-time.After(8 * time.Millisecond):
		return nil, nil
	}
}

func (w *wasapiPump) Close() error {
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
	<-w.done
	return nil
}

func openWASAPICapture() (client, capture uintptr, format pcmFormat, comOwned bool, err error) {
	hr, _, _ := procCoInitializeExL.Call(0, coinitMultithreaded)
	code := uint32(hr)
	comOwned = code == 0
	if failedHR(hr) && code != rpcEChangedMode {
		return 0, 0, pcmFormat{}, false, errors.New("com init failed")
	}
	fail := func(e error) (uintptr, uintptr, pcmFormat, bool, error) {
		if comOwned {
			_, _, _ = procCoUninitializeL.Call()
		}
		return 0, 0, pcmFormat{}, false, e
	}

	var enumerator uintptr
	hr, _, _ = procCoCreateInstanceL.Call(
		uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)),
		0,
		clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)),
		uintptr(unsafe.Pointer(&enumerator)),
	)
	if failedHR(hr) || enumerator == 0 {
		return fail(errors.New("mmdevice enumerator unavailable"))
	}
	defer comRelease(enumerator)

	var device uintptr
	if hr = comCall(enumerator, 4, uintptr(eRender), uintptr(eConsole), uintptr(unsafe.Pointer(&device))); failedHR(hr) || device == 0 {
		return fail(errors.New("default render device unavailable"))
	}
	defer comRelease(device)

	if hr = comCall(device, 3, uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&client))); failedHR(hr) || client == 0 {
		return fail(errors.New("audio client unavailable"))
	}

	var mixFmt *waveFormatEx
	if hr = comCall(client, 8, uintptr(unsafe.Pointer(&mixFmt))); failedHR(hr) || mixFmt == nil {
		comRelease(client)
		return fail(errors.New("mix format unavailable"))
	}
	format = pcmFormatFromWave(mixFmt)

	hr = comCall(client, 3,
		uintptr(audclntSharemodeShared),
		uintptr(audclntStreamflagsLoopback),
		uintptr(uint64(refTime1s)),
		0,
		uintptr(unsafe.Pointer(mixFmt)),
		0,
	)
	_, _, _ = procCoTaskMemFreeL.Call(uintptr(unsafe.Pointer(mixFmt)))
	if failedHR(hr) {
		comRelease(client)
		return fail(errors.New("loopback initialize failed"))
	}

	if hr = comCall(client, 14, uintptr(unsafe.Pointer(&iidIAudioCaptureClient)), uintptr(unsafe.Pointer(&capture))); failedHR(hr) || capture == 0 {
		comRelease(client)
		return fail(errors.New("capture client unavailable"))
	}
	if hr = comCall(client, 10); failedHR(hr) {
		comRelease(capture)
		comRelease(client)
		return fail(errors.New("loopback start failed"))
	}
	return client, capture, format, comOwned, nil
}

func captureLoopbackPacket(capture uintptr, format pcmFormat) ([]byte, error) {
	var packet uint32
	if hr := comCall(capture, 5, uintptr(unsafe.Pointer(&packet))); failedHR(hr) || packet == 0 {
		return nil, nil
	}
	var (
		data   uintptr
		frames uint32
		flags  uint32
		devPos uint64
		qpcPos uint64
	)
	hr := comCall(capture, 3,
		uintptr(unsafe.Pointer(&data)),
		uintptr(unsafe.Pointer(&frames)),
		uintptr(unsafe.Pointer(&flags)),
		uintptr(unsafe.Pointer(&devPos)),
		uintptr(unsafe.Pointer(&qpcPos)),
	)
	if failedHR(hr) || frames == 0 {
		return nil, nil
	}
	n := int(frames) * format.blockAlign
	raw := make([]byte, n)
	if flags&audclntBufferflagsSilent == 0 && data != 0 && n > 0 {
		copy(raw, unsafe.Slice((*byte)(ptrFromUintptr(data)), n))
	}
	_ = comCall(capture, 4, uintptr(frames))
	return convertTo16kMonoS16(raw, format), nil
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
