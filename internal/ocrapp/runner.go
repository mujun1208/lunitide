package ocrapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/doctext"
)

func runRapidOCR(ctx context.Context, packRoot string, raw []byte) (doctext.PDFOCRResult, error) {
	exe := FindPackExecutable(packRoot)
	if exe == "" {
		return doctext.PDFOCRResult{}, errors.New("PP-OCR 尚未安装")
	}
	dir, err := os.MkdirTemp("", "lunitide-ocr-*")
	if err != nil {
		return doctext.PDFOCRResult{}, err
	}
	defer os.RemoveAll(dir)
	img := filepath.Join(dir, "input.png")
	if err := os.WriteFile(img, raw, 0600); err != nil {
		return doctext.PDFOCRResult{}, err
	}
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, exe, "--image_path="+img, "--ensureAscii=1")
	cmd.Dir = filepath.Dir(exe)
	hideOCRWindow(cmd)
	out, err := cmd.CombinedOutput()
	if runCtx.Err() != nil {
		return doctext.PDFOCRResult{}, runCtx.Err()
	}
	if err != nil {
		return doctext.PDFOCRResult{}, fmt.Errorf("PP-OCR 识别失败: %w", err)
	}
	text, err := parseRapidOCROutput(out)
	if err != nil {
		return doctext.PDFOCRResult{}, err
	}
	return doctext.PDFOCRResult{Method: "ppocr", Pages: []doctext.OCRPage{{Page: 1, Text: text}}}, nil
}

type rapidOCRLine struct {
	Text string `json:"text"`
}

type rapidOCRResult struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

func parseRapidOCROutput(raw []byte) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", errors.New("PP-OCR 返回空结果")
	}
	// Some builds print a banner before the JSON object.
	if i := bytes.IndexByte(raw, '{'); i > 0 {
		raw = raw[i:]
	}
	var got rapidOCRResult
	if err := json.Unmarshal(raw, &got); err != nil {
		return "", fmt.Errorf("PP-OCR 结果无法解析")
	}
	if got.Code == 101 {
		return "", nil
	}
	if got.Code != 100 {
		return "", fmt.Errorf("PP-OCR 识别失败")
	}
	var lines []rapidOCRLine
	if err := json.Unmarshal(got.Data, &lines); err != nil {
		return "", fmt.Errorf("PP-OCR 结果无法解析")
	}
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		if text := strings.TrimSpace(line.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n"), nil
}
