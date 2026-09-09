package mcp6

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/mcp"
)

// Live handshake against the same isolated stdio path MCP 中心 uses.
// Catalog tests cannot prove npx/uvx servers start. Set LUNITIDE_MCP_LIVE=1.
func TestOfficeDocumentPresetsLiveHandshake(t *testing.T) {
	if os.Getenv("LUNITIDE_MCP_LIVE") == "" {
		t.Skip("set LUNITIDE_MCP_LIVE=1 to probe real office MCP servers")
	}
	for _, id := range []string{"excel-mcp", "word-mcp", "ppt-mcp", "pdf-mcp", "markitdown"} {
		id := id
		t.Run(id, func(t *testing.T) {
			p, ok := PresetByID(id)
			if !ok {
				t.Fatalf("missing preset %s", id)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			ctx = mcp.WithStdioStartupBudget(ctx)
			s, err := mcp.StdioDial(ctx, p.Command, p.Args, t.TempDir(), nil)
			if err != nil {
				t.Fatalf("initialize failed: %v", err)
			}
			defer s.Close()
			tools, err := s.ListTools(ctx)
			if err != nil {
				t.Fatalf("tools/list failed: %v", err)
			}
			if len(tools) == 0 {
				t.Fatal("server advertised no tools")
			}
			t.Logf("identity=%s tools=%d", s.Identity(), len(tools))
		})
	}
}
