package sqlite

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/people"
)

func TestThreadIdentityCollisionCannotAddGroupMembers(t *testing.T) {
	const self = "01ARZ3NDEKTSV4RRFFQ69G5FAA"
	const owner = "01ARZ3NDEKTSV4RRFFQ69G5FAB"
	const attacker = "01ARZ3NDEKTSV4RRFFQ69G5FAC"
	s := openAppStore(t, "people-identity")
	ctx := context.Background()
	for _, id := range []string{self, owner, attacker} {
		if err := s.UpsertContact(ctx, people.Contact{SubjectID: id, Nickname: id, TrustState: "trusted", Status: "offline", CreatedAt: "2026-09-06T00:00:00Z", UpdatedAt: "2026-09-06T00:00:00Z"}); err != nil {
			t.Fatal(err)
		}
	}
	group := people.Thread{ThreadID: "01ARZ3NDEKTSV4RRFFQ69G5FAD", Kind: "group", Title: "Private", OwnerID: owner, CreatedAt: "2026-09-06T00:00:00Z", UpdatedAt: "2026-09-06T00:00:00Z"}
	if err := s.InsertThread(ctx, group, []string{self, owner}, owner); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"direct", "group"} {
		forged := group
		forged.Kind, forged.OwnerID = kind, attacker
		if err := s.InsertThread(ctx, forged, []string{self, attacker}, attacker); err == nil {
			t.Fatal("colliding thread creation succeeded")
		}
		got, err := s.GetThread(ctx, group.ThreadID)
		if err != nil || got.Kind != "group" || got.OwnerID != owner || len(got.Members) != 2 {
			t.Fatalf("protected group mutated: %#v %v", got, err)
		}
		for _, member := range got.Members {
			if member.SubjectID == attacker {
				t.Fatal("attacker added to protected group")
			}
		}
	}
}
