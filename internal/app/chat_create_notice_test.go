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

func TestCollectExpertIDsPrefersMountedPack(t *testing.T) {
	const mounted = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	const extra = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	got := collectExpertIDs([]string{mounted}, "[引用专家 安全|"+extra+"]\n[引用专家 重复|"+mounted+"]")
	if len(got) != 2 || got[0] != mounted || got[1] != extra {
		t.Fatalf("got %#v", got)
	}
}
