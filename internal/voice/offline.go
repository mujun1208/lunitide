package voice

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
)

// Talking to sherpa's non-streaming websocket server.
//
// Why a second recognizer at all, when one already works: a streaming model
// has to commit to a word before it has heard the end of the sentence, and on
// this product's own recordings it gets 礼拜二 wrong where a non-streaming
// model gets it right. It also has no punctuation to give, and the text goes
// to a language model, which reads punctuation.
//
// The streaming model stays. It draws the caption while the user is still
// talking, and the endpointing rules that decide when a Chinese clause has
// finished are written against its partial text. What changes is which
// transcript is believed at the end of the turn.
//
// The cost is the wall time of one decode after the speaker stops. On the
// clips in this repository that is 40-50ms per second of audio, so a normal
// sentence adds about a fifth of a second before the reply begins — less than
// the language model takes to produce its first token.

// The wire format, from sherpa-onnx/csrc/offline-websocket-server-impl.h:
// the first message carries an 8-byte preamble, little endian, holding the
// sample rate and then the total number of sample bytes to expect. Samples
// follow as float32 normalized to [-1, 1], across as many binary messages as
// the client likes. The server replies with one text message once it has the
// number of bytes it was promised, and the client then says "Done".
const offlineHeaderBytes = 8

// offlineChunkBytes bounds one websocket frame. Sherpa's own client uses
// 10240; matching it keeps this inside whatever the server was tested with.
const offlineChunkBytes = 10240

// offlineRequest frames one utterance: preamble, then the samples.
//
// Returned whole rather than streamed because an utterance is bounded by how
// long a person talks without stopping — a few hundred kilobytes — and the
// caller already holds all of it.
func offlineRequest(pcm []byte) ([]byte, error) {
	if len(pcm)%BytesPerSample != 0 {
		return nil, fmt.Errorf("voice: %d bytes is not whole 16-bit samples", len(pcm))
	}
	samples := len(pcm) / BytesPerSample
	if samples == 0 {
		return nil, ErrNoAudio
	}
	body := pcmToFloat32(pcm)
	out := make([]byte, offlineHeaderBytes+len(body))
	binary.LittleEndian.PutUint32(out[0:4], uint32(SampleRate))
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(body)))
	copy(out[offlineHeaderBytes:], body)
	return out, nil
}

// offlineChunks splits a framed request into websocket-sized pieces.
func offlineChunks(request []byte) [][]byte {
	var chunks [][]byte
	for offset := 0; offset < len(request); offset += offlineChunkBytes {
		end := min(offset+offlineChunkBytes, len(request))
		chunks = append(chunks, request[offset:end])
	}
	return chunks
}

// parseOfflineResult reads the server's reply.
//
// The reply is JSON with the same "text" field the streaming server uses, but
// a server that fails mid-decode closes with a plain-text reason instead, so
// a parse failure is reported rather than swallowed as an empty transcript —
// silently returning "" here would look exactly like the user saying nothing.
func parseOfflineResult(frame []byte) (string, error) {
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(frame, &result); err != nil {
		return "", fmt.Errorf("voice: unreadable offline result %q: %w", truncateForError(frame), err)
	}
	return result.Text, nil
}

func truncateForError(frame []byte) string {
	const limit = 120
	if len(frame) <= limit {
		return string(frame)
	}
	return string(frame[:limit]) + "…"
}

// offlineServerArgs builds the command line for the non-streaming server.
func offlineServerArgs(arch ModelArchitecture, modelDir string, port int) ([]string, error) {
	args := []string{
		"--port=" + strconv.Itoa(port),
		"--tokens=" + filepath.Join(modelDir, "tokens.txt"),
		// One speaker, one utterance at a time. Extra workers each hold
		// their own copy of the model.
		"--num-work-threads=1",
		"--num-io-threads=1",
		"--max-batch-size=1",
		// Threads inside the model, which is where a single decode actually
		// gets faster. Two, not more: the decode is already a fraction of a
		// second and oversubscribing a laptop that is simultaneously running
		// the streaming recognizer and a browser makes both slower.
		"--num-threads=2",
		// A turn nobody would sit through, and a bound on how much memory a
		// stuck session can ask the server to allocate.
		"--max-utterance-length=60",
	}
	switch arch {
	case ArchSenseVoice:
		args = append(args,
			"--sense-voice-model="+filepath.Join(modelDir, "model.int8.onnx"),
			// Numbers as digits and sentences with punctuation. The
			// transcript is read by a language model, and "三点半" versus
			// "3点半" is the difference between it understanding a time.
			"--sense-voice-use-itn=true",
		)
	case ArchOfflineParaformer:
		args = append(args, "--paraformer="+filepath.Join(modelDir, "model.int8.onnx"))
	default:
		return nil, fmt.Errorf("voice: unsupported offline architecture %q", arch)
	}
	return args, nil
}

// offlineServerExecutable is the non-streaming server in the runtime bundle.
func offlineServerExecutable(runtimeDir string) string {
	return filepath.Join(runtimeDir, "bin", "sherpa-onnx-offline-websocket-server.exe")
}
