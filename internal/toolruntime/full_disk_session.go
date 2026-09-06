package toolruntime

import (
	"context"
	"errors"
	"time"
)

// ErrFullDiskConfirmRequired is returned by the unconfined execute path when
// full-disk full-access is armed (command-policy.json "fullAccess": true) but
// the current session has not yet confirmed the one-time full-disk unlock.
//
// S-05 (simplified, session-level one-time confirmation): the persisted
// opt-in only ARMS the capability. Actually running an unconfined mutating
// tool additionally needs an explicit per-session confirmation that lives in
// process memory and is therefore lost on engine restart — a new session (or
// a restart) must confirm again. This keeps a single settings toggle from
// silently granting every future session unconfined disk access.
var ErrFullDiskConfirmRequired = errors.New("full-disk access needs a one-time confirmation for this session")

// fullDiskSessionConfirmed reports whether session already confirmed the
// one-time full-disk unlock in this engine process.
func (r *Runtime) fullDiskSessionConfirmed(session string) bool {
	if session == "" {
		return false
	}
	r.fullDiskMu.Lock()
	defer r.fullDiskMu.Unlock()
	return r.fullDiskSessions[session]
}

// FullDiskSessionConfirmed is the exported read used by the app layer to decide
// whether a full-disk chat turn may run unconfined without prompting again.
func (r *Runtime) FullDiskSessionConfirmed(session string) bool {
	return r.fullDiskSessionConfirmed(session)
}

// ConfirmFullDiskSession records the one-time per-session full-disk unlock and
// appends an audit row (action full_disk.session.confirm). The confirmation is
// in-memory only: it is intentionally not persisted so an engine restart forces
// a fresh confirmation. Confirming without the persisted opt-in is refused so a
// confirmation can never widen access on its own.
func (r *Runtime) ConfirmFullDiskSession(ctx context.Context, session string) error {
	if session == "" {
		return errors.New("session required")
	}
	if !r.FullDiskEnabled() {
		return errors.New("full-disk full-access is not enabled")
	}
	r.fullDiskMu.Lock()
	if r.fullDiskSessions == nil {
		r.fullDiskSessions = make(map[string]bool)
	}
	already := r.fullDiskSessions[session]
	r.fullDiskSessions[session] = true
	r.fullDiskMu.Unlock()
	if !already {
		r.recordFullDiskAudit(ctx, session, "confirm")
	}
	return nil
}

// RevokeFullDiskSession clears the one-time confirmation for one session
// (e.g. the operator turned the opt-in off mid-session). Best-effort audit.
func (r *Runtime) RevokeFullDiskSession(ctx context.Context, session string) {
	if session == "" {
		return
	}
	r.fullDiskMu.Lock()
	had := r.fullDiskSessions[session]
	delete(r.fullDiskSessions, session)
	r.fullDiskMu.Unlock()
	if had {
		r.recordFullDiskAudit(ctx, session, "revoke")
	}
}

// recordFullDiskAudit appends one row to full_disk_confirmations. A storage
// failure never breaks the confirmation — the audit is best-effort, exactly
// like recordHookEvents.
func (r *Runtime) recordFullDiskAudit(ctx context.Context, session, action string) {
	if err := r.ensureAudit(); err != nil || r.db == nil {
		return
	}
	_, _ = r.db.ExecContext(ctx,
		`INSERT INTO full_disk_confirmations(session_id, action, created_at) VALUES(?,?,?)`,
		session, action, r.now().Format(time.RFC3339Nano))
}