package mroapp

import (
	"testing"
	"time"
)

func TestCanCheckoutToolRejectsOverdue(t *testing.T) {
	today := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	ok, reason := CanCheckoutTool(Tool{CalibDue: "2020-01-01"}, today)
	if ok || reason != "校准过期" {
		t.Fatalf("overdue checkout = %v %q", ok, reason)
	}
	ok, reason = CanCheckoutTool(Tool{CalibDue: "2026-12-01"}, today)
	if !ok || reason != "" {
		t.Fatalf("valid checkout = %v %q", ok, reason)
	}
	ok, reason = CanCheckoutTool(Tool{}, today)
	if ok || reason != "校准到期未录入" {
		t.Fatalf("missing calib = %v %q", ok, reason)
	}
}

func TestTraceLotParentToTail(t *testing.T) {
	lots := []ChemLot{
		{ID: "p", LotNo: "M-1"},
		{ID: "c", LotNo: "M-1-A", ParentLotID: "p"},
	}
	uses := []ChemUse{{LotID: "c", TailNo: "B-0001"}}
	got := TraceLot(lots, uses, "p")
	if len(got.Children) != 1 || got.Children[0] != "c" {
		t.Fatalf("children = %#v", got.Children)
	}
	if len(got.Tails) != 1 || got.Tails[0] != "B-0001" {
		t.Fatalf("tails = %#v", got.Tails)
	}
}

func TestKitShortageListsMissingPN(t *testing.T) {
	got := KitShortage([]KitItem{{PN: "SEAL-1", Required: 2, OnHand: 0}, {PN: "OK", Required: 1, OnHand: 1}})
	if len(got) != 1 || got[0] != "SEAL-1" {
		t.Fatalf("shortage = %#v", got)
	}
}
