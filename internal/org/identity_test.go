// T-9.1.2 DoD: the identity negative matrix (M9-001/002/004/005/006) plus
// the clock-fresh expiry semantics and the revocation watermark.
package org_test

import (
	"context"
	"errors"
	"testing"
	"time"

	. "github.com/lunitide/lunitide/internal/org"
)

func TestIdentity(t *testing.T) {
	gate, svc, clock := newGate(t)
	ctx := context.Background()

	alpha, err := svc.CreateOrg(ctx, "Alpha-Org")
	if err != nil {
		t.Fatal(err)
	}
	ctxA := WithVerifiedOrg(ctx, alpha.OrgID)
	if _, err := svc.ActivateOrg(ctxA); err != nil {
		t.Fatal(err)
	}
	space, err := svc.CreateSpace(ctxA, "core")
	if err != nil {
		t.Fatal(err)
	}
	alice, err := svc.InvitePrincipal(ctxA, "alice", "ext-alice", "https://idp.example", "")
	if err != nil {
		t.Fatal(err)
	}
	expiry := Now(clock.now.Add(2 * time.Hour))
	if _, err := svc.BindRole(ctxA, alice.PrincipalID, space.SpaceID, RoleOperator, expiry); err != nil {
		t.Fatal(err)
	}

	t.Run("M9-001 org not found", func(t *testing.T) {
		ctxGhost := WithVerifiedOrg(ctx, "01GGGGGGGGGGGGGGGGGGGGGGG")
		if _, err := svc.CreateSpace(ctxGhost, "x"); Code(err) != "M9-001" && Code(err) != "M9-003" {
			t.Fatalf("ghost org scope must fail closed, got %v", err)
		}
		if _, err := svc.BindRole(ctxGhost, alice.PrincipalID, "", RoleMember, expiry); Code(err) != "M9-001" && Code(err) != "M9-003" {
			t.Fatalf("want M9-001, got %v", err)
		}
	})

	t.Run("M9-004 teamspace not found", func(t *testing.T) {
		_, err := svc.BindRole(ctxA, alice.PrincipalID, "01SSSSSSSSSSSSSSSSSSSSSSS", RoleOperator, expiry)
		if Code(err) != "M9-004" {
			t.Fatalf("want M9-004, got %v", err)
		}
	})

	t.Run("M9-005 principal expiry is clock fresh with no grace cache", func(t *testing.T) {
		// External identity carries its own expiry.
		bob, err := svc.InvitePrincipal(ctxA, "bob", "ext-bob", "https://idp.example", Now(clock.now.Add(time.Hour)))
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.CheckRole(ctxA, bob.PrincipalID, "", RoleMember); Code(err) != "M9-006" {
			t.Fatalf("precheck: want M9-006 (no binding yet), got %v", err)
		}
		// Advance past the external identity expiry: denial flips to
		// M9-005 without any rebinding - expiry is evaluated live.
		clock.now = clock.now.Add(2 * time.Hour)
		if err := svc.CheckRole(ctxA, bob.PrincipalID, "", RoleMember); Code(err) != "M9-005" {
			t.Fatalf("want M9-005 after external identity expiry, got %v", err)
		}
		// The role binding on alice expired at the same instant too.
		if err := svc.CheckRole(ctxA, alice.PrincipalID, space.SpaceID, RoleOperator); Code(err) != "M9-005" && Code(err) != "M9-006" {
			t.Fatalf("expired binding denied, got %v", err)
		}
		clock.now = clock.now.Add(-2 * time.Hour) // rewind
	})

	t.Run("M9-006 role binding denied", func(t *testing.T) {
		if err := svc.CheckRole(ctxA, alice.PrincipalID, space.SpaceID, RoleApprover); Code(err) != "M9-006" {
			t.Fatalf("want M9-006 for ungranted role, got %v", err)
		}
		// Non-positive duration is rejected at grant time.
		if _, err := svc.BindRole(ctxA, alice.PrincipalID, "", RoleMember, Now(clock.now.Add(-time.Minute))); Code(err) != "M9-006" {
			t.Fatalf("past expiry grant must be denied M9-006, got %v", err)
		}
		// Duplicate live grant denied.
		if _, err := svc.BindRole(ctxA, alice.PrincipalID, space.SpaceID, RoleOperator, expiry); Code(err) != "M9-006" {
			t.Fatalf("duplicate grant must be denied M9-006, got %v", err)
		}
	})

	t.Run("M9-002 suspended org refuses new runs", func(t *testing.T) {
		if err := gate.RawStore().UpdateOrgState(ctxA, alpha.OrgID, OrgSuspended, Now(clock.now)); err != nil {
			t.Fatal(err)
		}
		if err := svc.CheckRole(ctxA, alice.PrincipalID, space.SpaceID, RoleOperator); Code(err) != "M9-002" {
			t.Fatalf("want M9-002 on suspended org, got %v", err)
		}
		if err := gate.RawStore().UpdateOrgState(ctxA, alpha.OrgID, OrgActive, Now(clock.now)); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("revocation watermark kills issued tickets immediately", func(t *testing.T) {
		if err := svc.ValidateTicket(ctxA, alice.PrincipalID, alice.BindingVersion); err != nil {
			t.Fatalf("ticket at current version must pass, got %v", err)
		}
		if err := svc.RevokePrincipal(ctxA, alice.PrincipalID); err != nil {
			t.Fatal(err)
		}
		if err := svc.ValidateTicket(ctxA, alice.PrincipalID, alice.BindingVersion); !errors.Is(err, ErrPrincipalExpired) {
			t.Fatalf("ticket below watermark must be dead (M9-005), got %v", err)
		}
		if err := svc.CheckRole(ctxA, alice.PrincipalID, space.SpaceID, RoleOperator); Code(err) != "M9-005" {
			t.Fatalf("revoked principal denied, got %v", err)
		}
	})

	t.Run("cross org principal bind answers M9-003", func(t *testing.T) {
		beta, err := svc.CreateOrg(ctx, "Beta-Org")
		if err != nil {
			t.Fatal(err)
		}
		ctxB := WithVerifiedOrg(ctx, beta.OrgID)
		if _, err := svc.ActivateOrg(ctxB); err != nil {
			t.Fatal(err)
		}
		_, err = svc.BindRole(ctxB, alice.PrincipalID, "", RoleMember, expiry)
		if Code(err) != "M9-003" {
			t.Fatalf("want M9-003 binding foreign principal, got %v", err)
		}
	})
}
