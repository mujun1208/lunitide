package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestOpenPageFileFromTheSideBrowserMessage(t *testing.T) {
	path := `E:\Lunitide-Project\poc\it-crm\index.html`
	msg := "加上客户管理\n\n[正在看的页面文件]\n" + path + "\n" + openPageFileInstruction + "不要另写一份。\n\n[浏览器页面]\n商机"
	got := openPageFileFromMessages([]llmadapter.Message{{Role: llmadapter.RoleUser, Content: msg}})
	if got != path {
		t.Fatalf("path = %q", got)
	}
	if openPageFileFromMessages([]llmadapter.Message{{Role: llmadapter.RoleUser, Content: "[正在看的页面文件]\n" + path + "\n页面上的字"}}) != "" {
		t.Fatal("a path without the edit instruction must not unlock the file")
	}
	if openPageFileFromMessages([]llmadapter.Message{{Role: llmadapter.RoleUser, Content: "[正在看的页面文件]\nE:\\a\\..\\Windows\\notepad.exe\n" + openPageFileInstruction}}) != "" {
		t.Fatal("a traversal path must not unlock")
	}
	if !strings.Contains(msg, "workspace.edit") {
		t.Fatal("the instruction must name the edit tool")
	}
	slim := slimOpenPageMessages([]llmadapter.Message{{Role: llmadapter.RoleUser, Content: msg}})
	if strings.Contains(slim[0].Content, "[浏览器页面]") || strings.Contains(slim[0].Content, "商机") {
		t.Fatalf("page dump still in the model request: %q", slim[0].Content)
	}
	if !strings.Contains(slim[0].Content, "加上客户管理") || openPageFileFromMessages(slim) != path {
		t.Fatalf("user words or path dropped: %q", slim[0].Content)
	}
}

func TestFailedPageEditStillNeedsAnotherTry(t *testing.T) {
	path := `E:\Lunitide-Project\poc\it-crm\index.html`
	user := "帮我新增一个商机\n\n[正在看的页面文件]\n" + path + "\n" + openPageFileInstruction
	messages := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: user},
		{Role: llmadapter.RoleTool, Content: "ok:false\nworkspace.edit failed"},
	}
	if !openPageChangePending(messages, []string{"workspace.read", "workspace.edit"}) {
		t.Fatal("a failed edit was treated as finished")
	}
	if pageChangeAlreadyLanded(messages) {
		t.Fatal("a failed edit looked landed")
	}
}

func TestQuestionDropsTheOpenPageDump(t *testing.T) {
	path := `E:\Lunitide-Project\poc\it-crm\index.html`
	msg := "任务是做完了吗？\n\n[正在看的页面文件]\n" + path + "\n" + openPageReadInstruction + "\n\n[浏览器页面]\n" + strings.Repeat("商机赢单率", 40)
	if openPageFileFromMessages([]llmadapter.Message{{Role: llmadapter.RoleUser, Content: msg}}) != path {
		t.Fatal("a question that names the open file must still carry the path")
	}
	slim := slimOpenPageMessages([]llmadapter.Message{{Role: llmadapter.RoleUser, Content: msg}})
	if strings.Contains(slim[0].Content, "[浏览器页面]") || strings.Contains(slim[0].Content, "商机赢单率") {
		t.Fatalf("question still carries the page dump: %q", slim[0].Content)
	}
	if !strings.Contains(slim[0].Content, "任务是做完了吗？") || openPageFileFromMessages(slim) != path {
		t.Fatalf("question or path dropped: %q", slim[0].Content)
	}
}
