package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

func ccRecoveryFixture(t *testing.T, count int) (*Store, *ccapp.Service, *fakeCcHost) {
	t.Helper()
	s, err := OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "cc-recovery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	svc := ccapp.New(s.AgentRuntimeRepository())
	host := newFakeCcHost()
	svc.SetHost(host)
	err = s.AgentRuntimeRepository().TransactCc(context.Background(), func(tx ccapp.Tx) error {
		cfg, err := tx.GetCcSettings()
		if err != nil {
			return err
		}
		cfg.Enabled = true
		cfg.Revision++
		if err = tx.PutCcSettings(cfg); err != nil {
			return err
		}
		for i := 0; i < count; i++ {
			meta, _ := json.Marshal(map[string]any{"phase": "prepared", "operationId": ulid.Make().String(), "tool": ccapp.ToolPaste, "risk": ccapp.RiskHigh})
			if err = tx.PutAudit(providerapp.Audit{ID: ulid.Make().String(), Action: "cc.operation.confirmed", AggregateID: "session", Actor: "test", Metadata: meta, CreatedAt: time.Now().UTC()}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, svc, host
}
func TestCcRecoveryConvergesAllPendingIntentsWithoutHostOrReplay(t *testing.T) {
	s, svc, host := ccRecoveryFixture(t, 201)
	count, err := svc.ReconcilePendingIntents(context.Background())
	if err != nil || count != 201 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	count, err = svc.ReconcilePendingIntents(context.Background())
	if err != nil || count != 0 {
		t.Fatalf("replayed count=%d err=%v", count, err)
	}
	var recovered int
	if err = s.db.QueryRow(`SELECT count(*) FROM cc_audit_log WHERE status='failed' AND json_extract(detail_json,'$.outcome')='unknown' AND json_extract(detail_json,'$.phase')='receipt'`).Scan(&recovered); err != nil || recovered != 201 {
		t.Fatalf("recovered=%d err=%v", recovered, err)
	}
	var enabled, stopped int
	if err = s.db.QueryRow(`SELECT enabled,emergency_stopped FROM cc_security_config WHERE id=1`).Scan(&enabled, &stopped); err != nil || enabled != 0 || stopped != 1 {
		t.Fatalf("enabled=%d stopped=%d err=%v", enabled, stopped, err)
	}
	if len(host.typed)+len(host.clicks)+len(host.moves)+len(host.actions)+len(host.shortcuts)+host.captures != 0 {
		t.Fatal("recovery replayed native host")
	}
}
func TestCcRecoveryAuditFailureRollsBackReceiptAndDisarm(t *testing.T) {
	s, svc, _ := ccRecoveryFixture(t, 1)
	if _, err := s.db.Exec(`CREATE TRIGGER reject_cc_recovery BEFORE INSERT ON audit_events WHEN NEW.action='cc.operation.executed' BEGIN SELECT RAISE(ABORT,'mirror unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if count, err := svc.ReconcilePendingIntents(context.Background()); err == nil || count != 0 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM cc_audit_log`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial receipt=%d err=%v", count, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER reject_cc_recovery`); err != nil {
		t.Fatal(err)
	}
	if count, err := svc.ReconcilePendingIntents(context.Background()); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}
