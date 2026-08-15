// M5 T-5.2.4 ChangeSetService: stage -> apply/revert with CAS patches,
// base-digest conflict detection and compensating rollback. Apply captures
// pre-apply bytes for modify/delete entries, writes through the T-5.2.1
// path safety core atomically, charges the workspace quota, and refuses
// when the workspace base digest drifted (CHANGESET_BASE_CONFLICT).
package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/m5workspace"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

type csStore interface {
	GetM5Workspace(id string) (m5workspace.Workspace, error)
	TransitionM5Workspace(id string, expectedVersion int64, to m5workspace.State, usedBytes int64, lease time.Time, at time.Time) (m5workspace.Workspace, error)
	PutM5ChangeSet(m5workspace.ChangeSet) error
	GetM5ChangeSet(id string) (m5workspace.ChangeSet, error)
	TransitionM5ChangeSetState(id string, expectedVersion int64, to m5workspace.ChangeSetState, appliedAt time.Time, at time.Time) (m5workspace.ChangeSet, error)
	PutM5ChangeSetItem(m5workspace.ChangeSetItem) error
	ListM5ChangeSetItems(changesetID string) ([]m5workspace.ChangeSetItem, error)
	SetM5ChangeSetItemRollback(itemID, ref string) error
}

// ChangeSetService owns the AdHocWorkspace ChangeSet aggregate.
type ChangeSetService struct {
	uow   agentrunapp.UnitOfWork
	cas   *CASStore
	clock Clock
}

func NewChangeSetService(uow agentrunapp.UnitOfWork, cas *CASStore) *ChangeSetService {
	return &ChangeSetService{uow: uow, cas: cas, clock: systemClock{}}
}

func (s *ChangeSetService) SetClock(c Clock) { s.clock = c }

func (s *ChangeSetService) store(tx agentrunapp.Tx) (csStore, error) {
	st, ok := tx.(csStore)
	if !ok {
		return nil, ErrUOWUnavailable
	}
	return st, nil
}

// StageInput describes one staged change set.
type StageInput struct {
	RunID       string
	WorkspaceID string
	Source      string // generator label (e.g. "agent", "git-restore")
	Items       []StageItem
}

// StageItem is one file change; Content is the target content for
// add/modify and ignored for delete.
type StageItem struct {
	Path    string `json:"path"`
	Change  string `json:"change"` // add | modify | delete
	Content []byte `json:"content,omitempty"`
}

var (
	ErrChangeSetEmpty     = errors.New("workspace: changeset has no items")
	ErrChangeKindInvalid  = errors.New("workspace: change must be add, modify or delete")
	ErrChangeSetStateBad  = errors.New("workspace: changeset state does not allow this operation")
)

