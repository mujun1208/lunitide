package mroapp

import "testing"

func TestPublishScheduleTodos(t *testing.T) {
	todos := PublishScheduleTodos(WorkPackage{ID: "wp1", Title: "C检"})
	if len(todos) != 2 || todos[0].Kind != "kit_staging" || todos[1].Kind != "parts_request" {
		t.Fatalf("todos = %#v", todos)
	}
	if todos[0].Ref != "wp1" || todos[0].Status != "open" {
		t.Fatalf("ref/status = %#v", todos[0])
	}
}

func TestQualityBulletinChainListsTails(t *testing.T) {
	got := QualityBulletinChain("lot-1", []ChemUse{{LotID: "lot-1", TailNo: "B-9"}})
	if len(got.Tails) != 1 || got.Tails[0] != "B-9" || !got.Freeze || !got.RecomputeDue {
		t.Fatalf("chain = %+v", got)
	}
}
