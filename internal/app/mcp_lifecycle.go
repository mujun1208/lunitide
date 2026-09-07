package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/mcp"
	"github.com/lunitide/lunitide/internal/mcp6"
)

func (e *Engine) wireMcpLifecycle() {
	if e.mcp6Registry == nil {
		return
	}
	e.mcp6Registry.SetInvokeGate(e.checkMcpCapability)
	e.mcp6Registry.SetInvokeScope(e.acquireMcpCapability)
	e.mcp6Registry.SetSecurityFailureHook(e.persistMcpSecurityFailure)
	if e.m7mcp != nil {
		e.m7mcp.SetProber(settingsGatewayProber{e})
		e.m7mcp.SetInvalidator(e.dropSettingsMcp)
	}
	if e.mcmarket != nil {
		e.mcmarket.SetProber(settingsGatewayProber{e})
		e.mcmarket.SetInvalidator(e.dropSettingsMcp)
	}
}

func settingsMcpInput(ep m7flow.McpEndpointConfig) (mcp6.EndpointInput, error) {
	var args []string
	if ep.ArgsJSON != "" && json.Unmarshal([]byte(ep.ArgsJSON), &args) != nil {
		return mcp6.EndpointInput{}, m7app.ErrMcpSchema
	}
	var launchDigest string
	if ep.Security.LaunchArgsJSON != "" {
		if strings.HasPrefix(ep.Security.LaunchArgsJSON, "{") {
			var lock mcp.LaunchLock
			if json.Unmarshal([]byte(ep.Security.LaunchArgsJSON), &lock) != nil {
				return mcp6.EndpointInput{}, m7app.ErrMcpSchema
			}
			if lock.SourceArgs == ep.ArgsJSON {
				args = lock.Args
				launchDigest = lock.Digest
			}
		} else if json.Unmarshal([]byte(ep.Security.LaunchArgsJSON), &args) != nil {
			return mcp6.EndpointInput{}, m7app.ErrMcpSchema
		}
	}
	seed := ep.URL
	if ep.Transport == "stdio" {
		seed = ep.Command + " " + strings.Join(args, " ")
	}
	pin := mcp6.BootstrapPin(seed)
	if ep.Security.PinJSON != "" {
		pin = mcp6.CapabilityPin{}
		if json.Unmarshal([]byte(ep.Security.PinJSON), &pin) != nil {
			return mcp6.EndpointInput{}, m7app.ErrMcpSchema
		}
	}
	refs := map[string]string{}
	if ep.Security.EnvRefsJSON != "" && json.Unmarshal([]byte(ep.Security.EnvRefsJSON), &refs) != nil {
		return mcp6.EndpointInput{}, m7app.ErrMcpSchema
	}
	if err := m7app.ValidateMcpSecretRefs(ep.Security.AuthRef, refs); err != nil {
		return mcp6.EndpointInput{}, err
	}
	return mcp6.EndpointInput{ID: chatMcpEndpointID(ep.EndpointID), Transport: ep.Transport,
		URL: ep.URL, Command: ep.Command, Args: args, AuthRef: ep.Security.AuthRef, EnvSecretRefs: refs, SecurityVersion: ep.Security.Version, LaunchDigest: launchDigest, Pin: pin}, nil
}

func (e *Engine) checkMcpCapability(ctx context.Context, runtime *mcp6.Endpoint) error {
	if err := e.checkMcpPlugin(ctx, runtime.Command, runtime.Args); err != nil {
		return err
	}
	if e.m7mcp == nil {
		return nil
	}
	endpoints, err := e.m7mcp.List(ctx, "")
	if err != nil {
		return err
	}
	for _, ep := range endpoints {
		if chatMcpEndpointID(ep.EndpointID) != runtime.ID {
			continue
		}
		if !ep.Enabled || ep.State == m7flow.McpStateRevoked || ep.State == m7flow.McpStateQuarantined {
			return mcp6.ErrEndpointRevoked
		}
		input, err := settingsMcpInput(ep)
		if err != nil {
			return err
		}
		if input.Transport != runtime.Transport || input.Command != runtime.Command || (input.Transport == "https" && input.URL != runtime.URL) || mustJSONArgs(input.Args) != mustJSONArgs(runtime.Args) || input.SecurityVersion != runtime.SecurityVersion || input.AuthRef != runtime.AuthRef {
			return mcp6.ErrEndpointRevoked
		}
		return nil
	}
	return mcp6.ErrEndpointRevoked // production execution requires a durable grant
}

func (e *Engine) checkMcpPlugin(ctx context.Context, command string, args []string) error {
	return e.CheckCapability(ctx, mcpPluginIDs(command, args)...)
}

func (e *Engine) acquireMcpCapability(ctx context.Context, endpoint *mcp6.Endpoint) (context.Context, func(), error) {
	return e.AcquireCapability(ctx, mcpPluginIDs(endpoint.Command, endpoint.Args)...)
}

