package app

import (
	"context"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
)

// A pack install envelope must be able to hold one cold MCP handshake plus the
// bookkeeping that follows it, or every pack carrying an MCP dies at the
// deadline and reports a failure instead of a skipped component.
func TestPackInstallEnvelopeHoldsMcpSetup(t *testing.T) {
	envelope := time.Duration(bridge.MaxDeadlineMS("plugin.pack.install")) * time.Millisecond
	if envelope < packMcpBudget+packBookkeepingReserve {
		t.Fatalf("pack envelope %s cannot hold mcp budget %s + reserve %s", envelope, packMcpBudget, packBookkeepingReserve)
	}
	if bridge.MaxDeadlineMS("plugin.pack.uninstall") != bridge.MaxDeadlineMS("plugin.pack.install") {
		t.Fatalf("uninstall envelope %d differs from install", bridge.MaxDeadlineMS("plugin.pack.uninstall"))
	}
}

func TestPackMcpContextLeavesReserveForBookkeeping(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), packMcpBudget+packBookkeepingReserve+30*time.Second)
	defer cancelParent()
	ctx, cancel := packMcpContext(parent)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("mcp step has no deadline")
	}
	if remaining := time.Until(deadline); remaining > packMcpBudget {
		t.Fatalf("mcp step budget %s exceeds cap %s", remaining, packMcpBudget)
	}

	// A parent with less time left than the reserve must not hand the whole
	// remainder to the MCP: the pack still has to journal the skip.
	tight, cancelTight := context.WithTimeout(context.Background(), packBookkeepingReserve/2)
	defer cancelTight()
	short, cancelShort := packMcpContext(tight)
	defer cancelShort()
	shortDeadline, ok := short.Deadline()
	if !ok {
		t.Fatal("tight mcp step has no deadline")
	}
	if spent := time.Until(shortDeadline); spent > packBookkeepingReserve/4 {
		t.Fatalf("tight mcp step kept %s of a %s parent", spent, packBookkeepingReserve/2)
	}
}
