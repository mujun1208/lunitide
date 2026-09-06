package mcp6

import "context"

func (r *Registry) SetInvokeScope(scope func(context.Context, *Endpoint) (context.Context, func(), error)) {
	r.mu.Lock()
	r.invokeScope = scope
	r.mu.Unlock()
}

// Check probes a configuration without admitting a callable endpoint. Settings
// can validate a disabled endpoint without accidentally granting execution.
func (r *Registry) Check(ctx context.Context, input EndpointInput) error {
	_, err := r.CheckEndpoint(ctx, input)
	return err
}

func (r *Registry) CheckEndpoint(ctx context.Context, input EndpointInput) (*Endpoint, error) {
	r.mu.Lock()
	probe, describe := r.probe, r.describe
	lease, catalogue, invoke := r.credentialLease, r.catalogue, r.verifiedInvoke
	resolver := r.launchResolver
	verifier := r.launchVerifier
	scope := r.invokeScope
	r.mu.Unlock()
	if probe == nil && catalogue == nil {
		return nil, ErrHealthCheckFailed
	}
	temporary := NewRegistry(probe, nil, nil)
	temporary.SetDescribeFunc(describe)
	temporary.SetSecurityAdapters(lease, catalogue, invoke)
	temporary.SetLaunchResolver(resolver)
	temporary.SetLaunchVerifier(verifier)
	temporary.SetInvokeScope(scope)
	return temporary.Register(ctx, input)
}

func (r *Registry) SetLaunchVerifier(fn func(context.Context, EndpointInput) (string, error)) {
	r.mu.Lock()
	r.launchVerifier = fn
	r.mu.Unlock()
}

func (r *Registry) SetLaunchResolver(fn func(context.Context, string, []string) ([]string, error)) {
	r.mu.Lock()
	r.launchResolver = fn
	r.mu.Unlock()
}

// SetInvokeGate checks durable authorization even when a caller retained a
// tool snapshot across a settings update or uninstall.
func (r *Registry) SetInvokeGate(gate func(context.Context, *Endpoint) error) {
	r.mu.Lock()
	r.invokeGate = gate
	r.mu.Unlock()
}

// SetRevokeHook tears down transport resources after lifecycle cancellation.
// The callback runs outside the registry lock.
func (r *Registry) SetRevokeHook(hook func(string)) {
	r.mu.Lock()
	r.revokeHook = hook
	r.mu.Unlock()
}

func endpointContext(parent, lifetime context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(lifetime, cancel)
	if lifetime.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}

func sameArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func clonePin(pin CapabilityPin) CapabilityPin {
	copy := CapabilityPin{ServerIdentityDigest: pin.ServerIdentityDigest, ToolSchemaDigests: make(map[string]string, len(pin.ToolSchemaDigests))}
	for name, digest := range pin.ToolSchemaDigests {
		copy.ToolSchemaDigests[name] = digest
	}
	return copy
}

func cloneSchemas(schemas map[string]ToolSchema) map[string]ToolSchema {
	copy := make(map[string]ToolSchema, len(schemas))
	for name, schema := range schemas {
		schema.InputSchema = append([]byte(nil), schema.InputSchema...)
		copy[name] = schema
	}
	return copy
}

func cloneEndpoint(e *Endpoint) *Endpoint {
	copy := *e
	copy.Args = append([]string(nil), e.Args...)
	copy.EnvSecretRefs = cloneStringMap(e.EnvSecretRefs)
	copy.Pin = clonePin(e.Pin)
	copy.toolSchemas = cloneSchemas(e.toolSchemas)
	return &copy
}

func cloneStringMap(v map[string]string) map[string]string {
	out := make(map[string]string, len(v))
	for k, s := range v {
		out[k] = s
	}
	return out
}