// Stage validates items, stores target content in CAS and journals the
// changeset with the current workspace base digest.
func (s *ChangeSetService) Stage(ctx context.Context, in StageInput) (m5workspace.ChangeSet, error) {
	if len(in.Items) == 0 {
		return m5workspace.ChangeSet{}, ErrChangeSetEmpty
	}
	if in.Source == "" {
		in.Source = "agent"
	}
	seen := map[string]bool{}
	items := make([]m5workspace.ChangeSetItem, 0, len(in.Items))
	for _, it := range in.Items {
		if err := ValidateRelPath(it.Path); err != nil {
			return m5workspace.ChangeSet{}, err
		}
		if seen[it.Path] {
			return m5workspace.ChangeSet{}, fmt.Errorf("%w: duplicate path %s", ErrInvalidInput, it.Path)
		}
		seen[it.Path] = true
		switch it.Change {
		case m5workspace.ChangeAdd, m5workspace.ChangeModify:
			ref, err := s.cas.Put(it.Content)
			if err != nil {
				return m5workspace.ChangeSet{}, err
			}
			sum := sha256.Sum256(it.Content)
			items = append(items, m5workspace.ChangeSetItem{
				Path: it.Path, Change: it.Change, PatchRef: ref,
				SHA256: hex.EncodeToString(sum[:]), Size: int64(len(it.Content)),
			})
		case m5workspace.ChangeDelete:
			items = append(items, m5workspace.ChangeSetItem{Path: it.Path, Change: it.Change, PatchRef: sha256Of(nil), SHA256: sha256Of(nil), Size: 0})
		default:
			return m5workspace.ChangeSet{}, ErrChangeKindInvalid
		}
	}
	var out m5workspace.ChangeSet
	err := s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		w, err := st.GetM5Workspace(in.WorkspaceID)
		if err != nil {
			return err
		}
		if w.State == m5workspace.StateDeleted {
			return ErrFsWorkspaceGone
		}
		root, err := NewSecureRoot(w.RootCanonical)
		if err != nil {
			return err
		}
		digest, err := TreeDigest(root)
		if err != nil {
			return err
		}
		now := s.clock.Now().UTC()
		out = m5workspace.ChangeSet{
			ID: ulid.Make().String(), RunID: in.RunID, WorkspaceID: in.WorkspaceID,
			BaseDigest: digest, State: m5workspace.ChangeSetStaged, Source: in.Source,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := st.PutM5ChangeSet(out); err != nil {
			return err
		}
		for i := range items {
			items[i].ChangeSetID = out.ID
			if err := st.PutM5ChangeSetItem(items[i]); err != nil {
				return err
			}
		}
		meta, mErr := json.Marshal(map[string]any{
			"workspaceId": in.WorkspaceID, "items": len(items), "baseDigest": digest,
		})
		if mErr != nil {
			return mErr
		}
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "changeset.previewed",
			AggregateID: out.ID, Actor: convertActor, Metadata: meta, CreatedAt: now,
		})
	})
	return out, err
}

// Preview returns the changeset and its items for review.
func (s *ChangeSetService) Preview(ctx context.Context, changesetID string) (m5workspace.ChangeSet, []m5workspace.ChangeSetItem, error) {
	var cs m5workspace.ChangeSet
	var items []m5workspace.ChangeSetItem
	err := s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		if cs, err = st.GetM5ChangeSet(changesetID); err != nil {
			return err
		}
		items, err = st.ListM5ChangeSetItems(changesetID)
		return err
	})
	return cs, items, err
}

// Apply writes the staged items. When the workspace content drifted since
// staging the changeset transitions to conflict and nothing is written.
func (s *ChangeSetService) Apply(ctx context.Context, changesetID string) (m5workspace.ChangeSet, error) {
	out, err := s.applyOrConflict(ctx, changesetID)
	return out, err
}

