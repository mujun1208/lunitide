package networkpolicy

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Options struct {
	Policy                Policy
	Resolver              Resolver
	ConnectTimeout        time.Duration
	ResponseHeaderTimeout time.Duration
	OverallTimeout        time.Duration
	MaxResponseBytes      int64
	MaxSSELineBytes       int
	MaxSSEEventBytes      int
	TLSConfig             *tls.Config
}

type Connector struct {
	BaseURL   string
	Client    *http.Client
	scheme    string
	host      string
	port      string
	authority string
	maxBody   int64
	maxLine   int
	maxEvent  int
	basePath  string
}

func New(ctx context.Context, rawBase, apiPath string, o Options) (*Connector, error) {
	u, err := validateAndJoin(rawBase, apiPath, o.Policy)
	if err != nil {
		return nil, err
	}
	tlsConfig, err := secureTLSConfig(o.TLSConfig, u.Hostname())
	if err != nil {
		return nil, err
	}
	scheme := strings.ToLower(u.Scheme)
	host := canonicalHost(u.Hostname())
	port, err := effectivePort(scheme, u.Port())
	if err != nil {
		return nil, &Error{Code: CodeSSRFBlocked, Op: "validate endpoint", Err: err}
	}
	authority := net.JoinHostPort(host, port)
	// Canonicalize the caller-visible URL to the exact authority that the
	// transport is permitted to dial, including its effective port.
	u.Scheme = scheme
	u.Host = authority
	r := o.Resolver
	if r == nil {
		r = SystemResolver{}
	}
	ips, err := resolveAllowed(ctx, r, host)
	if err != nil {
		return nil, err
	}
	if o.ConnectTimeout <= 0 {
		o.ConnectTimeout = 10 * time.Second
	}
	if o.ResponseHeaderTimeout <= 0 {
		o.ResponseHeaderTimeout = 20 * time.Second
	}
	if o.OverallTimeout <= 0 {
		o.OverallTimeout = 60 * time.Second
	}
	if o.MaxResponseBytes <= 0 {
		o.MaxResponseBytes = 8 << 20
	}
	if o.MaxSSELineBytes <= 0 {
		o.MaxSSELineBytes = 64 << 10
	}
	if o.MaxSSEEventBytes <= 0 {
		o.MaxSSEEventBytes = 1 << 20
	}
	dialer := &net.Dialer{Timeout: o.ConnectTimeout}
	tr := &http.Transport{
		Proxy:                 nil,
		TLSClientConfig:       tlsConfig,
		ResponseHeaderTimeout: o.ResponseHeaderTimeout,
		DialContext:           pinnedDial(dialer, ips, authority),
	}
	c := &Connector{BaseURL: u.String(), scheme: scheme, host: host, port: port, authority: authority, maxBody: o.MaxResponseBytes, maxLine: o.MaxSSELineBytes, maxEvent: o.MaxSSEEventBytes, basePath: u.EscapedPath()}
	c.Client = &http.Client{Transport: tr, Timeout: o.OverallTimeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return &Error{Code: CodeRedirectBlocked, Op: "redirect"}
	}}
	return c, nil
}

func secureTLSConfig(src *tls.Config, serverName string) (*tls.Config, error) {
	if src != nil && (src.InsecureSkipVerify || src.VerifyPeerCertificate != nil || src.VerifyConnection != nil) {
		return nil, &Error{Code: CodeTLSError, Op: "configure TLS"}
	}
	c := &tls.Config{ServerName: canonicalHost(serverName), MinVersion: tls.VersionTLS12}
	if src == nil {
		return c, nil
	}
	cloned := src.Clone()
	c.RootCAs = cloned.RootCAs
	c.Certificates = cloned.Certificates
	c.GetClientCertificate = cloned.GetClientCertificate
	c.NextProtos = append([]string(nil), cloned.NextProtos...)
	c.CipherSuites = append([]uint16(nil), cloned.CipherSuites...)
	c.CurvePreferences = append([]tls.CurveID(nil), cloned.CurvePreferences...)
	if cloned.MinVersion > c.MinVersion {
		c.MinVersion = cloned.MinVersion
	}
	c.MaxVersion = cloned.MaxVersion
	return c, nil
}

func canonicalHost(host string) string { return strings.ToLower(strings.TrimSuffix(host, ".")) }

func effectivePort(scheme, explicit string) (string, error) {
	if explicit == "" {
		switch scheme {
		case "https":
			return "443", nil
		case "http":
			return "80", nil
		default:
			return "", errors.New("unsupported scheme")
		}
	}
	n, err := strconv.Atoi(explicit)
	if err != nil || n < 1 || n > 65535 {
		return "", errors.New("invalid port")
	}
	return strconv.Itoa(n), nil
}

func pinnedDial(d *net.Dialer, ips []netip.Addr, authority string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != authority {
			return nil, &Error{Code: CodeSSRFBlocked, Op: "dial authority"}
		}
		_, port, err := net.SplitHostPort(authority)
		if err != nil {
			return nil, &Error{Code: CodeSSRFBlocked, Op: "dial authority", Err: err}
		}
		var last error
		for _, ip := range ips {
			conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		return nil, last
	}
}

func (c *Connector) Do(req *http.Request) (*http.Response, error) {
	if err := c.validateRequest(req); err != nil {
		return nil, err
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, classifyError("request", err)
	}
	resp.Body = &limitedBody{r: resp.Body, closer: resp.Body, remaining: c.maxBody}
	return resp, nil
}

