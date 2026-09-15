package officestudio

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func TestWriteVisualReviewWorkspaceWritesEveryPage(t *testing.T) {
	dir := t.TempDir()
	pages := [][]byte{[]byte("page-a"), []byte("page-b"), []byte("page-c")}
	man, err := WriteVisualReviewWorkspace(dir, pages)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "page.bin")); err == nil {
		t.Fatal("must not write only page.bin")
	}
	for i := range pages {
		name := filepath.Join(dir, visualPageFileName(i))
		got, readErr := os.ReadFile(name)
		if readErr != nil || string(got) != string(pages[i]) {
			t.Fatalf("page %d missing: %v %q", i, readErr, got)
		}
	}
	if err = ValidateVisualManifest(man, 3); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored VisualManifest
	if err = json.Unmarshal(raw, &stored); err != nil || stored.PageCount != 3 {
		t.Fatalf("manifest: %v %#v", err, stored)
	}
}

func TestRunConfiguredVisualReviewRejectsExitZeroEmptyJSON(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "vision.cmd")
	if err := os.WriteFile(exe, []byte("@echo off\r\nexit /b 0\r\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LUNITIDE_OFFICE_VISION", exe)
	_, _, err := RunConfiguredVisualReview([][]byte{[]byte("p0"), []byte("p1")})
	if err == nil {
		t.Fatal("exit 0 without JSON must fail")
	}
}

func TestRunConfiguredVisualReviewRequiresAllReviewedPages(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "stdout.json")
	if err := os.WriteFile(out, []byte(`{"reviewer":"fixture","version":"1","reviewedPages":[0],"issues":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "vision.cmd")
	if err := os.WriteFile(exe, []byte("@echo off\r\ntype \""+out+"\"\r\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LUNITIDE_OFFICE_VISION", exe)
	_, _, err := RunConfiguredVisualReview([][]byte{[]byte("p0"), []byte("p1")})
	if err == nil {
		t.Fatal("JSON that reviews only page 0 must fail")
	}
}
