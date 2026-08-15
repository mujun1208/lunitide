package sqlite

import (
	"testing"

	"github.com/lunitide/lunitide/internal/m6app"
	"github.com/lunitide/lunitide/internal/stdioworker"
)

// TestStdioWorkerAuditActionsAccepted (slice 5B): every stdio.worker.*
// action of migration 0050 persists through the m6app bridge into the
// rebuilt audit_events table, and an action outside the catalog is
// refused by the bridge before touching the schema CHECK.
func TestStdioWorkerAuditActionsAccepted(t *testing.T) {
	s := openAppStore(t, "stdio-audit")
	sink := m6app.NewStdioAuditSink(s.AgentRuntimeRepository())
	for _, action := range []string{
		stdioworker.AuditLaunched, stdioworker.AuditCompleted,
		stdioworker.AuditRevoked, stdioworker.AuditExpired,
		stdioworker.AuditRecovered,
	} {
		if err := sink.Emit(action, "01JZZSTDIOWORKERRUN0000", "stdio-runtime", []byte(`{"probe":true}`)); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM audit_events WHERE action LIKE 'stdio.worker.%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("want 5 stdio.worker.* rows, got %d", n)
	}
	if err := sink.Emit("stdio.worker.fabricated", "01JZZSTDIOWORKERRUN0000", "stdio-runtime", []byte(`{}`)); err == nil {
		t.Fatal("action outside the 0050 catalog must be refused by the bridge")
	}
}
