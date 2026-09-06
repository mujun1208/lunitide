// Package browsernetwork supplies a browser-owned, loopback-only forward proxy.
// Every target is validated and DNS-pinned at dial time, covering redirects,
// subresources and new tabs instead of relying on a one-time navigation check.
package browsernetwork

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

type Policy struct {
	AllowHTTP      bool
	AllowPrivate   bool
	AllowedOrigins []string
}

type Proxy struct {
	policy      Policy
	listener    net.Listener
	server      *http.Server
	transport   *http.Transport
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	connections map[net.Conn]struct{}
	slots       chan struct{}
	resolve     func(context.Context, string) ([]net.IPAddr, error)
	dial        func(context.Context, string, string) (net.Conn, error)
}

func Start(policy Policy) (*Proxy, error) {
	policy.AllowedOrigins = append([]string(nil), policy.AllowedOrigins...)
	for _, origin := range policy.AllowedOrigins {
		if _, err := Origin(origin); err != nil {
			return nil, err
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &Proxy{policy: policy, listener: listener, ctx: ctx, cancel: cancel, connections: map[net.Conn]struct{}{}, slots: make(chan struct{}, 64), resolve: net.DefaultResolver.LookupIPAddr, dial: (&net.Dialer{Timeout: 5 * time.Second}).DialContext}
	p.transport = &http.Transport{Proxy: nil, DialContext: p.dialPinned, ResponseHeaderTimeout: 15 * time.Second, MaxIdleConns: 32, IdleConnTimeout: 30 * time.Second, DisableCompression: true}
	p.server = &http.Server{Handler: p, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 2 * time.Minute, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10, BaseContext: func(net.Listener) context.Context { return ctx }, ConnState: func(c net.Conn, state http.ConnState) {
		p.mu.Lock()
		defer p.mu.Unlock()
		if state == http.StateClosed {
			delete(p.connections, c)
		} else {
			if state == http.StateNew && len(p.connections) >= 128 {
				_ = c.Close()
				return
			}
			p.connections[c] = struct{}{}
		}
	}}
	go func() { _ = p.server.Serve(listener) }()
	return p, nil
}

func (p *Proxy) URL() string { return "http://" + p.listener.Addr().String() }

func (p *Proxy) Close() error {
	p.cancel()
	err := p.server.Close()
	p.transport.CloseIdleConnections()
	p.mu.Lock()
	for c := range p.connections {
		_ = c.Close()
		delete(p.connections, c)
	}
	p.mu.Unlock()
	return err
}

// Origin deliberately compares parsed authorities, never string prefixes.
func Origin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("browser allowlist requires an HTTP(S) origin")
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return u.Scheme + "://" + net.JoinHostPort(strings.ToLower(strings.TrimSuffix(u.Hostname(), ".")), port), nil
}

func (p *Proxy) authorize(u *url.URL) error {
	if u == nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" || (u.Scheme != "https" && (u.Scheme != "http" || !p.policy.AllowHTTP)) {
		return errors.New("browser URL scheme or authority denied")
	}
	port := u.Port()
	if port != "" && port != "80" && port != "443" && port != "8080" && port != "8443" {
		return errors.New("browser destination port denied")
	}
	if len(p.policy.AllowedOrigins) > 0 {
		origin, _ := Origin(u.Scheme + "://" + u.Host)
		found := false
		for _, allowed := range p.policy.AllowedOrigins {
			canonical, _ := Origin(allowed)
			if origin == canonical {
				found = true
				break
			}
		}
		if !found {
			return errors.New("browser origin not in allowlist")
		}
	}
	return nil
}

