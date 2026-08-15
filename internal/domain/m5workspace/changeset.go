// M5 T-5.2.4 ChangeSet domain: staged -> applied/reverted/conflict with
// CAS-addressed patches and compensating rollback refs.
package m5workspace

import (
	"errors"
	"time"
)

type ChangeSetState string

const (
	ChangeSetStaged   ChangeSetState = "staged"
	ChangeSetApplied  ChangeSetState = "applied"
	ChangeSetReverted ChangeSetState = "reverted"
	ChangeSetConflict ChangeSetState = "conflict"
)

func (s ChangeSetState) Valid() bool {
	switch s {
	case ChangeSetStaged, ChangeSetApplied, ChangeSetReverted, ChangeSetConflict:
		return true
	}
	return false
}

var (
	ErrChangeSetNotFound  = errors.New("m5workspace: changeset not found")
	ErrChangeSetState     = errors.New("m5workspace: illegal changeset state transition")
	ErrChangeSetConflict  = errors.New("m5workspace: workspace base digest drifted (CHANGESET_BASE_CONFLICT)")
)

// ChangeSet is the m5_changeset row.
type ChangeSet struct {
	ID          string         `json:"id"`
	RunID       string         `json:"runId"`
	WorkspaceID string         `json:"workspaceId"`
	BaseDigest  string         `json:"baseDigest"`
	State       ChangeSetState `json:"state"`
	Source      string         `json:"source"`
	Version     int64          `json:"version"`
	CreatedAt   time.Time      `json:"createdAt"`
	AppliedAt   *time.Time     `json:"appliedAt,omitempty"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

// ValidChangeKinds are the item change kinds.
const (
	ChangeAdd    = "add"
	ChangeModify = "modify"
	ChangeDelete = "delete"
)

// ChangeSetItem is the m5_changeset_item row.
type ChangeSetItem struct {
	ID          string `json:"id"`
	ChangeSetID string `json:"changesetId"`
	Path        string `json:"path"`
	Change      string `json:"change"`
	PatchRef    string `json:"patchRef"`    // CAS address of the target content
	SHA256      string `json:"sha256"`      // sha256 of target content
	Size        int64  `json:"size"`        // bytes of target content
	RollbackRef string `json:"rollbackRef"` // CAS address of pre-apply bytes (modify/delete)
}
