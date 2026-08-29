package meetings

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	audioSampleRate     = 16000
	audioBytesPerSample = 2
	audioBytesPerMS     = audioSampleRate * audioBytesPerSample / 1000 // 32
	audioWAVHeader      = 44
	// AudioChunkSeconds is one rolling WAV. Rotating must not end the meeting.
	AudioChunkSeconds = 120
	audioChunkBytes   = audioSampleRate * audioBytesPerSample * AudioChunkSeconds
	// CatchupSpanSeconds is one sherpa-sized decode. The offline server
	// rejects >60s utterances; 20s leaves margin and keeps catch-up moving.
	CatchupSpanSeconds = 20
	catchupSpanBytes   = audioSampleRate * audioBytesPerSample * CatchupSpanSeconds
)

type audioSink struct {
	mu         sync.Mutex
	dir        string
	file       *os.File
	seq        int
	chunkBytes int64
	totalBytes int64
	closed     bool
}

func audioDir(root, meetingID string) string {
	return filepath.Join(root, meetingID)
}

func pcmDurationMS(pcmBytes int64) int64 {
	if pcmBytes <= 0 {
		return 0
	}
	return pcmBytes / int64(audioBytesPerMS)
}

func (s *Service) audioDurationMS(meetingID string) int64 {
	if s == nil {
		return 0
	}
	s.audioMu.Lock()
	sink := s.sinks[meetingID]
	s.audioMu.Unlock()
	if sink != nil {
		sink.mu.Lock()
		n := sink.totalBytes
		sink.mu.Unlock()
		if n > 0 {
			return pcmDurationMS(n)
		}
	}
	root := s.audioRoot
	if strings.TrimSpace(root) == "" {
		return 0
	}
	return dirAudioDurationMS(audioDir(root, meetingID))
}

func dirAudioDurationMS(dir string) int64 {
	return pcmDurationMS(dirAudioBytes(dir))
}

func dirAudioBytes(dir string) int64 {
	matches, err := filepath.Glob(filepath.Join(dir, "chunk_*.wav"))
	if err != nil {
		return 0
	}
	var total int64
	for _, path := range matches {
		info, statErr := os.Stat(path)
		if statErr != nil || info.Size() <= audioWAVHeader {
			continue
		}
		total += info.Size() - audioWAVHeader
	}
	return total
}

func (s *Service) ensureSink(meetingID string) (*audioSink, error) {
	if strings.TrimSpace(s.audioRoot) == "" {
		return nil, ErrUnavailable
	}
	s.audioMu.Lock()
	defer s.audioMu.Unlock()
	if s.sinks == nil {
		s.sinks = map[string]*audioSink{}
	}
	if sink := s.sinks[meetingID]; sink != nil {
		return sink, nil
	}
	dir := audioDir(s.audioRoot, meetingID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	sink := &audioSink{dir: dir, seq: nextChunkSeq(dir), totalBytes: dirAudioBytes(dir)}
	s.sinks[meetingID] = sink
	return sink, nil
}

func nextChunkSeq(dir string) int {
	matches, err := filepath.Glob(filepath.Join(dir, "chunk_*.wav"))
	if err != nil || len(matches) == 0 {
		return 1
	}
	max := 0
	for _, path := range matches {
		base := filepath.Base(path)
		var n int
		if _, err := fmt.Sscanf(base, "chunk_%d.wav", &n); err == nil && n > max {
			max = n
		}
	}
	return max + 1
}

func (s *Service) closeSink(meetingID string) {
	s.audioMu.Lock()
	sink := s.sinks[meetingID]
	delete(s.sinks, meetingID)
	s.audioMu.Unlock()
	if sink != nil {
		sink.close()
	}
}

func (s *Service) removeAudio(meetingID string) {
	s.closeSink(meetingID)
	if strings.TrimSpace(s.audioRoot) == "" {
		return
	}
	_ = os.RemoveAll(audioDir(s.audioRoot, meetingID))
}

func (s *audioSink) appendPCM(pcm []byte) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return pcmDurationMS(s.totalBytes), ErrNotRecording
	}
	if len(pcm) == 0 {
		return pcmDurationMS(s.totalBytes), nil
	}
	if len(pcm)%2 != 0 {
		pcm = pcm[:len(pcm)-1]
	}
	if len(pcm) == 0 {
		return pcmDurationMS(s.totalBytes), nil
	}
	for len(pcm) > 0 {
		if s.file == nil || s.chunkBytes >= audioChunkBytes {
			if err := s.rotateLocked(); err != nil {
				return pcmDurationMS(s.totalBytes), err
			}
		}
		room := int(audioChunkBytes - s.chunkBytes)
		if room < 2 {
			if err := s.rotateLocked(); err != nil {
				return pcmDurationMS(s.totalBytes), err
			}
			continue
		}
		if room%2 != 0 {
			room--
		}
		chunk := pcm
		if len(chunk) > room {
			chunk = pcm[:room]
		}
		n, err := s.file.Write(chunk)
		s.chunkBytes += int64(n)
		s.totalBytes += int64(n)
		pcm = pcm[n:]
		if err != nil {
			return pcmDurationMS(s.totalBytes), err
		}
	}
	return pcmDurationMS(s.totalBytes), nil
}

