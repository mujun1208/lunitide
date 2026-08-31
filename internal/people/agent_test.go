package people_test

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/people"
)

func TestUpsertAgentContactJoinsGroupWithoutPairing(t *testing.T) {
	roster, ident, _ := testRoster(t)
	ctx := context.Background()
	agentID := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if err := roster.UpsertAgentContact(ctx, people.Contact{
		SubjectID: agentID, Nickname: "PPT专家", Avatar: "📊", Title: "产品", Department: "product",
	}); err != nil {
		t.Fatal(err)
	}
	items, err := roster.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var agent people.Contact
	for _, item := range items {
		if item.SubjectID == agentID {
			agent = item
		}
	}
	if !people.IsAgentContact(agent) || agent.TrustState != "trusted" {
		t.Fatalf("agent contact = %+v", agent)
	}
	group, err := roster.CreateGroup(ctx, "同事聊天", ident.SubjectID(), []string{agentID})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := roster.SendAs(ctx, agentID, group.ThreadID, "我来做演示稿。")
	if err != nil {
		t.Fatal(err)
	}
	if msg.SenderID != agentID || msg.Body != "我来做演示稿。" {
		t.Fatalf("send-as = %+v", msg)
	}
}
