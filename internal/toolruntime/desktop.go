package toolruntime

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// desktopWritePath resolves an office/html generator output. When desktop is
// set, the file lands in the real Windows Desktop (full-disk full-access
// required) and the caller should advertise a renderer-safe desktop/ preview
// path instead of a C:\ absolute path (those break the chat stream).
func (r *Runtime) desktopWritePath(requested, fallback, requiredExt string, desktop, unconfined bool) (string, error) {
	requested = strings.TrimSpace(requested)
	if !desktop {
		if requested == "" {
			return "", errors.New("relative path required")
		}
		return requested, nil
	}
	if r == nil || !unconfined || !r.FullDiskEnabled() {
		return "", errors.New("desktop=true requires full-disk full-access")
	}
	dir, err := userDesktopDir()
	if err != nil {
		return "", err
	}
	base := filepath.Base(requested)
	if base == "." || base == "" {
		base = fallback
	}
	ext := strings.ToLower(requiredExt)
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	if ext != "" && strings.ToLower(filepath.Ext(base)) != ext {
		base += ext
	}
	return filepath.Join(dir, base), nil
}

// desktopPreviewPath is the renderer-safe artifact name. Host/renderer
// reject backslashes, drive letters, and file:// URLs; desktop writes
// therefore preview as desktop/basename.ext.
func desktopPreviewPath(requested string, desktop bool, fallback string) string {
	base := filepath.Base(filepath.Clean(strings.TrimSpace(requested)))
	base = strings.ReplaceAll(base, `\`, "")
	if base == "" || base == "." || strings.Contains(base, "..") {
		base = fallback
	}
	if desktop {
		return "desktop/" + base
	}
	return filepath.ToSlash(base)
}

func userDesktopDir() (string, error) {
	candidates := desktopDirCandidates()
	if len(candidates) == 0 {
		var legacy []string
		if runtime.GOOS == "windows" {
			if p := os.Getenv("USERPROFILE"); p != "" {
				legacy = append(legacy, filepath.Join(p, "Desktop"), filepath.Join(p, "桌面"))
			}
		}
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			legacy = append(legacy, filepath.Join(home, "Desktop"), filepath.Join(home, "桌面"))
		}
		seen := map[string]bool{}
		for _, c := range legacy {
			if c == "" || seen[c] {
				continue
			}
			seen[c] = true
			if st, err := os.Stat(c); err == nil && st.IsDir() {
				candidates = append(candidates, c)
			}
		}
	}
	if len(candidates) == 0 {
		return "", errors.New("desktop folder not found")
	}
	return candidates[0], nil
}

type desktopHit struct {
	path  string
	base  string
	score int
}

func desktopNameScore(base, query string) int {
	q := strings.TrimSpace(query)
	if q == "" || utf8.RuneCountInString(q) > 200 {
		return 0
	}
	if strings.HasPrefix(base, "~$") || strings.HasPrefix(base, ".") {
		return 0
	}
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if strings.EqualFold(stem, q) || strings.EqualFold(base, q) {
		return 100
	}
	if utf8.RuneCountInString(q) < 2 {
		return 0
	}
	lowerStem := strings.ToLower(stem)
	lowerQ := strings.ToLower(q)
	if strings.HasPrefix(lowerStem, lowerQ) || strings.HasPrefix(stem, q) {
		return 80
	}
	if utf8.RuneCountInString(stem) >= 2 && (strings.HasPrefix(lowerQ, lowerStem) || strings.HasPrefix(q, stem)) {
		return 75
	}
	if strings.Contains(lowerStem, lowerQ) || strings.Contains(stem, q) {
		return 50
	}
	if strings.Contains(strings.ToLower(base), lowerQ) || strings.Contains(base, q) {
		return 40
	}
	return 0
}

// pickBestDesktopHit returns the unique best match from scored hits.
func pickBestDesktopHit(hits []desktopHit, query string) (string, []string, error) {
	if len(hits) == 0 {
		return "", nil, errors.New("no match for " + strings.TrimSpace(query))
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].base < hits[j].base
	})
	names := make([]string, len(hits))
	for i, h := range hits {
		names[i] = h.base
	}
	if len(hits) > 1 && hits[0].score == hits[1].score {
		return "", names, nil
	}
	return hits[0].path, names, nil
}

// pickDesktopNamedFile returns the unique best match on the real Desktop.
// Directories, shortcuts (.lnk), and apps (.exe) are included so voice
// commands like “打开汽水音乐” can launch desktop folders or shortcuts.
func pickDesktopNamedFile(dir, query string) (string, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, err
	}
	var hits []desktopHit
	for _, e := range entries {
		base := e.Name()
		score := desktopNameScore(base, query)
		if score <= 0 {
			continue
		}
		if e.IsDir() {
			score -= 2
			if score <= 0 {
				continue
			}
		}
		hits = append(hits, desktopHit{path: filepath.Join(dir, base), base: base, score: score})
	}
	return pickBestDesktopHit(hits, query)
}

// ProbeDesktopApp reports whether a named desktop client (微信 / QQ) can
// be launched on this PC. Settings uses it so enable fails loud.
func ProbeDesktopApp(name string) error {
	path, others, err := pickLaunchTarget(name)
	if err != nil {
		return err
	}
	if path == "" {
		return fmt.Errorf("多个程序匹配%s：%s", name, strings.Join(others, "、"))
	}
	return nil
}

// pickLaunchTarget resolves a desktop file/folder/shortcut, then falls
// back to known install paths (e.g. cloudmusic.exe) and Start Menu
// shortcuts (e.g. 汽水音乐 / 网易云音乐 when only installed globally).
func pickLaunchTarget(query string) (string, []string, error) {
	searches := desktopQueryCandidates(query)
	if len(searches) == 0 {
		core := launchQueryCore(query)
		if core == "" {
			core = strings.TrimSpace(query)
		}
		searches = []string{core}
	}
	if canon := CanonicalMusicAppFromText(query); canon != "" {
		searches = append([]string{canon}, searches...)
	}
	if known, ok := matchKnownLaunchApp(query); ok {
		if known.Canonical != searches[0] {
			searches = append([]string{known.Canonical}, searches...)
		}
		searches = append(searches, known.Aliases...)
	}
	if recalled := recallDesktopOpen(query); recalled != "" && looksLikeReopenQuery(query) {
		return recalled, nil, nil
	}
	if looksLikeGenericDocumentQuery(query) {
		if dir, err := userDesktopDir(); err == nil {
			names := listDesktopDocuments(dir)
			if len(names) == 1 {
				return filepath.Join(dir, names[0]), names, nil
			}
			if len(names) > 1 {
				return "", names, nil
			}
		}
		return "", nil, errors.New("无法执行：桌面上没有文档")
	}
	if dir, err := userDesktopDir(); err == nil {
		for _, q := range searches {
			path, others, err := pickDesktopNamedFile(dir, q)
			if path != "" {
				return path, others, nil
			}
			if len(others) > 1 {
				return "", others, err
			}
		}
	}
	for _, q := range searches {
		if path, ok := pickKnownAppExecutable(q); ok {
			return path, nil, nil
		}
	}
	for _, q := range searches {
		path, others, err := pickStartMenuShortcut(q)
		if path != "" || len(others) > 0 {
			return path, others, err
		}
	}
	if recalled := recallDesktopOpen(query); recalled != "" {
		return recalled, nil, nil
	}
	// Clear, spoken-friendly reason (Issue 6/7): the model relays this to the
	// user instead of a raw English "no match" string. It names what we looked
	// through (桌面 / 安装目录 / 开始菜单) so the user knows the app is simply not
	// found on this PC, not that the command was misunderstood.
	return "", nil, errors.New("无法执行：桌面、安装目录和开始菜单里都没找到「" + strings.TrimSpace(query) + "」。请确认它已安装，或把它的快捷方式放到桌面后再试")
}

func openWithDefaultApp(path string) error {
	err := startOpenedPath(path, func(cmd *exec.Cmd) error { return cmd.Start() })
	if err != nil {
		time.Sleep(350 * time.Millisecond)
		err = startOpenedPath(path, func(cmd *exec.Cmd) error { return cmd.Start() })
	}
	if err == nil {
		rememberDesktopOpen(path)
	}
	return err
}

var (
	lastDesktopOpenMu   sync.Mutex
	lastDesktopOpenPath string
)

func rememberDesktopOpen(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	lastDesktopOpenMu.Lock()
	lastDesktopOpenPath = path
	lastDesktopOpenMu.Unlock()
}

func looksLikeReopenQuery(query string) bool {
	q := foldLaunchQuery(launchQueryCore(query))
	if q == "" {
		q = foldLaunchQuery(query)
	}
	switch q {
	case "刚才", "那个", "刚才的", "那个文档", "刚才那个", "刚才的文档", "再打开", "重新打开":
		return true
	}
	return strings.Contains(q, "再打开") || strings.Contains(q, "重新打开") || strings.Contains(q, "再开一下")
}

func looksLikeGenericDocumentQuery(query string) bool {
	q := foldLaunchQuery(launchQueryCore(query))
	if q == "" {
		q = foldLaunchQuery(query)
	}
	switch q {
	case "文档", "文件", "桌面文档", "桌面文件":
		return true
	}
	return false
}

func listDesktopDocuments(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		base := e.Name()
		if strings.HasPrefix(base, "~$") || strings.HasPrefix(base, ".") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(base))
		switch ext {
		case ".docx", ".doc", ".xlsx", ".xls", ".pptx", ".ppt", ".pdf", ".txt", ".md":
			names = append(names, base)
		}
	}
	sort.Strings(names)
	return names
}

func recallDesktopOpen(query string) string {
	lastDesktopOpenMu.Lock()
	path := lastDesktopOpenPath
	lastDesktopOpenMu.Unlock()
	if path == "" {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	q := foldLaunchQuery(launchQueryCore(query))
	if q == "" {
		q = foldLaunchQuery(query)
	}
	if q == "" {
		return ""
	}
	base := strings.ToLower(filepath.Base(path))
	stem := strings.TrimSuffix(base, strings.ToLower(filepath.Ext(path)))
	if looksLikeReopenQuery(query) {
		return path
	}
	if strings.Contains(stem, q) || strings.Contains(base, q) || strings.Contains(q, stem) {
		return path
	}
	return ""
}

// startOpenedPath launches a Desktop/Start-Menu target. Windows .exe files
// must run from their install directory so DLLs next to cloudmusic.exe load;
// shortcuts stay on `start` so .lnk/.url resolve through the shell.
func startOpenedPath(path string, startFn func(*exec.Cmd) error) error {
	if startFn == nil {
		startFn = (*exec.Cmd).Start
	}
	if runtime.GOOS == "windows" {
		if strings.EqualFold(filepath.Ext(path), ".exe") {
			cmd := exec.Command(path)
			cmd.Dir = filepath.Dir(path)
			return startFn(cmd)
		}
		cmd := exec.Command("cmd", "/c", "start", "", path)
		return startFn(cmd)
	}
	return startFn(exec.Command("xdg-open", path))
}
