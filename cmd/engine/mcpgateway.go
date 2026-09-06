// Production MCP transport constructors. Authentication, observed identity,
// catalogue verification and scoped leases are wired in mcp_security.go.
package main

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/lunitide/lunitide/internal/mcp"
	"github.com/lunitide/lunitide/internal/mcp6"
)

// mcpStdioWorkDir is the sandbox root for stdio MCP sessions (per-call
// process lifetime); injected by main from the tool-workspaces tree.
var mcpStdioWorkDir string

// mcpGatewaySetStdioWorkDir installs the stdio session sandbox root.
func mcpGatewaySetStdioWorkDir(dir string) { mcpStdioWorkDir = dir }

// mcpStdioSession dials one isolated stdio server for e. The dial carries
// the caller's context so an already-expired registry deadline skips the
// handshake instead of spawning a doomed worker for StdioHandshakeTimeout.
func mcpStdioSession(ctx context.Context, e *mcp6.Endpoint) (*mcp.StdioSession, error) {
	return mcpStdioSessionWithEnv(ctx, e, nil)
}

func mcpStdioSessionWithEnv(ctx context.Context, e *mcp6.Endpoint, env []string) (*mcp.StdioSession, error) {
	if mcpStdioWorkDir == "" {
		return nil, fmt.Errorf("mcp6: stdio work dir not configured")
	}
	return mcp.StdioDial(ctx, e.Command, e.Args, filepath.Join(mcpStdioWorkDir, e.ID), env)
}

// mcpClientFor builds the hardened GET client for one endpoint, allowing
// only the endpoint's own host.
func mcpClientFor(e *mcp6.Endpoint) (*mcp.Client, error) {
	u, err := url.Parse(e.URL)
	if err != nil {
		return nil, fmt.Errorf("mcp6: unparseable endpoint url: %w", err)
	}
	return mcp.NewClient(mcp.RemoteEndpoint{ID: e.ID, BaseURL: e.URL}, []string{u.Host})
}

// mcpStdioPool contains only sessions without environment credentials.
var mcpStdioPool = mcp.NewStdioPool(mcp.StdioPoolDefaultMax, mcp.StdioPoolDefaultIdle)
