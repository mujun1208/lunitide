package app

import (
	"strings"
	"testing"
)

func TestCreateTurnClosingNotice(t *testing.T) {
	if got := createTurnClosingNotice([]string{"workspace.write", "skill.create"}); !strings.Contains(got, "技能中心") {
		t.Fatalf("skill.create notice = %q", got)
	}
	if got := createTurnClosingNotice([]string{"plugin.create"}); !strings.Contains(got, "插件") {
		t.Fatalf("plugin.create notice = %q", got)
	}
	if got := createTurnClosingNotice([]string{"workspace.read"}); got != "" {
		t.Fatalf("unrelated tools should not close the turn: %q", got)
	}
}

func TestExtractExpertRefIDs(t *testing.T) {
	const id = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	got := extractExpertRefIDs("[引用专家 安全工程师|" + id + "]\n请审查")
	if len(got) != 1 || got[0] != id {
		t.Fatalf("got %#v", got)
	}
}
