package scheduler

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	WriterJSON   = "json"
	WriterSQLite = "sqlite"
)

type Manifest struct {
	SourceJobs   int
	SourceRuns   int
	ImportedJobs int
	ImportedRuns int
	Cutover      bool
}

func writerPath(root string) string {
	return filepath.Join(root, "automation", "writer")
}

func ClassifiedRecover(writer, decision string) bool {
	if writer != WriterSQLite {
		return false
	}
	switch decision {
	case "native_continue", "business_rebuild":
		return true
	default:
		return false
	}
}

func OpenAutomationRepository(root string) (Repository, func(), error) {
	if CurrentWriter(root) == WriterSQLite {
		s, err := OpenLiveSQL(root)
		if err != nil {
			return nil, nil, err
		}
		return s, func() { _ = s.Close() }, nil
	}
	store, err := NewStore(root)
	if err != nil {
		return nil, nil, err
	}
	return store, func() {}, nil
}

func CurrentWriter(root string) string {
	raw, err := os.ReadFile(writerPath(root))
	if err != nil {
		return WriterJSON
	}
	switch strings.TrimSpace(string(raw)) {
	case WriterSQLite:
		return WriterSQLite
	default:
		return WriterJSON
	}
}

func ImportJSON(root string, onExecute func()) (*SQLStore, Manifest, error) {
	if onExecute != nil {
		// Import is a data copy. Callers may pass a probe; we never invoke it.
		_ = onExecute
	}
	jsonStore, err := NewStore(root)
	if err != nil {
		return nil, Manifest{}, err
	}
	sqlStore, err := OpenSQL(root)
	if err != nil {
		return nil, Manifest{}, err
	}
	jobs, err := jsonStore.ListJobs()
	if err != nil {
		_ = sqlStore.Close()
		return nil, Manifest{}, err
	}
	runs, err := jsonStore.LatestRuns("")
	if err != nil {
		_ = sqlStore.Close()
		return nil, Manifest{}, err
	}
	for _, j := range jobs {
		if err := sqlStore.PutJob(j); err != nil {
			_ = sqlStore.Close()
			return nil, Manifest{}, err
		}
	}
	for _, r := range runs {
		if err := sqlStore.AppendRun(r); err != nil {
			_ = sqlStore.Close()
			return nil, Manifest{}, err
		}
	}
	importedJobs, err := sqlStore.ListJobs()
	if err != nil {
		_ = sqlStore.Close()
		return nil, Manifest{}, err
	}
	importedRuns, err := sqlStore.LatestRuns("")
	if err != nil {
		_ = sqlStore.Close()
		return nil, Manifest{}, err
	}
	return sqlStore, Manifest{
		SourceJobs: len(jobs), SourceRuns: len(runs),
		ImportedJobs: len(importedJobs), ImportedRuns: len(importedRuns),
	}, nil
}

func Cutover(root string, manifest Manifest) error {
	if manifest.SourceJobs != manifest.ImportedJobs || manifest.SourceRuns != manifest.ImportedRuns {
		return errors.New("scheduler import count mismatch; keep JSON writer")
	}
	if err := os.MkdirAll(filepath.Join(root, "automation"), 0700); err != nil {
		return err
	}
	return os.WriteFile(writerPath(root), []byte(WriterSQLite+"\n"), 0600)
}
