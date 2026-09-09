//go:build windows

package meetings

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iidProbeRenderClient   = windows.GUID{Data1: 0xF294ACFC, Data2: 0x3146, Data3: 0x4483, Data4: [8]byte{0xA7, 0xBF, 0xAD, 0xDC, 0xA7, 0xC2, 0x60, 0xE2}}
	iidProbeEndpointVolume = windows.GUID{Data1: 0x5CDF2C82, Data2: 0x841E, Data3: 0x4546, Data4: [8]byte{0x97, 0x22, 0x0C, 0xF7, 0x40, 0x78, 0x22, 0x9A}}
)

type probeVolume struct {
	Muted int32
	Level float32
}
type probeAudioState struct {
	Defaults [2][3]string
	Volumes  map[string]probeVolume
}

func TestWASAPIMultiEndpointGeneratedTonesPreserveConfiguration(t *testing.T) {
	if os.Getenv("LUNITIDE_LOOPBACK_MULTI_LIVE") != "1" {
		t.Skip("opt-in local generated-tone hardware fixture")
	}
	probeMultiEndpointTones(t, false)
}

func TestWASAPIMultiEndpointMutedGeneratedTones(t *testing.T) {
	if os.Getenv("LUNITIDE_LOOPBACK_MULTI_MUTED_LIVE") != "1" {
		t.Skip("opt-in generated-tone fixture with temporary, restored endpoint mute")
	}
	probeMultiEndpointTones(t, true)
}

// Captured PCM stays in memory. No microphone, recognizer, network or audio
// file is used. The muted variant restores each original mute value before
// checking all endpoint settings; neither variant sets volume or defaults.
func probeMultiEndpointTones(t *testing.T, muted bool) {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	enumerator, cleanup, err := openLoopbackEnumerator()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	before := probeAudioConfiguration(t, enumerator)
	defer func() {
		after := probeAudioConfiguration(t, enumerator)
		if !reflect.DeepEqual(before, after) {
			t.Error("audio configuration changed during probe; no settings were overwritten")
		}
		t.Logf("configuration_preserved=%t (render/capture endpoint mute, volume, all default roles)", reflect.DeepEqual(before, after))
	}()
	ids, err := activeRenderEndpointIDs(enumerator)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) == 0 {
		t.Skip("no active render endpoints")
	}
	if muted {
		for _, id := range ids {
			text, err := windows.UTF16PtrFromString(id)
			if err != nil {
				t.Fatal(err)
			}
			var device, volume uintptr
			if hr := comCall(enumerator, 5, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(&device))); failedHR(hr) {
				t.Fatalf("mute device: %#x", hr)
			}
			hr := comCall(device, 3, uintptr(unsafe.Pointer(&iidProbeEndpointVolume)), clsctxAll, 0, uintptr(unsafe.Pointer(&volume)))
			comRelease(device)
			if failedHR(hr) {
				t.Fatalf("mute interface: %#x", hr)
			}
			original := before.Volumes[id].Muted
			defer func() {
				defer comRelease(volume)
				if hr := comCall(volume, 14, uintptr(original), 0); failedHR(hr) {
					t.Errorf("restore original mute: %#x", hr)
				}
			}()
			if hr := comCall(volume, 14, 1, 0); failedHR(hr) {
				t.Fatalf("set probe mute: %#x", hr)
			}
			var actual int32
			if hr := comCall(volume, 15, uintptr(unsafe.Pointer(&actual))); failedHR(hr) || actual == 0 {
				t.Fatalf("probe mute not applied: %#x", hr)
			}
		}
	}
	t.Logf("active_render_endpoints=%d separate_communications=%t", len(ids), before.Defaults[0][0] != before.Defaults[0][2])
	source, err := openPlatformLoopback()
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	for i, id := range ids {
		frequency := float64(733 + i*137)
		pcm := probeCaptureWindow(t, source, func() { probePlayTones(t, enumerator, []string{id}, []float64{frequency}) })
		amplitude := probeToneAmplitude(pcm, frequency)
		t.Logf("endpoint=%d console=%t communications=%t muted=%t volume=%.6f bytes=%d tone_hz=%.0f amplitude=%.2f", i,
			id == before.Defaults[0][0], id == before.Defaults[0][2], muted || before.Volumes[id].Muted != 0, before.Volumes[id].Level, len(pcm), frequency, amplitude)
		if amplitude < 80 {
			t.Errorf("endpoint %d generated tone missing: amplitude=%.2f", i, amplitude)
		}
	}
	if len(ids) >= 2 {
		frequencies := []float64{733, 997}
		pcm := probeCaptureWindow(t, source, func() { probePlayTones(t, enumerator, ids[:2], frequencies) })
		for i, frequency := range frequencies {
			amplitude := probeToneAmplitude(pcm, frequency)
			t.Logf("simultaneous_endpoint=%d tone_hz=%.0f amplitude=%.2f bytes=%d", i, frequency, amplitude, len(pcm))
			if amplitude < 80 {
				t.Errorf("simultaneous output %d missing: amplitude=%.2f", i, amplitude)
			}
		}
	} else {
		t.Log("simultaneous distinct hardware endpoints not available; covered by deterministic fixtures only")
	}
}

