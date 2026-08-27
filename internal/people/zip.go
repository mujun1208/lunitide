package people

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func zipDirectory(src, dest string, limit int64) error {
	info, err := os.Stat(src)
	if err != nil || !info.IsDir() {
		return ErrInvalid
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	var total int64
	err = filepath.Walk(src, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() {
			return nil
		}
		if fi.Size() < 0 {
			return nil
		}
		total += fi.Size()
		if total > limit {
			return ErrTooLarge
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || strings.HasPrefix(rel, "..") {
			return nil
		}
		w, err := zw.Create(rel)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, io.LimitReader(f, limit+1))
		_ = f.Close()
		return copyErr
	})
	if closeErr := zw.Close(); err == nil {
		err = closeErr
	}
	_ = out.Close()
	if err != nil {
		for i := 0; i < 8; i++ {
			if os.Remove(dest) == nil {
				break
			}
			if i < 7 {
				time.Sleep(20 * time.Millisecond)
			}
		}
	}
	return err
}
