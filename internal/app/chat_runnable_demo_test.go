package app

import (
	"strings"
	"testing"
)

func TestUserRunHandoffBecomesTheWorkspaceServer(t *testing.T) {
	goal := "我要让为直接生成一个可以演示的系统，要求结果可以直接看到并在页面原子操作，有数据交互，数据存储。"
	reply := "已写入 pm/ai_sales_poc/app.py。\n未完成（被拦截）\n你需要做的（一条命令）\npython poc\\ui_sales_assistant\\server.py\n然后打开登录页。"
	script := userRunHandoffScript(goal, reply)
	if script != `poc\ui_sales_assistant\server.py` {
		t.Fatalf("script=%q", script)
	}
	cleaned := replaceUserRunHandoff(reply, "服务已在本机运行，无需用户再执行命令。\n地址：http://127.0.0.1:8765")
	if userRunHandoffScript(goal, cleaned) != "" {
		t.Fatal("cleaned reply still hands the user a command")
	}
	if !strings.Contains(cleaned, "pm/ai_sales_poc/app.py") || !strings.Contains(cleaned, "http://127.0.0.1:8765") {
		t.Fatalf("cleaned=%q", cleaned)
	}
}

func TestNodeHandoffKeepsTheInterpreter(t *testing.T) {
	goal := "做一个可以演示的系统，页面可以操作，有数据交互。"
	got := userRunHandoffArgv(goal, "你需要做的\nnode poc/server.js")
	if len(got) != 2 || got[0] != "node" || got[1] != "poc/server.js" {
		t.Fatalf("argv=%q", got)
	}
}

func TestWeeklyReportDoesNotStartAServerFromACommandMention(t *testing.T) {
	if userRunHandoffScript("生成Word周报", "你需要做的\npython poc\\ui_sales_assistant\\server.py") != "" {
		t.Fatal("a document request must not be treated as a demo server handoff")
	}
}
