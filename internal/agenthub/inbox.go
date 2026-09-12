package agenthub

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type InboxFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

const (
	maxIngestFiles      = 20
	maxIngestFileBytes  int64 = 104857600
	maxIngestTotalBytes int64 = 209715200
)

func (s *Service) Inbox(action, workDir, name string) (canceled bool, dir string, files []InboxFile, skipped []string, err error) {
	switch action {
	case "files":
		return s.inboxFiles(workDir)
	case "folder":
		return s.inboxFolder(workDir)
	case "list":
		return s.inboxList(workDir)
	case "drop":
		return s.inboxDrop(workDir, name)
	default:
		return false, "", nil, nil, fmt.Errorf("未知操作")
	}
}

func (s *Service) inboxFiles(workDir string) (bool, string, []InboxFile, []string, error) {
	dir, err := s.resolveWorkDir(workDir)
	if err != nil {
		return false, "", nil, nil, err
	}
	pick := s.PickFiles
	if pick == nil {
		return true, dir, []InboxFile{}, nil, nil
	}
	paths, err := pick()
	if errors.Is(err, ErrPickCanceled) {
		return true, dir, []InboxFile{}, nil, nil
	}
	if err != nil {
		return false, dir, nil, nil, err
	}
	if len(paths) == 0 {
		return true, dir, []InboxFile{}, nil, nil
	}
	inboxDir := filepath.Join(dir, inboxDirName)
	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		return false, dir, nil, nil, err
	}
	skipped, err := s.ingestFilePaths(inboxDir, paths)
	if err != nil {
		return false, dir, nil, skipped, err
	}
	files, err := listInbox(inboxDir)
	if err != nil {
		return false, dir, nil, skipped, err
	}
	if files == nil {
		files = []InboxFile{}
	}
	return false, dir, files, skipped, nil
}

func (s *Service) inboxFolder(workDir string) (bool, string, []InboxFile, []string, error) {
	dir, err := s.resolveWorkDir(workDir)
	if err != nil {
		return false, "", nil, nil, err
	}
	pick := s.PickFolder
	if pick == nil {
		return true, dir, []InboxFile{}, nil, nil
	}
	srcFolder, err := pick()
	if errors.Is(err, ErrPickCanceled) {
		return true, dir, []InboxFile{}, nil, nil
	}
	if err != nil {
		return false, dir, nil, nil, err
	}
	srcFolder = filepath.Clean(strings.TrimSpace(srcFolder))
	if srcFolder == "" {
		return true, dir, []InboxFile{}, nil, nil
	}
	inboxDir := filepath.Join(dir, inboxDirName)
	if err := os.MkdirAll(inboxDir, 0o755); err != nil {
		return false, dir, nil, nil, err
	}
	skipped, err := s.ingestFolder(inboxDir, srcFolder)
	if err != nil {
		return false, dir, nil, skipped, err
	}
	files, err := listInbox(inboxDir)
	if err != nil {
		return false, dir, nil, skipped, err
	}
	if files == nil {
		files = []InboxFile{}
	}
	return false, dir, files, skipped, nil
}

func (s *Service) inboxList(workDir string) (bool, string, []InboxFile, []string, error) {
	clean := filepath.Clean(workDir)
	if !filepath.IsAbs(clean) || forbiddenWorkDir(clean) {
		return false, "", nil, nil, fmt.Errorf("工作目录不受支持")
	}
	inboxDir := filepath.Join(clean, inboxDirName)
	if _, err := os.Stat(inboxDir); os.IsNotExist(err) {
		return false, clean, []InboxFile{}, nil, nil
	}
	files, err := listInbox(inboxDir)
	if err != nil {
		return false, clean, nil, nil, err
	}
	if files == nil {
		files = []InboxFile{}
	}
	return false, clean, files, nil, nil
}

