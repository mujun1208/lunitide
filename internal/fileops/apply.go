package fileops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

func (s *Service) Apply(ctx context.Context, id string) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	plan, err := s.Get(id)
	if err != nil {
		return Status{}, err
	}
	st, err := s.Status(id)
	if err != nil {
		return Status{}, err
	}
	if st.State == "applied" {
		return st, nil
	}
	if st.State == "undone" {
		return st, ErrAlreadyUndone
	}
	done := map[string]ItemStatus{}
	if st.State == "partial" {
		for _, item := range st.Items {
			if item.State == "succeeded" && item.ID != "" {
				done[item.ID] = item
			}
		}
	}
	st.Items = make([]ItemStatus, len(plan.Items))
	for i, item := range plan.Items {
		if err := ctx.Err(); err != nil {
			st.State = "partial"
			_ = s.writeJSON(s.statusPath(id), st)
			return st, err
		}
		if prev, ok := done[item.ID]; ok {
			st.Items[i] = prev
			continue
		}
		itemSt := ItemStatus{ID: item.ID, State: "running"}
		if appErr := s.applyItem(item, &itemSt); appErr != nil {
			itemSt.State = "failed"
			itemSt.Error = UserMessage(appErr)
			st.Items[i] = itemSt
			st.State = "partial"
			_ = s.writeJSON(s.statusPath(id), st)
			if errors.Is(appErr, ErrInputChanged) {
				return st, ErrInputChanged
			}
			if errors.Is(appErr, ErrCollision) {
				return st, ErrCollision
			}
			if errors.Is(appErr, ErrCrossVolume) {
				return st, ErrCrossVolume
			}
			if errors.Is(appErr, ErrOutsideRoot) {
				return st, ErrOutsideRoot
			}
			return st, ErrApplyPartial
		}
		itemSt.State = "succeeded"
		st.Items[i] = itemSt
	}
	st.State = "applied"
	st.AppliedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.writeJSON(s.statusPath(id), st); err != nil {
		return st, err
	}
	return st, nil
}

func (s *Service) applyItem(item Item, st *ItemStatus) error {
	if err := s.confineItem(item); err != nil {
		return err
	}
	if err := s.verifySource(item); err != nil {
		return err
	}
	switch item.Action {
	case ActionMkdir:
		return os.MkdirAll(s.abs(item.To), 0700)
	case ActionRename, ActionMove:
		from, to := s.abs(item.From), s.abs(item.To)
		if err := os.MkdirAll(filepath.Dir(to), 0700); err != nil {
			return err
		}
		if _, err := os.Stat(to); err == nil {
			return ErrCollision
		}
		st.UndoTo = item.From
		return mapRenameError(os.Rename(from, to))
	case ActionWrite:
		to := s.abs(item.To)
		if err := os.MkdirAll(filepath.Dir(to), 0700); err != nil {
			return err
		}
		if _, err := os.Stat(to); err == nil {
			return ErrCollision
		}
		st.UndoTo = item.To
		return os.WriteFile(to, []byte(item.Body), 0600)
	default:
		return ErrInvalidPlan
	}
}

func (s *Service) Undo(ctx context.Context, id string) (Status, error) {
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	plan, err := s.Get(id)
	if err != nil {
		return Status{}, err
	}
	st, err := s.Status(id)
	if err != nil {
		return Status{}, err
	}
	if st.State != "applied" && st.State != "partial" {
		return st, ErrNothingToUndo
	}
	byID := map[string]Item{}
	for _, item := range plan.Items {
		byID[item.ID] = item
	}
	for i := len(st.Items) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return st, err
		}
		itemSt := st.Items[i]
		if itemSt.State != "succeeded" {
			continue
		}
		item := byID[itemSt.ID]
		if err := s.confineItem(item); err != nil {
			st.Items[i].Error = UserMessage(err)
			st.State = "partial"
			_ = s.writeJSON(s.statusPath(id), st)
			return st, err
		}
		if err := s.undoItem(item, itemSt); err != nil {
			st.Items[i].Error = UserMessage(err)
			st.State = "partial"
			_ = s.writeJSON(s.statusPath(id), st)
			return st, err
		}
		st.Items[i].State = "undone"
	}
	st.State = "undone"
	st.UndoneAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.writeJSON(s.statusPath(id), st); err != nil {
		return st, err
	}
	return st, nil
}

func (s *Service) undoItem(item Item, st ItemStatus) error {
	switch item.Action {
	case ActionMkdir:
		return os.Remove(s.abs(item.To))
	case ActionRename, ActionMove:
		from, to := s.abs(item.To), s.abs(item.From)
		if st.UndoTo != "" {
			to = s.abs(st.UndoTo)
		}
		if _, err := os.Stat(to); err == nil {
			return ErrUndoCollision
		}
		return mapRenameError(os.Rename(from, to))
	case ActionWrite:
		return os.Remove(s.abs(item.To))
	default:
		return ErrInvalidPlan
	}
}
