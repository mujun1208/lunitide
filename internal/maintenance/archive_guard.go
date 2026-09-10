package maintenance

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type ProbeResult struct {
	Format string
	Files  int
	OK     bool
	Reason string
}

func RejectUnsafeArchivePath(name string) error {
	clean := filepath.ToSlash(strings.TrimSpace(name))
	if clean == "" || strings.HasPrefix(clean, "/") || strings.Contains(clean, "..") {
		return errors.New("archive path rejected")
	}
	return nil
}

func CheckManifestPaths(paths []string) error {
	for _, name := range paths {
		if err := RejectUnsafeArchivePath(name); err != nil {
			return err
		}
	}
	return nil
}

func RejectZipBomb(uncompressed, compressed int64) error {
	if compressed <= 0 {
		return errors.New("archive ratio rejected")
	}
	if uncompressed > 100<<20 && uncompressed/compressed > 100 {
		return errors.New("archive ratio rejected")
	}
	return nil
}

func Probe(directory string) (ProbeResult, error) {
	clean := filepath.ToSlash(strings.TrimSpace(directory))
	if clean == "" || strings.Contains(clean, "..") {
		return ProbeResult{}, errors.New("archive path rejected")
	}
	raw, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		return ProbeResult{Reason: err.Error()}, err
	}
	var m struct {
		Format string `json:"format"`
		Files  []any  `json:"files"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return ProbeResult{Reason: err.Error()}, err
	}
	if m.Format != "lunitide-directory-backup-v1" {
		return ProbeResult{Format: m.Format, Reason: "unknown format"}, errors.New("unknown backup format")
	}
	if err := CheckManifestPaths(manifestEntryPaths(m.Files)); err != nil {
		return ProbeResult{Format: m.Format, Files: len(m.Files), Reason: err.Error()}, err
	}
	return ProbeResult{Format: m.Format, Files: len(m.Files), OK: true}, nil
}

func manifestEntryPaths(files []any) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		switch row := file.(type) {
		case string:
			out = append(out, row)
		case map[string]any:
			if path, ok := row["path"].(string); ok {
				out = append(out, path)
			}
		}
	}
	return out
}
