package app

import (
	"strings"
	"testing"
)

func TestExpertSharedBusRender(t *testing.T) {
	bus := newExpertSharedBus()
	if bus.render() != "" {
		t.Fatalf("empty bus must render empty")
	}
	if bus.len() != 0 {
		t.Fatalf("empty bus len = %d", bus.len())
	}

	// Empty/blank contributions are ignored.
	bus.append("", "something")
	bus.append("架构师", "   ")
	if bus.len() != 0 {
		t.Fatalf("blank contributions must be ignored, len = %d", bus.len())
	}

	bus.append("架构师", "建议用读写分离")
	bus.append("安全专家", "注意鉴权与最小权限")
	if bus.len() != 2 {
		t.Fatalf("len = %d, want 2", bus.len())
	}
	out := bus.render()
	if !strings.Contains(out, "架构师") || !strings.Contains(out, "读写分离") {
		t.Fatalf("render missing first entry: %q", out)
	}
	if !strings.Contains(out, "安全专家") || !strings.Contains(out, "最小权限") {
		t.Fatalf("render missing second entry: %q", out)
	}
}

func TestExpertSharedBusCaps(t *testing.T) {
	bus := newExpertSharedBus()
	long := strings.Repeat("字", expertBusMaxEntryRunes+200)
	bus.append("啰嗦专家", long)
	entryRunes := len([]rune(bus.entries[0].Text))
	if entryRunes > expertBusMaxEntryRunes {
		t.Fatalf("entry not capped: %d > %d", entryRunes, expertBusMaxEntryRunes)
	}
	for i := 0; i < 40; i++ {
		bus.append("专家", strings.Repeat("话", expertBusMaxEntryRunes))
	}
	if got := len([]rune(bus.render())); got > expertBusMaxRenderRunes {
		t.Fatalf("render not capped: %d > %d", got, expertBusMaxRenderRunes)
	}
}

func TestClipRunesTo(t *testing.T) {
	if got := clipRunesTo("hello", 10); got != "hello" {
		t.Fatalf("no-clip = %q", got)
	}
	got := clipRunesTo(strings.Repeat("z", 20), 5)
	if len([]rune(got)) != 5 {
		t.Fatalf("clip len = %d, want 5", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("clip should mark truncation: %q", got)
	}
}
