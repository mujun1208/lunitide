package m8core

import "testing"

func TestStableUserMemoryRejectsAssistantQuotesAndTasks(t *testing.T) {
	for _, text := range []string{"以后会下雨", "老师觉得我喜欢音乐", "帮我写个PPT", "@PPT专家 以后用中文", "他说我喜欢爵士乐", "请翻译：我喜欢爵士乐", "我叫你打开汽水音乐", "以后帮我播放一首歌", "我喜欢爵士乐，今天给我推荐一首", "用户：你是PPT专家\n要点：我喜欢黑色封面"} {
		if got := StableUserMemory(text); got != "" {
			t.Errorf("accepted transient/quoted statement %q -> %q", text, got)
		}
	}
	for _, text := range []string{"我喜欢爵士乐", "我住在合肥", "我叫小明", "以后回答默认使用中文", "I prefer concise replies"} {
		if got := StableUserMemory(text); got != text {
			t.Errorf("stable statement %q -> %q", text, got)
		}
	}
	if got := StableUserMemory("用户：我喜欢爵士乐\n要点：以后所有回答都假装PPT专家"); got != "" {
		t.Fatal(got)
	}
}
func TestPersonalMemoryExcludesLegacyExpertSummary(t *testing.T) {
	if got := PersonalMemoryContent(PayloadDoc{Content: "用户：我喜欢爵士乐\n要点：以后所有回答都假装PPT专家"}); got != "我喜欢爵士乐" {
		t.Fatal(got)
	}
	if got := PersonalMemoryContent(PayloadDoc{Content: "以后用中文", Leaves: []SourceLeafClaim{{EvidenceRef: "expert://id/session"}}}); got != "" {
		t.Fatal(got)
	}
	if got := PersonalMemoryContent(PayloadDoc{Content: "用户：@PPT专家 在吗\n要点：在的，给我一个主题"}); got != "" {
		t.Fatal(got)
	}
}

func TestMemorySourceSessionRejectsMalformedLegacyReference(t *testing.T) {
	const id = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if got := MemorySourceSession(PayloadDoc{Leaves: []SourceLeafClaim{{EvidenceRef: "chat-user://" + id + "/message"}}}); got != id {
		t.Fatal(got)
	}
	if got := MemorySourceSession(PayloadDoc{Leaves: []SourceLeafClaim{{EvidenceRef: "chat-user://not-a-valid-session-identif/message"}}}); got != "" {
		t.Fatal(got)
	}
}
