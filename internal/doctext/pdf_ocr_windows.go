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
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/commandworker"
)

//go:embed pdf_ocr.ps1
var pdfOCRScript []byte

// ExtractPDFOCR runs the OS PDF renderer and OCR in a bounded child process.
// No cloud request, Office automation, source-file mutation or script evaluation
// from the document is involved. Callers must label this as fallible OCR evidence.
func ExtractPDFOCR(ctx context.Context, raw []byte) (PDFOCRResult, error) {
	if err := ctx.Err(); err != nil {
		return PDFOCRResult{}, err
	}
	if len(raw) > MaxInputBytes {
		return PDFOCRResult{}, ErrBudgetExceeded
	}
	if !bytes.HasPrefix(raw, []byte("%PDF-")) {
		return PDFOCRResult{}, ErrUnsupportedFormat
	}
	select {
	case parserSlots <- struct{}{}:
		defer func() { <-parserSlots }()
	default:
		return PDFOCRResult{}, errors.New("local OCR is busy; retry this document")
	}
	parent := ""
	if config := parserConfig.Load(); config != nil {
		parent = config.root
	}
	root, err := os.MkdirTemp(parent, "parse-ocr-")
	if err != nil {
		return PDFOCRResult{}, err
	}
	defer os.RemoveAll(root)
	input, output, script := filepath.Join(root, "input.pdf"), filepath.Join(root, "result.json"), filepath.Join(root, "ocr.ps1")
	if err = os.WriteFile(input, raw, 0600); err != nil {
		return PDFOCRResult{}, err
	}
	if err = os.WriteFile(script, pdfOCRScript, 0600); err != nil {
		return PDFOCRResult{}, err
	}
	env := []string{"TEMP=" + root, "TMP=" + root}
	for _, key := range []string{"SYSTEMROOT", "WINDIR", "USERPROFILE", "LOCALAPPDATA", "APPDATA"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	exe := filepath.Join(os.Getenv("SYSTEMROOT"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	outcome, err := commandworker.Run(ctx, commandworker.Spec{Exe: exe, Args: []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, "-InputPath", input, "-OutputPath", output}, Dir: root, Env: env, Timeout: 90 * time.Second, MaxMemoryBytes: 768 << 20, MaxOutputBytes: 4096}, nil, nil)
	if ctx.Err() != nil {
		return PDFOCRResult{}, ctx.Err()
	}
	if err != nil {
		return PDFOCRResult{}, err
	}
	if outcome.TimedOut {
		return PDFOCRResult{}, errors.New("local PDF OCR timed out; no complete text was read")
	}
	if outcome.ExitCode != 0 {
		return PDFOCRResult{}, errors.New("local PDF OCR failed; check the installed Windows OCR language and PDF rendering support")
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
	if result.Method != "windows-ocr" || len(result.Pages) == 0 || len(result.Pages) > 100 {
		return PDFOCRResult{}, ErrNoTextLayer
	}
	chars := 0
	for i, p := range result.Pages {
		if p.Page != i+1 {
			return PDFOCRResult{}, fmt.Errorf("incomplete OCR page sequence")
		}
		chars += len(strings.TrimSpace(p.Text))
	}
	if chars == 0 {
		return PDFOCRResult{}, ErrNoTextLayer
	}
	return result, nil
}
