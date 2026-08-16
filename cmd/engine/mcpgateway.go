// Production MCP gateway adapters: the mcp6 registry seams (probe /
// describe / invoke / lease) wired onto the frozen M5 HTTPS read-only
// client. The client is pinned to the endpoint's own registered host
// (self-allowlist, redirects refused), so a registration can never reach
// beyond the host the subject authorised. The GET transport carries no
// credential bytes; authRef stays a governance handle resolved as an empty
// lease until a credential-bearing transport is designed.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

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
	if mcpStdioWorkDir == "" {
		return nil, fmt.Errorf("mcp6: stdio work dir not configured")
	}
	return mcp.StdioDial(ctx, e.Command, e.Args, filepath.Join(mcpStdioWorkDir, e.ID), nil)
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

// mcpGatewayProbe reaches the endpoint's read-only tools catalogue; a live
// 2xx listing is the M6 health check. stdio endpoints answer via one
// isolated dial + tools/list.
func mcpGatewayProbe(ctx context.Context, e *mcp6.Endpoint) error {
	if e.Transport == "stdio" {
		s, err := mcpStdioSession(ctx, e)
		if err != nil {
			return err
		}
		defer s.Close()
		_, err = s.ListTools(ctx)
		return err
	}
	client, err := mcpClientFor(e)
	if err != nil {
		return err
	}
	_, err = client.ListTools(ctx)
	return err
}

// mcpGatewayDescribe refreshes the endpoint's schema cache from the same
// catalogue the probe validated.
func mcpGatewayDescribe(ctx context.Context, e *mcp6.Endpoint) (map[string]mcp6.ToolSchema, error) {
	if e.Transport == "stdio" {
		s, err := mcpStdioSession(ctx, e)
		if err != nil {
			return nil, err
		}
		defer s.Close()
		tools, err := s.ListTools(ctx)
		if err != nil {
			return nil, err
		}
		out := make(map[string]mcp6.ToolSchema, len(tools))
		for _, t := range tools {
			out[t.Name] = mcp6.ToolSchema{Description: t.Description, InputSchema: t.InputSchema}
		}
		return out, nil
	}
	client, err := mcpClientFor(e)
	if err != nil {
		return nil, err
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]mcp6.ToolSchema, len(tools))
	for _, t := range tools {
		out[t.Name] = mcp6.ToolSchema{Description: t.Description, InputSchema: t.InputSchema}
	}
	return out, nil
}

// mcpGatewayInvoke executes one read-only GET invocation and flattens the
// response body into the mcp6 result map. An upstream 401 maps to
// ErrCredentialRevoked so the registry lifecycle stays wired. stdio
// endpoints run one isolated tools/call session instead.
func mcpGatewayInvoke(ctx context.Context, e *mcp6.Endpoint, tool string, args map[string]any, _ []byte) (map[string]any, error) {
	if e.Transport == "stdio" {
		s, err := mcpStdioSession(ctx, e)
		if err != nil {
			return nil, err
		}
		defer s.Close()
		argsJSON, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("mcp6: arguments not serialisable: %w", err)
		}
		out, err := s.CallTool(ctx, tool, argsJSON)
		if err != nil {
			return nil, err
		}
		result := map[string]any{}
		if len(out.Texts) == 1 {
			result["text"] = out.Texts[0]
		} else if len(out.Texts) > 1 {
			result["texts"] = out.Texts
		}
		if len(out.StructuredContent) > 0 {
			result["structured"] = json.RawMessage(out.StructuredContent)
		}
		result["isError"] = out.IsError
		return result, nil
	}
	client, err := mcpClientFor(e)
	if err != nil {
		return nil, err
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("mcp6: arguments not serialisable: %w", err)
	}
	out, err := client.Invoke(ctx, mcp.InvokeInput{Tool: tool, ArgsJSON: argsJSON})
	if err != nil {
		if errors.Is(err, mcp.ErrHttpStatus) && strings.Contains(err.Error(), "401") {
			return nil, mcp6.ErrCredentialRevoked
		}
		return nil, err
	}
	var result map[string]any
	if jerr := json.Unmarshal(out.Data, &result); jerr != nil || result == nil {
		result = map[string]any{"data": string(out.Data)}
	}
	return result, nil
}

// mcpEmptyLease resolves every authRef to an empty credential: the frozen
// M5 GET transport attaches no secret material to requests.
type mcpEmptyLease struct{}

func (mcpEmptyLease) WithLease(_ context.Context, _ string, fn func(auth []byte) error) error {
	return fn(nil)
}
