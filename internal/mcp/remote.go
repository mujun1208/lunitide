// Package mcp: M5 T-5.4.4/T-5.4.5 HTTPS read-only remote MCP access. The
// client speaks only idempotent GET over TLS 1.2+ with the system trust
// store against a host allowlist, refuses redirects and manual content
// encodings, caps responses at 4 MiB (refused, never truncated) and retries
// transport failures exactly once. The registry admits read-only tool
// declarations only and circuit-breaks an endpoint for 60 s after five
// consecutive failures. Every constant below is a frozen M5 parameter
// (design doc M5/02); changing any of them needs an ADR.
package mcp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Errors carry the MCP wire codes: MCP-001 transport policy (https only,
// allowlisted host, no redirects, read-only verb subset), MCP-002 response
// policy (status, encoding, size cap), MCP-003 invocation policy (exactly
// one retry on transport failures). Callers translate with errors.Is.
var (
	// ErrNotHttps is MCP-001: endpoint scheme is not https.
	ErrNotHttps = errors.New("mcp: endpoint must use https (MCP-001)")
	// ErrHostNotAllowed is MCP-001: endpoint host outside the allowlist.
	ErrHostNotAllowed = errors.New("mcp: endpoint host outside allowlist (MCP-001)")
	// ErrRedirectBlocked is MCP-001: any redirect, authorised target or
	// not, is refused before a second request is ever sent.
	ErrRedirectBlocked = errors.New("mcp: redirects are blocked (MCP-001)")
	// ErrMethodNotAllowed is MCP-001: the read-only subset has exactly one
	// verb (GET); write-semantics invocations do not exist on this client.
	ErrMethodNotAllowed = errors.New("mcp: only read-only GET invocations exist (MCP-001)")
	// ErrEncodingBlocked is MCP-002: manual content-encoding on a response.
	ErrEncodingBlocked = errors.New("mcp: response content-encoding blocked (MCP-002)")
	// ErrResponseTooLarge is MCP-002: body above the 4 MiB cap, refused
	// without truncation.
	ErrResponseTooLarge = errors.New("mcp: response exceeds the 4 MiB cap (MCP-002)")
	// ErrHttpStatus is MCP-002: endpoint answered a non-2xx status.
	ErrHttpStatus = errors.New("mcp: endpoint answered non-2xx (MCP-002)")
	// ErrInvokeFailed is MCP-003: both the attempt and its single retry
	// failed at the transport level.
	ErrInvokeFailed = errors.New("mcp: invocation failed after retry (MCP-003)")
)

// Frozen M5 transport parameters (design doc M5/02); changes need an ADR.
const (
	// DefaultDialTimeout bounds TCP connection establishment.
	DefaultDialTimeout = 10 * time.Second
	// DefaultHeaderTimeout bounds waiting for the first response byte.
	DefaultHeaderTimeout = 15 * time.Second
	// DefaultTotalTimeout bounds one full invocation, retry included.
	DefaultTotalTimeout = 30 * time.Second
	// MaxResponseBytes caps a response body; larger bodies are refused
	// outright and never truncated.
	MaxResponseBytes = 4 << 20 // 4 MiB
	// MaxRetries is the exact retry budget for idempotent read-only calls
	// (MCP-003): one retry, never more.
	MaxRetries = 1
)

// RemoteConfig is the loaded remote endpoint set; keys are endpoint IDs.
type RemoteConfig struct {
	Endpoints map[string]RemoteEndpoint
}

// RemoteEndpoint is one remote MCP host. AllowedTools is consumed by the
// registry layer; this package only validates the transport shape
// (BaseURL) of the endpoint.
type RemoteEndpoint struct {
	ID           string
	BaseURL      string
	AllowedTools []string
}

// ValidateBaseURL enforces MCP-001 transport policy: an endpoint must be
// https (any port, default or not) with a non-empty host. Plain http and
// every other scheme answer ErrNotHttps.
func ValidateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: unparseable URL %q", ErrNotHttps, raw)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%w: scheme %q", ErrNotHttps, u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("%w: empty host", ErrNotHttps)
	}
	return nil
}

