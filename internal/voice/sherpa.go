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
		// Endpointing is decided upstream: the renderer already runs the
		// tiered rules that know when a Chinese clause is unfinished, and a
		// second opinion here would cut utterances the renderer is still
		// holding open.
		"--enable-endpoint=false",
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
