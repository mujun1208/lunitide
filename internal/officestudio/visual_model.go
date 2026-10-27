package officestudio

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/lunitide/lunitide/internal/doctext"
)

// maxVisualRenderPages bounds per-request page rasterization for visual QA.
// It matches MaxPptxSlides; larger imported PDFs are QA-sampled on the first
// pages instead of rendering unbounded page counts.
const maxVisualRenderPages = 30

type VisualModelRequest struct {
	Configured bool
	Pages      [][]byte
	Review     func(pages [][]byte) (issues []Issue, message string, err error)
}

func VisionModelConfigured() bool {
	return strings.TrimSpace(os.Getenv("LUNITIDE_OFFICE_VISION")) != ""
}

var visualReviewRunner func([][]byte) ([]Issue, string, error)
var visualPageImages func([]byte) ([][]byte, error)

func SetVisualReviewForTest(t interface{ Cleanup(func()) }, run func([][]byte) ([]Issue, string, error)) {
	prev := visualReviewRunner
	visualReviewRunner = run
	t.Cleanup(func() { visualReviewRunner = prev })
}

func SetVisualPageImagesForTest(t interface{ Cleanup(func()) }, run func([]byte) ([][]byte, error)) {
	prev := visualPageImages
	visualPageImages = run
	t.Cleanup(func() { visualPageImages = prev })
}

// ExtractVisualPages rasterizes a PDF into per-page PNG bytes. The production
// path parses the page tree, then renders bounded pages through the local
// doctext pipeline (Windows platform renderer); failures degrade the caller
// to an unsupported visual check instead of blocking delivery.
func ExtractVisualPages(ctx context.Context, pdf []byte) ([][]byte, error) {
	if visualPageImages != nil {
		return visualPageImages(pdf)
	}
	if len(pdf) == 0 {
		return nil, fmt.Errorf("no pdf")
	}
	tree, err := doctext.ExtractPDFPages(pdf)
	if err != nil || len(tree) == 0 {
		return nil, fmt.Errorf("pdf page tree unavailable")
	}
	if len(tree) > maxVisualRenderPages {
		tree = tree[:maxVisualRenderPages]
	}
	indexes := make([]int, len(tree))
	for i, page := range tree {
		indexes[i] = page.Page
	}
	rendered, err := doctext.RenderPDFPages(ctx, pdf, indexes)
	if err != nil {
		return nil, err
	}
	pages := make([][]byte, 0, len(rendered))
	for _, page := range rendered {
		pages = append(pages, page.PNG)
	}
	return pages, nil
}

func RunConfiguredVisualReview(pages [][]byte) ([]Issue, string, error) {
	if visualReviewRunner != nil {
		return visualReviewRunner(pages)
	}
	exe := strings.TrimSpace(os.Getenv("LUNITIDE_OFFICE_VISION"))
	if exe == "" {
		return nil, "", fmt.Errorf("unconfigured")
	}
	if len(pages) == 0 {
		return nil, "", fmt.Errorf("no pages")
	}
	dir, err := os.MkdirTemp("", "lunitide-vision-*")
	if err != nil {
		return nil, "", err
	}
	defer os.RemoveAll(dir)
	if _, err = WriteVisualReviewWorkspace(dir, pages); err != nil {
		return nil, "", err
	}
	cmd := exec.Command(exe, filepath.Join(dir, "manifest.json"))
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	outLimit := &limitWriter{buf: &stdout, max: VisualStdoutMaxBytes}
	errLimit := &limitWriter{buf: &stderr, max: VisualStderrMaxBytes}
	cmd.Stdout = outLimit
	cmd.Stderr = errLimit
	runErr := cmd.Run()
	exit := 0
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			return nil, "", runErr
		}
	}
	if outLimit.n > VisualStdoutMaxBytes {
		return nil, "", fmt.Errorf("visual: stdout exceeds 1 MiB")
	}
	out := stdout.String()
	if err = ValidateVisualProcess(exit, out, stderr.Bytes()); err != nil {
		return nil, "", err
	}
	result, err := ParseVisualReview(out, len(pages))
	if err != nil {
		return nil, "", err
	}
	issues := make([]Issue, 0, len(result.Issues))
	for _, item := range result.Issues {
		issues = append(issues, Issue{Code: item.Code, Severity: item.Severity, Message: item.Message, NodeID: item.NodeID})
	}
	return issues, fmt.Sprintf("视觉模型已检查 %d 页渲染图", len(pages)), nil
}

type limitWriter struct {
	buf *bytes.Buffer
	max int
	n   int
}

func (w *limitWriter) Write(p []byte) (int, error) {
	w.n += len(p)
	if w.buf.Len() < w.max {
		take := len(p)
		if remain := w.max - w.buf.Len(); take > remain {
			take = remain
		}
		_, _ = w.buf.Write(p[:take])
	}
	return len(p), nil
}

func VisualModelCheck(req VisualModelRequest) Check {
	if !req.Configured {
		return Check{ID: "visual-model", Status: "unsupported", Message: "未接入视觉模型，不能当作已校准视觉验收"}
	}
	if len(req.Pages) == 0 || req.Review == nil {
		return Check{ID: "visual-model", Status: "missing", Message: "已配置视觉模型但没有渲染图或检查函数，待验渲染图，不能当作已校准视觉验收"}
	}
	issues, message, err := req.Review(req.Pages)
	if err != nil {
		return Check{ID: "visual-model", Status: "missing", Message: "视觉模型调用失败，不能当作已完成视觉检查"}
	}
	message = strings.TrimSpace(message)
	if len(issues) > 0 {
		if message == "" {
			message = issues[0].Message
		}
		return Check{ID: "visual-model", Status: "failed", Message: message}
	}
	if message == "" {
		message = "视觉模型已检查渲染图"
	}
	return Check{ID: "visual-model", Status: "passed", Message: message + "；规则分仍未校准，visualScore=uncalibrated"}
}