// Client is one allowlisted remote endpoint. The timeout fields default to
// the frozen M5 triple (10/15/30 s); tests may shorten them, production
// callers must not change the defaults without an ADR.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	// Frozen transport timeouts (dial / first byte / whole invocation).
	// Tests may shorten them; defaults must not change without an ADR.
	DialTimeout   time.Duration
	HeaderTimeout time.Duration
	TotalTimeout  time.Duration

	// hostAllowlist is the host policy this client was born with; every
	// redirect is refused outright, so requests can never leave it.
	hostAllowlist []string
}

// NewClient validates the endpoint (https + allowlisted host, MCP-001) and
// builds a hardened HTTP stack: TLS 1.2 minimum against the system CA
// pool, transparent compression off (a decompression bomb must stay raw
// bytes so the 4 MiB cap applies to what the wire actually sent), and
// every redirect refused so an endpoint cannot bounce to an unauthorised
// host. The three-layer timeout is dial 10 s, first byte 15 s, whole
// invocation 30 s (frozen).
func NewClient(ep RemoteEndpoint, allowlist []string) (*Client, error) {
	if err := ValidateBaseURL(ep.BaseURL); err != nil {
		return nil, err
	}
	u, err := url.Parse(ep.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: unparseable URL %q", ErrNotHttps, ep.BaseURL)
	}
	if !hostAllowed(u.Host, allowlist) {
		return nil, fmt.Errorf("%w: %s", ErrHostNotAllowed, u.Host)
	}
	tlsCfg, err := systemTLSConfig()
	if err != nil {
		return nil, err
	}
	c := &Client{
		BaseURL:       strings.TrimSuffix(ep.BaseURL, "/"),
		DialTimeout:   DefaultDialTimeout,
		HeaderTimeout: DefaultHeaderTimeout,
		TotalTimeout:  DefaultTotalTimeout,
		hostAllowlist: append([]string(nil), allowlist...),
	}
	c.HTTP = c.buildHTTPClient(tlsCfg)
	return c, nil
}

// systemTLSConfig pins the frozen TLS policy: TLS 1.2 minimum verified
// against the system certificate pool; private CAs are not consulted.
func systemTLSConfig() (*tls.Config, error) {
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("mcp: system cert pool unavailable: %w", err)
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, nil
}

// SetTLSConfig swaps the TLS configuration while keeping every other
// hardening (no redirects, no compression, frozen timeouts). It exists so
// tests can point the client at httptest self-signed servers; production
// code keeps the system trust chain installed by NewClient.
func (c *Client) SetTLSConfig(cfg *tls.Config) {
	c.HTTP = c.buildHTTPClient(cfg)
}

// buildHTTPClient assembles the hardened transport: dial and header
// timeouts, compression disabled, no proxy, and CheckRedirect refusing
// every redirect (MCP-001) so no follow-up request is ever issued.
func (c *Client) buildHTTPClient(tlsCfg *tls.Config) *http.Client {
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: c.DialTimeout}).DialContext,
		DisableCompression:    true,
		ResponseHeaderTimeout: c.HeaderTimeout,
		TLSClientConfig:       tlsCfg,
		Proxy:                 nil,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("%w: %s", ErrRedirectBlocked, req.URL)
		},
	}
}

// hostAllowed matches the URL host (host[:port]) exactly, case-insensitive,
// against the allowlist.
func hostAllowed(host string, allowlist []string) bool {
	for _, allowed := range allowlist {
		if strings.EqualFold(allowed, host) {
			return true
		}
	}
	return false
}

// InvokeInput is one read-only tool invocation; ArgsJSON rides the query
// string because the transport is GET-only.
type InvokeInput struct {
	Tool      string
	ArgsJSON  []byte
	SecretRef string
}

