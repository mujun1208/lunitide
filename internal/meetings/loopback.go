package meetings

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

const (
	loopbackBufMax = audioSampleRate * audioBytesPerSample * 8 // 8s of 16 kHz s16
	pollPCMMax     = 48 * 1024                                 // stay under bridge 65536-char base64
)

var errLoopbackUnavailable = io.ErrClosedPipe

type loopbackSource interface {
	ReadPCM() ([]byte, error) // 16 kHz mono s16le; empty slice means wait
	Close() error
}

var openLoopback = openPlatformLoopback

type loopbackSession struct {
	meetingID string
	src       loopbackSource
	stop      chan struct{}
	done      chan struct{}
	mu        sync.Mutex
	pollBuf   []byte
	mixBuf    []byte
}

func (s *Service) startLoopback(meetingID string) error {
	src, err := openLoopback()
	if err != nil || src == nil {
		if err == nil {
			err = errLoopbackUnavailable
		}
		return err
	}
	sess := &loopbackSession{
		meetingID: meetingID,
		src:       src,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	s.audioMu.Lock()
	if s.loopback != nil {
		prev := s.loopback
		s.loopback = sess
		s.audioMu.Unlock()
		prev.close()
	} else {
		s.loopback = sess
		s.audioMu.Unlock()
	}
	go sess.run()
	return nil
}

func (s *Service) stopLoopback(meetingID string) {
	s.audioMu.Lock()
	sess := s.loopback
	if sess == nil || (meetingID != "" && sess.meetingID != meetingID) {
		s.audioMu.Unlock()
		return
	}
	s.loopback = nil
	s.audioMu.Unlock()
	sess.close()
}

func (s *Service) PollLoopback(ctx context.Context, meetingID string) (pcm []byte, active bool, err error) {
	if err := s.ready(); err != nil {
		return nil, false, err
	}
	if _, parseErr := ulid.ParseStrict(meetingID); parseErr != nil {
		return nil, false, ErrInvalid
	}
	s.audioMu.Lock()
	sess := s.loopback
	s.audioMu.Unlock()
	if sess == nil || sess.meetingID != meetingID {
		return []byte{}, false, nil
	}
	select {
	case <-ctx.Done():
		return nil, true, ctx.Err()
	default:
	}
	return sess.takePoll(), true, nil
}

func (s *Service) takeMixPCM(meetingID string, want int) []byte {
	s.audioMu.Lock()
	sess := s.loopback
	s.audioMu.Unlock()
	if sess == nil || sess.meetingID != meetingID || want < 2 {
		return nil
	}
	return sess.takeMix(want)
}

func (sess *loopbackSession) run() {
	defer close(sess.done)
	defer func() { _ = sess.src.Close() }()
	for {
		select {
		case <-sess.stop:
			return
		default:
		}
		pcm, err := sess.src.ReadPCM()
		if len(pcm) > 0 {
			sess.append(pcm)
		}
		if err != nil {
			return
		}
		if len(pcm) == 0 {
			time.Sleep(8 * time.Millisecond)
		}
	}
}

func (sess *loopbackSession) append(pcm []byte) {
	if len(pcm)%2 != 0 {
		pcm = pcm[:len(pcm)-1]
	}
	if len(pcm) == 0 {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.pollBuf = capAppend(sess.pollBuf, pcm, loopbackBufMax)
	sess.mixBuf = capAppend(sess.mixBuf, pcm, loopbackBufMax)
}

func capAppend(dst, extra []byte, max int) []byte {
	if max < 2 {
		return extra
	}
	if max%2 != 0 {
		max--
	}
	need := len(dst) + len(extra)
	if need <= max {
		return append(dst, extra...)
	}
	drop := need - max
	if drop%2 != 0 {
		drop++
	}
	if drop >= len(dst) {
		keep := extra
		if len(keep) > max {
			keep = keep[len(keep)-max:]
			if len(keep)%2 != 0 {
				keep = keep[1:]
			}
		}
		out := make([]byte, len(keep))
		copy(out, keep)
		return out
	}
	out := append([]byte{}, dst[drop:]...)
	return append(out, extra...)
}

func (sess *loopbackSession) takePoll() []byte {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	n := len(sess.pollBuf)
	if n > pollPCMMax {
		n = pollPCMMax
	}
	if n%2 != 0 {
		n--
	}
	if n < 2 {
		return []byte{}
	}
	out := append([]byte{}, sess.pollBuf[:n]...)
	sess.pollBuf = append([]byte{}, sess.pollBuf[n:]...)
	return out
}

func (sess *loopbackSession) takeMix(want int) []byte {
	if want < 2 {
		return nil
	}
	if want%2 != 0 {
		want--
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	n := want
	if n > len(sess.mixBuf) {
		n = len(sess.mixBuf)
	}
	if n%2 != 0 {
		n--
	}
	if n < 2 {
		return nil
	}
	out := append([]byte{}, sess.mixBuf[:n]...)
	sess.mixBuf = append([]byte{}, sess.mixBuf[n:]...)
	return out
}

func (sess *loopbackSession) close() {
	select {
	case <-sess.stop:
	default:
		close(sess.stop)
	}
	<-sess.done
}

func mixS16le(mic, loop []byte) []byte {
	if len(loop) < 2 {
		return mic
	}
	out := make([]byte, len(mic))
	copy(out, mic)
	n := len(loop)
	if n > len(mic) {
		n = len(mic)
	}
	if n%2 != 0 {
		n--
	}
	for i := 0; i+1 < n; i += 2 {
		a := int32(int16(out[i]) | int16(out[i+1])<<8)
		b := int32(int16(loop[i]) | int16(loop[i+1])<<8)
		s := a + b
		if s > 32767 {
			s = 32767
		} else if s < -32768 {
			s = -32768
		}
		out[i] = byte(s)
		out[i+1] = byte(s >> 8)
	}
	return out
}

// SilenceLoopbackForTest keeps Start() from opening WASAPI in unit tests.
func SilenceLoopbackForTest(t testing.TB) {
	t.Helper()
	prev := openLoopback
	openLoopback = func() (loopbackSource, error) { return nil, errLoopbackUnavailable }
	t.Cleanup(func() { openLoopback = prev })
}

// InstallLoopbackForTest injects a 16 kHz s16le source used by Start().
func InstallLoopbackForTest(t testing.TB, read func() []byte) {
	t.Helper()
	prev := openLoopback
	src := &funcLoopback{read: read, stop: make(chan struct{})}
	openLoopback = func() (loopbackSource, error) { return src, nil }
	t.Cleanup(func() {
		_ = src.Close()
		openLoopback = prev
	})
}

type funcLoopback struct {
	read func() []byte
	stop chan struct{}
}

func (f *funcLoopback) ReadPCM() ([]byte, error) {
	select {
	case <-f.stop:
		return nil, io.EOF
	default:
	}
	if f.read == nil {
		time.Sleep(8 * time.Millisecond)
		return nil, nil
	}
	pcm := f.read()
	if len(pcm) == 0 {
		time.Sleep(8 * time.Millisecond)
	}
	return pcm, nil
}

func (f *funcLoopback) Close() error {
	select {
	case <-f.stop:
	default:
		close(f.stop)
	}
	return nil
}
