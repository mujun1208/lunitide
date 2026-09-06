package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

// S-05: with full-disk armed, the first unconfined mutating call in an
// unconfirmed session must gate (ErrApprovalRequired). After the one-time
// per-session confirmation it runs; a different session still gates.
func TestFullDiskSessionConfirmationGate(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	enableFullDisk(t, r)

	s := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	outside := filepath.Join(t.TempDir(), "note.txt")
	args, _ := json.Marshal(map[string]any{"path": filepath.ToSlash(outside), "content": "hi"})

	// Unconfirmed session: mutating unconfined call gates.
	if _, err := r.ExecuteUnconfined(context.Background(), s, "workspace.write", args, false); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("unconfirmed session must gate, got %v", err)
	}
	if r.FullDiskSessionConfirmed(s) {
		t.Fatal("session must not be confirmed before ConfirmFullDiskSession")
	}

	// Confirm and retry: now it runs.
	if err := r.ConfirmFullDiskSession(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if !r.FullDiskSessionConfirmed(s) {
		t.Fatal("session must be confirmed after ConfirmFullDiskSession")
	}
	if _, err := r.ExecuteUnconfined(context.Background(), s, "workspace.write", args, false); err != nil {
		t.Fatalf("confirmed session write: %v", err)
	}

	// A different session inherits nothing: it still gates.
	other := "01ARZ3NDEKTSV4RRFFQ69G5FBW"
	if _, err := r.ExecuteUnconfined(context.Background(), other, "workspace.write", args, false); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("second session must gate independently, got %v", err)
	}
}

// Confirming without the persisted opt-in is refused so a confirmation can
// never widen access on its own.
func TestConfirmFullDiskSessionRequiresOptIn(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.ConfirmFullDiskSession(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV"); err == nil {
		t.Fatal("confirm without fullAccess opt-in must be refused")
	}
	if r.FullDiskSessionConfirmed("01ARZ3NDEKTSV4RRFFQ69G5FAV") {
		t.Fatal("refused confirm must not mark the session")
	}
}

// Revoking clears the confirmation so the next unconfined call gates again
// (e.g. the operator turned the opt-in off mid-session).
func TestRevokeFullDiskSession(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	enableFullDisk(t, r)
	s := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if err := r.ConfirmFullDiskSession(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	r.RevokeFullDiskSession(context.Background(), s)
	if r.FullDiskSessionConfirmed(s) {
		t.Fatal("revoke must clear the confirmation")
	}
	outside := filepath.Join(t.TempDir(), "note.txt")
	args, _ := json.Marshal(map[string]any{"path": filepath.ToSlash(outside), "content": "hi"})
	if _, err := r.ExecuteUnconfined(context.Background(), s, "workspace.write", args, false); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("revoked session must gate again, got %v", err)
	}
}

// The confirmation and its revocation each leave one audit row.
func TestFullDiskConfirmationAudited(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	enableFullDisk(t, r)
	s := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if err := r.ConfirmFullDiskSession(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	// Confirming twice records only once (idempotent).
	if err := r.ConfirmFullDiskSession(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	r.RevokeFullDiskSession(context.Background(), s)
	if err := r.ensureAudit(); err != nil {
		t.Fatal(err)
	}
	var confirms, revokes int
	if err := r.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM full_disk_confirmations WHERE session_id=? AND action='confirm'`, s).Scan(&confirms); err != nil {
		t.Fatal(err)
	}
	if err := r.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM full_disk_confirmations WHERE session_id=? AND action='revoke'`, s).Scan(&revokes); err != nil {
		t.Fatal(err)
	}
	if confirms != 1 {
		t.Fatalf("confirm audit rows = %d, want 1", confirms)
	}
	if revokes != 1 {
		t.Fatalf("revoke audit rows = %d, want 1", revokes)
	}
}