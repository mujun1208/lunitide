package tts

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

//go:embed edge_synth.py
var edgeSynthScript []byte

var (
	edgePythonOnce   sync.Once
	edgePythonBin    string
	edgePythonPrefix []string
	edgePythonOK     bool
)

func edgeSynthesizePython(ctx context.Context, in SynthesizeInput) (SynthesizeResult, bool, error) {
	py := edgeFindPython()
	if py == "" {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 云端语音合成失败", ErrSynthesisFailed)
	}
	script, err := edgeWriteSynthScript()
	if err != nil {
		return SynthesizeResult{}, false, fmt.Errorf("%w: %v", ErrSynthesisFailed, err)
	}
	defer os.Remove(script)

	tmp, err := os.CreateTemp("", "lunitide-edge-*.mp3")
	if err != nil {
		return SynthesizeResult{}, false, fmt.Errorf("%w: %v", ErrSynthesisFailed, err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	args := append(append(append([]string{}, edgePythonPrefix...), script),
		"--voice", in.VoiceID,
		"--text", in.Text,
		"--style", strings.TrimSpace(in.Style),
		"--rate", signedPercent(clampEdgeRate(in.Rate)*10),
		"--volume", strconv.Itoa(clampEdgeVolume(in.Volume)),
		"--out", tmpPath,
	)
	cmd := exec.CommandContext(ctx, py, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return SynthesizeResult{}, false, fmt.Errorf("%w: %s", ErrSynthesisFailed, msg)
	}
	data, err := os.ReadFile(tmpPath)
	if err != nil || len(data) < 64 {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 云端未返回音频", ErrSynthesisFailed)
	}
	hint := float64(len(data)) / 6000
	if hint < 0.25 {
		hint = 0.25
	}
	return SynthesizeResult{
		WavBase64:    base64.StdEncoding.EncodeToString(data),
		DurationHint: hint,
	}, false, nil
}

func edgeWriteSynthScript() (string, error) {
	f, err := os.CreateTemp("", "lunitide-edge-synth-*.py")
	if err != nil {
		return "", err
	}
	path := f.Name()
	if _, err := f.Write(edgeSynthScript); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

func edgeFindPython() string {
	edgePythonOnce.Do(func() {
		type candidate struct {
			bin  string
			args []string
		}
		candidates := []candidate{
			{"python", nil},
			{"python3", nil},
			{"py", []string{"-3"}},
		}
		for _, c := range candidates {
			path, err := exec.LookPath(c.bin)
			if err != nil {
				continue
			}
			args := append(append([]string{}, c.args...), "-m", "edge_tts", "--version")
			cmd := exec.Command(path, args...)
			if err := cmd.Run(); err != nil {
				continue
			}
			edgePythonBin = path
			edgePythonPrefix = append([]string(nil), c.args...)
			edgePythonOK = true
			return
		}
	})
	if !edgePythonOK {
		return ""
	}
	return edgePythonBin
}

func clampEdgeRate(rate int) int {
	if rate < -10 {
		return -10
	}
	if rate > 10 {
		return 10
	}
	return rate
}

func clampEdgeVolume(volume int) int {
	if volume < 0 {
		return 0
	}
	if volume > 100 {
		return 100
	}
	return volume
}