func (s *ChangeSetService) applyOrConflict(ctx context.Context, changesetID string) (m5workspace.ChangeSet, error) {
	var out m5workspace.ChangeSet
	var applied []m5workspace.ChangeSetItem
	drifted := false
	err := s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		cs, err := st.GetM5ChangeSet(changesetID)
		if err != nil {
			return err
		}
		if cs.State != m5workspace.ChangeSetStaged {
			return ErrChangeSetStateBad
		}
		w, err := st.GetM5Workspace(cs.WorkspaceID)
		if err != nil {
			return err
		}
		if w.State == m5workspace.StateReadonlyFull || w.State == m5workspace.StateDeleted {
			return ErrQuotaExceeded
		}
		items, err := st.ListM5ChangeSetItems(changesetID)
		if err != nil {
			return err
		}
		root, err := NewSecureRoot(w.RootCanonical)
		if err != nil {
			return err
		}
		digest, err := TreeDigest(root)
		if err != nil {
			return err
		}
		if digest != cs.BaseDigest {
			// Drift: mark conflict, change nothing on disk. The transition
			// itself must commit, so the flag is reported after commit; a
			// plain error return would roll the conflict state back too.
			out, err = st.TransitionM5ChangeSetState(cs.ID, cs.Version, m5workspace.ChangeSetConflict, time.Time{}, s.clock.Now().UTC())
			if err != nil {
				return err
			}
			cmeta, cmErr := json.Marshal(map[string]any{
				"workspaceId": cs.WorkspaceID, "stagedBaseDigest": cs.BaseDigest, "currentDigest": digest,
			})
			if cmErr != nil {
				return cmErr
			}
			if err := tx.PutAudit(providerapp.Audit{
				ID: ulid.Make().String(), Action: "changeset.conflicted",
				AggregateID: out.ID, Actor: convertActor, Metadata: cmeta, CreatedAt: s.clock.Now().UTC(),
			}); err != nil {
				return err
			}
			drifted = true
			return nil
		}
		// Pre-flight quota so a refused set never writes bytes.
		var delta int64
		for _, it := range items {
			switch it.Change {
			case m5workspace.ChangeAdd:
				delta += it.Size
			case m5workspace.ChangeModify:
				if old, ok := readFileSecure(root, it.Path); ok {
					delta += it.Size - int64(len(old))
				} else {
					delta += it.Size
				}
			case m5workspace.ChangeDelete:
				if old, ok := readFileSecure(root, it.Path); ok {
					delta -= int64(len(old))
				}
			}
		}
		if w.UsedBytes+delta > w.QuotaHard {
			return ErrQuotaExceeded
		}
		// Write with compensating capture: any failure undoes prior items.
		for _, it := range items {
			switch it.Change {
			case m5workspace.ChangeAdd, m5workspace.ChangeModify:
				if old, ok := readFileSecure(root, it.Path); ok {
					ref, rErr := s.cas.Put(old)
					if rErr != nil {
						return s.compensate(root, applied)
					}
					if err := st.SetM5ChangeSetItemRollback(it.ID, ref); err != nil {
						return s.compensate(root, applied)
					}
					it.RollbackRef = ref // keep the in-memory copy compensable
				}
				content, gErr := s.cas.Get(it.PatchRef)
				if gErr != nil {
					return s.compensate(root, applied)
				}
				if err := root.WriteAtomic(it.Path, content, 0o644); err != nil {
					return s.compensate(root, applied)
				}
			case m5workspace.ChangeDelete:
				if old, ok := readFileSecure(root, it.Path); ok {
					ref, rErr := s.cas.Put(old)
					if rErr != nil {
						return s.compensate(root, applied)
					}
					if err := st.SetM5ChangeSetItemRollback(it.ID, ref); err != nil {
						return s.compensate(root, applied)
					}
					it.RollbackRef = ref // keep the in-memory copy compensable
					if full, err := root.Resolve(it.Path); err == nil {
						if rmErr := os.Remove(full); rmErr != nil && !os.IsNotExist(rmErr) {
							return s.compensate(root, applied)
						}
					}
				}
			}
			applied = append(applied, it)
		}
		now := s.clock.Now().UTC()
		if _, err = st.TransitionM5Workspace(w.ID, w.Version, w.State, w.UsedBytes+delta, now.Add(m5workspace.ExpiryAfter), now); err != nil {
			return s.compensate(root, applied)
		}
		out, err = st.TransitionM5ChangeSetState(cs.ID, cs.Version, m5workspace.ChangeSetApplied, now, now)
		if err != nil {
			return err
		}
		ameta, amErr := json.Marshal(map[string]any{
			"workspaceId": cs.WorkspaceID, "items": len(items), "usedDelta": delta,
		})
		if amErr != nil {
			return amErr
		}
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "changeset.applied",
			AggregateID: out.ID, Actor: convertActor, Metadata: ameta, CreatedAt: now,
		})
	})
	if err != nil {
		return out, err
	}
	if drifted {
		return out, m5workspace.ErrChangeSetConflict
	}
	return out, nil
}

// compensate undoes already-applied items (delete added files, restore
// captured bytes) and reports the original failure.
func (s *ChangeSetService) compensate(root *SecureRoot, applied []m5workspace.ChangeSetItem) error {
	for i := len(applied) - 1; i >= 0; i-- {
		it := applied[i]
		switch it.Change {
		case m5workspace.ChangeAdd:
			if full, err := root.Resolve(it.Path); err == nil {
				_ = os.Remove(full)
			}
		case m5workspace.ChangeModify, m5workspace.ChangeDelete:
			if it.RollbackRef != "" {
				if content, err := s.cas.Get(it.RollbackRef); err == nil {
					_ = root.WriteAtomic(it.Path, content, 0o644)
				}
			}
		}
	}
	return ErrChangeApplyFailed
}

// ErrChangeApplyFailed marks a compensated apply (all bytes restored).
var ErrChangeApplyFailed = errors.New("workspace: changeset apply failed and was compensated")