func (c *Connector) validateRequest(req *http.Request) error {
	if req == nil || req.URL == nil || req.URL.User != nil || req.URL.Opaque != "" {
		return &Error{Code: CodeSSRFBlocked, Op: "validate request"}
	}
	scheme := strings.ToLower(req.URL.Scheme)
	host := canonicalHost(req.URL.Hostname())
	port, err := effectivePort(scheme, req.URL.Port())
	if err != nil || scheme != c.scheme || host != c.host || port != c.port {
		return &Error{Code: CodeSSRFBlocked, Op: "validate request", Err: err}
	}
	// Request.Host overrides the authority emitted on the wire. Forbid this
	// second authority input even when it appears equivalent to the URL.
	if req.Host != "" {
		return &Error{Code: CodeSSRFBlocked, Op: "validate Host header"}
	}
	escaped := req.URL.EscapedPath()
	canonical := (&url.URL{Path: req.URL.Path}).EscapedPath()
	base := strings.TrimSuffix(c.basePath, "/")
	if req.URL.RawPath != "" || escaped != canonical || path.Clean(req.URL.Path) != req.URL.Path ||
		(escaped != base && !strings.HasPrefix(escaped, base+"/")) {
		return &Error{Code: CodeSSRFBlocked, Op: "validate request path"}
	}
	return nil
}

// NewRequest safely constructs a request below the connector's base path.
func (c *Connector) NewRequest(ctx context.Context, method, relativePath string, body io.Reader) (*http.Request, error) {
	relative, err := url.Parse(relativePath)
	if err != nil || relative.IsAbs() || relative.Host != "" || relative.User != nil || relative.RawQuery != "" || relative.Fragment != "" || relative.Opaque != "" {
		return nil, &Error{Code: CodeSSRFBlocked, Op: "build request", Err: err}
	}
	cleaned := path.Clean(relative.Path)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return nil, &Error{Code: CodeSSRFBlocked, Op: "build request"}
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, &Error{Code: CodeSSRFBlocked, Op: "build request", Err: err}
	}
	u.Path = path.Join("/", u.Path, cleaned)
	u.RawPath = ""
	return http.NewRequestWithContext(ctx, method, u.String(), body)
}

type limitedBody struct {
	r         io.Reader
	closer    io.Closer
	remaining int64
	exceeded  bool
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.exceeded {
		return 0, &Error{Code: CodeResponseTooLarge, Op: "read response"}
	}
	if int64(len(p)) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, err := b.r.Read(p)
	b.remaining -= int64(n)
	if b.remaining < 0 {
		b.exceeded = true
		return 0, &Error{Code: CodeResponseTooLarge, Op: "read response"}
	}
	return n, err
}
func (b *limitedBody) Close() error { return b.closer.Close() }

func classifyError(op string, err error) error {
	var pe *Error
	if errors.As(err, &pe) {
		return pe
	}
	if errors.Is(err, context.Canceled) {
		return &Error{Code: CodeCancelled, Op: op, Err: err}
	}
	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
		return &Error{Code: CodeTimeout, Op: op, Err: err}
	}
	var re *net.OpError
	if errors.As(err, &re) && errors.Is(re.Err, syscall.ECONNREFUSED) {
		return &Error{Code: CodeConnectionRefused, Op: op, Err: err}
	}
	var te tls.RecordHeaderError
	var ce *tls.CertificateVerificationError
	if errors.As(err, &te) || errors.As(err, &ce) || strings.Contains(strings.ToLower(err.Error()), "tls") || strings.Contains(strings.ToLower(err.Error()), "certificate") {
		return &Error{Code: CodeTLSError, Op: op, Err: err}
	}
	return &Error{Code: CodeConnectionRefused, Op: op, Err: err}
}

// ReadSSE skips empty/comment keepalives; eof distinguishes clean completion.
func (c *Connector) ReadSSE(r io.Reader) ([]byte, bool, error) {
	var event []byte
	line := make([]byte, 0, min(c.maxLine, 4096))
	one := []byte{0}
	for {
		n, err := r.Read(one)
		if n > 0 {
			if one[0] == '\n' {
				if len(line) > 0 && line[len(line)-1] == '\r' {
					line = line[:len(line)-1]
				}
				if len(line) == 0 {
					if meaningfulSSE(event) {
						return event, false, nil
					}
					event = event[:0]
					continue
				}
				if len(event)+len(line)+1 > c.maxEvent {
					return nil, false, &Error{Code: CodeResponseTooLarge, Op: "read SSE event"}
				}
				event = append(event, line...)
				event = append(event, '\n')
				line = line[:0]
			} else {
				line = append(line, one[0])
				if len(line) > c.maxLine {
					return nil, false, &Error{Code: CodeResponseTooLarge, Op: "read SSE line"}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				if len(line) > 0 {
					if len(event)+len(line)+1 > c.maxEvent {
						return nil, false, &Error{Code: CodeResponseTooLarge, Op: "read SSE event"}
					}
					event = append(event, line...)
					event = append(event, '\n')
				}
				if meaningfulSSE(event) {
					return event, false, nil
				}
				return nil, true, nil
			}
			return nil, false, classifyError("read SSE", err)
		}
	}
}

func meaningfulSSE(event []byte) bool {
	for _, line := range strings.Split(string(event), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, ":") {
			return true
		}
	}
	return false
}
