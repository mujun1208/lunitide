package meetings

import (
	"encoding/binary"
	"math"
	"time"
)

const (
	loopbackClockRate   = int64(10_000_000) // WASAPI QPC timestamps use 100 ns units.
	endpointRefresh     = 500 * time.Millisecond
	endpointRetry       = 2 * time.Second
	endpointMixDelay    = loopbackClockRate / 25
	endpointMixCapacity = audioSampleRate * 2
)

type endpointPacket struct {
	raw      []byte
	format   pcmFormat
	position int64
}

type endpointCapture interface {
	readPacket() (endpointPacket, error)
	close()
}

// All callbacks and endpoint interfaces belong to the pump's COM thread.
type loopbackEndpoints struct {
	list    func() ([]string, error)
	open    func(string) (endpointCapture, error)
	streams map[string]endpointCapture
	retryAt map[string]time.Time
	mixer   endpointMixer
}

func newLoopbackEndpoints(origin int64, list func() ([]string, error), open func(string) (endpointCapture, error)) *loopbackEndpoints {
	return &loopbackEndpoints{list: list, open: open, streams: make(map[string]endpointCapture), retryAt: make(map[string]time.Time), mixer: endpointMixer{origin: origin, ends: make(map[string]int64)}}
}

func (e *loopbackEndpoints) refresh(now time.Time) error {
	ids, err := e.list()
	if err != nil {
		return err // An enumeration failure must not tear down healthy streams.
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id != "" {
			wanted[id] = true
		}
	}
	for id, stream := range e.streams {
		if !wanted[id] {
			stream.close()
			delete(e.streams, id)
		}
	}
	// A reopened endpoint can overlap packets still awaiting mixdown. Keep its
	// watermark until those samples have been emitted, even across unplugging.
	for id, end := range e.mixer.ends {
		if !wanted[id] && end <= e.mixer.cursor {
			delete(e.mixer.ends, id)
		}
	}
	for id := range e.retryAt {
		if !wanted[id] {
			delete(e.retryAt, id)
		}
	}
	for id := range wanted {
		if e.streams[id] != nil || now.Before(e.retryAt[id]) {
			continue
		}
		stream, err := e.open(id)
		if err != nil || stream == nil {
			e.retryAt[id] = now.Add(endpointRetry)
			continue
		}
		e.streams[id] = stream
		delete(e.retryAt, id)
	}
	return nil
}

func (e *loopbackEndpoints) read(now time.Time) {
	for id, stream := range e.streams {
		// Bound a misbehaving driver without letting it starve another endpoint.
		for n := 0; n < 128; n++ {
			packet, err := stream.readPacket()
			if err != nil {
				stream.close()
				delete(e.streams, id)
				e.retryAt[id] = now.Add(endpointRetry)
				break
			}
			if len(packet.raw) == 0 {
				break
			}
			e.mixer.add(id, packet)
		}
	}
}

func (e *loopbackEndpoints) close() {
	for id, stream := range e.streams {
		stream.close()
		delete(e.streams, id)
	}
}

type endpointMixer struct {
	origin int64
	cursor int64
	sums   [endpointMixCapacity]int64
	ends   map[string]int64
}

func validLoopbackFormat(f pcmFormat) bool {
	return f.channels > 0 && f.channels <= 32 && f.rate >= 8000 && f.rate <= 384000 &&
		(f.bits == 16 || f.bits == 24 || f.bits == 32) && (!f.float || f.bits == 32) &&
		f.blockAlign >= f.channels*(f.bits/8)
}

func ceilSample(t int64) int64 {
	// Go truncates negative division toward zero, which is already ceiling.
	if t > 0 {
		return (t*audioSampleRate + loopbackClockRate - 1) / loopbackClockRate
	}
	return t * audioSampleRate / loopbackClockRate
}

func (m *endpointMixer) add(id string, p endpointPacket) {
	f := p.format
	if !validLoopbackFormat(f) {
		return
	}
	frames := len(p.raw) / f.blockAlign
	if frames == 0 || frames > f.rate {
		return
	}
	delta := p.position - m.origin
	// Reject invalid timestamps before multiplication and bound buffered audio.
	cursorTime := m.cursor * loopbackClockRate / audioSampleRate
	if delta < cursorTime-loopbackClockRate || delta > cursorTime+2*loopbackClockRate {
		return
	}
	start := ceilSample(delta)
	// Keep the fractional packet duration: rounding it to 100 ns first leaves
	// occasional one-sample holes at 44.1 kHz packet boundaries.
	scaled := delta * audioSampleRate
	denominator := loopbackClockRate * int64(f.rate)
	numerator := scaled%loopbackClockRate*int64(f.rate) + int64(frames)*loopbackClockRate*audioSampleRate
	end := scaled/loopbackClockRate + (numerator+denominator-1)/denominator
	start = max(start, m.cursor, m.ends[id])
	end = min(end, m.cursor+endpointMixCapacity)
	for index := start; index < end; index++ {
		// Resample on the common clock, preserving phase across packet boundaries.
		pos := (float64(index)*float64(loopbackClockRate)/audioSampleRate - float64(delta)) * float64(f.rate) / float64(loopbackClockRate)
		left := min(max(int(pos), 0), frames-1)
		right := min(left+1, frames-1)
		frac := min(max(pos-float64(left), 0), 1)
		var sample float64
		for c := 0; c < f.channels; c++ {
			sample += sampleAt(p.raw, left*f.blockAlign, c, f)*(1-frac) + sampleAt(p.raw, right*f.blockAlign, c, f)*frac
		}
		sample = min(max(sample/float64(f.channels), -1), 1)
		if math.IsNaN(sample) {
			sample = 0
		}
		m.sums[index%endpointMixCapacity] += int64(sample * 32767)
	}
	m.ends[id] = max(m.ends[id], end)
}

func (m *endpointMixer) render(now int64) []byte {
	end := (now - m.origin - endpointMixDelay) * audioSampleRate / loopbackClockRate
	// Emit one timeline, including silence. Idle endpoints never extend duration
	// or delay a speaking endpoint; a newly active endpoint starts at current time.
	end = end / (audioSampleRate / 10) * (audioSampleRate / 10)
	if end <= m.cursor {
		return nil
	}
	if end-m.cursor > endpointMixCapacity {
		m.sums = [endpointMixCapacity]int64{}
		m.cursor = end - endpointMixCapacity
	}
	pcm := make([]byte, (end-m.cursor)*2)
	for i := range len(pcm) / 2 {
		slot := m.cursor % endpointMixCapacity
		value := min(max(m.sums[slot], -32768), 32767)
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(int16(value)))
		m.sums[slot] = 0
		m.cursor++
	}
	return pcm
}
