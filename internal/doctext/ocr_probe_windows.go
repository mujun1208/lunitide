//go:build windows

package doctext

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/commandworker"
)

//go:embed ocr_probe.ps1
var windowsOCRProbeScript []byte

type windowsOCRProbeFile struct {
	State     string   `json:"state"`
	Languages []string `json:"languages"`
	Text      string   `json:"text"`
	ErrorCode string   `json:"errorCode"`
}

func probeWindowsOCRPlatform(ctx context.Context) WindowsOCRProbe {
	if err := ctx.Err(); err != nil {
		return WindowsOCRProbe{State: WindowsOCRTimedOut, ErrorCode: "OCR_PROBE_TIMEOUT"}
	}
	parent := ""
	if config := parserConfig.Load(); config != nil {
		parent = config.root
	}
	root, err := os.MkdirTemp(parent, "parse-ocr-probe-")
	if err != nil {
		return WindowsOCRProbe{State: WindowsOCRInitializationErr, ErrorCode: "OCR_PROBE_TEMP"}
	}
	defer os.RemoveAll(root)
	input := filepath.Join(root, "probe.png")
	output := filepath.Join(root, "probe.json")
	script := filepath.Join(root, "ocr_probe.ps1")
	if err = os.WriteFile(input, windowsOCRProbePNG(), 0600); err != nil {
		return WindowsOCRProbe{State: WindowsOCRInitializationErr, ErrorCode: "OCR_PROBE_WRITE"}
	}
	if err = os.WriteFile(script, windowsOCRProbeScript, 0600); err != nil {
		return WindowsOCRProbe{State: WindowsOCRInitializationErr, ErrorCode: "OCR_PROBE_SCRIPT"}
	}
	env := []string{"TEMP=" + root, "TMP=" + root}
	for _, key := range []string{"SYSTEMROOT", "WINDIR", "USERPROFILE", "LOCALAPPDATA", "APPDATA"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	exe := filepath.Join(os.Getenv("SYSTEMROOT"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	timeout := windowsOCRProbeTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remain := time.Until(deadline); remain > 0 && remain < timeout {
			timeout = remain
		}
	}
	outcome, err := commandworker.Run(ctx, commandworker.Spec{
		Exe: exe, Args: []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, "-InputPath", input, "-OutputPath", output},
		Dir: root, Env: env, Timeout: timeout, MaxMemoryBytes: 256 << 20, MaxOutputBytes: 4096,
	}, nil, nil)
	if ctx.Err() != nil || (outcome.TimedOut) {
		return WindowsOCRProbe{State: WindowsOCRTimedOut, ErrorCode: "OCR_PROBE_TIMEOUT"}
	}
	if err != nil {
		return WindowsOCRProbe{State: WindowsOCRInitializationErr, ErrorCode: "OCR_PROBE_LAUNCH"}
	}
	f, err := os.Open(output)
	if err != nil {
		return WindowsOCRProbe{State: WindowsOCRSampleFailed, ErrorCode: "OCR_PROBE_OUTPUT"}
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return WindowsOCRProbe{State: WindowsOCRSampleFailed, ErrorCode: "OCR_PROBE_READ"}
	}
	var file windowsOCRProbeFile
	if json.Unmarshal(body, &file) != nil {
		return WindowsOCRProbe{State: WindowsOCRSampleFailed, ErrorCode: "OCR_PROBE_JSON"}
	}
	state := WindowsOCRProbeState(strings.TrimSpace(file.State))
	switch state {
	case WindowsOCRReady, WindowsOCRUnsupportedOS, WindowsOCRInitializationErr, WindowsOCRLanguageMissing, WindowsOCRSampleFailed, WindowsOCRTimedOut:
	default:
		return WindowsOCRProbe{State: WindowsOCRSampleFailed, ErrorCode: "OCR_PROBE_STATE", Languages: file.Languages}
	}
	if state == WindowsOCRReady && !windowsOCRProbeTextOK(file.Text) {
		return WindowsOCRProbe{State: WindowsOCRSampleFailed, ErrorCode: "OCR_PROBE_MISREAD", Languages: file.Languages}
	}
	return WindowsOCRProbe{State: state, Languages: file.Languages, ErrorCode: strings.TrimSpace(file.ErrorCode)}
}

func windowsOCRProbeTextOK(text string) bool {
	return strings.TrimSpace(text) != ""
}

//go:embed ocr_repair.ps1
var windowsOCRRepairScript []byte

func repairWindowsOCRPlatform(ctx context.Context) {
	if err := ctx.Err(); err != nil {
		return
	}
	parent := ""
	if config := parserConfig.Load(); config != nil {
		parent = config.root
	}
	root, err := os.MkdirTemp(parent, "parse-ocr-repair-")
	if err != nil {
		return
	}
	defer os.RemoveAll(root)
	script := filepath.Join(root, "ocr_repair.ps1")
	if os.WriteFile(script, windowsOCRRepairScript, 0600) != nil {
		return
	}
	env := []string{"TEMP=" + root, "TMP=" + root}
	for _, key := range []string{"SYSTEMROOT", "WINDIR", "USERPROFILE", "LOCALAPPDATA", "APPDATA"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	exe := filepath.Join(os.Getenv("SYSTEMROOT"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	timeout := 45 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remain := time.Until(deadline) - windowsOCRProbeTimeout
		if remain > 5*time.Second && remain < timeout {
			timeout = remain
		}
	}
	_, _ = commandworker.Run(ctx, commandworker.Spec{
		Exe: exe, Args: []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script},
		Dir: root, Env: env, Timeout: timeout, MaxMemoryBytes: 256 << 20, MaxOutputBytes: 4096,
	}, nil, nil)
}
