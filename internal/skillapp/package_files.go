package skillapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

const packageMaxBytes = 64 << 20

// PackageFilePath is portable to Windows; a package can never select an
// absolute path, alternate data stream, device name, or parent directory.
func PackageFilePath(name string) bool {
	return skill.PackageFilePath(name)
}

// PackageFiles projects the actual stored agreement and version-bound shipped
// resources. It does not read arbitrary model-supplied filesystem locations.
func PackageFiles(sk skill.Skill) (map[string][]byte, error) {
	var manifest struct {
		Prompt string            `json:"prompt"`
		Files  map[string]string `json:"files"`
	}
	if err := json.Unmarshal([]byte(sk.ManifestJSON), &manifest); err != nil {
		return nil, fmt.Errorf("invalid skill manifest: %w", err)
	}
	files, err := BundledPackageFiles(sk)
	if err != nil {
		return nil, err
	}
	if files == nil {
		files = map[string][]byte{}
	}
	if source, exists := files["SKILL.md"]; exists && string(source) != manifest.Prompt {
		files["upstream/SKILL.md"] = source
	}
	for name, content := range manifest.Files {
		if name == "manifest.json" || name == "SKILL.md" {
			return nil, errors.New("manifest.files cannot replace SKILL.md or manifest.json; edit manifest.prompt instead")
		}
		if _, collision := files[name]; collision {
			return nil, fmt.Errorf("skill resource already exists: %s", name)
		}
		files[name] = []byte(content)
	}
	if strings.TrimSpace(manifest.Prompt) != "" {
		files["SKILL.md"] = []byte(manifest.Prompt)
	}
	files["manifest.json"] = []byte(sk.ManifestJSON)
	if len(files) > 4096 {
		return nil, errors.New("skill package exceeds 4096 files")
	}
	total := 0
	seen := map[string]bool{}
	for name, raw := range files {
		if !PackageFilePath(name) || seen[strings.ToLower(name)] {
			return nil, fmt.Errorf("invalid or colliding skill resource path: %s", name)
		}
		seen[strings.ToLower(name)] = true
		total += len(raw)
		if total > packageMaxBytes {
			return nil, errors.New("skill package exceeds 64 MiB")
		}
	}
	for name := range files {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if seen[strings.ToLower(parent)] {
				return nil, errors.New("skill package file/directory conflict")
			}
		}
	}
	return files, nil
}

func PackageRevision(sk skill.Skill, files map[string][]byte) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00%s\x00", sk.ID, sk.Rev, sk.Version)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(h, "%d:%s:%d:", len(name), name, len(files[name]))
		h.Write(files[name])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// MaterializePackage creates a versioned, inspectable copy. Existing bytes are
// verified, never overwritten. os.Root confines both files and parent symlinks.
func MaterializePackage(rootDir string, sk skill.Skill, files map[string][]byte) (string, string, error) {
	packageStorageMu.Lock()
	defer packageStorageMu.Unlock()
	if rootDir == "" {
		return "", "", errors.New("skill package storage unavailable")
	}
	if err := sk.Validate(); err != nil {
		return "", "", err
	}
	revision := PackageRevision(sk, files)
	if err := os.MkdirAll(rootDir, 0700); err != nil {
		return "", "", err
	}
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return "", "", err
	}
	defer root.Close()
	relRoot := filepath.Join(sk.ID, revision)
	if err = root.MkdirAll(relRoot, 0700); err != nil {
		return "", "", err
	}
	for name, raw := range files {
		if !PackageFilePath(name) {
			return "", "", errors.New("invalid skill package path")
		}
		rel := filepath.Join(relRoot, filepath.FromSlash(name))
		if err = root.MkdirAll(filepath.Dir(rel), 0700); err != nil {
			return "", "", err
		}
		f, createErr := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(createErr, os.ErrExist) {
			stored, readErr := root.ReadFile(rel)
			if readErr != nil {
				return "", "", readErr
			}
			if sha256.Sum256(stored) != sha256.Sum256(raw) {
				return "", "", errors.New("skill package file changed on disk; stored agreement remains unchanged")
			}
			continue
		}
		if createErr != nil {
			return "", "", createErr
		}
		_, writeErr := f.Write(raw)
		closeErr := f.Close()
		if writeErr != nil {
			_ = root.Remove(rel)
			return "", "", writeErr
		}
		if closeErr != nil {
			_ = root.Remove(rel)
			return "", "", closeErr
		}
	}
	abs, err := filepath.Abs(filepath.Join(rootDir, relRoot))
	return abs, revision, err
}

// CheckPackageFile verifies just the selected immutable file after initial
// materialization. Paged reads must not reread thousands of unrelated files.
func CheckPackageFile(rootDir string, sk skill.Skill, files map[string][]byte, name string) (string, string, error) {
	if rootDir == "" || !PackageFilePath(name) {
		return "", "", errors.New("invalid skill package location")
	}
	if err := sk.Validate(); err != nil {
		return "", "", err
	}
	expected, exists := files[name]
	if !exists {
		return "", "", errors.New("skill package file not found")
	}
	revision := PackageRevision(sk, files)
	relRoot := filepath.Join(sk.ID, revision)
	abs, err := filepath.Abs(filepath.Join(rootDir, relRoot))
	if err != nil {
		return "", "", err
	}
	// A first direct file read may arrive before the directory has been opened.
	if _, statErr := os.Stat(abs); errors.Is(statErr, os.ErrNotExist) {
		return MaterializePackage(rootDir, sk, files)
	} else if statErr != nil {
		return "", "", statErr
	}
	packageStorageMu.Lock()
	defer packageStorageMu.Unlock()
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return "", "", err
	}
	defer root.Close()
	f, err := root.Open(filepath.Join(relRoot, filepath.FromSlash(name)))
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", "", err
	}
	if !info.Mode().IsRegular() || info.Size() != int64(len(expected)) {
		return "", "", errors.New("skill package file changed on disk")
	}
	stored, err := io.ReadAll(io.LimitReader(f, int64(len(expected))+1))
	if err != nil {
		return "", "", err
	}
	if sha256.Sum256(stored) != sha256.Sum256(expected) {
		return "", "", errors.New("skill package file changed on disk")
	}
	return abs, revision, nil
}
