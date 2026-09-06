package brapp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/oklog/ulid/v2"
)

const (
	ApplyApplied  = "applied"
	ApplyApplying = "applying"
	ApplyFailed   = "failed"
)

// recoverSettings never treats an interrupted application as success. The
// receipt is durable, so the next process can offer an explicit retry.
func (s *Service) recoverSettings(ctx context.Context) error {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	if s.recovered {
		return nil
	}
	err := s.uow.TransactBr(ctx, func(tx Tx) error {
		row, err := tx.GetBrSettings()
		if err != nil || row.ApplyStatus != ApplyApplying {
			return err
		}
		row.ApplyStatus, row.ApplyError = ApplyFailed, "上次应用设置被中断，请重试应用。旧会话在确认停止前不可继续使用。"
		ok, err := tx.CompareAndSwapBrSettings(row.Revision, row)
		if err == nil && !ok {
			return ErrBrConflict
		}
		return err
	})
	if err == nil {
		s.recovered = true
	}
	return err
}

func (s *Service) GetSettings(ctx context.Context) (Settings, error) {
	if s == nil || s.uow == nil {
		return Settings{}, ErrBrNotFound
	}
	if err := s.recoverSettings(ctx); err != nil {
		return Settings{}, err
	}
	var out Settings
	err := s.uow.TransactBr(ctx, func(tx Tx) error {
		var err error
		out, err = tx.GetBrSettings()
		return err
	})
	return out, err
}

type SettingsPatch struct {
	ExpectedRevision    int64
	Mode                *string
	ChromePath          *string
	EdgePath            *string
	ExtensionPort       *int
	Allowlist           *[]string
	DataRetentionDays   *int
	BlockPrivateNetwork *bool
	Actor               string
}

// UpdateSettings commits desired policy first, applies it outside SQLite's
// writer transaction, then records the real outcome. While applying or failed,
// navigation and new connections fail closed against the desired policy.
func (s *Service) UpdateSettings(ctx context.Context, patch SettingsPatch) (Settings, error) {
	if s == nil || s.uow == nil {
		return Settings{}, ErrBrNotFound
	}
	if patch.ExpectedRevision < 1 {
		return Settings{}, ErrBrConflict
	}
	if err := s.recoverSettings(ctx); err != nil {
		return Settings{}, err
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	var out Settings
	var live []Session
	err := s.uow.TransactBr(ctx, func(tx Tx) error {
		cur, err := tx.GetBrSettings()
		if err != nil {
			return err
		}
		if cur.Revision != patch.ExpectedRevision {
			return ErrBrConflict
		}
		next := cur
		if patch.Mode != nil {
			next.Mode = *patch.Mode
		}
		if patch.ChromePath != nil {
			next.ChromePath = *patch.ChromePath
		}
		if patch.EdgePath != nil {
			next.EdgePath = *patch.EdgePath
		}
		if patch.ExtensionPort != nil {
			next.ExtensionPort = *patch.ExtensionPort
		}
		if patch.Allowlist != nil {
			next.Allowlist = append([]string{}, (*patch.Allowlist)...)
		}
		if patch.DataRetentionDays != nil {
			next.DataRetentionDays = *patch.DataRetentionDays
		}
		if patch.BlockPrivateNetwork != nil {
			next.BlockPrivateNetwork = *patch.BlockPrivateNetwork
		}
		if err := ValidateSettings(next); err != nil {
			return err
		}
		needsApply := cur.ApplyStatus != ApplyApplied || next.Mode != cur.Mode || next.ChromePath != cur.ChromePath || next.EdgePath != cur.EdgePath || next.ExtensionPort != cur.ExtensionPort || next.BlockPrivateNetwork != cur.BlockPrivateNetwork || !slices.Equal(next.Allowlist, cur.Allowlist)
		next.Revision++
		next.UpdatedAt = s.clock.Now().UTC().Format(time.RFC3339Nano)
		next.ApplyStatus, next.ApplyError = ApplyApplied, ""
		if needsApply {
			next.ApplyStatus = ApplyApplying
			live, err = tx.ListBrSessions()
			if err != nil {
				return err
			}
		}
		ok, err := tx.CompareAndSwapBrSettings(cur.Revision, next)
		if err != nil {
			return err
		}
		if !ok {
			return ErrBrConflict
		}
		out = next
		return nil
	})
	if err != nil {
		return Settings{}, err
	}
	if out.ApplyStatus == ApplyApplied {
		return out, nil
	}
	applyCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	var applyErr error
	for _, sess := range live {
		if _, err := s.disconnectSession(applyCtx, sess, patch.Actor, "settings-apply"); err != nil {
			applyErr = errors.Join(applyErr, err)
		}
	}
	out.ApplyStatus = ApplyApplied
	if applyErr != nil {
		out.ApplyStatus, out.ApplyError = ApplyFailed, clampDetail(applyErr.Error())
	}
	// Use a separate deadline so an exhausted host deadline cannot erase the
	// failure receipt. If storage fails, the persisted applying intent remains.
	receiptCtx, receiptCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer receiptCancel()
	err = s.uow.TransactBr(receiptCtx, func(tx Tx) error {
		ok, err := tx.CompareAndSwapBrSettings(out.Revision, out)
		if err == nil && !ok {
			return ErrBrConflict
		}
		return err
	})
	return out, err
}

func (s *Service) disconnectSession(ctx context.Context, sess Session, actor, correlation string) (Session, error) {
	if sess.State == StateDisconnected {
		return sess, nil
	}
	hostErr := s.host.Disconnect(ctx, sess.SessionID, sess.Mode)
	sess.UpdatedAt = s.clock.Now().UTC().Format(time.RFC3339Nano)
	if hostErr != nil {
		// Keep the endpoint and connection timestamp: a failed stop is not a
		// disconnected browser, and the same session must remain retryable.
		sess.State, sess.Detail = StateError, clampDetail("停止浏览器失败: "+hostErr.Error())
	} else {
		sess.State, sess.WsURL, sess.Detail, sess.ConnectedAt = StateDisconnected, "", "", ""
	}
	receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := s.uow.TransactBr(receiptCtx, func(tx Tx) error {
		if err := tx.PutBrSession(sess); err != nil {
			return err
		}
		if hostErr != nil {
			return nil
		}
		_, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "browser.disconnected", ResourceType: "br_session", ResourceID: sess.SessionID,
			Actor: actorOr(actor), CorrelationID: correlation, CreatedAt: sess.UpdatedAt,
		})
		return err
	})
	if hostErr != nil {
		hostErr = fmt.Errorf("%w: %s", ErrBrState, sess.Detail)
	}
	return sess, errors.Join(hostErr, err)
}
