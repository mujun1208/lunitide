package app

import (
	"strings"
	"testing"
)

func TestParseDesktopVerdictGrammar(t *testing.T) {
	cases := []struct {
		raw     string
		want    string
		ok      bool
		wantRsn string
	}{
		{`{"verdict":"done","reason":"消息已出现在聊天记录中"}`, verdictDone, true, "消息已出现在聊天记录中"},
		{"```json\n{\"verdict\":\"not_done\",\"reason\":\"文字仍在输入框\"}\n```", verdictNotDone, true, "文字仍在输入框"},
		{`{"verdict":"Not-Done","reason":""}`, verdictNotDone, true, ""},
		{`{"verdict":"BLOCKED","reason":"弹出了登录窗口"}`, verdictBlocked, true, "弹出了登录窗口"},
		{`{"verdict":"unclear"}`, verdictUnclear, true, ""},
		{`{"verdict":"completed","reason":"ok"}`, verdictDone, true, "ok"},
		{`{"verdict":"maybe"}`, "", false, ""},
		{`not json at all`, "", false, ""},
	}
	for _, c := range cases {
		got, ok := parseDesktopVerdict(c.raw)
		if ok != c.ok {
			t.Fatalf("%q: ok=%v want %v", c.raw, ok, c.ok)
		}
		if !ok {
			continue
		}
		if got.Verdict != c.want || got.Reason != c.wantRsn {
			t.Fatalf("%q: got %+v want %s/%s", c.raw, got, c.want, c.wantRsn)
		}
	}
	long, _ := parseDesktopVerdict(`{"verdict":"done","reason":"` + strings.Repeat("长", 300) + `"}`)
	if r := []rune(long.Reason); len(r) > 121 {
		t.Fatalf("reason not capped: %d runes", len(r))
	}
}

func TestDesktopVerifierAppliesOnlyToScreenTurns(t *testing.T) {
	if !desktopVerifierApplies([]string{"computer.act"}, false, false, true) {
		t.Fatal("computer.act turn should be audited")
	}
	if !desktopVerifierApplies([]string{"desktop.open", "desktop.type"}, false, false, true) {
		t.Fatal("desktop.type turn should be audited")
	}
	if !desktopVerifierApplies([]string{"browser.act"}, false, false, true) {
		t.Fatal("browser.act turn should be audited")
	}
	if !desktopVerifierApplies([]string{"cc.mouse_click"}, false, false, true) {
		t.Fatal("direct host click should be audited")
	}
	if desktopVerifierApplies([]string{"desktop.open"}, false, false, true) {
		t.Fatal("pure launch is already verified by foreground window; no audit")
	}
	if desktopVerifierApplies([]string{"computer.act"}, true, false, true) {
		t.Fatal("companion turns skip the audit")
	}
	if desktopVerifierApplies([]string{"computer.act"}, false, true, true) {
		t.Fatal("audit runs once per turn")
	}
	if desktopVerifierApplies([]string{"computer.act"}, false, false, false) {
		t.Fatal("no desktop tools used this turn")
	}
	if desktopVerifierApplies(nil, false, false, true) {
		t.Fatal("empty tool list")
	}
}

func TestDesktopVerdictLinesNeverOverclaim(t *testing.T) {
	if got := desktopVerdictEvidenceLine(desktopVerdict{Verdict: verdictNotDone, Reason: "x"}); got != "" {
		t.Fatalf("not_done must not get a success line: %q", got)
	}
	if got := desktopVerdictEvidenceLine(desktopVerdict{Verdict: verdictUnclear}); got != "" {
		t.Fatalf("unclear must stay silent: %q", got)
	}
	done := desktopVerdictEvidenceLine(desktopVerdict{Verdict: verdictDone, Reason: "文件已在前台打开"})
	if !strings.Contains(done, "已核对最终截图") || !strings.Contains(done, "文件已在前台打开") {
		t.Fatalf("done line: %q", done)
	}
	blocked := desktopVerdictEvidenceLine(desktopVerdict{Verdict: verdictBlocked, Reason: "UAC 提示挡住了"})
	if !strings.Contains(blocked, "需要你在电脑上处理") {
		t.Fatalf("blocked line: %q", blocked)
	}
	nudge := desktopVerdictNudge("")
	if nudge.Role != "system" || !strings.Contains(nudge.Content, "未完成") || !strings.Contains(nudge.Content, "see→act→verify") {
		t.Fatalf("nudge: %+v", nudge)
	}
	prompt := desktopVerifierUserPrompt("把文件发给张三", strings.Repeat("已完成", 500))
	if !strings.Contains(prompt, "Goal:\n把文件发给张三") || len([]rune(prompt)) > 600 {
		t.Fatalf("prompt should carry the goal and cap the claim: %d runes", len([]rune(prompt)))
	}
}
