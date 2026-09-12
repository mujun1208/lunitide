package officestudio

import (
	"errors"
	"strings"
	"testing"
)

func TestVisualModelCheckUnsupportedWithoutConfig(t *testing.T) {
	c := VisualModelCheck(VisualModelRequest{})
	if c.ID != "visual-model" || c.Status != "unsupported" {
		t.Fatalf("%#v", c)
	}
	if strings.Contains(c.Message, "已校准") && !strings.Contains(c.Message, "不能当作已校准") {
		t.Fatalf("claimed calibrated: %q", c.Message)
	}
}

func TestVisualModelCheckDoesNotPassWithoutPages(t *testing.T) {
	c := VisualModelCheck(VisualModelRequest{Configured: true})
	if c.Status != "missing" {
		t.Fatalf("configured without pages must be missing: %#v", c)
	}
}

func TestVisualModelCheckPassesOnlyAfterReview(t *testing.T) {
	c := VisualModelCheck(VisualModelRequest{
		Configured: true,
		Pages:      [][]byte{[]byte("png")},
		Review: func([][]byte) ([]Issue, string, error) {
			return nil, "已检查 1 页渲染图", nil
		},
	})
	if c.Status != "passed" {
		t.Fatalf("%#v", c)
	}
	if !strings.Contains(c.Message, "uncalibrated") {
		t.Fatalf("must keep uncalibrated: %q", c.Message)
	}
}

func TestVisualModelCheckFailedIssuesStayFailed(t *testing.T) {
	c := VisualModelCheck(VisualModelRequest{
		Configured: true,
		Pages:      [][]byte{[]byte("png")},
		Review: func([][]byte) ([]Issue, string, error) {
			return []Issue{{Code: "overlap", NodeID: "n1", Message: "重叠"}}, "页 1 节点 n1 重叠", nil
		},
	})
	if c.Status != "failed" || !strings.Contains(c.Message, "n1") {
		t.Fatalf("%#v", c)
	}
}

func TestVisualModelCheckReviewErrorIsMissing(t *testing.T) {
	c := VisualModelCheck(VisualModelRequest{
		Configured: true,
		Pages:      [][]byte{[]byte("png")},
		Review:     func([][]byte) ([]Issue, string, error) { return nil, "", errors.New("timeout") },
	})
	if c.Status != "missing" {
		t.Fatalf("%#v", c)
	}
}
