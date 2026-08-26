package voice

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
)

// Talking to sherpa-onnx.
//
// The recognizer is a separate process rather than a library. Binding it in
// would mean cgo, and cgo would mean this program stops cross-compiling,
// starts needing a C toolchain in CI, and links a large C++ runtime into the
// desktop binary for a feature most users will not enable. A child process
// costs a few milliseconds of IPC per frame and keeps all of that out.
//
// The channel is the websocket server sherpa ships, because it is the only
// stock entry point that streams: the command-line tools decode whole files,
// and the microphone tools capture their own audio, which is exactly what we
// must not let them do — the renderer already owns the microphone, and two
// processes competing for one device is not something to arrange on purpose.

// sherpaResult is one message from the server. It sends considerably more
// than this — token ids, per-token timestamps, lattice probabilities — and
// all of it is ignored: the companion needs the words.
type sherpaResult struct {
	Text string `json:"text"`
	// Segment counts endpoints within a connection. It increments when the
	// server decides an utterance ended, which is how a second sentence in
	// one session is told apart from a correction to the first.
	Segment int `json:"segment"`
	// IsFinal marks the server's last word on the current segment.
	IsFinal bool `json:"is_final"`
}

// TurnEndSilenceSeconds is how long the user has to stop talking for the turn
// to be treated as finished.
//
// The one number in the pipeline a user can feel directly: it is the gap
// between them finishing a sentence and anything happening. Too short cuts
// them off mid-thought, too long makes the companion seem slow to react, and
// the tolerable range is narrow enough that this is worth stating in one
// place rather than deriving.
const TurnEndSilenceSeconds = 1.2

// doneMarker is what the server sends after the client says it has finished
// sending audio. It is not JSON, so it must be recognised before parsing.
const doneMarker = "Done!"

// doneRequest is the text frame the client sends to close out the audio.
const doneRequest = "Done"

// parseSherpaMessage turns one server frame into a transcript.
//
// Reports ok=false for frames that carry no transcript — the end-of-stream
// marker, and empty results, which the server emits while it is listening to
// silence and which would otherwise blank the caption between words.
func parseSherpaMessage(frame []byte) (Transcript, bool) {
	if string(frame) == doneMarker {
		return Transcript{}, false
	}
	var result sherpaResult
	if err := json.Unmarshal(frame, &result); err != nil {
		return Transcript{}, false
	}
	if result.Text == "" {
		return Transcript{}, false
	}
	return Transcript{Text: result.Text, Final: result.IsFinal}, true
}

// pcmToFloat32 converts the signed 16-bit samples the renderer captures into
// the normalized floats sherpa expects, little-endian on both sides.
//
// Dividing by 32768 rather than 32767 is deliberate and matches sherpa's own
// clients: it makes the scale exact for the negative full-scale sample and
// costs a fraction of a decibel at positive full scale, which no acoustic
// model can tell apart from the same audio a hair quieter.
func pcmToFloat32(pcm []byte) []byte {
	samples := len(pcm) / BytesPerSample
	out := make([]byte, samples*4)
	for i := range samples {
		sample := int16(binary.LittleEndian.Uint16(pcm[i*BytesPerSample:]))
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(float32(sample)/32768))
	}
	return out
}

// serverArgs builds the command line for the websocket server.
//
// The flags differ by architecture, which is the whole reason Architecture
// exists: a transducer is three files and a paraformer is two, and passing
// one model's flags for the other produces a startup failure whose message
// names an ONNX input tensor rather than anything a user could act on.
func serverArgs(arch ModelArchitecture, modelDir string, port int) ([]string, error) {
	tokens := filepath.Join(modelDir, "tokens.txt")
	encoder := filepath.Join(modelDir, "encoder.int8.onnx")
	decoder := filepath.Join(modelDir, "decoder.int8.onnx")

	args := []string{
		"--port=" + strconv.Itoa(port),
		"--tokens=" + tokens,
		// One worker is enough for one speaker, and each additional one
		// holds its own copy of the model in memory.
		"--num-work-threads=1",
		"--num-io-threads=1",
		"--max-batch-size=1",
		// How often the server drains its queue and emits a partial. The
		// default is 10ms, which produces more revisions than a caption can
		// usefully redraw; 30ms still lands well inside the time it takes
		// to say the next syllable.
		"--loop-interval-ms=30",
		// Let the recognizer say when a turn ended.
		//
		// This was disabled, with endpointing left to rules in the renderer
		// that watched microphone energy and how long the transcript had
		// stopped changing. Neither is evidence about the speaker: a level
		// meter cannot tell a pause from a full stop, and a transcript that
		// stops growing means the decoder is between tokens as often as it
		// means the sentence is over. Turns were cut in half accordingly —
		// 「你好月汐」 committed and answered as 「你好」.
		//
		// The engine decides it from the decoder itself, which is what every
		// production voice stack does with a VAD in this position, and it is
		// already in the binary we ship.
		"--enable-endpoint=true",
		// Rule 2: silence after something was actually said. This is the
		// rule that ends an ordinary turn, and it is the one number a user
		// feels — the wait between finishing a sentence and being answered.
		"--rule2-min-trailing-silence=" + strconv.FormatFloat(TurnEndSilenceSeconds, 'f', 2, 64),
		// Rule 1 fires on trailing silence whether or not anything was said,
		// which would end "turns" made of room noise. Requiring speech makes
		// it a ceiling on a long pause mid-sentence rather than a second,
		// shorter version of rule 2.
		"--rule1-must-contain-nonsilence=true",
		// After a segment with nothing in it, start the encoder clean. A
		// session here spans a whole conversation, and carrying state across
		// every silence is how a recognizer that worked for two turns starts
		// missing the beginning of the third.
		"--reset-encoder=true",
	}

	switch arch {
	case ArchParaformer:
		args = append(args,
			"--paraformer-encoder="+encoder,
			"--paraformer-decoder="+decoder,
		)
	case ArchTransducer:
		args = append(args,
			"--encoder="+encoder,
			"--decoder="+decoder,
			"--joiner="+filepath.Join(modelDir, "joiner.int8.onnx"),
		)
	default:
		return nil, fmt.Errorf("voice: unsupported model architecture %q", arch)
	}
	return args, nil
}

// serverExecutable is the program inside the runtime bundle that serves
// streaming recognition.
func serverExecutable(runtimeDir string) string {
	return filepath.Join(runtimeDir, "bin", "sherpa-onnx-online-websocket-server.exe")
}
