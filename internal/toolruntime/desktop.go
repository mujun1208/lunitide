package toolruntime

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"
)

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

// pickLaunchTarget resolves a desktop file/folder/shortcut, then falls
// back to known install paths (e.g. cloudmusic.exe) and Start Menu
// shortcuts (e.g. 汽水音乐 / 网易云音乐 when only installed globally).
func pickLaunchTarget(query string) (string, []string, error) {
	core := launchQueryCore(query)
	if core == "" {
		core = strings.TrimSpace(query)
	}
	searches := []string{core}
	if known, ok := matchKnownLaunchApp(core); ok {
		if known.Canonical != core {
			searches = append(searches, known.Canonical)
		}
		searches = append(searches, known.Aliases...)
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
	if path, ok := pickKnownAppExecutable(core); ok {
		return path, nil, nil
	}
	for _, q := range searches {
		path, others, err := pickStartMenuShortcut(q)
		if path != "" || len(others) > 0 {
			return path, others, err
		}
	}
	return "", nil, errors.New("no desktop, install path, or start-menu item matching " + strings.TrimSpace(query))
}

func openWithDefaultApp(path string) error {
	return startOpenedPath(path, func(cmd *exec.Cmd) error { return cmd.Start() })
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
