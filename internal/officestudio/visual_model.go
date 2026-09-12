package officestudio

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type VisualModelRequest struct {
	Configured bool
	Pages      [][]byte
	Review     func(pages [][]byte) (issues []Issue, message string, err error)
}

func VisionModelConfigured() bool {
	return strings.TrimSpace(os.Getenv("LUNITIDE_OFFICE_VISION")) != ""
}

var visualReviewRunner func([][]byte) ([]Issue, string, error)

func SetVisualReviewForTest(t interface{ Cleanup(func()) }, run func([][]byte) ([]Issue, string, error)) {
	prev := visualReviewRunner
	visualReviewRunner = run
	t.Cleanup(func() { visualReviewRunner = prev })
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
	src := filepath.Join(dir, "page.bin")
	if err = os.WriteFile(src, pages[0], 0600); err != nil {
		return nil, "", err
	}
	cmd := exec.Command(exe, src)
	cmd.Dir = dir
	if err = cmd.Run(); err != nil {
		return nil, "", err
	}
	return nil, "视觉模型已检查当前预览", nil
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