func (p *Proxy) dialPinned(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if strings.Contains(host, "%") {
		return nil, errors.New("browser address zone denied")
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addresses, err := p.resolve(lookupCtx, host)
	if err != nil || len(addresses) == 0 {
		return nil, fmt.Errorf("browser DNS lookup failed: %w", err)
	}
	for _, a := range addresses {
		if !p.policy.AllowPrivate && (strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") || strings.HasSuffix(strings.ToLower(strings.TrimSuffix(host, ".")), ".localhost") || m7flow.ToolSSRFReject(a.IP.String())) {
			return nil, errors.New("browser target resolves to a forbidden network")
		}
	}
	// Dial literal addresses from this checked answer set. No second resolver
	// lookup can exchange a public validation result for a private destination.
	for _, a := range addresses {
		conn, dialErr := p.dial(lookupCtx, network, net.JoinHostPort(a.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		err = dialErr
	}
	return nil, err
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	default:
		http.Error(w, "browser connection budget exceeded", http.StatusServiceUnavailable)
		return
	}
	select {
	case <-p.ctx.Done():
		http.Error(w, "browser session closed", http.StatusServiceUnavailable)
		return
	default:
	}
	u := r.URL
	if r.Method == http.MethodConnect {
		u = &url.URL{Scheme: "https", Host: r.Host}
	}
	if err := p.authorize(u); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	if r.Method == http.MethodConnect {
		p.tunnel(w, r)
		return
	}
	if r.Header.Get("Upgrade") != "" {
		if r.URL.Scheme != "http" || r.Method != http.MethodGet || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "browser protocol upgrade denied", http.StatusForbidden)
			return
		}
		p.upgrade(w, r)
		return
	}
	requestCtx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	request := r.Clone(requestCtx)
	request.RequestURI = ""
	request.Host = u.Host
	request.Header.Del("Proxy-Authorization")
	request.Header.Del("Proxy-Connection")
	request.Body = http.MaxBytesReader(w, request.Body, 16<<20)
	response, err := p.transport.RoundTrip(request)
	if err != nil {
		http.Error(w, "browser upstream denied or unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.ContentLength > 64<<20 {
		http.Error(w, "browser response exceeds 64 MiB", http.StatusBadGateway)
		return
	}
	for name, values := range response.Header {
		if strings.EqualFold(name, "Connection") || strings.EqualFold(name, "Proxy-Authenticate") {
			continue
		}
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(response.StatusCode)
	n, err := io.Copy(w, io.LimitReader(response.Body, 64<<20))
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	if n == 64<<20 {
		var extra [1]byte
		if count, _ := response.Body.Read(extra[:]); count > 0 {
			panic(http.ErrAbortHandler)
		}
	}
}

func (p *Proxy) tunnel(w http.ResponseWriter, r *http.Request) {
	upstream, err := p.dialPinned(r.Context(), "tcp", r.Host)
	if err != nil {
		http.Error(w, "browser upstream denied or unavailable", http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "tunnel unavailable", http.StatusInternalServerError)
		return
	}
	client, buffer, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	defer func() { p.mu.Lock(); delete(p.connections, client); p.mu.Unlock() }()
	_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	if err := buffer.Flush(); err != nil {
		return
	}
	p.relay(client, buffer, upstream)
}

func (p *Proxy) upgrade(w http.ResponseWriter, r *http.Request) {
	port := r.URL.Port()
	if port == "" {
		port = "80"
	}
	upstream, err := p.dialPinned(r.Context(), "tcp", net.JoinHostPort(r.URL.Hostname(), port))
	if err != nil {
		http.Error(w, "browser upstream denied or unavailable", http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	request := r.Clone(r.Context())
	request.RequestURI = ""
	request.Header.Del("Proxy-Authorization")
	request.Header.Del("Proxy-Connection")
	_ = upstream.SetDeadline(time.Now().Add(5 * time.Second))
	if err := request.Write(upstream); err != nil {
		http.Error(w, "browser upgrade failed", http.StatusBadGateway)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "browser upgrade unavailable", http.StatusInternalServerError)
		return
	}
	client, buffer, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	defer func() { p.mu.Lock(); delete(p.connections, client); p.mu.Unlock() }()
	p.relay(client, buffer, upstream)
}

func (p *Proxy) relay(client net.Conn, buffer io.Reader, upstream net.Conn) {
	deadline := time.Now().Add(5 * time.Minute)
	_ = upstream.SetDeadline(deadline)
	_ = client.SetDeadline(deadline)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(upstream, io.LimitReader(buffer, 64<<20))
		_ = upstream.Close()
		_ = client.Close()
	}()
	_, _ = io.Copy(client, io.LimitReader(upstream, 64<<20))
	_ = upstream.Close()
	_ = client.Close()
	<-done
}
