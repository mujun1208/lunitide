package officestudio

import (
	"context"
	"time"
)

// Snapshot metadata deliberately excludes document specifications, indexes,
// and large raw receipts. Immutable record IDs bind abbreviated text to the
// original evidence, which remains stored without modification.
type Snapshot struct {
	Task          Task
	Heads         []Head
	Versions      []Version
	Checks        map[string][]SnapshotCheck
	ValidationIDs map[string]string
	Steps         []SnapshotStep
	Sources       []SnapshotSource
}
type SnapshotCheck struct {
	Check
	DetailTruncated bool
}
type SnapshotStep struct {
	ID, RunID, Label, State, Summary string
	SummaryTruncated                 bool
	CreatedAt                        time.Time
}
type SnapshotSource struct {
	ID, VersionID, TargetVersionID, Location, TargetLocation, Transform string
	TransformTruncated                                                  bool
	CreatedAt                                                           time.Time
}
type SnapshotTask struct {
	Task
	GoalTruncated bool
	ProjectID     string
}
type SnapshotStore interface {
	ReadOfficeSnapshot(context.Context, string) (Snapshot, error)
	ReadOfficeTaskListSnapshot(context.Context, string, string) ([]SnapshotTask, error)
}
