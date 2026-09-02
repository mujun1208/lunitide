package people

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/oklog/ulid/v2"
)

func (s *Service) DecideFile(ctx context.Context, offerID string, accept bool) (FileOffer, error) {
	if err := s.readyUnlocked(); err != nil {
		return FileOffer{}, err
	}
	offer, err := s.store.GetOffer(ctx, offerID)
	if err != nil {
		return FileOffer{}, ErrNotFound
	}
	if offer.Status != "pending" {
		return FileOffer{}, ErrOfferDecided
	}
	now := nowRFC3339()
	if !accept {
		_ = os.Remove(offer.StagingPath)
		if err := s.store.DecideOffer(ctx, offerID, "rejected", "", now); err != nil {
			return FileOffer{}, err
		}
		offer.Status = "rejected"
		offer.DecidedAt = now
		offer.DestPath = ""
		return offer, nil
	}
	if s.receiveDir == "" {
		return FileOffer{}, fmt.Errorf("receive directory missing")
	}
	if err := os.MkdirAll(s.receiveDir, 0o700); err != nil {
		return FileOffer{}, err
	}
	safe := sanitizeName(offer.FileName)
	dest := filepath.Join(s.receiveDir, offer.OfferID+"-"+safe)
	if offer.StagingPath != "" {
		if err := copyFile(offer.StagingPath, dest); err != nil {
			return FileOffer{}, err
		}
		_ = os.Remove(offer.StagingPath)
	}
	if err := s.store.DecideOffer(ctx, offerID, "accepted", dest, now); err != nil {
		return FileOffer{}, err
	}
	offer.Status = "accepted"
	offer.DestPath = dest
	offer.DecidedAt = now
	return offer, nil
}

func decodeFile(b64 string) ([]byte, error) {
	b64 = strings.TrimSpace(b64)
	if i := strings.Index(b64, ","); i >= 0 && strings.Contains(b64[:i], "base64") {
		b64 = b64[i+1:]
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(b64)
	}
	return raw, err
}

func sanitizeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/|?*`, r) {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	if utf8.RuneCountInString(name) > 80 {
		runes := []rune(name)
		name = string(runes[:80])
	}
	return name
}

func (s *Service) StageFile(ctx context.Context, in StageInput) (StageResult, error) {
	if err := s.readyUnlocked(); err != nil {
		return StageResult{}, err
	}
	id := strings.TrimSpace(in.UploadID)
	if len(id) != 26 {
		return StageResult{}, ErrInvalid
	}
	raw, err := decodeFile(in.ContentBase64)
	if err != nil || len(raw) == 0 {
		return StageResult{}, ErrInvalid
	}
	if s.stagingDir == "" {
		return StageResult{}, fmt.Errorf("staging directory missing")
	}
	if err := os.MkdirAll(s.stagingDir, 0o700); err != nil {
		return StageResult{}, err
	}
	s.mu.Lock()
	up := s.uploads[id]
	if up == nil {
		path := filepath.Join(s.stagingDir, "up-"+id)
		f, openErr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if openErr != nil {
			s.mu.Unlock()
			return StageResult{}, openErr
		}
		up = &fileUpload{name: in.FileName, mime: in.FileMIME, path: path, file: f}
		s.uploads[id] = up
	}
	s.mu.Unlock()
	up.mu.Lock()
	defer up.mu.Unlock()
	if up.size+int64(len(raw)) > maxFileBytes {
		return StageResult{}, ErrTooLarge
	}
	if _, err := up.file.Write(raw); err != nil {
		return StageResult{}, err
	}
	up.size += int64(len(raw))
	if !in.Last {
		return StageResult{Bytes: up.size}, nil
	}
	_ = up.file.Close()
	up.file = nil
	s.mu.Lock()
	delete(s.uploads, id)
	s.mu.Unlock()
	return StageResult{Ready: true, LocalPath: up.path, Bytes: up.size}, nil
}

func (s *Service) PickFile(folder bool) (PickResult, error) {
	if err := s.readyUnlocked(); err != nil {
		return PickResult{}, err
	}
	path, err := pickLocalPath(folder)
	if err != nil {
		return PickResult{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return PickResult{}, err
	}
	return PickResult{Path: path, FileName: info.Name(), Size: info.Size(), Directory: info.IsDir()}, nil
}

func (s *Service) OpenFile(destPath string) (string, error) {
	if err := s.readyUnlocked(); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(strings.TrimSpace(destPath))
	if err != nil || abs == "" {
		return "", ErrInvalid
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", ErrInvalid
	}
	if info.IsDir() {
		return "", ErrInvalid
	}
	if !pathUnderRoot(s.receiveDir, abs) && !pathUnderRoot(s.stagingDir, abs) {
		return "", ErrInvalid
	}
	if err := openPathFn(abs); err != nil {
		return "", ErrOpenFailed
	}
	return abs, nil
}

func ReplaceOpenPathForTest(fn func(string) error) func() {
	prev := openPathFn
	openPathFn = fn
	return func() { openPathFn = prev }
}

func pathUnderRoot(root, target string) bool {
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return false
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return false
	}
	return true
}

func (s *Service) materializeFile(in SendInput) (string, int64, string, error) {
	if s.stagingDir == "" {
		return "", 0, "", fmt.Errorf("staging directory missing")
	}
	if err := os.MkdirAll(s.stagingDir, 0o700); err != nil {
		return "", 0, "", err
	}
	stage := filepath.Join(s.stagingDir, ulid.Make().String())
	srcPath := strings.TrimSpace(in.LocalPath)
	if srcPath != "" {
		info, err := os.Stat(srcPath)
		if err != nil {
			return "", 0, "", ErrInvalid
		}
		if info.IsDir() {
			zipPath := stage + ".zip"
			if err := zipDirectory(srcPath, zipPath, maxFileBytes); err != nil {
				return "", 0, "", err
			}
			// The zip is itself the staging payload; the caller consumes the
			// returned path, so there is nothing to clean up here.
			stage = zipPath
		} else {
			if err := copyFileLimited(srcPath, stage, maxFileBytes); err != nil {
				return "", 0, "", err
			}
		}
		sum, size, err := hashFile(stage)
		return stage, size, sum, err
	}
	raw, err := decodeFile(in.ContentBase64)
	if err != nil {
		return "", 0, "", err
	}
	if len(raw) == 0 || int64(len(raw)) > maxFileBytes {
		return "", 0, "", ErrTooLarge
	}
	if err := os.WriteFile(stage, raw, 0o600); err != nil {
		return "", 0, "", err
	}
	sum := sha256.Sum256(raw)
	return stage, int64(len(raw)), hex.EncodeToString(sum[:]), nil
}

func copyFile(src, dest string) error { return copyFileLimited(src, dest, maxFileBytes) }

func copyFileLimited(src, dest string, limit int64) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer out.Close()
	n, err := io.Copy(out, io.LimitReader(in, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		_ = os.Remove(dest)
		return ErrTooLarge
	}
	return nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
