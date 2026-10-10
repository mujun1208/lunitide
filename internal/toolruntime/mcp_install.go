package toolruntime

import (
	"context"
	"encoding/json"
)

// SetMcpInstaller delegates the mcp.install settings-plane preset installer
// to the host application (capability-self-bootstrap P3). The runtime keeps
// the mutation gate, hooks and audit trail; the host keeps the mcp6 preset
// catalog, stdio admission and endpoint wiring. A nil installer leaves the
// tool unavailable, so a runtime without the chat host still fails closed.
func (r *Runtime) SetMcpInstaller(f func(ctx context.Context, session string, raw json.RawMessage) (string, error)) {
	r.mcpInstaller = f
}
