package toolruntime

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestVoiceFilenameSpacingAndImageSuffix(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"企业AI智能助手.txt", "飞算AI.png", "无关图片.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for query, want := range map[string]string{"企业 AI 智能助手": "企业AI智能助手.txt", "飞算 AI": "飞算AI.png"} {
		path, _, err := pickDesktopNamedFile(dir, query)
		if err != nil || filepath.Base(path) != want {
			t.Fatalf("%s => %s %v", query, path, err)
		}
	}
	for _, query := range []string{"飞算 AI 的图片。", "打开桌面的飞算 AI 的图片"} {
		candidates := desktopQueryCandidates(query)
		if !slices.Contains(candidates, "飞算 AI ") && !slices.Contains(candidates, "飞算 AI") {
			t.Fatal(candidates)
		}
		if slices.Contains(candidates, "图片") {
			t.Fatal("generic suffix could open unrelated picture", candidates)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "企业 AI 智能助手.txt"), []byte("another"), 0600); err != nil {
		t.Fatal(err)
	}
	path, names, err := pickDesktopNamedFile(dir, "企业  AI 智能助手")
	if err != nil || path != "" || len(names) != 2 {
		t.Fatalf("ambiguous normalized names must not open: %s %v %v", path, names, err)
	}
}
