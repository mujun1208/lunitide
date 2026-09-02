package brapp

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

// ── navigation ──────────────────────────────────────────────────────────────

// NavigateResult answers one navigation.
type NavigateResult struct {
	SessionID   string `json:"sessionId"`
	URL         string `json:"url"`
	NavigatedAt string `json:"navigatedAt"`
}

// Navigate sends one URL to a connected session after the URL policy
// gate (allowlist prefix match, port allow-set, private-network block).
func (s *Service) Navigate(ctx context.Context, sessionID, rawURL, actor string) (NavigateResult, error) {
	if s == nil || s.uow == nil {
		return NavigateResult{}, ErrBrNotFound
	}
	if len(sessionID) < 1 || len(sessionID) > 64 {
		return NavigateResult{}, fmt.Errorf("%w: sessionId length", ErrBrSchema)
	}
	if len(rawURL) < 1 || len(rawURL) > 2048 {
		return NavigateResult{}, fmt.Errorf("%w: url length", ErrBrSchema)
	}
	now := s.clock.Now().UTC()
	if !s.navLimit.allow(now, BrNavigateRatePerMinute) {
		return NavigateResult{}, ErrBrRateLimited
	}
	settings, err := s.GetSettings(ctx)
	if err != nil {
		return NavigateResult{}, err
	}
	if reason := s.checkNavigateURL(ctx, rawURL, settings); reason != "" {
		return NavigateResult{}, fmt.Errorf("%w: %s", ErrBrURLPolicy, reason)
	}
	var sess Session
	err = s.uow.TransactBr(ctx, func(tx Tx) error {
		row, err := tx.GetBrSession(sessionID)
		sess = row
		return err
	})
	if isNotFound(err) {
		return NavigateResult{}, fmt.Errorf("%w: %s", ErrBrNotFound, sessionID)
	}
	if err != nil {
		return NavigateResult{}, err
	}
	if sess.State != StateConnected {
		return NavigateResult{}, fmt.Errorf("%w: session state %s", ErrBrState, sess.State)
	}
	if nerr := s.host.Navigate(ctx, sess, rawURL); nerr != nil {
		return NavigateResult{}, nerr
	}
	ts := now.Format(time.RFC3339)
	_ = s.uow.TransactBr(ctx, func(tx Tx) error {
		_, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "browser.navigated",
			ResourceType: "br_session", ResourceID: sessionID,
			Actor: actorOr(actor), CorrelationID: rawURL, CreatedAt: ts,
		})
		return err
	})
	return NavigateResult{SessionID: sessionID, URL: rawURL, NavigatedAt: ts}, nil
}

// checkNavigateURL applies the URL policy with the service resolver.
func (s *Service) checkNavigateURL(ctx context.Context, rawURL string, settings Settings) string {
	resolve := s.resolve
	if resolve == nil {
		resolve = defaultResolver
	}
	return checkNavigateURL(ctx, rawURL, settings, resolve)
}

// CheckNavigateURL enforces the navigation URL policy; empty string
// means accepted, otherwise the rejection reason.
func CheckNavigateURL(rawURL string, s Settings) string {
	return checkNavigateURL(context.Background(), rawURL, s, defaultResolver)
}

// checkNavigateURL is the resolver-injectable core.
func checkNavigateURL(ctx context.Context, rawURL string, s Settings, resolve HostResolver) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "url unparseable"
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "scheme must be http/https"
	}
	host := u.Hostname()
	if host == "" {
		return "url host missing"
	}
	if port := u.Port(); port != "" {
		allowed := false
		for _, p := range []string{"80", "443", "8080", "8443"} {
			if port == p {
				allowed = true
				break
			}
		}
		if !allowed {
			return "port " + port + " not allowed"
		}
	}
	if len(s.Allowlist) > 0 {
		origin := u.Scheme + "://" + u.Host
		matched := false
		for _, entry := range s.Allowlist {
			e := strings.TrimSuffix(entry, "/")
			if origin == e || strings.HasPrefix(origin, e+"/") || strings.HasPrefix(origin+"/", e) {
				matched = true
				break
			}
		}
		if !matched {
			return "origin not in navigation allowlist"
		}
	}
	if s.BlockPrivateNetwork {
		// localhost is always loopback regardless of resolver answers.
		if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
			return "address " + host + " is a forbidden network target"
		}
		if ip := net.ParseIP(host); ip != nil {
			if m7flow.ToolSSRFReject(ip.String()) {
				return "address " + host + " is a forbidden network target"
			}
			return ""
		}
		addrs, rerr := resolve(ctx, host)
		if rerr != nil || len(addrs) == 0 {
			return "host " + host + " does not resolve"
		}
		for _, a := range addrs {
			if m7flow.ToolSSRFReject(a.IP.String()) {
				return "host " + host + " resolves to forbidden address " + a.IP.String()
			}
		}
	}
	return ""
}
