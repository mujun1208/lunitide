package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestAppendLocalFileReadsMissingPath(t *testing.T) {
	got := appendLocalFileReads([]llmadapter.Message{{
		Role:    llmadapter.RoleUser,
		Content: `请读这个文件 C:\No\Such\需求.md`,
	}})
	if !strings.Contains(got[0].Content, "[已读取本地文件]") || !strings.Contains(got[0].Content, "路径不存在") {
		t.Fatalf("missing path note = %q", got[0].Content)
	}
}

func TestAppendLocalFileReadsExistingMarkdown(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive-letter paths")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "AI 销售助手.md")
	if err := os.WriteFile(path, []byte("# 需求\n支持 PDF 上传"), 0600); err != nil {
		t.Fatal(err)
	}
	got := appendLocalFileReads([]llmadapter.Message{{
		Role:    llmadapter.RoleUser,
		Content: "按这个路径做：" + path,
	}})
	if !strings.Contains(got[0].Content, "支持 PDF 上传") || !strings.Contains(got[0].Content, path) {
		t.Fatalf("read note = %q", got[0].Content)
	}
}
