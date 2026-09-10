//go:build windows

// Package maintenance implements offline whole-data-directory snapshots. Every
// entry point owns the same cross-process lock used by desktop and engine.
package maintenance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/workspace"
)

const format = "lunitide-directory-backup-v1"
const controlDir = ".maintenance"
const maxManifestBytes = 32 << 20
const maxEntries = 250000

type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	SQLite bool   `json:"sqlite,omitempty"`
}
type Manifest struct {
	Format      string    `json:"format"`
	SourceRoot  string    `json:"sourceRoot"`
	CreatedAt   time.Time `json:"createdAt"`
	Directories []string  `json:"directories"`
	Files       []File    `json:"files"`
}

func excluded(rel string) bool {
	top := strings.Split(filepath.ToSlash(rel), "/")[0]
	return strings.EqualFold(top, controlDir) || strings.EqualFold(top, datadir.MaintenanceLockFile) || top == "document-parser" || top == "engine.pid" || top == "gateway-session.nonce"
}

// OpenRuntime completes interrupted restoration before opening any Store. A
// second live runtime prevents exclusive recovery, so startup fails closed.
func OpenRuntime(ctx context.Context, root *datadir.SecureRoot) (*datadir.DataLock, error) {
	for attempt := 0; attempt < 3; attempt++ {
		lock, err := root.AcquireDataLock(false)
		if err != nil {
			return nil, err
		}
		pending, err := pendingRestore(root.Path())
		if err != nil {
			_ = lock.Close()
			return nil, err
		}
		if !pending {
			return lock, nil
		}
		_ = lock.Close()
		exclusive, err := root.AcquireDataLock(true)
		if err != nil {
			return nil, err
		}
		err = recoverLocked(ctx, root.Path(), nil)
		closeErr := exclusive.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	return nil, errors.New("maintenance state changed repeatedly during startup")
}

func Backup(ctx context.Context, root *datadir.SecureRoot, destination string) (*Manifest, error) {
	lock, err := root.AcquireDataLock(true)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err = recoverLocked(ctx, root.Path(), nil); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(destination) || !separate(root.Path(), destination) {
		return nil, errors.New("backup destination must be an absolute directory outside the data root")
	}
	// A new directory plus a manifest published last makes incomplete backups
	// recognizable and prevents accidental overwriting of a previous backup.
	if err = os.Mkdir(destination, 0700); err != nil {
		return nil, err
	}
	protected, err := datadir.PrepareForTest(destination)
	if err != nil {
		return nil, err
	}
	defer protected.Close()
	data := filepath.Join(destination, "data")
	if err = os.Mkdir(data, 0700); err != nil {
		return nil, err
	}
	m, err := capture(ctx, root.Path(), data)
	if err != nil {
		return nil, err
	}
	if err = verifyData(ctx, data, m, true); err != nil {
		return nil, err
	}
	if err = writeJSON(destination, "manifest.json", m); err != nil {
		return nil, err
	}
	return m, nil
}

func Verify(ctx context.Context, directory string) (*Manifest, error) {
	m, err := readManifest(directory)
	if err != nil {
		return nil, err
	}
	if err = verifyData(ctx, filepath.Join(directory, "data"), m, true); err != nil {
		return nil, err
	}
	return m, nil
}

func separate(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	within := func(parent, child string) bool {
		rel, err := filepath.Rel(strings.ToLower(parent), strings.ToLower(child))
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	return !within(a, b) && !within(b, a)
}

func capture(ctx context.Context, source, destination string) (*Manifest, error) {
	files, dirs, err := inventory(ctx, source, true)
	if err != nil {
		return nil, err
	}
	m := &Manifest{Format: format, SourceRoot: source, CreatedAt: time.Now().UTC(), Directories: dirs}
	for _, dir := range dirs {
		if err = os.MkdirAll(filepath.Join(destination, filepath.FromSlash(dir)), 0700); err != nil {
			return nil, err
		}
	}
	dbPaths := map[string]bool{}
	for _, rel := range files {
		isDB, detectErr := isSQLite(source, rel)
		if detectErr != nil {
			return nil, detectErr
		}
		dbPaths[rel] = isDB
	}
	if !dbPaths["lunitide.db"] {
		return nil, errors.New("lunitide.db is missing or is not a SQLite image")
	}
	for _, rel := range files {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		skip := false
		for _, suffix := range []string{"-wal", "-shm", "-journal"} {
			if strings.HasSuffix(rel, suffix) && dbPaths[strings.TrimSuffix(rel, suffix)] {
				skip = true
			}
		}
		if skip {
			continue
		}
		dest := filepath.Join(destination, filepath.FromSlash(rel))
		if dbPaths[rel] {
			tmp := dest + ".snapshot.db"
			err = sqlite.SnapshotDatabase(ctx, filepath.Join(source, filepath.FromSlash(rel)), tmp)
			if err == nil {
				err = moveDurable(tmp, dest)
			}
		} else {
			err = copyFile(ctx, source, rel, dest)
		}
		if err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", rel, err)
		}
		f, hashErr := hashFile(ctx, destination, rel)
		if hashErr != nil {
			return nil, hashErr
		}
		f.SQLite = dbPaths[rel]
		m.Files = append(m.Files, f)
	}
	return m, nil
}

func inventory(ctx context.Context, root string, skipRuntime bool) ([]string, []string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, workspace.ErrPathEscape
	}
	var files, dirs []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if skipRuntime && excluded(rel) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if err = workspace.ValidateRelPath(rel); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return workspace.ErrPathEscape
		}
		if info.IsDir() {
			dirs = append(dirs, rel)
		} else if info.Mode().IsRegular() {
			files = append(files, rel)
		} else {
			return fmt.Errorf("unsupported file type: %s", rel)
		}
		if len(files)+len(dirs) > maxEntries {
			return errors.New("backup file count exceeds 250000")
		}
		return nil
	})
	sort.Strings(files)
	sort.Strings(dirs)
	return files, dirs, err
}