// rotateLocked finalizes the current WAV and opens the next file. It must
// not set closed, drop extra tracks, or otherwise end the meeting.
func (s *audioSink) rotateLocked() error {
	if s.file != nil {
		_ = finalizeWAV(s.file, s.chunkBytes)
		_ = s.file.Close()
		s.file = nil
		s.chunkBytes = 0
	}
	path := filepath.Join(s.dir, fmt.Sprintf("chunk_%04d.wav", s.seq))
	s.seq++
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(wavHeader(0)); err != nil {
		_ = f.Close()
		return err
	}
	s.file = f
	s.chunkBytes = 0
	return nil
}

func (s *audioSink) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	if s.file != nil {
		_ = finalizeWAV(s.file, s.chunkBytes)
		_ = s.file.Close()
		s.file = nil
	}
}

func wavHeader(pcmBytes int64) []byte {
	header := make([]byte, audioWAVHeader)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+pcmBytes))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], 1)
	binary.LittleEndian.PutUint32(header[24:28], uint32(audioSampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(audioSampleRate*audioBytesPerSample))
	binary.LittleEndian.PutUint16(header[32:34], audioBytesPerSample)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(pcmBytes))
	return header
}

func finalizeWAV(f *os.File, pcmBytes int64) error {
	if f == nil {
		return nil
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err := f.Write(wavHeader(pcmBytes))
	return err
}

type audioSpan struct {
	startedMS int64
	pcm       []byte
}

func walkAudioSpans(dir string, fromMS int64, fn func(audioSpan) error) error {
	matches, err := filepath.Glob(filepath.Join(dir, "chunk_*.wav"))
	if err != nil {
		return err
	}
	sort.Strings(matches)
	var (
		cursor   int64
		fromByte = fromMS * int64(audioBytesPerMS)
	)
	if fromByte%2 != 0 {
		fromByte--
	}
	for _, path := range matches {
		pcm, readErr := readWAVPCM(path)
		if readErr != nil {
			return readErr
		}
		start := cursor
		end := cursor + int64(len(pcm))
		cursor = end
		if end <= fromByte {
			continue
		}
		offset := int64(0)
		started := pcmDurationMS(start)
		if start < fromByte {
			offset = fromByte - start
			if offset%2 != 0 {
				offset--
			}
			if offset > int64(len(pcm)) {
				continue
			}
			pcm = pcm[offset:]
			started = pcmDurationMS(start + offset)
		}
		if len(pcm) == 0 {
			continue
		}
		if err := yieldAudioSpans(audioSpan{startedMS: started, pcm: pcm}, fn); err != nil {
			return err
		}
	}
	return nil
}

func yieldAudioSpans(span audioSpan, fn func(audioSpan) error) error {
	pcm := span.pcm
	started := span.startedMS
	max := int(catchupSpanBytes)
	if max < 2 {
		return fn(span)
	}
	if max%2 != 0 {
		max--
	}
	for len(pcm) > 0 {
		n := len(pcm)
		if n > max {
			n = max
		}
		if n%2 != 0 {
			n--
		}
		if n < 2 {
			break
		}
		if err := fn(audioSpan{startedMS: started, pcm: pcm[:n]}); err != nil {
			return err
		}
		started += pcmDurationMS(int64(n))
		pcm = pcm[n:]
	}
	return nil
}

func readWAVPCM(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) <= audioWAVHeader {
		return nil, nil
	}
	pcm := raw[audioWAVHeader:]
	if len(pcm)%2 != 0 {
		pcm = pcm[:len(pcm)-1]
	}
	return pcm, nil
}
