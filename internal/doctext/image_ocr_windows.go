//go:build windows

package doctext

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/lunitide/lunitide/internal/commandworker"
)

//go:embed image_ocr.ps1
var imageOCRScript []byte

func imageOCRExt(raw []byte) string {
	switch {
	case bytes.HasPrefix(raw, []byte("\xff\xd8\xff")):
		return ".jpg"
	case bytes.HasPrefix(raw, []byte("GIF8")):
		return ".gif"
	case bytes.HasPrefix(raw, []byte("BM")):
		return ".bmp"
	case len(raw) >= 12 && bytes.HasPrefix(raw, []byte("RIFF")) && bytes.Equal(raw[8:12], []byte("WEBP")):
		return ".webp"
	default:
		return ".png"
	}
}

// ExtractImageOCR runs Windows.Media.Ocr on one still image in a bounded
// child process. It is the same OS backend as PDF OCR, not a bundled PP-OCR pack.
func ExtractImageOCR(ctx context.Context, raw []byte) (PDFOCRResult, error) {
	if err := ctx.Err(); err != nil {
		return PDFOCRResult{}, err
	}
	if len(raw) > MaxInputBytes {
		return PDFOCRResult{}, ErrBudgetExceeded
	}
	if !LooksLikeRasterImage("", "", raw) {
		return PDFOCRResult{}, ErrUnsupportedFormat
	}
	select {
	case parserSlots <- struct{}{}:
		defer func() { <-parserSlots }()
	default:
		return PDFOCRResult{}, errors.New("本地识别正忙，请稍后重试该文档")
	}
	parent := ""
	if config := parserConfig.Load(); config != nil {
		parent = config.root
	}
	root, err := os.MkdirTemp(parent, "parse-ocr-img-")
	if err != nil {
		return PDFOCRResult{}, err
	}
	defer os.RemoveAll(root)
	input, output, script := filepath.Join(root, "input"+imageOCRExt(raw)), filepath.Join(root, "result.json"), filepath.Join(root, "ocr.ps1")
	if err = os.WriteFile(input, raw, 0600); err != nil {
		return PDFOCRResult{}, err
	}
	if err = os.WriteFile(script, imageOCRScript, 0600); err != nil {
		return PDFOCRResult{}, err
	}
	env := []string{"TEMP=" + root, "TMP=" + root}
	for _, key := range []string{"SYSTEMROOT", "WINDIR", "USERPROFILE", "LOCALAPPDATA", "APPDATA"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	exe := filepath.Join(os.Getenv("SYSTEMROOT"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	outcome, err := commandworker.Run(ctx, commandworker.Spec{Exe: exe, Args: []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, "-InputPath", input, "-OutputPath", output}, Dir: root, Env: env, Timeout: 15 * time.Second, MaxMemoryBytes: 256 << 20, MaxOutputBytes: 4096}, nil, nil)
	if ctx.Err() != nil {
		return PDFOCRResult{}, ctx.Err()
	}
	if err != nil {
		return PDFOCRResult{}, err
	}
	if outcome.TimedOut {
		return PDFOCRResult{}, errors.New("本地图片识别超时，未读到完整文本")
	}
	if outcome.ExitCode != 0 {
		return PDFOCRResult{}, errors.New("本地图片识别失败，请检查已安装的 Windows OCR 语言包")
	}
	f, err := os.Open(output)
	if err != nil {
		return PDFOCRResult{}, err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, 4<<20+1))
	if err != nil {
		return PDFOCRResult{}, err
	}
	if len(body) > 4<<20 {
		return PDFOCRResult{}, ErrBudgetExceeded
	}
	var result PDFOCRResult
	if err = json.Unmarshal(body, &result); err != nil {
		return PDFOCRResult{}, err
	}
	if result.Method != "windows-ocr" || len(result.Pages) != 1 || result.Pages[0].Page != 1 {
		return PDFOCRResult{}, fmt.Errorf("图片识别结果不完整")
	}
	return result, nil
}
