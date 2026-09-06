package mcp6

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Credentials live only within the lease callback. Adapters must not retain
// these bytes, place them in result/error bodies, or cache them with sessions.
type Credentials struct {
	Context context.Context
	Bearer  []byte
	Env     map[string][]byte
}

type CredentialLease func(context.Context, *Endpoint, func(Credentials) error) error
type Catalogue struct {
	Identity string
	Tools    map[string]ToolSchema
}
type CatalogueFunc func(context.Context, *Endpoint, Credentials) (Catalogue, error)
type VerifiedInvokeFunc func(context.Context, *Endpoint, string, map[string]any, Credentials) (map[string]any, error)

// SetSecurityAdapters switches production to one credential scope for both
// handshake and execution. Legacy seams remain available for embedded clients.
func (r *Registry) SetSecurityAdapters(lease CredentialLease, describe CatalogueFunc, invoke VerifiedInvokeFunc) {
	r.mu.Lock()
	r.credentialLease, r.catalogue, r.verifiedInvoke = lease, describe, invoke
	r.mu.Unlock()
}

func (r *Registry) SetCredentialLease(lease CredentialLease) {
	r.mu.Lock()
	r.credentialLease = lease
	r.mu.Unlock()
}

func (r *Registry) SecurityEnabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.catalogue != nil
}
func (r *Registry) AdoptSecurityVersion(id string, expected, next int64, pin CapabilityPin) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.endpoint[id]
	if e == nil || e.State == StateRevoked {
		return ErrEndpointRevoked
	}
	a, _ := json.Marshal(e.Pin)
	b, _ := json.Marshal(pin)
	if e.SecurityVersion != expected || string(a) != string(b) {
		return ErrCapabilityDrift
	}
	e.SecurityVersion = next
	return nil
}
func (r *Registry) SetSecurityFailureHook(fn func(context.Context, *Endpoint, bool) error) {
	r.mu.Lock()
	r.securityFailureHook = fn
	r.mu.Unlock()
}
func (r *Registry) recordSecurityFailure(ctx context.Context, e *Endpoint, err error) error {
	if !securityFailure(err) {
		return err
	}
	r.mu.Lock()
	hook := r.securityFailureHook
	r.mu.Unlock()
	if hook != nil {
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if failure := hook(persistCtx, e, errors.Is(err, ErrCapabilityDrift)); failure != nil {
			return errors.Join(err, fmt.Errorf("MCP security state could not be saved: %w", failure))
		}
	}
	return err
}

func (c Catalogue) Pin() (CapabilityPin, error) {
	if strings.TrimSpace(c.Identity) == "" || len(c.Identity) > 4096 || len(c.Tools) == 0 || len(c.Tools) > 512 {
		return CapabilityPin{}, ErrCapabilityDrift
	}
	identity := sha256.Sum256([]byte(c.Identity))
	p := CapabilityPin{ServerIdentityDigest: hex.EncodeToString(identity[:]), ToolSchemaDigests: make(map[string]string, len(c.Tools))}
	var total int
	for name, t := range c.Tools {
		if name == "" || len(name) > 256 || name == bootstrapPinTool || len(t.Description) > 32768 || len(t.InputSchema) > 65536 {
			return CapabilityPin{}, ErrCapabilityDrift
		}
		var schema map[string]any
		if json.Unmarshal(t.InputSchema, &schema) != nil || schema == nil {
			return CapabilityPin{}, ErrCapabilityDrift
		}
		b, err := json.Marshal(struct {
			Description string         `json:"description"`
			Schema      map[string]any `json:"schema"`
		}{t.Description, schema})
		if err != nil {
			return CapabilityPin{}, ErrCapabilityDrift
		}
		total += len(b)
		if total > 2<<20 {
			return CapabilityPin{}, ErrCapabilityDrift
		}
		sum := sha256.Sum256(b)
		p.ToolSchemaDigests[name] = hex.EncodeToString(sum[:])
	}
	encoded, _ := json.Marshal(p)
	if len(encoded) > 65536 {
		return CapabilityPin{}, ErrCapabilityDrift
	}
	return p, nil
}

func (c Catalogue) Verify(pin CapabilityPin) error {
	actual, err := c.Pin()
	if err != nil {
		return err
	}
	if actual.ServerIdentityDigest != pin.ServerIdentityDigest || len(actual.ToolSchemaDigests) != len(pin.ToolSchemaDigests) {
		return ErrCapabilityDrift
	}
	for name, digest := range actual.ToolSchemaDigests {
		if pin.ToolSchemaDigests[name] != digest {
			return fmt.Errorf("%w: %s", ErrCapabilityDrift, name)
		}
	}
	return nil
}

func (r *Registry) secureRefresh(ctx context.Context, e *Endpoint, lease CredentialLease, describe CatalogueFunc) error {
	if lease == nil {
		return ErrCredentialRevoked
	}
	var catalog Catalogue
	r.mu.Lock()
	snapshot := cloneEndpoint(e)
	r.mu.Unlock()
	err := lease(ctx, snapshot, func(credentials Credentials) error {
		var err error
		leaseCtx := ctx
		if credentials.Context != nil {
			leaseCtx = credentials.Context
		}
		catalog, err = describe(leaseCtx, snapshot, credentials)
		return redactCredentials(err, credentials)
	})
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	pin, err := catalog.Pin()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.endpoint[e.ID] != e || e.State == StateRevoked {
		return ErrEndpointRevoked
	}
	if !isBootstrapPin(e.Pin) {
		if err = catalog.Verify(e.Pin); err != nil {
			return err
		}
	}
	e.Pin = pin
	e.toolSchemas = cloneSchemas(catalog.Tools)
	return nil
}

func redactCredentials(err error, c Credentials) error {
	err = redactError(err, c.Bearer)
	for _, v := range c.Env {
		err = redactError(err, v)
	}
	return err
}

func (r *Registry) runSecure(ctx context.Context, e *Endpoint, tool string, args map[string]any, lease CredentialLease, invoke VerifiedInvokeFunc) (map[string]any, error) {
	if lease == nil || invoke == nil {
		return nil, ErrNotReady
	}
	var out map[string]any
	r.mu.Lock()
	gate := r.invokeGate
	r.mu.Unlock()
	err := lease(ctx, e, func(c Credentials) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		leaseCtx := ctx
		if c.Context != nil {
			leaseCtx = c.Context
		}
		if gate != nil {
			if err := gate(leaseCtx, e); err != nil {
				return err
			}
		}
		out, err = invoke(leaseCtx, e, tool, args, c)
		if err == nil {
			out, err = redactCredentialResult(out, c)
		}
		return redactCredentials(err, c)
	})
	return out, err
}

func redactCredentialResult(out map[string]any, c Credentials) (map[string]any, error) {
	if len(c.Bearer) == 0 && len(c.Env) == 0 {
		return out, nil
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, ErrTransport
	}
	text := string(encoded)
	redact := func(value []byte) {
		if len(value) == 0 {
			return
		}
		escaped, _ := json.Marshal(string(value))
		text = strings.ReplaceAll(text, string(escaped[1:len(escaped)-1]), "[REDACTED]")
	}
	redact(c.Bearer)
	for _, v := range c.Env {
		redact(v)
	}
	var safe map[string]any
	if json.Unmarshal([]byte(text), &safe) != nil {
		return nil, ErrTransport
	}
	return safe, nil
}

func securityFailure(err error) bool {
	return errors.Is(err, ErrCredentialRevoked) || errors.Is(err, ErrCapabilityDrift)
}
