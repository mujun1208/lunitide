package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

// localAbsPath matches a Windows path the user typed, including spaces and
// Chinese names. The tail is trimmed before the extension is checked.
var localAbsPath = regexp.MustCompile(`(?i)[a-z]:[\\/][^\r\n:*?"<>|]+`)

var localReadableExt = map[string]bool{
	".txt": true, ".md": true, ".markdown": true, ".csv": true, ".json": true,
	".html": true, ".xml": true, ".log": true, ".pdf": true, ".docx": true,
	".xlsx": true, ".pptx": true, ".png": true, ".jpg": true, ".jpeg": true,
	".gif": true, ".webp": true, ".bmp": true,
}

const (
	localPathReadMaxFiles = 4
	localPathReadMaxBytes = 8 << 20
	localPathReadMaxRunes = 6000
)

// appendLocalFileReads puts the text of absolute paths in the latest user
// message into that message. The model then has the document even when a
// later workspace.read call is refused or aimed at the wrong directory.
func appendLocalFileReads(messages []llmadapter.Message) []llmadapter.Message {
	idx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llmadapter.RoleUser && strings.TrimSpace(messages[i].Content) != "" {
			idx = i
			break
		}
	}
	if idx < 0 || strings.Contains(messages[idx].Content, "[已读取本地文件]") {
		return messages
	}
	note := readLocalPaths(messages[idx].Content)
	if note == "" {
		return messages
	}
	messages[idx].Content = strings.TrimRight(messages[idx].Content, "\n") + "\n\n" + note
	return messages
}

func readLocalPaths(text string) string {
	seen := map[string]bool{}
	var b strings.Builder
	n := 0
	for _, raw := range localAbsPath.FindAllString(text, 12) {
		if n >= localPathReadMaxFiles {
			break
		}
		cleaned := strings.TrimRight(strings.TrimSpace(raw), " \t.,;，。；、)）]】\"'`")
		if cleaned == "" || len(cleaned) > 1024 {
			continue
		}
		ext := strings.ToLower(filepath.Ext(cleaned))
		if !localReadableExt[ext] {
			continue
		}
		abs, err := filepath.Abs(filepath.Clean(cleaned))
		if err != nil || seen[abs] {
			continue
		}
		seen[abs] = true
		n++
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(readOneLocalPath(abs, ext))
	}
	if b.Len() == 0 {
		return ""
	}
	return "[已读取本地文件]\n下面是用户给出的路径对应的文件内容。直接根据正文回答。不要再让用户粘贴正文、改名或猜测看不见的字符。\n\n" + b.String()
}

func readOneLocalPath(path, ext string) string {
	info, err := os.Lstat(path)
	if err != nil {
		return "路径不存在，系统找不到这个文件：\n" + path
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "这个路径不是可读取的文件：\n" + path
	}
	if info.Size() > localPathReadMaxBytes {
		return "文件超过 8 MiB，没有整份读入：\n" + path
	}
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return "图片文件在这个路径，可以当作参考：\n" + path
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "路径存在，但读取失败：\n" + path
	}
	extracted, err := doctext.Extract(path, raw, "")
	if err != nil || strings.TrimSpace(extracted.Text) == "" {
		if utf8.Valid(raw) && strings.TrimSpace(string(raw)) != "" {
			return "路径：\n" + path + "\n正文：\n" + trimRunes(string(raw), localPathReadMaxRunes)
		}
		return "文件在这个路径，但没有解析出正文：\n" + path
	}
	return "路径：\n" + path + "\n正文：\n" + trimRunes(extracted.Text, localPathReadMaxRunes)
}

func trimRunes(text string, limit int) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "\n…（后面还有，需要时用 workspace.read 的 offset 继续读这个绝对路径）"
}