// Revert restores the pre-apply bytes of an applied changeset. add entries
// are removed, modify/delete entries restored from their rollback blobs.
func (s *ChangeSetService) Revert(ctx context.Context, changesetID string) (m5workspace.ChangeSet, error) {
	var out m5workspace.ChangeSet
	err := s.uow.TransactAgentRuntime(ctx, func(tx agentrunapp.Tx) error {
		st, err := s.store(tx)
		if err != nil {
			return err
		}
		cs, err := st.GetM5ChangeSet(changesetID)
		if err != nil {
			return err
		}
		if cs.State != m5workspace.ChangeSetApplied {
			return ErrChangeSetStateBad
		}
		w, err := st.GetM5Workspace(cs.WorkspaceID)
		if err != nil {
			return err
		}
		if w.State == m5workspace.StateDeleted {
			return ErrFsWorkspaceGone
		}
		items, err := st.ListM5ChangeSetItems(changesetID)
		if err != nil {
			return err
		}
		root, err := NewSecureRoot(w.RootCanonical)
		if err != nil {
			return err
		}
		var delta int64
		for _, it := range items {
			switch it.Change {
			case m5workspace.ChangeAdd:
				if full, rerr := root.Resolve(it.Path); rerr == nil {
					if rmErr := os.Remove(full); rmErr != nil && !os.IsNotExist(rmErr) {
						return rmErr
					}
				}
				delta -= it.Size
			case m5workspace.ChangeModify, m5workspace.ChangeDelete:
				if it.RollbackRef == "" {
					continue
				}
				content, gerr := s.cas.Get(it.RollbackRef)
				if gerr != nil {
					return gerr
				}
				if werr := root.WriteAtomic(it.Path, content, 0o644); werr != nil {
					return werr
				}
				if it.Change == m5workspace.ChangeModify {
					// it.Size is the applied target size recorded at Stage;
					// reading the file here would measure the just-restored
					// bytes (delta always 0) and never refund the quota
					delta += int64(len(content)) - it.Size
				} else {
					delta += int64(len(content))
				}
			}
		}
		now := s.clock.Now().UTC()
		used := w.UsedBytes + delta
		if used < 0 {
			used = 0
		}
		if _, err = st.TransitionM5Workspace(w.ID, w.Version, w.State, used, now.Add(m5workspace.ExpiryAfter), now); err != nil {
			return err
		}
		out, err = st.TransitionM5ChangeSetState(cs.ID, cs.Version, m5workspace.ChangeSetReverted, time.Time{}, now)
		if err != nil {
			return err
		}
		rmeta, rmErr := json.Marshal(map[string]any{
			"workspaceId": cs.WorkspaceID, "items": len(items), "usedDelta": delta,
		})
		if rmErr != nil {
			return rmErr
		}
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "changeset.reverted",
			AggregateID: out.ID, Actor: convertActor, Metadata: rmeta, CreatedAt: now,
		})
	})
	return out, err
}

func sha256Of(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func readFileSecure(root *SecureRoot, rel string) ([]byte, bool) {
	f, err := root.OpenSecure(rel)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	buf := make([]byte, info.Size())
	n, err := f.ReadAt(buf, 0)
	if err != nil && n != len(buf) {
		return nil, false
	}
	return buf, true
}

// TreeDigest fingerprints a workspace: sorted (path, size, sha256) triples
// hashed in order. Used for base-drift detection at apply time.
func TreeDigest(root *SecureRoot) (string, error) {
	type entry struct {
		path   string
		size   int64
		sum    string
	}
	var entries []entry
	rootPath := root.Root()
	err := filepath.WalkDir(rootPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(rootPath, p)
		if relErr != nil {
			return relErr
		}
		data, rErr := os.ReadFile(p)
		if rErr != nil {
			return rErr
		}
		entries = append(entries, entry{path: filepath.ToSlash(rel), size: int64(len(data)), sum: sha256Of(data)})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	h := sha256.New()
	for _, e := range entries {
		fmt.Fprintf(h, "%s\x00%d\x00%s\x00", e.path, e.size, e.sum)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ErrFsWorkspaceGone re-exports the deleted-workspace refusal.
var ErrFsWorkspaceGone = errors.New("workspace: workspace is deleted")