func openFile(root, rel string) (*os.File, error) {
	s, err := workspace.NewSecureRoot(root)
	if err != nil {
		return nil, err
	}
	f, err := s.OpenSecure(rel)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, errors.Join(err, errors.New("backup requires ordinary files"))
	}
	return f, nil
}
func isSQLite(root, rel string) (bool, error) {
	f, err := openFile(root, rel)
	if err != nil {
		return false, err
	}
	defer f.Close()
	var header [16]byte
	n, err := io.ReadFull(f, header[:])
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return false, nil
	}
	return n == 16 && string(header[:]) == "SQLite format 3\x00", err
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
func copyFile(ctx context.Context, root, rel, dest string) error {
	in, err := openFile(root, rel)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.CopyBuffer(out, contextReader{ctx, in}, make([]byte, 64<<10))
	if err == nil {
		err = out.Sync()
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}
func hashFile(ctx context.Context, root, rel string) (File, error) {
	f, err := openFile(root, rel)
	if err != nil {
		return File{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.CopyBuffer(h, contextReader{ctx, f}, make([]byte, 64<<10))
	return File{Path: rel, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, err
}
func writeJSON(root, rel string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(b) > maxManifestBytes {
		return errors.New("maintenance manifest exceeds 32 MiB")
	}
	s, err := workspace.NewSecureRoot(root)
	if err != nil {
		return err
	}
	return s.WriteAtomic(rel, b, 0600)
}
func readJSON(root, rel string, value any) error {
	f, err := openFile(root, rel)
	if err != nil {
		return err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxManifestBytes+1))
	if err != nil {
		return err
	}
	if len(b) > maxManifestBytes {
		return errors.New("maintenance manifest exceeds 32 MiB")
	}
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err = d.Decode(value); err != nil {
		return err
	}
	var trailing any
	if !errors.Is(d.Decode(&trailing), io.EOF) {
		return errors.New("trailing maintenance data")
	}
	return nil
}
func readManifest(root string) (*Manifest, error) {
	var m Manifest
	if err := readJSON(root, "manifest.json", &m); err != nil {
		return nil, err
	}
	if err := validManifest(&m); err != nil {
		return nil, err
	}
	return &m, nil
}
func validManifest(m *Manifest) error {
	if m.Format != format || !filepath.IsAbs(m.SourceRoot) || m.CreatedAt.IsZero() || len(m.Files) == 0 || len(m.Files)+len(m.Directories) > maxEntries {
		return errors.New("invalid backup manifest")
	}
	seen := map[string]bool{}
	mainDB := false
	for _, dir := range m.Directories {
		if strings.Contains(dir, `\`) {
			return errors.New("backup directory paths must use canonical slashes")
		}
		if err := workspace.ValidateRelPath(dir); err != nil {
			return err
		}
		key := strings.ToLower(filepath.ToSlash(dir))
		if excluded(dir) || seen[key] {
			return errors.New("invalid backup directory")
		}
		seen[key] = true
	}
	filePaths := make([]string, 0, len(m.Files)+len(m.Directories))
	filePaths = append(filePaths, m.Directories...)
	for _, f := range m.Files {
		filePaths = append(filePaths, f.Path)
	}
	if err := CheckManifestPaths(filePaths); err != nil {
		return err
	}
	for _, f := range m.Files {
		if strings.Contains(f.Path, `\`) {
			return errors.New("backup file paths must use canonical slashes")
		}
		if err := workspace.ValidateRelPath(f.Path); err != nil {
			return err
		}
		key := strings.ToLower(filepath.ToSlash(f.Path))
		digest, err := hex.DecodeString(f.SHA256)
		if excluded(f.Path) || seen[key] || f.Size < 0 || err != nil || len(digest) != 32 {
			return errors.New("invalid backup file")
		}
		seen[key] = true
		if f.Path == "lunitide.db" && f.SQLite {
			mainDB = true
		}
	}
	if !mainDB {
		return errors.New("backup has no main database")
	}
	return nil
}

func verifyData(ctx context.Context, root string, m *Manifest, exact bool) error {
	if err := validManifest(m); err != nil {
		return err
	}
	want := map[string]File{}
	for _, f := range m.Files {
		got, err := hashFile(ctx, root, f.Path)
		if err != nil {
			return fmt.Errorf("verify %s: %w", f.Path, err)
		}
		if got.Size != f.Size || got.SHA256 != f.SHA256 {
			return fmt.Errorf("backup content changed: %s", f.Path)
		}
		if f.SQLite {
			if err = sqlite.ValidateBackupImage(ctx, filepath.Join(root, filepath.FromSlash(f.Path))); err != nil {
				return err
			}
		}
		want[f.Path] = f
	}
	if exact {
		files, dirs, err := inventory(ctx, root, false)
		if err != nil {
			return err
		}
		if len(files) != len(m.Files) || len(dirs) != len(m.Directories) {
			return errors.New("backup contains missing or unlisted entries")
		}
		for _, f := range files {
			if _, ok := want[f]; !ok {
				return errors.New("unlisted backup file")
			}
		}
		gotDirs := map[string]bool{}
		for _, d := range dirs {
			gotDirs[d] = true
		}
		for _, d := range m.Directories {
			if !gotDirs[d] {
				return errors.New("missing backup directory")
			}
		}
	}
	refs, err := sqlite.BackupFileReferences(ctx, filepath.Join(root, "lunitide.db"))
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if err = workspace.ValidateRelPath(ref.Key); err != nil {
			return err
		}
		rel := ref.Directory + "/" + filepath.ToSlash(ref.Key)
		f, ok := want[rel]
		if !ok {
			return fmt.Errorf("database references missing file: %s", rel)
		}
		digest := strings.TrimPrefix(ref.Digest, "sha256:")
		if digest != "" && digest != f.SHA256 {
			return fmt.Errorf("database file digest differs: %s", rel)
		}
	}
	return nil
}
