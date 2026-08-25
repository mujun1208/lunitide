// Package voice is the seam between the companion's ear and whatever is
// doing the listening.
//
// Until now there was no seam: the renderer called the browser's Web Speech
// API and the Go side never saw a sample. That works and costs nothing, but
// it is Microsoft's cloud recognizer reached through WebView2 — it needs the
// network, it cannot be tuned, and it stops at whatever Windows ships. A
// local engine and a speech-to-speech model both want the audio itself, and
// neither should require rewriting the companion to get it.
//
// So the contract here is deliberately narrow: a Backend hands out Sessions,
// a Session eats PCM frames and emits transcripts. Everything that differs
// between a subprocess running an ONNX model, a websocket to a hosted
// speech-to-speech endpoint, and a fake in a test lives behind that.
package voice

import (
	"context"
	"errors"
)

// The audio format every Backend receives. These are not preferences; they
// are the format the renderer's capture path produces (see pcmFrames.ts) and
// the one every recognizer worth using accepts. A Backend that wants
// something else converts internally rather than pushing the choice back up.
const (
	// SampleRate in hertz. 16 kHz is the rate acoustic models are trained
	// at; higher rates are resampled down by the recognizer anyway.
	SampleRate = 16000
	// Channels is mono. Recognizers downmix, and stereo doubles the bytes
	// crossing the bridge for nothing.
	Channels = 1
	// BytesPerSample for signed 16-bit little-endian samples.
	BytesPerSample = 2
	// FrameMillis is the cadence the renderer sends at. It is a property of
	// the capture path rather than a rule, but backends size their buffers
	// against it, so it belongs in the same place as the rest of the format.
	FrameMillis = 100
	// FrameBytes is one frame at the format above: 3200 bytes.
	FrameBytes = SampleRate * Channels * BytesPerSample * FrameMillis / 1000
)

// Transcript is one thing the recognizer believes it heard.
//
// Partials are revisions, not additions: each carries the recognizer's
// current best guess at the whole utterance so far, so a consumer replaces
// rather than appends. That matches how both Web Speech and the streaming
// ONNX recognizers behave, and getting it backwards produces text that
// stutters every correction back onto the screen.
type Transcript struct {
	// Text is the utterance so far, or the settled utterance when Final.
	Text string
	// Final marks the recognizer's last word on this utterance. A session
	// may produce several finals if the speaker pauses and continues.
	Final bool
}

// SessionOptions configures one recognition.
type SessionOptions struct {
	// Language is a BCP-47 tag. Empty asks the backend for its default,
	// which for a Chinese-first product is Chinese.
	Language string
	// OnTranscript receives every partial and final. It is called from the
	// backend's own goroutine and must not block: a backend reading a
	// subprocess pipe cannot drain it while the callback is away.
	OnTranscript func(Transcript)
}

// Session is one continuous recognition: audio in, transcripts out.
//
// A Session is not safe for concurrent use. It is fed by whichever goroutine
// owns the audio and torn down by the same one.
type Session interface {
	// Append feeds one frame of PCM in the package's format. Short frames
	// are accepted — the tail of an utterance rarely lands on a boundary —
	// but the length must be a whole number of samples.
	Append(ctx context.Context, pcm []byte) error
	// Finish stops the audio, flushes whatever the recognizer is holding,
	// and returns the settled transcript. Callers that only want the
	// callbacks may ignore the return.
	Finish(ctx context.Context) (string, error)
	// Close abandons the session and releases its resources. Safe to call
	// after Finish and safe to call twice, so it can be deferred.
	Close() error
}

// Transcriber re-reads a finished utterance in one pass.
//
// Separate from Backend because the two are asked different questions. A
// Backend is fed audio as it arrives and answers continuously, which is what
// a caption needs and what forces it to guess at a word before the sentence
// that would disambiguate it has been spoken. A Transcriber is handed the
// whole utterance and answers once, which is slower to start and markedly
// more accurate — see DefaultRefiner for the measurements.
//
// An interface rather than the concrete refiner so a session can be tested
// without a subprocess, and so a hosted recognizer can take the same slot
// later without touching the session code.
type Transcriber interface {
	// Transcribe returns what was said. An error means the caller should
	// keep whatever the streaming recognizer produced.
	Transcribe(ctx context.Context, pcm []byte) (string, error)
}

// Backend is a recognizer that can be asked for sessions.
type Backend interface {
	// Name identifies the backend in diagnostics and in the status the
	// settings page shows. Stable across versions; it is a key, not prose.
	Name() string
	// Ready reports nil when Start would succeed right now. A backend whose
	// model has not been downloaded, whose sidecar is missing, or which
	// needs a network it cannot reach says so here rather than failing at
	// the start of a turn, so the caller can fall back before the user has
	// spoken instead of after.
	Ready(ctx context.Context) error
	// Start opens a session. The context bounds startup only; the session
	// outlives it.
	Start(ctx context.Context, opts SessionOptions) (Session, error)
}

// Errors a Backend reports from Ready, so a caller can tell "ask the user to
// install something" apart from "try again later" without parsing strings.
var (
	// ErrUnsupported means this backend cannot run on this machine at all —
	// wrong OS, missing CPU feature. Falling back is the only option.
	ErrUnsupported = errors.New("voice backend unsupported on this system")
	// ErrModelMissing means the backend would work once its model is on
	// disk. This is the one the UI turns into a download prompt.
	ErrModelMissing = errors.New("voice model not installed")
	// ErrBackendUnavailable means a transient failure: the sidecar died, the
	// endpoint refused. Worth retrying, not worth prompting about.
	ErrBackendUnavailable = errors.New("voice backend unavailable")
)

// ErrSessionClosed is returned by Append and Finish once the session is done.
var ErrSessionClosed = errors.New("voice session closed")

// ErrNoAudio means a turn ended with nothing to recognize. Distinguished from
// a failure because it is the ordinary result of the user pressing the button
// and saying nothing, and the caller's response is to do nothing rather than
// to report a fault.
var ErrNoAudio = errors.New("voice: no audio in utterance")

// Errors from the installer.
var (
	// ErrUnknownBundle means a caller named something not in the catalogue.
	// Bundle IDs cross the bridge, so this is reachable from a payload and
	// has to be an error rather than a panic.
	ErrUnknownBundle = errors.New("unknown voice bundle")
	// ErrDigestMismatch means the bytes that arrived are not the bytes the
	// catalogue pinned. The download is discarded.
	ErrDigestMismatch = errors.New("voice download digest mismatch")
	// ErrArchiveUnsafe means an archive member tried to write outside the
	// directory it was being unpacked into, or was a link rather than a
	// file. Refused without unpacking any of it.
	ErrArchiveUnsafe = errors.New("voice archive member unsafe")
)

// ValidFrame reports whether a payload can be interpreted as PCM in this
// package's format. It rejects an odd byte count, which would mean a sample
// was cut in half somewhere between the microphone and here, and an
// oversized one, which would mean the sender is not framing at all.
func ValidFrame(pcm []byte) bool {
	if len(pcm) == 0 || len(pcm)%BytesPerSample != 0 {
		return false
	}
	// Ten frames of headroom. Enough that a sender batching a few frames or
	// flushing a tail is fine, small enough that a runaway buffer is caught
	// before it is copied through the bridge.
	return len(pcm) <= FrameBytes*10
}

// FrameDurationMillis reports how much speech a payload carries. Used for
// pacing and for the diagnostics that report how much audio a turn consumed.
func FrameDurationMillis(pcm []byte) int {
	return len(pcm) / BytesPerSample * 1000 / SampleRate
}