func (s *Service) inboxDrop(workDir, name string) (bool, string, []InboxFile, []string, error) {
	clean := filepath.Clean(workDir)
	if !filepath.IsAbs(clean) || forbiddenWorkDir(clean) {
		return false, "", nil, nil, fmt.Errorf("工作目录不受支持")
	}
	abs, err := safeInboxPath(clean, name)
	if err != nil {
		return false, clean, nil, nil, err
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return false, clean, nil, nil, err
	}
	inboxDir := filepath.Join(clean, inboxDirName)
	files, err := listInbox(inboxDir)
	if err != nil {
		return false, clean, nil, nil, err
	}
	if files == nil {
		files = []InboxFile{}
	}
	return false, clean, files, nil, nil
}

func (s *Service) ingestFilePaths(inboxDir string, paths []string) ([]string, error) {
	var skipped []string
	var totalBytes int64
	var ingested int
	for _, raw := range paths {
		src := filepath.Clean(strings.TrimSpace(raw))
		if src == "" {
			continue
		}
		info, err := os.Stat(src)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if info.Name() == promptFileName {
			continue
		}
		name := info.Name()
		size := info.Size()
		if size > maxIngestFileBytes {
			skipped = append(skipped, fmt.Sprintf("%s 超过 100MB", name))
			continue
		}
		if ingested >= maxIngestFiles {
			skipped = append(skipped, "超过 20 个文件")
			continue
		}
		if totalBytes+size > maxIngestTotalBytes {
			skipped = append(skipped, "合计超过 200MB")
			continue
		}
		dest := uniqueInboxDest(inboxDir, name)
		if err := copyRegularFile(src, dest); err != nil {
			return skipped, err
		}
		totalBytes += size
		ingested++
	}
	return skipped, nil
}

func (s *Service) ingestFolder(inboxDir, srcFolder string) ([]string, error) {
	var skipped []string
	var totalBytes int64
	var ingested int
	folderBase := filepath.Base(srcFolder)
	destRoot := filepath.Join(inboxDir, folderBase)
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return nil, err
	}
	err := filepath.WalkDir(srcFolder, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if path != srcFolder {
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
		rel, relErr := filepath.Rel(srcFolder, path)
		if relErr != nil {
			return nil
		}
		dest := filepath.Clean(filepath.Join(inboxDir, folderBase, rel))
		if !insideDir(inboxDir, dest) {
			return nil
		}
		name := info.Name()
		size := info.Size()
		if size > maxIngestFileBytes {
			skipped = append(skipped, fmt.Sprintf("%s 超过 100MB", name))
			return nil
		}
		if ingested >= maxIngestFiles {
			skipped = append(skipped, "超过 20 个文件")
			return nil
		}
		if totalBytes+size > maxIngestTotalBytes {
			skipped = append(skipped, "合计超过 200MB")
			return nil
		}
		dest = uniqueInboxDest(filepath.Dir(dest), filepath.Base(dest))
		if err := copyRegularFile(path, dest); err != nil {
			return err
		}
		totalBytes += size
		ingested++
		return nil
	})
	return skipped, err
}

func listInbox(inboxRoot string) ([]InboxFile, error) {
	var files []InboxFile
	err := filepath.WalkDir(inboxRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if path != inboxRoot {
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
		rel, relErr := filepath.Rel(inboxRoot, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		files = append(files, InboxFile{
			Name: d.Name(),
			Path: rel,
			Size: info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func safeInboxPath(workDir, name string) (string, error) {
	inboxRoot := filepath.Join(workDir, inboxDirName)
	clean := filepath.Clean(strings.ReplaceAll(name, "/", string(filepath.Separator)))
	abs := filepath.Clean(filepath.Join(inboxRoot, clean))
	if !insideDir(inboxRoot, abs) {
		return "", fmt.Errorf("路径不受支持")
	}
	return abs, nil
}

func uniqueInboxDest(destDir, baseName string) string {
	candidate := filepath.Join(destDir, baseName)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}
	ext := filepath.Ext(baseName)
	stem := strings.TrimSuffix(baseName, ext)
	for n := 2; ; n++ {
		next := filepath.Join(destDir, fmt.Sprintf("%s (%d)%s", stem, n, ext))
		if _, err := os.Stat(next); os.IsNotExist(err) {
			return next
		}
	}
}

func copyRegularFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
