package projectsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/projectapp"
)

const MaxCopyBytes = 4 * 1024 * 1024

type FileEntry struct {
	Rel    string `json:"rel"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
	Copied bool   `json:"copied"`
	Skip   string `json:"skip,omitempty"`
}

type Receipt struct {
	Version    int         `json:"version"`
	ProjectID  string      `json:"projectId"`
	SourceRoot string      `json:"sourceRoot"`
	DestPath   string      `json:"destPath"`
	At         string      `json:"at"`
	Files      []FileEntry `json:"files"`
}

func ValidateDest(root, dest string) (string, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(dest) == "" {
		return "", projectapp.ErrSyncInvalid
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", projectapp.ErrSyncInvalid
	}
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return "", projectapp.ErrSyncInvalid
	}
	info, err := os.Stat(absDest)
	if err != nil || !info.IsDir() {
		return "", projectapp.ErrSyncInvalid
	}
	if samePath(absRoot, absDest) || isAncestor(absDest, absRoot) || isAncestor(absRoot, absDest) {
		return "", projectapp.ErrSyncInvalid
	}
	return absDest, nil
}

func SyncTree(root, dest, projectID string) (Receipt, error) {
	absDest, err := ValidateDest(root, dest)
	if err != nil {
		return Receipt{}, err
	}
	absRoot, _ := filepath.Abs(root)
	receipt := Receipt{
		Version: 1, ProjectID: projectID, SourceRoot: absRoot, DestPath: absDest,
		At: time.Now().UTC().Format(time.RFC3339),
	}
	err = filepath.Walk(absRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(absRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if shouldSkip(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(absDest, filepath.FromSlash(rel)), 0o755)
		}
		entry := FileEntry{Rel: rel, Size: info.Size()}
		sum, copyErr := hashFile(path)
		if copyErr != nil {
			return copyErr
		}
		entry.Digest = sum
		if info.Size() > MaxCopyBytes {
			entry.Skip = "skipped-too-large"
			receipt.Files = append(receipt.Files, entry)
			return nil
		}
		destFile := filepath.Join(absDest, filepath.FromSlash(rel))
		if err = os.MkdirAll(filepath.Dir(destFile), 0o755); err != nil {
			return err
		}
		if err = copyFile(path, destFile); err != nil {
			return err
		}
		entry.Copied = true
		receipt.Files = append(receipt.Files, entry)
		return nil
	})
	if err != nil {
		return receipt, err
	}
	body, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return receipt, err
	}
	if err = os.WriteFile(filepath.Join(absDest, ".lunitide-sync.json"), body, 0o644); err != nil {
		return receipt, err
	}
	if err = os.MkdirAll(filepath.Join(absRoot, ".lunitide"), 0o755); err != nil {
		return receipt, err
	}
	_ = os.WriteFile(filepath.Join(absRoot, ".lunitide", "sync-receipt.json"), body, 0o644)
	return receipt, nil
}

func LoadReceipt(root string) (Receipt, error) {
	raw, err := os.ReadFile(filepath.Join(root, ".lunitide", "sync-receipt.json"))
	if err != nil {
		return Receipt{}, projectapp.ErrSyncRequired
	}
	var rec Receipt
	if json.Unmarshal(raw, &rec) != nil || rec.Version != 1 {
		return Receipt{}, projectapp.ErrSyncRequired
	}
	return rec, nil
}

func shouldSkip(rel string) bool {
	rel = strings.ToLower(rel)
	return strings.HasPrefix(rel, "node_modules/") || rel == "node_modules" ||
		strings.HasPrefix(rel, ".git/") || rel == ".git"
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func isAncestor(ancestor, child string) bool {
	ancestor, child = filepath.Clean(ancestor), filepath.Clean(child)
	if runtime.GOOS == "windows" {
		ancestor = strings.ToLower(ancestor)
		child = strings.ToLower(child)
	}
	rel, err := filepath.Rel(ancestor, child)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..")
}
