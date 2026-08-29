package app

import (
	"strings"
	"testing"
)

func TestParseDesktopTypeArgsFromGoal(t *testing.T) {
	after, text, ok := parseDesktopTypeArgsFromGoal("在身份证号码后面写204040")
	if !ok || after != "身份证号码" || text != "204040" {
		t.Fatalf("in 身份证号码后面写204040: after=%q text=%q ok=%v", after, text, ok)
	}
	after, text, ok = parseDesktopTypeArgsFromGoal("身份证号码后面写204040")
	if !ok || after != "身份证号码" || text != "204040" {
		t.Fatalf("身份证号码后面写204040: after=%q text=%q ok=%v", after, text, ok)
	}
	after, text, ok = parseDesktopTypeArgsFromGoal("帮我在文档的身份证号码后面写上姓名")
	if !ok || after != "身份证号码" || text != "姓名" {
		t.Fatalf("文档身份证号码: after=%q text=%q ok=%v", after, text, ok)
	}
}

func TestFallbackDesktopTypeArgs(t *testing.T) {
	raw := fallbackDesktopTypeArgs("在身份证号码后面写204040")
	if len(raw) == 0 {
		t.Fatal("expected args")
	}
	s := string(raw)
	if !strings.Contains(s, `"text":"204040"`) || !strings.Contains(s, `"after":"身份证号码"`) {
		t.Fatalf("args = %s", raw)
	}
}

func TestLooksLikeTypeAfterLabelTurnComplete(t *testing.T) {
	if !looksLikeTypeAfterLabelTurn("身份证号码后面写204040") {
		t.Fatal("complete command should match")
	}
	if !looksLikeTypeAfterLabelTurn("在证件号码后面填一下") {
		t.Fatal("incomplete fill should match")
	}
	if looksLikeTypeAfterLabelTurn("今晚月色如何") {
		t.Fatal("idle chat should not match")
	}
}