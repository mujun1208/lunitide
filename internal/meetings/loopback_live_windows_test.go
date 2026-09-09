//go:build windows

package meetings

import (
	"encoding/binary"
	"math"
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	playSoundMemory    = 0x0004
	playSoundNoDefault = 0x0002
)

var playSoundW = windows.NewLazySystemDLL("winmm.dll").NewProc("PlaySoundW")

func TestWASAPILoopbackCapturesMutedOutput(t *testing.T) {
	if os.Getenv("LUNITIDE_LOOPBACK_LIVE") != "1" {
		t.Skip("opt-in hardware probe")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := procCoInitializeExL.Call(0, coinitMultithreaded)
	if failedHR(hr) {
		t.Fatalf("COM init: %#x", hr)
	}
	defer procCoUninitializeL.Call()
	var enumerator uintptr
	hr, _, _ = procCoCreateInstanceL.Call(uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll, uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enumerator)))
	if failedHR(hr) {
		t.Fatalf("enumerator: %#x", hr)
	}
	defer comRelease(enumerator)
	var device uintptr
	if hr = comCall(enumerator, 4, eRender, eConsole, uintptr(unsafe.Pointer(&device))); failedHR(hr) {
		t.Fatalf("device: %#x", hr)
	}
	defer comRelease(device)
	iid := windows.GUID{Data1: 0x5CDF2C82, Data2: 0x841E, Data3: 0x4546, Data4: [8]byte{0x97, 0x22, 0x0C, 0xF7, 0x40, 0x78, 0x22, 0x9A}}
	var volume uintptr
	if hr = comCall(device, 3, uintptr(unsafe.Pointer(&iid)), clsctxAll, 0, uintptr(unsafe.Pointer(&volume))); failedHR(hr) {
		t.Fatalf("endpoint volume: %#x", hr)
	}
	defer comRelease(volume)
	var original int32
	if hr = comCall(volume, 15, uintptr(unsafe.Pointer(&original))); failedHR(hr) {
		t.Fatalf("get mute: %#x", hr)
	}
	if hr = comCall(volume, 14, 1, 0); failedHR(hr) {
		t.Fatalf("mute: %#x", hr)
	}
	defer func() {
		if hr := comCall(volume, 14, uintptr(original), 0); failedHR(hr) {
			t.Errorf("restore mute: %#x", hr)
		}
	}()
	t.Logf("original_muted=%t probe_muted=true", original != 0)
	TestWASAPILoopbackCapturesDefaultOutput(t)
}

// TestWASAPILoopbackCapturesDefaultOutput is an opt-in hardware probe. It
// plays a quiet generated tone through the default output and verifies that
// the production WASAPI loopback path returns non-silent 16 kHz mono PCM.
func TestWASAPILoopbackCapturesDefaultOutput(t *testing.T) {
	if os.Getenv("LUNITIDE_LOOPBACK_LIVE") != "1" {
		t.Skip("set LUNITIDE_LOOPBACK_LIVE=1 to exercise the default audio device")
	}

	source, err := openPlatformLoopback()
	if err != nil {
		t.Fatalf("open loopback: %v", err)
	}
	defer source.Close()

	wav := generatedToneWAV(16_000, 700*time.Millisecond, 440)
	played := make(chan bool, 1)
	go func() {
		result, _, _ := playSoundW.Call(
			uintptr(unsafe.Pointer(&wav[0])),
			0,
			playSoundMemory|playSoundNoDefault,
		)
		played <- result != 0
	}()

	deadline := time.Now().Add(4 * time.Second)
	peak := int16(0)
	bytesSeen := 0
	for time.Now().Before(deadline) {
		pcm, readErr := source.ReadPCM()
		if readErr != nil {
			t.Fatalf("read loopback: %v", readErr)
		}
		bytesSeen += len(pcm)
		if got := peakPCM16(pcm); got > peak {
			peak = got
		}
		if peak >= 200 {
			break
		}
	}
	if ok := <-played; !ok {
		t.Fatal("default output device rejected the generated tone")
	}
	if peak < 200 {
		t.Fatalf("loopback stayed silent: bytes=%d peak=%d", bytesSeen, peak)
	}
	t.Logf("loopback captured default output: bytes=%d peak=%d", bytesSeen, peak)
}

func generatedToneWAV(sampleRate int, duration time.Duration, frequency float64) []byte {
	sampleCount := int(float64(sampleRate) * duration.Seconds())
	dataBytes := sampleCount * 2
	wav := make([]byte, 44+dataBytes)
	copy(wav[0:4], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(36+dataBytes))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1)
	binary.LittleEndian.PutUint16(wav[22:24], 1)
	binary.LittleEndian.PutUint32(wav[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(wav[28:32], uint32(sampleRate*2))
	binary.LittleEndian.PutUint16(wav[32:34], 2)
	binary.LittleEndian.PutUint16(wav[34:36], 16)
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:44], uint32(dataBytes))

	rampSamples := sampleRate / 50
	for i := 0; i < sampleCount; i++ {
		envelope := 1.0
		if i < rampSamples {
			envelope = float64(i) / float64(rampSamples)
		} else if remaining := sampleCount - i - 1; remaining < rampSamples {
			envelope = float64(remaining) / float64(rampSamples)
		}
		sample := int16(1200 * envelope * math.Sin(2*math.Pi*frequency*float64(i)/float64(sampleRate)))
		binary.LittleEndian.PutUint16(wav[44+i*2:], uint16(sample))
	}
	return wav
}

func peakPCM16(pcm []byte) int16 {
	peak := int16(0)
	for i := 0; i+1 < len(pcm); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(pcm[i:]))
		if sample < 0 {
			if sample == -32768 {
				return 32767
			}
			sample = -sample
		}
		if sample > peak {
			peak = sample
		}
	}
	return peak
}
