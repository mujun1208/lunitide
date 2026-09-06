package sqlite

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
)

// SnapshotDatabase is the offline directory-backup adapter. It deliberately
// skips application migrations and reconciliation, and reuses VACUUM INTO and
// the validated/fsynced publication implementation used by online backups.
func SnapshotDatabase(ctx context.Context, source, destination string) error {
	u := &url.URL{Scheme: "file", Opaque: filepath.ToSlash(source)}
	q := u.Query()
	q.Set("mode", "rw")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, path: source}
	err = s.CreateBackup(ctx, destination)
	if closeErr := db.Close(); err == nil {
		err = closeErr
	}
	return err
}

func ValidateBackupImage(ctx context.Context, path string) error {
	return validateSQLiteImage(ctx, path)
}

// BackupFileReferences returns durable application file references. Directory
// snapshots also include files from other modules (workspaces, recordings,
// personas, browser profiles, release publications), without guessing which
// arbitrary files are safe to discard.
type BackupFileReference struct{ Directory, Key, Digest string }

func BackupFileReferences(ctx context.Context, path string) ([]BackupFileReference, error) {
	u := &url.URL{Scheme: "file", Opaque: filepath.ToSlash(path)}
	q := u.Query()
	q.Set("mode", "ro")
	q.Set("immutable", "1")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var refs []BackupFileReference
	for _, spec := range []struct{ table, directory, query string }{
		{"attachments", "attachments", `SELECT file_ref,sha256 FROM attachments WHERE deleted_at IS NULL`},
		{"project_attachments", "project-attachments", `SELECT file_path,digest FROM project_attachments`},
		{"asset_templates", "asset-templates", `SELECT file_path,'' FROM asset_templates WHERE file_path<>''`},
	} {
		var exists int
		if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, spec.table).Scan(&exists); err != nil {
			return nil, err
		}
		if exists == 0 {
			continue
		}
		rows, queryErr := db.QueryContext(ctx, spec.query)
		if queryErr != nil {
			return nil, queryErr
		}
		for rows.Next() {
			var r BackupFileReference
			r.Directory = spec.directory
			if err = rows.Scan(&r.Key, &r.Digest); err != nil {
				break
			}
			refs = append(refs, r)
		}
		if err == nil {
			err = rows.Err()
		}
		if closeErr := rows.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, err
		}
	}
	return refs, nil
}
