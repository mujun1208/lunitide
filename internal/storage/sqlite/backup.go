package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// CreateBackup creates an online, transactionally consistent standalone
// SQLite image. VACUUM INTO includes committed WAL contents without requiring
// the application to stop. The destination is published only after a full
// integrity check and fsync.
func (s *Store) CreateBackup(ctx context.Context, destination string) error {
	if err := safeBackupPath(destination); err != nil {
		return err
	}
	if filepath.Clean(destination) == filepath.Clean(s.path) {
		return errors.New("backup destination must differ from live database")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	tmp, err := reserveTempPath(filepath.Dir(destination), ".lunitide-backup-*.db")
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if _, err = s.db.ExecContext(ctx, `VACUUM INTO ?`, tmp); err != nil {
		return fmt.Errorf("create consistent backup: %w", err)
	}
	if err = validateSQLiteImage(ctx, tmp); err != nil {
		return fmt.Errorf("validate backup: %w", err)
	}
	if err = syncFile(tmp); err != nil {
		return fmt.Errorf("sync backup: %w", err)
	}
	// A previous destination may have SQLite sidecars. They must not survive the
	// main-image replacement, and on Windows removing them here also detects an
	// outstanding SQLite handle before we start the rename sequence.
	if err = removeSQLiteSidecars(destination); err != nil {
		return fmt.Errorf("remove backup SQLite sidecars: %w", err)
	}
	if err = replaceFileAtomically(tmp, destination); err != nil {
		return fmt.Errorf("publish backup: %w", err)
	}
	return nil
}

// RestoreBackup validates and stages a backup before replacing the live image.
// It closes this Store immediately before the switch; callers must reopen it
// whether restore succeeds or fails. The previous image is restored if the
// staged-image switch fails. No migration or write is attempted on the input.
func (s *Store) RestoreBackup(ctx context.Context, source string) error {
	if err := safeBackupPath(source); err != nil {
		return err
	}
	if filepath.Clean(source) == filepath.Clean(s.path) {
		return errors.New("backup source must differ from live database")
	}
	if err := validateSQLiteImage(ctx, source); err != nil {
		return fmt.Errorf("validate restore source: %w", err)
	}
	staged, err := reserveTempPath(filepath.Dir(s.path), ".lunitide-restore-*.db")
	if err != nil {
		return err
	}
	defer os.Remove(staged)
	if err = copyAndSync(source, staged); err != nil {
		return fmt.Errorf("stage restore: %w", err)
	}
	if err = validateSQLiteImage(ctx, staged); err != nil {
		return fmt.Errorf("validate staged restore: %w", err)
	}
	if err = s.Close(); err != nil {
		return fmt.Errorf("close live database: %w", err)
	}
	// A clean close checkpoints/removes sidecars. Do not publish while any stale
	// remnant remains: pairing an old WAL with the replacement main image can
	// make a valid restore appear corrupt (or expose transactions from the old
	// database). In particular, Windows reports sharing violations here rather
	// than allowing the subsequent rename to proceed safely.
	if err = removeSQLiteSidecars(s.path); err != nil {
		return fmt.Errorf("remove live SQLite sidecars: %w", err)
	}
	if err = replaceFileAtomically(staged, s.path); err != nil {
		return fmt.Errorf("switch restored database (live image rolled back): %w", err)
	}
	return nil
}

func safeBackupPath(path string) error {
	if path == "" || !filepath.IsAbs(path) || strings.ToLower(filepath.Ext(path)) != ".db" || strings.ContainsAny(path, "\x00\r\n") {
		return fmt.Errorf("unsafe SQLite backup path %q", path)
	}
	return nil
}

func reserveTempPath(dir, pattern string) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", fmt.Errorf("reserve temporary database: %w", err)
	}
	name := f.Name()
	if err = f.Chmod(0o600); err == nil {
		err = f.Close()
	} else {
		_ = f.Close()
	}
	if err != nil {
		_ = os.Remove(name)
		return "", err
	}
	// VACUUM INTO requires a non-existent destination; staging copy does not.
	if strings.Contains(pattern, "backup") {
		err = os.Remove(name)
	}
	return name, err
}

func validateSQLiteImage(ctx context.Context, path string) error {
	// immutable=1 makes validation inspect this exact standalone main-db image
	// without creating or consuming same-named WAL/SHM sidecars.
	// Keep the Windows volume name in the URI authority-free file: form. Using
	// url.URL.Path produces file:///C:/..., which this SQLite driver interprets
	// as a different path and can report as the misleading "out of memory".
	u := &url.URL{Scheme: "file", Opaque: filepath.ToSlash(path)}
	q := u.Query()
	q.Set("mode", "ro")
	q.Set("immutable", "1")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	rows, err := db.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		_ = db.Close()
		return err
	}
	var results []string
	for rows.Next() {
		var result string
		if err = rows.Scan(&result); err != nil {
			break
		}
		results = append(results, result)
	}
	if err == nil {
		err = rows.Err()
	}
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	// Close before reporting success and preserve close errors: immediate rename
	// after a deferred close was the Windows ACCESS_DENIED failure mode.
	if closeErr := db.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if len(results) != 1 || results[0] != "ok" {
		return fmt.Errorf("integrity_check: %s", strings.Join(results, "; "))
	}
	return nil
}

func copyAndSync(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		_ = in.Close()
		return err
	}
	_, copyErr := io.Copy(out, in)
	if copyErr == nil {
		copyErr = out.Sync()
	}
	outCloseErr := out.Close()
	inCloseErr := in.Close()
	if copyErr != nil {
		return copyErr
	}
	if outCloseErr != nil {
		return outCloseErr
	}
	return inCloseErr
}

func syncFile(path string) error {
	// FlushFileBuffers may reject a read-only Windows handle with ACCESS_DENIED.
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	syncErr := f.Sync()
	closeErr := f.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

// removeSQLiteSidecars removes files that SQLite can associate with a main
// image and persists the directory mutation where the platform supports it.
// It is called only after all database handles for path have been closed.
func removeSQLiteSidecars(path string) error {
	removed := false
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if err := os.Remove(path + suffix); err == nil {
			removed = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", suffix, err)
		}
	}
	if removed {
		return syncDir(filepath.Dir(path))
	}
	return nil
}

func syncDir(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = f.Sync(); errors.Is(err, os.ErrInvalid) {
		return nil
	}
	return err
}

// replaceFileAtomically retains the old destination until the new file is in
// place and restores it on failure. Renames stay within one directory/filesystem.
func replaceFileAtomically(staged, destination string) error {
	rollback := destination + ".restore-rollback"
	_ = os.Remove(rollback)
	hadOld := false
	if _, err := os.Stat(destination); err == nil {
		if err = os.Rename(destination, rollback); err != nil {
			return err
		}
		hadOld = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(staged, destination); err != nil {
		if hadOld {
			_ = os.Rename(rollback, destination)
		}
		return err
	}
	if err := syncDir(filepath.Dir(destination)); err != nil {
		_ = os.Remove(destination)
		if hadOld {
			_ = os.Rename(rollback, destination)
		}
		return err
	}
	if hadOld {
		if err := os.Remove(rollback); err != nil {
			return fmt.Errorf("remove replacement rollback: %w", err)
		}
		// The first directory sync made both names durable. A second one is
		// required to make deletion of the rollback image durable as well.
		if err := syncDir(filepath.Dir(destination)); err != nil {
			return err
		}
	}
	return nil
}
