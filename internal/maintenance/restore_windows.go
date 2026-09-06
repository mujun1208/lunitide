//go:build windows

package maintenance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lunitide/lunitide/internal/datadir"
	"github.com/lunitide/lunitide/internal/workspace"
	"golang.org/x/sys/windows"
)

type restoreEntry struct {
	Name  string `json:"name"`
	Old   bool   `json:"old"`
	New   bool   `json:"new"`
	Phase string `json:"phase"`
}
type restoreJournal struct {
	Format   string         `json:"format"`
	Job      string         `json:"job"`
	Complete bool           `json:"complete"`
	Manifest Manifest       `json:"manifest"`
	Entries  []restoreEntry `json:"entries"`
}

const journalName = controlDir + "/restore.json"

func pendingRestore(root string) (bool, error) {
	var j restoreJournal
	err := readJSON(root, journalName, &j)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err = validJournal(root, &j); err != nil {
		return false, err
	}
	return !j.Complete, nil
}

// Restore stages and verifies every byte before writing a durable intent. Old
// top-level entries are retained under the job's rollback directory. From the
// intent onward recovery always rolls forward, never opens mixed-generation
// Stores, and never reruns application work. Context cancellation leaves an
// explicit pending operation that the next startup/maintenance call resumes.
func Restore(ctx context.Context, root *datadir.SecureRoot, backup string) error {
	return restoreWithHook(ctx, root, backup, nil)
}

func restoreWithHook(ctx context.Context, root *datadir.SecureRoot, backup string, afterMove func(string) error) error {
	lock, err := root.AcquireDataLock(true)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = recoverLocked(ctx, root.Path(), nil); err != nil {
		return err
	}
	m, err := Verify(ctx, backup)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Clean(m.SourceRoot), root.Path()) {
		return errors.New("backup belongs to a different data root; relocation is not supported")
	}
	if !separate(root.Path(), backup) {
		return errors.New("restore source must be outside the data root")
	}
	control, err := root.PrepareSubdirectory(controlDir)
	if err != nil {
		return err
	}
	defer control.Close()
	jobPath, err := os.MkdirTemp(control.Path(), "restore-")
	if err != nil {
		return err
	}
	job := filepath.Base(jobPath)
	stage := filepath.Join(jobPath, "staged")
	for _, dir := range []string{stage, filepath.Join(jobPath, "rollback")} {
		if err = os.Mkdir(dir, 0700); err != nil {
			return err
		}
	}
	for _, dir := range m.Directories {
		if err = os.MkdirAll(filepath.Join(stage, filepath.FromSlash(dir)), 0700); err != nil {
			return err
		}
	}
	for _, file := range m.Files {
		if err = copyFile(ctx, filepath.Join(backup, "data"), file.Path, filepath.Join(stage, filepath.FromSlash(file.Path))); err != nil {
			return err
		}
	}
	if err = verifyData(ctx, stage, m, true); err != nil {
		return err
	}
	// Inventory the current tree before the intent, refusing reparse points and
	// unsupported types rather than moving unknown external directory trees.
	if _, _, err = inventory(ctx, root.Path(), true); err != nil {
		return err
	}
	entries, err := os.ReadDir(root.Path())
	if err != nil {
		return err
	}
	byName := map[string]*restoreEntry{}
	for _, e := range entries {
		if e.Name() == controlDir || e.Name() == datadir.MaintenanceLockFile {
			continue
		}
		byName[e.Name()] = &restoreEntry{Name: e.Name(), Old: true, Phase: "pending"}
	}
	for _, file := range m.Files {
		top := strings.Split(file.Path, "/")[0]
		if byName[top] == nil {
			byName[top] = &restoreEntry{Name: top, Phase: "pending"}
		}
		byName[top].New = true
	}
	for _, dir := range m.Directories {
		top := strings.Split(dir, "/")[0]
		if byName[top] == nil {
			byName[top] = &restoreEntry{Name: top, Phase: "pending"}
		}
		byName[top].New = true
	}
	var names []string
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	j := restoreJournal{Format: format, Job: job, Manifest: *m}
	for _, name := range names {
		j.Entries = append(j.Entries, *byName[name])
	}
	if err = validJournal(root.Path(), &j); err != nil {
		return err
	}
	if err = writeJSON(root.Path(), journalName, &j); err != nil {
		return err
	}
	return recoverLocked(ctx, root.Path(), afterMove)
}

func validJournal(root string, j *restoreJournal) error {
	if j.Format != format || !strings.HasPrefix(j.Job, "restore-") || filepath.Base(j.Job) != j.Job || workspace.ValidateRelPath(j.Job) != nil || !strings.EqualFold(filepath.Clean(j.Manifest.SourceRoot), filepath.Clean(root)) {
		return errors.New("invalid restore journal")
	}
	if err := validManifest(&j.Manifest); err != nil {
		return err
	}
	seen := map[string]bool{}
	newTop := map[string]bool{}
	for _, f := range j.Manifest.Files {
		newTop[strings.Split(f.Path, "/")[0]] = true
	}
	for _, d := range j.Manifest.Directories {
		newTop[strings.Split(d, "/")[0]] = true
	}
	for _, e := range j.Entries {
		key := strings.ToLower(e.Name)
		if workspace.ValidateRelPath(e.Name) != nil || strings.ContainsAny(e.Name, "/\\") || key == controlDir || key == datadir.MaintenanceLockFile || seen[key] || (!e.Old && !e.New) || (e.Phase != "pending" && e.Phase != "old_saved" && e.Phase != "installed") || e.New != newTop[e.Name] {
			return errors.New("invalid restore entry")
		}
		seen[key] = true
		delete(newTop, e.Name)
	}
	if len(newTop) != 0 {
		return errors.New("restore journal omits new files")
	}
	return nil
}