// InvokeResult carries the endpoint's response body. Truncated is always
// false: an oversized body is refused outright, never partially returned.
type InvokeResult struct {
	Tool      string
	Data      []byte
	Truncated bool
}

// Invoke performs one read-only invocation: GET {BaseURL}/tools/{tool}
// with args on the query string, inside the frozen 30 s overall deadline.
// Only transport-level failures (connection, TLS, timeouts) retry, exactly
// once (MCP-003); policy answers — non-2xx status, blocked encoding, size
// cap, redirect — are definitive and never retried.
func (c *Client) Invoke(ctx context.Context, in InvokeInput) (InvokeResult, error) {
	if in.Tool == "" {
		return InvokeResult{}, fmt.Errorf("%w: empty tool name", ErrMethodNotAllowed)
	}
	ctx, cancel := context.WithTimeout(ctx, c.TotalTimeout)
	defer cancel()
	target := c.BaseURL + "/tools/" + url.PathEscape(in.Tool)
	if len(in.ArgsJSON) > 0 {
		target += "?args=" + url.QueryEscape(string(in.ArgsJSON))
	}
	var lastErr error
	for attempt := 0; attempt <= MaxRetries; attempt++ {
		data, err := c.attempt(ctx, target)
		if err == nil {
			return InvokeResult{Tool: in.Tool, Data: data}, nil
		}
		lastErr = err
		if !retryable(err) {
			return InvokeResult{}, err
		}
	}
	// MCP-003: read-only idempotent calls retry exactly once; both
	// attempts failed at the transport level, surface the last error.
	return InvokeResult{}, fmt.Errorf("%w after %d attempts: %v", ErrInvokeFailed, MaxRetries+1, lastErr)
}

// ToolInfo is one tool advertisement returned by ListTools.
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ListTools performs the read-only catalogue fetch: GET {BaseURL}/tools
// under the same frozen response policy as Invoke (status / encoding / size
// cap, no redirects, single retry on transport failure). The response is
// either a JSON array of tool advertisements or an error.
func (c *Client) ListTools(ctx context.Context) ([]ToolInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, c.TotalTimeout)
	defer cancel()
	target := c.BaseURL + "/tools"
	var lastErr error
	for attempt := 0; attempt <= MaxRetries; attempt++ {
		data, err := c.attempt(ctx, target)
		if err == nil {
			var tools []ToolInfo
			if err := json.Unmarshal(data, &tools); err != nil {
				return nil, fmt.Errorf("mcp: tools catalogue is not a JSON array: %w", err)
			}
			return tools, nil
		}
		lastErr = err
		if !retryable(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("mcp: tools catalogue fetch failed after %d attempts: %v", MaxRetries+1, lastErr)
}

// attempt executes one GET and applies the MCP-002 response policy in
// order: status, encoding, size cap. Reading stops at MaxResponseBytes+1
// so an oversized body is detected without ever buffering it fully.
func (c *Client) attempt(ctx context.Context, target string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// Transport failure: dial, TLS verification, timeout or the
		// redirect block; all surface as *url.Error wrapping our policy
		// error where one applies.
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("%w: %s", ErrHttpStatus, resp.Status)
	}
	if enc := resp.Header.Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "identity") {
		return nil, fmt.Errorf("%w: %q", ErrEncodingBlocked, enc)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxResponseBytes {
		return nil, fmt.Errorf("%w: %d bytes > %d, refused without truncation", ErrResponseTooLarge, len(data), MaxResponseBytes)
	}
	return data, nil
}

// retryable separates transport failures (safe to retry on an idempotent
// GET, MCP-003) from definitive policy answers.
func retryable(err error) bool {
	switch {
	case errors.Is(err, ErrHttpStatus),
		errors.Is(err, ErrResponseTooLarge),
		errors.Is(err, ErrEncodingBlocked),
		errors.Is(err, ErrRedirectBlocked):
		return false
	}
	return true
}
