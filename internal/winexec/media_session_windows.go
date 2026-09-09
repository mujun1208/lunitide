//go:build windows

package winexec

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unicode/utf16"
)

//go:embed media_session.ps1
var mediaSessionScript string

// MediaSessionAction targets a single exact app identity, never the global media key.
func MediaSessionAction(ctx context.Context, aliases []string, action string, shuffle bool) (MediaSessionResult, error) {
	var result MediaSessionResult
	switch action {
	case "status", "play", "pause", "next", "prev", "stop":
	default:
		return result, errors.New("unsupported media session action")
	}
	if len(aliases) == 0 {
		return result, errors.New("media app identity required")
	}
	input, err := json.Marshal(map[string]any{"aliases": aliases, "action": action, "shuffle": shuffle})
	if err != nil {
		return result, err
	}
	// Windows PowerShell 5 provides WinRT interop; pwsh 7 does not.
	units := utf16.Encode([]rune(mediaSessionScript))
	encoded := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(encoded[i*2:], unit)
	}
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	path := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, path, "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.Stdin = bytes.NewReader(input)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		return result, errors.New("media session unavailable or app rejected action")
	}
	if err := json.Unmarshal(bytes.TrimPrefix(out, []byte{0xef, 0xbb, 0xbf}), &result); err != nil {
		return result, errors.New("invalid media session response")
	}
	return result, nil
}