func recoverLocked(ctx context.Context, root string, afterMove func(string) error) error {
	var j restoreJournal
	err := readJSON(root, journalName, &j)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err = validJournal(root, &j); err != nil {
		return err
	}
	if j.Complete {
		return nil
	}
	// Keep every control directory pinned while moving children. In particular
	// a substituted rollback junction must never redirect a live-directory move.
	control, err := datadir.PrepareForTest(filepath.Join(root, controlDir))
	if err != nil {
		return err
	}
	defer control.Close()
	jobRoot, err := control.PrepareSubdirectory(j.Job)
	if err != nil {
		return err
	}
	defer jobRoot.Close()
	stagedRoot, err := jobRoot.PrepareSubdirectory("staged")
	if err != nil {
		return err
	}
	defer stagedRoot.Close()
	rollbackRoot, err := jobRoot.PrepareSubdirectory("rollback")
	if err != nil {
		return err
	}
	defer rollbackRoot.Close()
	base := filepath.Join(root, controlDir, j.Job)
	// Validate the staged/live combination before making any further changes.
	// A crash after rename but before journal fsync is resolved from the two
	// names and the exact expected bytes, not by replaying the move blindly.
	for _, e := range j.Entries {
		if !e.New {
			continue
		}
		stage := filepath.Join(base, "staged", e.Name)
		present, statErr := ordinaryExists(stage)
		if statErr != nil {
			return statErr
		}
		if present {
			err = verifyTop(ctx, filepath.Join(base, "staged"), e.Name, &j.Manifest)
		} else if e.Phase != "pending" {
			err = verifyTop(ctx, root, e.Name, &j.Manifest)
		} else {
			return fmt.Errorf("restore staged entry missing: %s", e.Name)
		}
		if err != nil {
			return err
		}
	}
	for i := range j.Entries {
		if err = ctx.Err(); err != nil {
			return err
		}
		e := &j.Entries[i]
		live := filepath.Join(root, e.Name)
		old := filepath.Join(base, "rollback", e.Name)
		stage := filepath.Join(base, "staged", e.Name)
		if e.Phase == "pending" {
			if e.Old {
				present, statErr := ordinaryExists(old)
				if statErr != nil {
					return statErr
				}
				if !present {
					if err = moveDurable(live, old); err != nil {
						return err
					}
					if afterMove != nil {
						if err = afterMove("old:" + e.Name); err != nil {
							return err
						}
					}
				}
			}
			e.Phase = "old_saved"
			if err = writeJSON(root, journalName, &j); err != nil {
				return err
			}
		}
		if e.Phase == "old_saved" {
			if e.New {
				present, statErr := ordinaryExists(stage)
				if statErr != nil {
					return statErr
				}
				if present {
					if err = moveDurable(stage, live); err != nil {
						return err
					}
					if afterMove != nil {
						if err = afterMove("new:" + e.Name); err != nil {
							return err
						}
					}
				} else if err = verifyTop(ctx, root, e.Name, &j.Manifest); err != nil {
					return err
				}
			} else {
				present, statErr := ordinaryExists(live)
				if statErr != nil {
					return statErr
				}
				if present {
					return fmt.Errorf("removed restore entry unexpectedly exists: %s", e.Name)
				}
			}
			e.Phase = "installed"
			if err = writeJSON(root, journalName, &j); err != nil {
				return err
			}
		}
	}
	if err = verifyData(ctx, root, &j.Manifest, false); err != nil {
		return err
	}
	files, dirs, err := inventory(ctx, root, true)
	if err != nil {
		return err
	}
	if len(files) != len(j.Manifest.Files) || len(dirs) != len(j.Manifest.Directories) {
		return errors.New("restored directory contains unlisted entries")
	}
	j.Complete = true
	return writeJSON(root, journalName, &j)
}

func verifyTop(ctx context.Context, root, top string, m *Manifest) error {
	for _, f := range m.Files {
		if f.Path != top && !strings.HasPrefix(f.Path, top+"/") {
			continue
		}
		got, err := hashFile(ctx, root, f.Path)
		if err != nil {
			return err
		}
		if got.Size != f.Size || got.SHA256 != f.SHA256 {
			return fmt.Errorf("restore entry content differs: %s", f.Path)
		}
	}
	for _, d := range m.Directories {
		if d != top && !strings.HasPrefix(d, top+"/") {
			continue
		}
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(d)))
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return workspace.ErrPathEscape
		}
	}
	return nil
}
func ordinaryExists(path string) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
		return false, workspace.ErrPathEscape
	}
	return true, nil
}
func moveDurable(source, destination string) error {
	if _, err := ordinaryExists(source); err != nil {
		return err
	}
	if exists, err := ordinaryExists(destination); err != nil {
		return err
	} else if exists {
		return errors.New("maintenance destination already exists")
	}
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}
