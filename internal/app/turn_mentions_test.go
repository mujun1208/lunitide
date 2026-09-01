package app

import "testing"

func TestParseTurnMentionsSameActor(t *testing.T) {
	id := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	token := ParseTurnMentions("[引用专家 安全工程师|" + id + "] 请审一下")
	at := ParseTurnMentions("@安全工程师 请审一下")
	if len(token) != 1 || token[0].Kind != "expert" || token[0].ID != id || token[0].Name != "安全工程师" {
		t.Fatalf("token = %#v", token)
	}
	if len(at) != 1 || at[0].Kind != "member" || at[0].Name != "安全工程师" {
		t.Fatalf("at = %#v", at)
	}
	if extractExpertRefNames("[引用专家 安全工程师|" + id + "] 请审一下")[0] != "安全工程师" {
		t.Fatal("extractExpertRefNames must keep the display name")
	}
}
