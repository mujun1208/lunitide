package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/imapp"
)

// Two colleagues writing to the same channel must land in different inbound
// sessions; the same colleague must reuse theirs. Before T5 the title was keyed
// only by channel, so everyone shared one history.
func TestInboundSessionTitleIsolatesSenders(t *testing.T) {
	a := inboundSessionTitleFor(imapp.KindFeishu, "Alice")
	b := inboundSessionTitleFor(imapp.KindFeishu, "Bob")
	if a == b {
		t.Fatalf("distinct senders shared a session title: %q", a)
	}
	if a != inboundSessionTitleFor(imapp.KindFeishu, "Alice") {
		t.Fatal("same sender must map to the same session title")
	}
}

func TestInboundSessionTitleFallsBackWithoutSender(t *testing.T) {
	base := inboundSessionTitleFor(imapp.KindFeishu, "   ")
	if base != imapp.InboundSessionTitle(imapp.KindFeishu) {
		t.Fatalf("blank sender should fall back to the channel title, got %q", base)
	}
}

func TestInboundSessionTitleStaysWithinLimit(t *testing.T) {
	long := strings.Repeat("名", 500)
	title := inboundSessionTitleFor(imapp.KindWeCom, long)
	if n := len([]rune(title)); n > 200 {
		t.Fatalf("title must stay within the 200-rune ceiling, got %d", n)
	}
}
