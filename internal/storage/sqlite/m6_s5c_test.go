// Legacy S5 governance repository coverage (migration 0053 tables):
// optimistic CAS on credential/integration/operation rows, UNIQUE tuple
// guards and the atomic commit of the paired audit rows.
package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/m6app"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

func newS5Store(t *testing.T) *Store {
	t.Helper()
	store, err := OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "m6.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestM6S5CredentialCASAndAudit(t *testing.T) {
	store := newS5Store(t)
	repo := store.AgentRuntimeRepository()
	ctx := context.Background()
	now := time.Now().UTC()

	var ref m6supply.CredentialRef
	err := repo.TransactM6(ctx, func(tx m6app.Tx) error {
		ref = m6supply.CredentialRef{
			ID: ulid.Make().String(), Provider: "acme", SecretHandle: "vault://acme/s5",
			ScopesJSON: `["read"]`, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.PutM6CredentialRef(ref); err != nil {
			return err
		}
		if err := tx.PutAudit(auditRow("credential.revoked", ref.ID)); err != nil {
			return err
		}
		revoked, err := tx.RevokeM6CredentialRef(ref.ID, 1, now)
		if err != nil {
			return err
		}
		if revoked.Version != 2 || revoked.RevokedAt == nil {
			t.Fatalf("revoke: %+v", revoked)
		}
		// stale CAS conflicts while the row is still clean is impossible
		// after the revoke above; the idempotent replay answers the row.
		replay, err := tx.RevokeM6CredentialRef(ref.ID, 99, now)
		if err != nil {
			return err
		}
		if replay.RevokedAt == nil || replay.Version != 2 {
			t.Fatalf("replay must answer the stored revocation: %+v", replay)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// the audit row committed atomically with the entity writes
	var n int
	if err := store.db.QueryRow(`SELECT count(*) FROM audit_events WHERE action='credential.revoked' AND aggregate_id=?`, ref.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("credential.revoked audit rows: %d", n)
	}

	// unknown ref answers ErrNotFound
	_ = repo.TransactM6(ctx, func(tx m6app.Tx) error {
		if _, err := tx.GetM6CredentialRef("01AAAAAAAAAAAAAAAAAAAAAAAA"); err != m6supply.ErrNotFound {
			t.Fatalf("unknown credential: %v", err)
		}
		return nil
	})
}

func TestM6S5IntegrationTransitionCAS(t *testing.T) {
	store := newS5Store(t)
	repo := store.AgentRuntimeRepository()
	ctx := context.Background()
	now := time.Now().UTC()

	ig := m6supply.Integration{
		ID: ulid.Make().String(), Name: "s5-cas", Kind: m6supply.IntegrationKindOpenAPI,
		SpecDigest: "aaaa", SpecVersion: "1", AuthType: m6supply.AuthTypeNone,
		Direction: m6supply.DirectionOutbound, Role: m6supply.RoleClient,
		EnvironmentBindings: `{"development":{}}`, State: m6supply.IntegrationDraft,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	// spec_digest CHECK wants 64 hex — use a real one
	ig.SpecDigest = strings.Repeat("a", 64)

	var moved m6supply.Integration
	err := repo.TransactM6(ctx, func(tx m6app.Tx) error {
		if err := tx.PutM6Integration(ig); err != nil {
			return err
		}
		next, err := tx.TransitionM6Integration(ig.ID, 1, m6supply.IntegrationValidating, now)
		if err != nil {
			return err
		}
		moved = next
		// stale CAS conflicts
		if _, err := tx.TransitionM6Integration(ig.ID, 1, m6supply.IntegrationActive, now); err != m6supply.ErrVersionConflict {
			t.Fatalf("stale transition: %v", err)
		}
		// unknown row conflicts too (caller Get maps it first)
		if _, err := tx.TransitionM6Integration("01AAAAAAAAAAAAAAAAAAAAAAAA", 1, m6supply.IntegrationActive, now); err != m6supply.ErrVersionConflict {
			t.Fatalf("unknown transition: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if moved.State != m6supply.IntegrationValidating || moved.Version != 2 {
		t.Fatalf("transition: %+v", moved)
	}
}

func TestM6S5OperationAndMappingGuards(t *testing.T) {
	store := newS5Store(t)
	repo := store.AgentRuntimeRepository()
	ctx := context.Background()
	now := time.Now().UTC()

	ig := m6supply.Integration{
		ID: ulid.Make().String(), Name: "s5-ops", Kind: m6supply.IntegrationKindOpenAPI,
		SpecDigest: strings.Repeat("b", 64), SpecVersion: "1", AuthType: m6supply.AuthTypeNone,
		Direction: m6supply.DirectionInbound, Role: m6supply.RoleServer,
		EnvironmentBindings: `{"test":{}}`, State: m6supply.IntegrationDraft,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	op := m6supply.ApiOperation{
		ID: ulid.Make().String(), IntegrationID: ig.ID, OperationID: "opOne",
		Method: "GET", PathTemplate: "/one", InputSchemaJSON: `{"type":"object"}`,
		OutputSchemaJSON: `{"type":"object"}`, Risk: m6supply.OperationRiskLow,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	mapping := m6supply.FieldMapping{
		ID: ulid.Make().String(), OperationRowID: op.ID, Source: "a", Target: "b",
		Direction: m6supply.MappingRequest, TransformID: "identity",
		SchemaVersion: 1, CreatedAt: now,
	}

	err := repo.TransactM6(ctx, func(tx m6app.Tx) error {
		if err := tx.PutM6Integration(ig); err != nil {
			return err
		}
		if err := tx.PutM6ApiOperation(op); err != nil {
			return err
		}
		if err := tx.PutM6FieldMapping(mapping); err != nil {
			return err
		}
		// enabled CAS
		enabled, err := tx.SetM6ApiOperationEnabled(op.ID, 1, true, now)
		if err != nil {
			return err
		}
		if !enabled.Enabled || enabled.Version != 2 {
			t.Fatalf("enable: %+v", enabled)
		}
		if _, err := tx.SetM6ApiOperationEnabled(op.ID, 1, false, now); err != m6supply.ErrVersionConflict {
			t.Fatalf("stale enable: %v", err)
		}
		// lookups
		found, err := tx.FindM6ApiOperationByOperationID(ig.ID, "opOne")
		if err != nil || found.ID != op.ID {
			t.Fatalf("findByOperationID: %+v %v", found, err)
		}
		if _, err := tx.FindM6FieldMapping(op.ID, "a", "b", m6supply.MappingRequest); err != nil {
			t.Fatalf("findMapping: %v", err)
		}
		ops, err := tx.ListM6ApiOperations(ig.ID)
		if err != nil || len(ops) != 1 {
			t.Fatalf("listOperations: %d %v", len(ops), err)
		}
		maps, err := tx.ListM6FieldMappings(op.ID)
		if err != nil || len(maps) != 1 {
			t.Fatalf("listMappings: %d %v", len(maps), err)
		}
		integrations, err := tx.ListM6Integrations()
		if err != nil || len(integrations) != 1 {
			t.Fatalf("listIntegrations: %d %v", len(integrations), err)
		}
		byName, err := tx.FindM6IntegrationByName("s5-ops", "1")
		if err != nil || byName.ID != ig.ID {
			t.Fatalf("findByName: %+v %v", byName, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// UNIQUE guards poison the writer transaction (statement error), so the
	// duplicate probes run as their own expected-to-fail transactions.
	dupOp := op
	dupOp.ID = ulid.Make().String()
	if err := repo.TransactM6(ctx, func(tx m6app.Tx) error {
		return tx.PutM6ApiOperation(dupOp)
	}); err == nil {
		t.Fatal("duplicate operationId must violate the UNIQUE guard")
	}
	dupMap := mapping
	dupMap.ID = ulid.Make().String()
	if err := repo.TransactM6(ctx, func(tx m6app.Tx) error {
		return tx.PutM6FieldMapping(dupMap)
	}); err == nil {
		t.Fatal("duplicate mapping tuple must violate the UNIQUE guard")
	}
	// the failed probes rolled back — the committed rows are intact
	var n int
	if err := store.db.QueryRow(`SELECT count(*) FROM m6_api_operation`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("operation rows after rollback: %d", n)
	}
}

func auditRow(action, aggregate string) providerapp.Audit {
	return providerapp.Audit{ID: ulid.Make().String(), Action: action, AggregateID: aggregate, Actor: "desktop-host", Metadata: []byte(`{}`), CreatedAt: time.Now().UTC()}
}