func probeAudioConfiguration(t *testing.T, enumerator uintptr) probeAudioState {
	t.Helper()
	state := probeAudioState{Volumes: make(map[string]probeVolume)}
	for flow := 0; flow < 2; flow++ {
		for role := 0; role < 3; role++ {
			var device uintptr
			if hr := comCall(enumerator, 4, uintptr(flow), uintptr(role), uintptr(unsafe.Pointer(&device))); !failedHR(hr) && device != 0 {
				id, err := endpointID(device)
				comRelease(device)
				if err != nil {
					t.Fatal(err)
				}
				state.Defaults[flow][role] = id
			}
		}
		var collection uintptr
		if hr := comCall(enumerator, 3, uintptr(flow), deviceStateActive, uintptr(unsafe.Pointer(&collection))); failedHR(hr) {
			t.Fatalf("enumerate configuration: %#x", hr)
		}
		func() {
			defer comRelease(collection)
			var count uint32
			if hr := comCall(collection, 3, uintptr(unsafe.Pointer(&count))); failedHR(hr) {
				t.Fatalf("configuration count: %#x", hr)
			}
			for i := uint32(0); i < count; i++ {
				var device uintptr
				if hr := comCall(collection, 4, uintptr(i), uintptr(unsafe.Pointer(&device))); failedHR(hr) {
					t.Fatalf("configuration device: %#x", hr)
				}
				func() {
					defer comRelease(device)
					id, err := endpointID(device)
					if err != nil {
						t.Fatal(err)
					}
					var volume uintptr
					if hr := comCall(device, 3, uintptr(unsafe.Pointer(&iidProbeEndpointVolume)), clsctxAll, 0, uintptr(unsafe.Pointer(&volume))); failedHR(hr) {
						t.Fatalf("configuration volume: %#x", hr)
					}
					defer comRelease(volume)
					var value probeVolume
					if hr := comCall(volume, 15, uintptr(unsafe.Pointer(&value.Muted))); failedHR(hr) {
						t.Fatalf("read mute: %#x", hr)
					}
					if hr := comCall(volume, 9, uintptr(unsafe.Pointer(&value.Level))); failedHR(hr) {
						t.Fatalf("read volume: %#x", hr)
					}
					state.Volumes[id] = value
				}()
			}
		}()
	}
	return state
}

func probeCaptureWindow(t *testing.T, source loopbackSource, play func()) []byte {
	t.Helper()
	for {
		pcm, err := source.ReadPCM()
		if err != nil {
			t.Fatal(err)
		}
		if len(pcm) == 0 {
			break
		}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	var captured []byte
	var readErr error
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			pcm, err := source.ReadPCM()
			if err != nil {
				readErr = err
				return
			}
			if len(captured)+len(pcm) <= audioSampleRate*2*4 {
				captured = append(captured, pcm...)
			}
		}
	}()
	var once sync.Once
	finish := func() { once.Do(func() { close(stop) }); <-done }
	defer finish()
	play()
	time.Sleep(180 * time.Millisecond)
	// Stop and join before examining the in-memory samples.
	finish()
	if readErr != nil {
		t.Fatal(readErr)
	}
	return captured
}

type probeRenderer struct {
	client, render uintptr
	format         pcmFormat
	buffer         uint32
	frame          int
	frequency      float64
}

