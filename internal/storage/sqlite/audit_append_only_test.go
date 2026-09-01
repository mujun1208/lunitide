package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// audit_events is where a surprising action gets reconstructed from after the
// fact, so it has to survive the code that writes it. Every other evidence
// table in this schema is already sealed (trg_sao_*, trg_art_immutable_*,
// trg_evd_*); this one could still be rewritten or erased outright.
func TestAuditEventsRejectUpdateAndDelete(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.newULID(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_events(id,action,aggregate_id,actor,metadata_json,created_at) VALUES(?,?,?,?,?,?)`,
		id, "cc.operation.executed", id, "engine", `{"tool":"cc.mouse_click","risk":"medium"}`, formatTime(time.Now().UTC())); err != nil {
		t.Fatalf("append audit row: %v", err)
	}

	_, err = s.db.ExecContext(ctx, `UPDATE audit_events SET action='web.fetched' WHERE id=?`, id)
	if err == nil || !strings.Contains(err.Error(), "M10-AUD-001") {
		t.Fatalf("audit row was editable: err=%v", err)
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM audit_events WHERE id=?`, id)
	if err == nil || !strings.Contains(err.Error(), "M10-AUD-001") {
		t.Fatalf("audit row was deletable: err=%v", err)
	}

	var action string
	if err := s.db.QueryRowContext(ctx, `SELECT action FROM audit_events WHERE id=?`, id).Scan(&action); err != nil {
		t.Fatal(err)
	}
	if action != "cc.operation.executed" {
		t.Fatalf("action = %q; the original row must be untouched", action)
	}
}
