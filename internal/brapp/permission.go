package brapp

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
)

// ── permission approval ─────────────────────────────────────────────────────

// RequestPermission enqueues one ask; allow/deny policies resolve
// immediately at creation.
func (s *Service) RequestPermission(ctx context.Context, origin, permission, sessionID, actor string) (Permission, error) {
	if s == nil || s.uow == nil {
		return Permission{}, ErrBrNotFound
	}
	switch permission {
	case PermGeolocation, PermCamera, PermMicrophone, PermNotifications, PermClipboardRead, PermDownloads:
	default:
		return Permission{}, fmt.Errorf("%w: permission %q", ErrBrSchema, permission)
	}
	if len(origin) < 1 || len(origin) > 512 {
		return Permission{}, fmt.Errorf("%w: origin length", ErrBrSchema)
	}
	if u, err := url.Parse(origin); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Permission{}, fmt.Errorf("%w: origin must be http(s)://host[:port]", ErrBrSchema)
	}
	if sessionID != "" && len(sessionID) > 64 {
		return Permission{}, fmt.Errorf("%w: sessionId length", ErrBrSchema)
	}
	now := s.clock.Now().UTC()
	ts := now.Format(time.RFC3339)
	var out Permission
	err := s.uow.TransactBr(ctx, func(tx Tx) error {
		policy := PolicyAsk
		if latest, err := tx.FindBrPermission(origin, permission); err == nil {
			policy = latest.Policy
		} else if !isNotFound(err) {
			return err
		}
		row := Permission{
			PermissionID: "brp-" + ulid.Make().String(),
			Origin:       origin, Permission: permission, Policy: policy,
			State: PermStatePending, SessionID: sessionID, CreatedAt: ts,
		}
		switch policy {
		case PolicyAllow:
			row.State = PermStateGranted
			row.DecidedAt = ts
		case PolicyDeny:
			row.State = PermStateDenied
			row.DecidedAt = ts
		}
		if err := tx.PutBrPermission(row); err != nil {
			return err
		}
		out = row
		if row.State != PermStatePending {
			action := "browser.permission.denied"
			if row.State == PermStateGranted {
				action = "browser.permission.granted"
			}
			_, err := tx.AppendAuditEvent(audit.Event{
				ID: ulid.Make().String(), Action: action,
				ResourceType: "br_permission", ResourceID: row.PermissionID,
				Actor: actorOr(actor), CorrelationID: "policy:" + policy, CreatedAt: ts,
			})
			return err
		}
		return nil
	})
	if err != nil {
		return Permission{}, err
	}
	return out, nil
}

// ListPermissions answers the queue (state filter optional).
func (s *Service) ListPermissions(ctx context.Context, state string) ([]Permission, error) {
	if s == nil || s.uow == nil {
		return nil, ErrBrNotFound
	}
	if state != "" && state != PermStatePending && state != PermStateGranted && state != PermStateDenied {
		return nil, fmt.Errorf("%w: state %q", ErrBrSchema, state)
	}
	var out []Permission
	err := s.uow.TransactBr(ctx, func(tx Tx) error {
		list, err := tx.ListBrPermissions(state)
		out = list
		return err
	})
	return out, err
}

// DecidePermission resolves one pending ask (grant/deny) and audits
// browser.permission.granted / browser.permission.denied.
func (s *Service) DecidePermission(ctx context.Context, permissionID, decision, actor string) (Permission, error) {
	if s == nil || s.uow == nil {
		return Permission{}, ErrBrNotFound
	}
	if decision != "grant" && decision != "deny" {
		return Permission{}, fmt.Errorf("%w: decision %q", ErrBrSchema, decision)
	}
	if len(permissionID) < 1 || len(permissionID) > 64 {
		return Permission{}, fmt.Errorf("%w: permissionId length", ErrBrSchema)
	}
	now := s.clock.Now().UTC()
	ts := now.Format(time.RFC3339)
	var out Permission
	err := s.uow.TransactBr(ctx, func(tx Tx) error {
		row, err := tx.GetBrPermission(permissionID)
		if isNotFound(err) {
			return fmt.Errorf("%w: %s", ErrBrNotFound, permissionID)
		}
		if err != nil {
			return err
		}
		if row.State != PermStatePending {
			return fmt.Errorf("%w: permission already %s", ErrBrState, row.State)
		}
		next := PermStateDenied
		action := "browser.permission.denied"
		if decision == "grant" {
			next = PermStateGranted
			action = "browser.permission.granted"
		}
		if err := tx.UpdateBrPermissionState(permissionID, PermStatePending, next, ts); err != nil {
			return err
		}
		row.State = next
		row.DecidedAt = ts
		if _, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: action,
			ResourceType: "br_permission", ResourceID: permissionID,
			Actor: actorOr(actor), CreatedAt: ts,
		}); err != nil {
			return err
		}
		out = row
		return nil
	})
	if err != nil {
		return Permission{}, err
	}
	return out, nil
}

// SetPermissionPolicy upserts the ask/allow/deny policy for one
// origin+permission pair; a pending row resolves immediately when the
// new policy is not ask.
func (s *Service) SetPermissionPolicy(ctx context.Context, origin, permission, policy, actor string) (Permission, error) {
	if s == nil || s.uow == nil {
		return Permission{}, ErrBrNotFound
	}
	switch policy {
	case PolicyAsk, PolicyAllow, PolicyDeny:
	default:
		return Permission{}, fmt.Errorf("%w: policy %q", ErrBrSchema, policy)
	}
	switch permission {
	case PermGeolocation, PermCamera, PermMicrophone, PermNotifications, PermClipboardRead, PermDownloads:
	default:
		return Permission{}, fmt.Errorf("%w: permission %q", ErrBrSchema, permission)
	}
	if len(origin) < 1 || len(origin) > 512 {
		return Permission{}, fmt.Errorf("%w: origin length", ErrBrSchema)
	}
	if u, err := url.Parse(origin); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Permission{}, fmt.Errorf("%w: origin must be http(s)://host[:port]", ErrBrSchema)
	}
	now := s.clock.Now().UTC()
	ts := now.Format(time.RFC3339)
	var out Permission
	err := s.uow.TransactBr(ctx, func(tx Tx) error {
		latest, err := tx.FindBrPermission(origin, permission)
		if err == nil && latest.State == PermStatePending {
			state := PermStatePending
			decided := ""
			switch policy {
			case PolicyAllow:
				state = PermStateGranted
				decided = ts
			case PolicyDeny:
				state = PermStateDenied
				decided = ts
			}
			if err := tx.ApplyBrPermissionPolicy(latest.PermissionID, policy, state, decided); err != nil {
				return err
			}
			latest.Policy = policy
			latest.State = state
			latest.DecidedAt = decided
			out = latest
			return nil
		}
		if err != nil && !isNotFound(err) {
			return err
		}
		row := Permission{
			PermissionID: "brp-" + ulid.Make().String(),
			Origin:       origin, Permission: permission, Policy: policy,
			State: PermStatePending, CreatedAt: ts,
		}
		switch policy {
		case PolicyAllow:
			row.State = PermStateGranted
			row.DecidedAt = ts
		case PolicyDeny:
			row.State = PermStateDenied
			row.DecidedAt = ts
		}
		if err := tx.PutBrPermission(row); err != nil {
			return err
		}
		out = row
		return nil
	})
	if err != nil {
		return Permission{}, err
	}
	return out, nil
}
