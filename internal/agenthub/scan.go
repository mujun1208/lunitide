package agenthub

import (
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const inboxDirName = ".agenthub-inbox"

const promptFileName = ".agenthub-prompt.txt"

var skipScanDirNames = []string{
	"node_modules", ".git", ".hg", ".svn", "dist", "build", "out", "coverage",
	".venv", "venv", "__pycache__", ".cursor", ".kimi-code", ".codex", "vendor", ".idea", ".vs",
}

func skipScanDir(name string) bool {
	for _, item := range skipScanDirNames {
		if strings.EqualFold(name, item) {
			return true
		}
	}
	return false
}

func ScanWorkDir(workDir string, eventPaths []string, startedAt time.Time) []Artifact {
	workDir = filepath.Clean(workDir)
	seen := map[string]Artifact{}
	for _, raw := range eventPaths {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		abs := raw
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(workDir, raw)
		}
		abs = filepath.Clean(abs)
		art := fileArtifact(abs)
		if insideDir(workDir, abs) {
			art.Source = "event"
		} else {
			art.Source = "outside"
		}
		seen[abs] = art
	}
	_ = filepath.WalkDir(workDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if path != workDir {
				if skipScanDir(d.Name()) {
					return fs.SkipDir
				}
				if d.Type()&os.ModeSymlink != 0 {
					return fs.SkipDir
				}
			}
			return nil
		}
		if d.Name() == promptFileName {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			return nil
		}
		abs := filepath.Clean(path)
		if _, ok := seen[abs]; ok {
			return nil
		}
		art := fileArtifact(abs)
		art.Size = info.Size()
		art.Source = scanSource(workDir, abs, info, startedAt)
		seen[abs] = art
		return nil
	})
	out := make([]Artifact, 0, len(seen))
	for _, art := range seen {
		out = append(out, art)
	}
	return out
}

func scanSource(workDir, abs string, info fs.FileInfo, startedAt time.Time) string {
	if inboxRel(workDir, abs) {
		return "inbox"
	}
	if !startedAt.IsZero() && info.ModTime().After(startedAt.Add(-2*time.Second)) {
		return "changed"
	}
	return "scan"
}

func inboxRel(workDir, abs string) bool {
	abs = filepath.Clean(abs)
	if workDir != "" {
		workDir = filepath.Clean(workDir)
		rel, err := filepath.Rel(workDir, abs)
		if err != nil {
			return false
		}
		rel = filepath.ToSlash(rel)
		if rel == ".." || strings.HasPrefix(rel, "../") {
			return false
		}
		first, _, _ := strings.Cut(rel, "/")
		return strings.EqualFold(first, inboxDirName)
	}
	slash := filepath.ToSlash(abs)
	if strings.Contains(slash, "/"+inboxDirName+"/") {
		return true
	}
	return strings.HasPrefix(slash, inboxDirName+"/") || slash == inboxDirName
}

func persistableSource(source string) string {
	switch source {
	case "inbox", "changed":
		return "scan"
	default:
		return source
	}
}

func rematerializeSource(workDir, path, stored string, startedAt time.Time) string {
	if inboxRel(workDir, path) {
		return "inbox"
	}
	if stored == "event" {
		return "event"
	}
	if stored == "outside" {
		return "outside"
	}
	if !startedAt.IsZero() {
		if info, err := os.Stat(path); err == nil && info.ModTime().After(startedAt.Add(-2*time.Second)) {
			return "changed"
		}
	}
	return "scan"
}

func decorateArtifacts(workDir, startedAt string, arts []Artifact) []Artifact {
	started := parseRFC3339(startedAt)
	for i := range arts {
		arts[i].Source = rematerializeSource(workDir, arts[i].Path, arts[i].Source, started)
	}
	return arts
}

func parseRFC3339(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

func fileArtifact(path string) Artifact {
	info, err := os.Stat(path)
	size := int64(0)
	if err == nil {
		size = info.Size()
	}
	ext := strings.ToLower(filepath.Ext(path))
	return Artifact{
		Name: filepath.Base(path),
		Path: path,
		Size: size,
		MIME: mime.TypeByExtension(ext),
	}
}

func insideDir(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	cursor := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		cursor = filepath.Join(cursor, part)
		step, stepErr := filepath.Rel(root, resolvedLocation(cursor))
		if stepErr != nil || step == ".." || strings.HasPrefix(step, ".."+string(filepath.Separator)) {
			return false
		}
	}
	return true
}

func resolvedLocation(path string) string {
	if target, err := os.Readlink(path); err == nil && strings.TrimSpace(target) != "" {
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		return filepath.Clean(target)
	}
	if eval, err := filepath.EvalSymlinks(path); err == nil {
		return eval
	}
	return filepath.Clean(path)
}
