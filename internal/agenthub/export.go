package agenthub

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var exportSkipExts = map[string]struct{}{
	".go": {}, ".ts": {}, ".tsx": {}, ".js": {}, ".py": {},
}

var exportAllowExts = map[string]struct{}{
	".pptx": {}, ".pdf": {}, ".docx": {}, ".xlsx": {}, ".zip": {},
}

func CopyThreadExport(workspace, exportDir string) error {
	if exportDir == "" {
		return nil
	}
	return filepath.WalkDir(workspace, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if path != workspace {
				if skipScanDir(d.Name()) {
					return fs.SkipDir
				}
				if d.Type()&os.ModeSymlink != 0 {
					return fs.SkipDir
				}
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if _, skip := exportSkipExts[ext]; skip {
			return nil
		}
		if _, allow := exportAllowExts[ext]; !allow {
			return nil
		}
		if !PathAllowed(workspace, exportDir, path) {
			return nil
		}
		dest := filepath.Join(exportDir, filepath.Base(path))
		if !PathAllowed(workspace, exportDir, dest) {
			return nil
		}
		if sameExportPath(path, dest) {
			return nil
		}
		return copyRegularFile(path, dest)
	})
}

func sameExportPath(src, dest string) bool {
	absSrc, errSrc := filepath.Abs(src)
	absDest, errDest := filepath.Abs(dest)
	if errSrc != nil || errDest != nil {
		return false
	}
	return absSrc == absDest
}
