package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const skillDocumentMaxBytes = 1 << 20

// Read one complete reference with a bound. No prefix truncation and no UTF-8
// repair: a partial/corrupted agreement is not an executable instruction set.
func readLocalSkillDocument(roots, keys []string, rel string) (string, string, bool, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.Contains(rel, "..") || strings.Contains(rel, ":") {
		return "", "", false, errors.New("invalid skill reference path")
	}
	rel = filepath.Clean(rel)
	for _, root := range roots {
		for _, key := range keys {
			if root == "" || key == "" || strings.Contains(key, "..") || strings.ContainsAny(key, `/\`) {
				continue
			}
			base := filepath.Join(root, ".agents", "skills", key)
			path := filepath.Join(base, rel)
			info, err := os.Lstat(path)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return "", "", false, err
			}
			if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return "", "", false, errors.New("skill reference is not a regular file")
			}
			// Check resolved parents too; a directory junction/symlink must not make
			// a reference escape its selected skill directory.
			realBase, err := filepath.EvalSymlinks(base)
			if err != nil {
				return "", "", false, err
			}
			realPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				return "", "", false, err
			}
			realRel, err := filepath.Rel(realBase, realPath)
			if err != nil || realRel == ".." || strings.HasPrefix(realRel, ".."+string(filepath.Separator)) || filepath.IsAbs(realRel) {
				return "", "", false, errors.New("skill reference escapes its directory")
			}
			if info.Size() > skillDocumentMaxBytes {
				return "", "", false, errors.New("技能参考文件超过 1 MiB；未读取不完整前缀，请拆分文件后重试")
			}
			f, err := os.Open(realPath)
			if err != nil {
				return "", "", false, err
			}
			raw, readErr := io.ReadAll(io.LimitReader(f, skillDocumentMaxBytes+1))
			closeErr := f.Close()
			if readErr != nil {
				return "", "", false, readErr
			}
			if closeErr != nil {
				return "", "", false, closeErr
			}
			if len(raw) > skillDocumentMaxBytes {
				return "", "", false, errors.New("技能参考文件读取期间超出 1 MiB；未返回不完整内容")
			}
			if !utf8.Valid(raw) || strings.IndexByte(string(raw), 0) >= 0 {
				return "", "", false, errors.New("技能参考文件不是有效 UTF-8 文本")
			}
			return string(raw), realPath, true, nil
		}
	}
	return "", "", false, nil
}
