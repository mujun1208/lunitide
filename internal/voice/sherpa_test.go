package voice

import (
	"encoding/binary"
	"math"
	"strings"
	"testing"
)

func TestPcmToFloat32(t *testing.T) {
	pcm := make([]byte, 4*BytesPerSample)
	for i, sample := range []int16{0, 32767, -32768, -1} {
		binary.LittleEndian.PutUint16(pcm[i*BytesPerSample:], uint16(sample))
	}

	got := pcmToFloat32(pcm)
	if len(got) != 4*4 {
		t.Fatalf("converted length = %d bytes; want %d", len(got), 4*4)
	}

	want := []float32{0, 32767.0 / 32768, -1, -1.0 / 32768}
	for i, expected := range want {
		actual := math.Float32frombits(binary.LittleEndian.Uint32(got[i*4:]))
		if actual != expected {
			t.Errorf("sample %d = %v; want %v", i, actual, expected)
		}
		// Anything outside [-1, 1] is a clipped sample to the recognizer.
		if actual < -1 || actual > 1 {
			t.Errorf("sample %d = %v is outside the normalized range", i, actual)
		}
	}
}

func TestPcmToFloat32HandlesAnEmptyFrame(t *testing.T) {
	if got := pcmToFloat32(nil); len(got) != 0 {
		t.Errorf("converting nothing produced %d bytes", len(got))
	}
}

func TestParseSherpaMessage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame string
		want  Transcript
		ok    bool
	}{
		{
			name:  "partial",
			frame: `{"text":"今天天气","segment":0,"is_final":false}`,
			want:  Transcript{Text: "今天天气", Final: false},
			ok:    true,
		},
		{
			name:  "final",
			frame: `{"text":"今天天气怎么样","segment":0,"is_final":true}`,
			want:  Transcript{Text: "今天天气怎么样", Final: true},
			ok:    true,
		},
		{
			name:  "the fields we ignore do not confuse it",
			frame: `{"text":"你好","tokens":["你","好"],"timestamps":[0.1,0.2],"ys_probs":[-0.1],"segment":2,"start_time":1.5,"is_final":false}`,
			want:  Transcript{Text: "你好", Final: false},
			ok:    true,
		},
		{
			// Emitted while the server listens to silence. Delivering it
			// would blank the caption between words.
			name:  "empty result",
			frame: `{"text":"","segment":0,"is_final":false}`,
			ok:    false,
		},
		{
			name:  "end of stream marker is not json",
			frame: doneMarker,
			ok:    false,
		},
		{
			name:  "malformed",
			frame: `{"text":`,
			ok:    false,
		},
		{
			name:  "empty frame",
			frame: "",
			ok:    false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseSherpaMessage([]byte(tc.frame))
			if ok != tc.ok {
				t.Fatalf("ok = %v; want %v", ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Errorf("transcript = %+v; want %+v", got, tc.want)
			}
		})
	}
}

func TestServerArgsMatchTheModelArchitecture(t *testing.T) {
	paraformer, err := serverArgs(ArchParaformer, `C:\models\p`, 6006)
	if err != nil {
		t.Fatalf("paraformer args: %v", err)
	}
	joined := strings.Join(paraformer, " ")
	if !strings.Contains(joined, "--paraformer-encoder=") || !strings.Contains(joined, "--paraformer-decoder=") {
		t.Errorf("paraformer args missing its encoder/decoder flags: %v", paraformer)
	}
	if strings.Contains(joined, "--joiner=") {
		t.Errorf("a paraformer has no joiner: %v", paraformer)
	}

	transducer, err := serverArgs(ArchTransducer, `C:\models\z`, 6007)
	if err != nil {
		t.Fatalf("transducer args: %v", err)
	}
	joined = strings.Join(transducer, " ")
	for _, flag := range []string{"--encoder=", "--decoder=", "--joiner="} {
		if !strings.Contains(joined, flag) {
			t.Errorf("transducer args missing %s: %v", flag, transducer)
		}
	}
	if strings.Contains(joined, "--paraformer-") {
		t.Errorf("a transducer takes no paraformer flags: %v", transducer)
	}

	if _, err := serverArgs("nonsense", `C:\models\x`, 1); err == nil {
		t.Error("an unknown architecture should be rejected rather than guessed at")
	}
}

func TestServerArgsCarryThePortAndTokens(t *testing.T) {
	args, err := serverArgs(ArchParaformer, `C:\models\p`, 51234)
	if err != nil {
		t.Fatalf("args: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--port=51234") {
		t.Errorf("port not passed through: %v", args)
	}
	if !strings.Contains(joined, "tokens.txt") {
		t.Errorf("tokens not passed through: %v", args)
	}
	// The recognizer decides when a turn ended, from the decoder's own state
	// and the silence it measured. This was off, with the decision left to
	// rules above the bridge that watched microphone level and how long the
	// transcript had been unchanged — neither of which is evidence about the
	// speaker, and both of which are shorter than an ordinary pause, so
	// 「你好月汐」 was committed and answered as 「你好」.
	if !strings.Contains(joined, "--enable-endpoint=true") {
		t.Errorf("server-side endpointing should be on: %v", args)
	}
	// The wait between the user finishing and being answered.
	if !strings.Contains(joined, "--rule2-min-trailing-silence=1.20") {
		t.Errorf("turn-end silence not passed through: %v", args)
	}
	// Rule 1 ignores whether anything was said, so without this it ends
	// "turns" made of room noise on a shorter clock than rule 2.
	if !strings.Contains(joined, "--rule1-must-contain-nonsilence=true") {
		t.Errorf("rule 1 would fire on silence alone: %v", args)
	}
}
