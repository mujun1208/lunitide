package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Replaying the manifest into a new file costs roughly ninety-four percent of
// what opening a fresh database costs, and reopening one already at the latest
// version costs a seventeenth as much. Production pays that replay once, on
// install, and does not care. Tests pay it once per test — hundreds of times
// per package, under a race detector that multiplies it by twenty — and that
// is most of the CI budget.
//
// OpenTemplated exists for those tests. It replays the manifest once per
// process into a snapshot held in memory, then seeds each new database from
// the snapshot. What it does not do is skip any check: the seeded file is
// handed to the same open() as any other, so the migration journal is
// validated and the schema audited exactly as they would be on a real install.
// A snapshot that disagreed with the manifest in any way would be rejected on
// first use rather than quietly accepted, and TestTemplatedOpenMatchesReplay
// pins the two against each other object by object.
//
// Production must keep using OpenSecure. This is not faster there — the
// snapshot has to be built from a real replay before it can serve anyone, so
// the first caller in a process pays full price either way.
func OpenTemplated(ctx context.Context, path string) (*Store, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Ext(path) != ".db" || strings.ContainsAny(path, "\x00\r\n") {
		return nil, fmt.Errorf("unsafe SQLite path %q", path)
	}
	path = filepath.Clean(path)
	// An existing file is already at some version of its own; seeding over it
	// would destroy data, and replaying onto it is what open() does anyway.
	if _, err := os.Stat(path); err == nil {
		return open(ctx, path, nil, nil)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	image, err := migratedImage(ctx)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, image, 0o600); err != nil {
		return nil, err
	}
	s, err := open(ctx, path, nil, nil)
	if err != nil {
		// Leaving a seeded-but-rejected file behind would make a retry take the
		// branch above and report a different failure than the real one.
		os.Remove(path)
		return nil, err
	}
	return s, nil
}

var (
	templateOnce  sync.Once
	templateImage []byte
	templateErr   error
)

// migratedImage returns the bytes of a database with the manifest applied,
// building it on first call.
func migratedImage(ctx context.Context) ([]byte, error) {
	templateOnce.Do(func() {
		templateImage, templateErr = buildMigratedImage(ctx)
	})
	return templateImage, templateErr
}

func buildMigratedImage(ctx context.Context) ([]byte, error) {
	dir, err := os.MkdirTemp("", "lunitide-schema-template")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	source, err := open(ctx, filepath.Join(dir, "template.db"), nil, nil)
	if err != nil {
		return nil, fmt.Errorf("build schema template: %w", err)
	}
	// VACUUM INTO writes a self-contained file rather than a copy of one that
	// may still have a write-ahead log beside it, so the bytes read back are
	// the whole database and not a prefix of it.
	snapshot := filepath.Join(dir, "snapshot.db")
	_, err = source.db.ExecContext(ctx, `VACUUM INTO ?`, snapshot)
	if closeErr := source.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("snapshot schema template: %w", err)
	}
	image, err := os.ReadFile(snapshot)
	if err != nil {
		return nil, err
	}
	if len(image) == 0 {
		return nil, fmt.Errorf("schema template snapshot is empty")
	}
	return image, nil
}