func probePlayTones(t *testing.T, enumerator uintptr, ids []string, frequencies []float64) {
	t.Helper()
	var outputs []*probeRenderer
	defer func() {
		for _, out := range outputs {
			comCall(out.client, 11)
			comRelease(out.render)
			comRelease(out.client)
		}
	}()
	for i, id := range ids {
		out := &probeRenderer{frequency: frequencies[i]}
		outputs = append(outputs, out)
		if err := out.open(enumerator, id); err != nil {
			t.Fatal(err)
		}
		if err := out.fill(); err != nil {
			t.Fatal(err)
		}
		if hr := comCall(out.client, 10); failedHR(hr) {
			t.Fatalf("start generated tone: %#x", hr)
		}
	}
	until := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(until) {
		for _, out := range outputs {
			if err := out.fill(); err != nil {
				t.Fatal(err)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (r *probeRenderer) open(enumerator uintptr, id string) error {
	text, err := windows.UTF16PtrFromString(id)
	if err != nil {
		return err
	}
	var device uintptr
	if hr := comCall(enumerator, 5, uintptr(unsafe.Pointer(text)), uintptr(unsafe.Pointer(&device))); failedHR(hr) {
		return fmt.Errorf("tone device: %#x", hr)
	}
	defer comRelease(device)
	if hr := comCall(device, 3, uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&r.client))); failedHR(hr) {
		return fmt.Errorf("tone client: %#x", hr)
	}
	var mix *waveFormatEx
	if hr := comCall(r.client, 8, uintptr(unsafe.Pointer(&mix))); failedHR(hr) || mix == nil {
		return fmt.Errorf("tone format: %#x", hr)
	}
	defer procCoTaskMemFreeL.Call(uintptr(unsafe.Pointer(mix)))
	r.format = pcmFormatFromWave(mix)
	if !validLoopbackFormat(r.format) {
		return fmt.Errorf("unsupported tone format")
	}
	if hr := comCall(r.client, 3, audclntSharemodeShared, 0, refTime1s, 0, uintptr(unsafe.Pointer(mix)), 0); failedHR(hr) {
		return fmt.Errorf("tone initialize: %#x", hr)
	}
	if hr := comCall(r.client, 4, uintptr(unsafe.Pointer(&r.buffer))); failedHR(hr) {
		return fmt.Errorf("tone buffer: %#x", hr)
	}
	if hr := comCall(r.client, 14, uintptr(unsafe.Pointer(&iidProbeRenderClient)), uintptr(unsafe.Pointer(&r.render))); failedHR(hr) {
		return fmt.Errorf("tone render service: %#x", hr)
	}
	return nil
}

func (r *probeRenderer) fill() error {
	var padding uint32
	if hr := comCall(r.client, 6, uintptr(unsafe.Pointer(&padding))); failedHR(hr) {
		return fmt.Errorf("tone padding: %#x", hr)
	}
	frames := min(r.buffer-padding, uint32(r.format.rate/10))
	if frames == 0 {
		return nil
	}
	var data uintptr
	if hr := comCall(r.render, 3, uintptr(frames), uintptr(unsafe.Pointer(&data))); failedHR(hr) {
		return fmt.Errorf("tone get buffer: %#x", hr)
	}
	raw := unsafe.Slice((*byte)(ptrFromUintptr(data)), int(frames)*r.format.blockAlign)
	clear(raw)
	for i := 0; i < int(frames); i++ {
		sample := .04 * math.Sin(2*math.Pi*r.frequency*float64(r.frame)/float64(r.format.rate))
		r.frame++
		for c := 0; c < r.format.channels; c++ {
			off := i*r.format.blockAlign + c*r.format.bits/8
			switch {
			case r.format.float:
				binary.LittleEndian.PutUint32(raw[off:], math.Float32bits(float32(sample)))
			case r.format.bits == 16:
				binary.LittleEndian.PutUint16(raw[off:], uint16(int16(sample*32767)))
			case r.format.bits == 24:
				v := int32(sample * 8388607)
				raw[off], raw[off+1], raw[off+2] = byte(v), byte(v>>8), byte(v>>16)
			case r.format.bits == 32:
				binary.LittleEndian.PutUint32(raw[off:], uint32(int32(sample*2147483647)))
			}
		}
	}
	if hr := comCall(r.render, 4, uintptr(frames), 0); failedHR(hr) {
		return fmt.Errorf("tone release buffer: %#x", hr)
	}
	return nil
}

func probeToneAmplitude(pcm []byte, frequency float64) float64 {
	n := len(pcm) / 2
	if n == 0 {
		return 0
	}
	var real, imaginary float64
	for i := 0; i < n; i++ {
		value := float64(int16(binary.LittleEndian.Uint16(pcm[i*2:])))
		phase := 2 * math.Pi * frequency * float64(i) / audioSampleRate
		real += value * math.Cos(phase)
		imaginary += value * math.Sin(phase)
	}
	return 2 * math.Hypot(real, imaginary) / float64(n)
}
