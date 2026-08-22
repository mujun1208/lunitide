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
	var candidates []string
	if runtime.GOOS == "windows" {
		if p := os.Getenv("USERPROFILE"); p != "" {
			candidates = append(candidates, filepath.Join(p, "Desktop"), filepath.Join(p, "桌面"))
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, "Desktop"), filepath.Join(home, "桌面"))
	}
	seen := map[string]bool{}
	for _, c := range candidates {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c, nil
		}
	}
	return "", errors.New("desktop folder not found")
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

// pickDesktopNamedFile returns the unique best match. When several files
// share the top score it returns an empty path plus the candidate names so
// the caller can refuse to open anything.
func pickDesktopNamedFile(dir, query string) (string, []string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, err
	}
	var hits []desktopHit
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		base := e.Name()
		score := desktopNameScore(base, query)
		if score <= 0 {
			continue
		}
		hits = append(hits, desktopHit{path: filepath.Join(dir, base), base: base, score: score})
	}
	if len(hits) == 0 {
		return "", nil, errors.New("no desktop file matching " + strings.TrimSpace(query))
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

func openWithDefaultApp(path string) error {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd", "/c", "start", "", path)
		return cmd.Start()
	}
	return exec.Command("xdg-open", path).Start()
}
