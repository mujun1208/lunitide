package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lunitide/lunitide/internal/mcp"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/networkpolicy"
)

var mcpSecureClientFor = mcpClientFor

func mcpVerifyLaunch(ctx context.Context, input mcp6.EndpointInput) (string, error) {
	if input.Command != "node" {
		return "", nil
	}
	digest, err := mcp.NodeScriptDigest(ctx, input.Args, filepath.Join(mcpStdioWorkDir, input.ID))
	if err != nil {
		return "", err
	}
	if input.LaunchDigest != "" && input.LaunchDigest != digest {
		return "", mcp6.ErrCapabilityDrift
	}
	return digest, nil
}

func mcpServerIdentity(e *mcp6.Endpoint, s *mcp.StdioSession) string {
	if e.LaunchDigest != "" {
		return s.Identity() + "|entry:" + e.LaunchDigest
	}
	return s.Identity()
}

func mcpResolveLaunch(ctx context.Context, command string, args []string) ([]string, error) {
	return mcp.ResolveLaunchArgs(ctx, command, args, func(ctx context.Context, target string) ([]byte, error) {
		result, err := networkpolicy.Fetch(ctx, target, networkpolicy.FetchOptions{})
		if err != nil {
			return nil, err
		}
		if result.Status < 200 || result.Status >= 300 || result.Truncated {
			return nil, mcp.ErrLaunchLock
		}
		return []byte(result.Body), nil
	})
}

func mcpCredentialError(err error) error {
	var status *mcp.HTTPStatusError
	if errors.As(err, &status) && status.StatusCode == 401 {
		return mcp6.ErrCredentialRevoked
	}
	return err
}

func mcpCatalogue(identity string, tools []mcp.ToolInfo) (mcp6.Catalogue, error) {
	out := mcp6.Catalogue{Identity: identity, Tools: make(map[string]mcp6.ToolSchema, len(tools))}
	for _, t := range tools {
		if _, duplicate := out.Tools[t.Name]; duplicate {
			return out, mcp6.ErrCapabilityDrift
		}
		out.Tools[t.Name] = mcp6.ToolSchema{Description: t.Description, InputSchema: t.InputSchema}
	}
	_, err := out.Pin()
	return out, err
}

func mcpSecretEnvironment(c mcp6.Credentials) ([]string, error) {
	keys := make([]string, 0, len(c.Env))
	for k := range c.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, name := range keys {
		value := c.Env[name]
		if strings.ContainsAny(string(value), "\x00\r\n") || strings.ContainsAny(name, "=\x00\r\n") {
			return nil, mcp6.ErrCredentialRevoked
		}
		out = append(out, name+"="+string(value))
	}
	return out, nil
}

func mcpSecureDescribe(ctx context.Context, e *mcp6.Endpoint, c mcp6.Credentials) (mcp6.Catalogue, error) {
	if e.Transport == "stdio" {
		if _, err := mcpVerifyLaunch(ctx, mcp6.EndpointInput{ID: e.ID, Command: e.Command, Args: e.Args, LaunchDigest: e.LaunchDigest}); err != nil {
			return mcp6.Catalogue{}, err
		}
		env, err := mcpSecretEnvironment(c)
		if err != nil {
			return mcp6.Catalogue{}, err
		}
		s, err := mcpStdioSessionWithEnv(ctx, e, env)
		if err != nil {
			return mcp6.Catalogue{}, err
		}
		defer s.Close()
		tools, err := s.ListTools(ctx)
		if err != nil {
			return mcp6.Catalogue{}, err
		}
		return mcpCatalogue(mcpServerIdentity(e, s), tools)
	}
	client, err := mcpSecureClientFor(e)
	if err != nil {
		return mcp6.Catalogue{}, err
	}
	tools, err := client.ListToolsAuthenticated(ctx, c.Bearer)
	if err != nil {
		return mcp6.Catalogue{}, mcpCredentialError(err)
	}
	// The existing HTTPS adapter is the product's legacy GET protocol. Its
	// authenticated TLS origin/URL is the server identity; it does not claim
	// support for JSON-RPC initialize or Streamable HTTP.
	return mcpCatalogue("legacy-get-v1|"+e.URL, tools)
}

func mcpSecureInvoke(ctx context.Context, e *mcp6.Endpoint, tool string, args map[string]any, c mcp6.Credentials) (map[string]any, error) {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	if e.Transport == "stdio" {
		if _, err := mcpVerifyLaunch(ctx, mcp6.EndpointInput{ID: e.ID, Command: e.Command, Args: e.Args, LaunchDigest: e.LaunchDigest}); err != nil {
			return nil, err
		}
		env, err := mcpSecretEnvironment(c)
		if err != nil {
			return nil, err
		}
		call := func(conn mcp.StdioConn) (mcp.StdioCallResult, error) {
			s, ok := conn.(*mcp.StdioSession)
			if !ok {
				return mcp.StdioCallResult{}, fmt.Errorf("MCP session identity unavailable")
			}
			tools, err := s.ListTools(ctx)
			if err != nil {
				return mcp.StdioCallResult{}, err
			}
			catalog, err := mcpCatalogue(mcpServerIdentity(e, s), tools)
			if err != nil {
				return mcp.StdioCallResult{}, err
			}
			if err = catalog.Verify(e.Pin); err != nil {
				return mcp.StdioCallResult{}, err
			}
			return s.CallTool(ctx, tool, argsJSON)
		}
		var result mcp.StdioCallResult
		if len(env) > 0 {
			// A credential-bearing process cannot outlive its lease. Plaintext
			// environment and process are discarded before leaving the callback.
			s, openErr := mcpStdioSessionWithEnv(ctx, e, env)
			if openErr != nil {
				return nil, openErr
			}
			defer s.Close()
			result, err = call(s)
		} else {
			result, err = mcpStdioPool.Invoke(ctx, "stdio:"+e.ID, func(ctx context.Context) (mcp.StdioConn, error) { return mcpStdioSession(ctx, e) }, call)
		}
		if err != nil {
			return nil, err
		}
		out := map[string]any{"isError": result.IsError}
		if len(result.Texts) == 1 {
			out["text"] = result.Texts[0]
		} else if len(result.Texts) > 1 {
			out["texts"] = result.Texts
		}
		if len(result.StructuredContent) > 0 {
			out["structured"] = json.RawMessage(result.StructuredContent)
		}
		return out, nil
	}
	client, err := mcpSecureClientFor(e)
	if err != nil {
		return nil, err
	}
	tools, err := client.ListToolsAuthenticated(ctx, c.Bearer)
	if err != nil {
		return nil, mcpCredentialError(err)
	}
	catalog, err := mcpCatalogue("legacy-get-v1|"+e.URL, tools)
	if err != nil {
		return nil, err
	}
	if err = catalog.Verify(e.Pin); err != nil {
		return nil, err
	}
	result, err := client.InvokeAuthenticated(ctx, mcp.InvokeInput{Tool: tool, ArgsJSON: argsJSON}, c.Bearer)
	if err != nil {
		return nil, mcpCredentialError(err)
	}
	var out map[string]any
	if json.Unmarshal(result.Data, &out) != nil || out == nil {
		out = map[string]any{"data": string(result.Data)}
	}
	return out, nil
}
