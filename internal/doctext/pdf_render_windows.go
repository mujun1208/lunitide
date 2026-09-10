//go:build windows

package doctext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/lunitide/lunitide/internal/commandworker"
)

func RenderPDFPages(ctx context.Context, raw []byte, pages []int) ([]RenderedPDFPage, error) {
	if err := rejectRenderPDFInput(ctx, raw, pages); err != nil {
		return nil, err
	}
	indexes, err := formatPDFPageIndexes(pages)
	if err != nil {
		return nil, err
	}
	select {
	case parserSlots <- struct{}{}:
		defer func() { <-parserSlots }()
	default:
		return nil, errors.New("本地识别正忙，请稍后重试该文档")
	}
	parent := ""
	if config := parserConfig.Load(); config != nil {
		parent = config.root
	}
	root, err := os.MkdirTemp(parent, "parse-ocr-render-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	input, output, script := filepath.Join(root, "input.pdf"), filepath.Join(root, "result.json"), filepath.Join(root, "ocr.ps1")
	if err = os.WriteFile(input, raw, 0600); err != nil {
		return nil, err
	}
	if err = os.WriteFile(script, pdfOCRScript, 0600); err != nil {
		return nil, err
	}
	env := []string{"TEMP=" + root, "TMP=" + root}
	for _, key := range []string{"SYSTEMROOT", "WINDIR", "USERPROFILE", "LOCALAPPDATA", "APPDATA"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	exe := filepath.Join(os.Getenv("SYSTEMROOT"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	timeout := 30 * time.Second * time.Duration(len(pages))
	if timeout > 90*time.Second {
		timeout = 90 * time.Second
	}
	if timeout < 30*time.Second {
		timeout = 30 * time.Second
	}
	outcome, err := commandworker.Run(ctx, commandworker.Spec{
		Exe: exe, Args: []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script, "-InputPath", input, "-OutputPath", output, "-PageIndexes", indexes, "-RenderOnly"},
		Dir: root, Env: env, Timeout: timeout, MaxMemoryBytes: 768 << 20, MaxOutputBytes: 4096,
	}, nil, nil)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	if outcome.TimedOut {
		return nil, errors.New("PDF 页面渲染超时")
	}
	if outcome.ExitCode != 0 {
		return nil, errors.New("PDF 页面渲染失败，请检查 Windows PDF 渲染支持")
	}
	f, err := os.Open(output)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 1<<20 {
		return nil, ErrBudgetExceeded
	}
	var manifest struct {
		Method string `json:"method"`
		Pages  []struct {
			Page int    `json:"page"`
			File string `json:"file"`
		} `json:"pages"`
	}
	if err = json.Unmarshal(body, &manifest); err != nil {
		return nil, err
	}
	if manifest.Method != "windows-render" || len(manifest.Pages) == 0 {
		return nil, fmt.Errorf("PDF 页面渲染结果不完整")
	}
	out := make([]RenderedPDFPage, 0, len(manifest.Pages))
	for _, page := range manifest.Pages {
		name := filepath.Base(page.File)
		if name == "." || name == "" || filepath.Ext(name) != ".png" {
			return nil, fmt.Errorf("PDF 页面渲染结果不完整")
		}
		png, readErr := os.ReadFile(filepath.Join(root, name))
		if readErr != nil {
			return nil, readErr
		}
		if len(png) == 0 || len(png) > MaxInputBytes {
			return nil, ErrBudgetExceeded
		}
		out = append(out, RenderedPDFPage{Page: page.Page, PNG: png})
	}
	return out, nil
}
