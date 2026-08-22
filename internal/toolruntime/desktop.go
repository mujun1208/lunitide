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
// back to Start Menu shortcuts (e.g. 汽水音乐 when only installed globally).
func pickLaunchTarget(query string) (string, []string, error) {
	if dir, err := userDesktopDir(); err == nil {
		path, others, err := pickDesktopNamedFile(dir, query)
		if path != "" {
			return path, others, nil
		}
		if len(others) > 0 {
			return "", others, err
		}
	}
	path, others, err := pickStartMenuShortcut(query)
	if path != "" || len(others) > 0 {
		return path, others, err
	}
	if err != nil {
		return "", nil, err
	}
	return "", nil, errors.New("no desktop or start-menu item matching " + strings.TrimSpace(query))
}

func openWithDefaultApp(path string) error {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd", "/c", "start", "", path)
		return cmd.Start()
	}
	return exec.Command("xdg-open", path).Start()
}
