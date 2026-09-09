package officestudio

import "context"

// ArchiveSnapshot is a complete, authorized view of one chat's Office
// destinations. It excludes large document specs, indexes and QA histories.
type ArchiveSnapshot struct {
	Task     Task
	Tasks    []Task
	Versions []ArchiveVersion
}

type ArchiveVersion struct {
	ID, TaskID, ArtifactID, SHA256, SourcePath string
}

type ArchiveStore interface {
	ReadOfficeArchiveSnapshot(context.Context, string) (ArchiveSnapshot, error)
}
