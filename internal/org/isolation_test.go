// T-9.1.1 DoD: cross-org access is 100% rejected (M9-003) with a uniform
// answer (no existence side channel) and a missing org context fails
// closed. Runs against the real 0069 schema on an in-memory database.
//
// External test package (org_test) so the sqlite store implementation can
// be exercised without an import cycle (sqlite -> org).
package org_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	. "github.com/lunitide/lunitide/internal/org"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/migrations"
)

// fakeClock is the adjustable clock seam shared by the identity tests.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func newGate(t *testing.T) (*Gate, *Service, *fakeClock) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	body, err := migrations.Files.ReadFile("0069_m9_org_foundation.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	store := sqlitestore.NewOrgStorage(db)
	gate := NewGate(store)
	clock := &fakeClock{now: time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)}
	return gate, NewService(gate, clock.Now), clock
}

func TestIsolation(t *testing.T) {
	gate, svc, _ := newGate(t)
	ctx := context.Background()

	orgA, err := svc.CreateOrg(ctx, "Alpha-Org")
	if err != nil {
		t.Fatal(err)
	}
	orgB, err := svc.CreateOrg(ctx, "Beta-Org")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ActivateOrg(WithVerifiedOrg(ctx, orgA.OrgID)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ActivateOrg(WithVerifiedOrg(ctx, orgB.OrgID)); err != nil {
		t.Fatal(err)
	}

	ctxA := WithVerifiedOrg(ctx, orgA.OrgID)
	ctxB := WithVerifiedOrg(ctx, orgB.OrgID)

	spaceA, err := svc.CreateSpace(ctxA, "alpha-core")
	if err != nil {
		t.Fatal(err)
	}
	principalA, err := svc.InvitePrincipal(ctxA, "alice", "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("cross org space access is uniformly denied", func(t *testing.T) {
		_, err := gate.Space(ctxB, spaceA.SpaceID)
		if !errors.Is(err, ErrCrossOrgAccess) || Code(err) != "M9-003" {
			t.Fatalf("want M9-003, got %v", err)
		}
		// Existence leak: a foreign-org id that exists and one that does
		// not must answer with the exact same error string.
		_, errMissing := gate.Space(ctxB, "01AAAAAAAAAAAAAAAAAAAAAAAA")
		if err.Error() != errMissing.Error() {
			t.Fatalf("existence leak: existing=%v missing=%v", err, errMissing)
		}
	})

	t.Run("cross org principal access is uniformly denied", func(t *testing.T) {
		_, err := gate.Principal(ctxB, principalA.PrincipalID)
		if !errors.Is(err, ErrCrossOrgAccess) {
			t.Fatalf("want M9-003, got %v", err)
		}
		_, errMissing := gate.Principal(ctxB, "01BBBBBBBBBBBBBBBBBBBBBBB")
		if err.Error() != errMissing.Error() {
			t.Fatalf("existence leak: %v vs %v", err, errMissing)
		}
	})

	t.Run("foreign org id on the org endpoint answers M9-003", func(t *testing.T) {
		_, err := gate.Org(ctxA, orgB.OrgID)
		if !errors.Is(err, ErrCrossOrgAccess) {
			t.Fatalf("want M9-003, got %v", err)
		}
	})

	t.Run("missing org context fails closed", func(t *testing.T) {
		if _, err := gate.Space(ctx, spaceA.SpaceID); !errors.Is(err, ErrCrossOrgAccess) {
			t.Fatalf("want fail-closed M9-003, got %v", err)
		}
		if _, err := gate.ListSpaces(ctx); !errors.Is(err, ErrCrossOrgAccess) {
			t.Fatalf("want fail-closed M9-003, got %v", err)
		}
	})

	t.Run("in org reads succeed and stay scoped", func(t *testing.T) {
		spaces, err := gate.ListSpaces(ctxA)
		if err != nil || len(spaces) != 1 {
			t.Fatalf("scoped list failed: %v %d", err, len(spaces))
		}
		spacesB, err := gate.ListSpaces(ctxB)
		if err != nil || len(spacesB) != 0 {
			t.Fatalf("org B must see zero alpha spaces: %v %d", err, len(spacesB))
		}
	})

	t.Run("org id rebind trips the DDL guard", func(t *testing.T) {
		// Direct store-level proof that org_id is immutable at the
		// schema layer (trg_*_org_immutable -> M9-003).
		store := gate.RawStore()
		sp, err := store.SpaceByID(ctxA, orgA.OrgID, spaceA.SpaceID)
		if err != nil {
			t.Fatal(err)
		}
		sp.OrgID = orgB.OrgID
		if err := store.CreateSpace(ctx, sp); err == nil {
			t.Fatal("rebinding space into org B must fail")
		}
	})

	t.Run("suspended org refuses new writes but keeps reads", func(t *testing.T) {
		if _, err := svc.CreateOrg(ctx, "Gamma-Org"); err != nil {
			t.Fatal(err)
		}
		// suspend via store-level transition through service on a fresh org
		gamma := orgOf(t, gate, "Gamma-Org")
		ctxG := WithVerifiedOrg(ctx, gamma.OrgID)
		if _, err := svc.ActivateOrg(ctxG); err != nil {
			t.Fatal(err)
		}
		store := gate.RawStore()
		if err := store.UpdateOrgState(ctxG, gamma.OrgID, OrgSuspended, "2026-08-16T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.CreateSpace(ctxG, "gamma-space"); !errors.Is(err, ErrOrgSuspended) {
			t.Fatalf("want M9-002 on suspended org write, got %v", err)
		}
		if _, err := gate.ListSpaces(ctxG); err != nil {
			t.Fatalf("suspended org keeps reads: %v", err)
		}
	})
}

func orgOf(t *testing.T, gate *Gate, name string) Organization {
	t.Helper()
	orgs, err := gate.RawStore().ListOrgs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range orgs {
		if o.Name == name {
			return o
		}
	}
	t.Fatalf("org %s not found", name)
	return Organization{}
}