func mcpPluginIDs(command string, args []string) []string {
	var plugins []string
	switch presetIDFromCommandArgs(command, args) {
	case "playwright":
		plugins = []string{"browser"}
	case "filesystem":
		plugins = []string{"filesystem"}
	case "memory":
		plugins = []string{"memory"}
	case "sequentialthinking":
		plugins = []string{"thinking"}
	case "git":
		plugins = []string{"git"}
	}
	return plugins
}

type settingsGatewayProber struct{ engine *Engine }

func (p settingsGatewayProber) Probe(ctx context.Context, ep m7flow.McpEndpointConfig) (result string, probeErr error) {
	defer func() {
		if p.engine != nil && p.engine.m7mcp != nil {
			p.engine.m7mcp.RecordDiagnostic(ep.EndpointID, probeErr)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 70*time.Second)
	defer cancel()
	ctx = mcp.WithStdioStartupBudget(ctx)
	input, err := settingsMcpInput(ep)
	if err != nil {
		return "", err
	}
	// Older Google Drive presets supplied a work directory as a positional
	// argument. The upstream server ignores that and requires an OAuth token
	// file. Do not launch it or an interactive auth browser without that binding.
	if presetIDFromCommandArgs(input.Command, input.Args) == "gdrive" && input.EnvSecretRefs["GDRIVE_CREDENTIALS_PATH"] == "" {
		return "", mcp.ErrCredentialRequired
	}
	ctx, release, err := p.engine.AcquireCapability(ctx, mcpPluginIDs(input.Command, input.Args)...)
	if err != nil {
		return "", err
	}
	defer release()
	var observed *mcp6.Endpoint
	if ep.Enabled && ep.State != m7flow.McpStateQuarantined {
		// A target update invalidates the previous generation before connecting
		// the new one; in-flight calls receive cancellation and pooled IO retires.
		if old, getErr := p.engine.mcp6Registry.Get(input.ID); getErr == nil && (old.Command != input.Command || mustJSONArgs(old.Args) != mustJSONArgs(input.Args) || (input.Transport == "https" && old.URL != input.URL)) {
			p.engine.dropSettingsMcp(ep.EndpointID)
		}
		observed, err = p.engine.mcp6Registry.Register(ctx, input)
	} else {
		observed, err = p.engine.mcp6Registry.CheckEndpoint(ctx, input)
	}
	if err != nil {
		if errors.Is(err, mcp6.ErrCapabilityDrift) || errors.Is(err, mcp6.ErrCredentialRevoked) {
			persistErr := p.engine.m7mcp.SecurityFailure(ctx, ep.EndpointID, ep.Security.Version, errors.Is(err, mcp6.ErrCapabilityDrift))
			p.engine.dropSettingsMcp(ep.EndpointID)
			return "", errors.Join(err, persistErr)
		}
		return "", err
	}
	if p.engine.mcp6Registry.SecurityEnabled() {
		security, persistErr := p.engine.m7mcp.ObservePin(ctx, ep, observed.Pin, observed.Args, observed.LaunchDigest)
		if persistErr != nil {
			p.engine.dropSettingsMcp(ep.EndpointID)
			if errors.Is(persistErr, m7app.ErrMcpDrift) {
				stateErr := p.engine.m7mcp.SecurityFailure(ctx, ep.EndpointID, ep.Security.Version, true)
				return "", errors.Join(mcp6.ErrCapabilityDrift, persistErr, stateErr)
			}
			return "", persistErr
		}
		if ep.Enabled {
			if err := p.engine.mcp6Registry.AdoptSecurityVersion(input.ID, input.SecurityVersion, security.Version, observed.Pin); err != nil {
				return "", err
			}
		}
	}
	// Keep the persisted descriptor digest compatible with existing settings.
	// The gateway separately owns live schema pins; a successful descriptor
	// hash alone is never used as evidence of health.
	return (m7app.LocalMcpProber{}).Probe(ctx, ep)
}

func (e *Engine) persistMcpSecurityFailure(ctx context.Context, runtime *mcp6.Endpoint, drift bool) error {
	if e.m7mcp == nil {
		return nil
	}
	eps, err := e.m7mcp.List(ctx, "")
	if err != nil {
		return err
	}
	for _, ep := range eps {
		if chatMcpEndpointID(ep.EndpointID) == runtime.ID {
			return e.m7mcp.SecurityFailure(ctx, ep.EndpointID, runtime.SecurityVersion, drift)
		}
	}
	return nil
}

func (e *Engine) syncSettingsMcp(ctx context.Context, endpointID string) error {
	if e.m7mcp == nil || e.mcp6Registry == nil {
		return nil
	}
	endpoints, err := e.m7mcp.List(ctx, "")
	if err != nil {
		return err
	}
	for _, ep := range endpoints {
		if ep.EndpointID == endpointID {
			return e.admitSettingsMcp(ctx, ep)
		}
	}
	e.dropSettingsMcp(endpointID)
	return m7app.ErrMcpNotFound
}

func mcpAdmissionError(err error) error {
	if errors.Is(err, mcp6.ErrCapabilityDrift) {
		return m7app.ErrMcpDrift
	}
	return errors.Join(m7app.ErrMcpProbe, err)
}
