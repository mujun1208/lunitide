package mcapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

// Validation rule identifiers (frozen wire contract).
const (
	RuleTransport    = "MC-VR-01"
	RuleStdioCommand = "MC-VR-02"
	RuleStdioArgs    = "MC-VR-03"
	RuleHTTPSURL     = "MC-VR-04"
	RuleSSRF         = "MC-VR-05"
	RuleEnvSecrets   = "MC-VR-06"
	RuleLengths      = "MC-VR-07"
	RuleQuota        = "MC-VR-08"
)

// ConfigInput is the normalized connector config subject to validation.
type ConfigInput struct {
	Transport     string            `json:"transport"`
	Command       string            `json:"command"`
	Args          []string          `json:"args"`
	URL           string            `json:"url"`
	EnvSecretRefs map[string]string `json:"envSecretRefs"`
}

// ValidationCheck is one rule outcome in the validation chain.
type ValidationCheck struct {
	Rule   string `json:"rule"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
}

// ValidationResult carries the full chain outcome.
type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Checks []ValidationCheck `json:"checks"`
}

// parseInstallConfig strictly decodes one market install config document.
func parseInstallConfig(raw string) (ConfigInput, error) {
	var cfg ConfigInput
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return ConfigInput{}, err
	}
	return cfg, nil
}

// ── mc.config.validate ──────────────────────────────────────────────────────

// ValidateConfig runs the full 8-rule chain (quota + fingerprint included)
// without writing anything.
func (s *Service) ValidateConfig(ctx context.Context, cfg ConfigInput) (ValidationResult, error) {
	if s == nil || s.uow == nil {
		return ValidationResult{}, ErrMcNotFound
	}
	res := s.validateRules(ctx, cfg)
	quota := s.checkQuota(ctx, cfg, "")
	res.Checks = append(res.Checks, quota)
	res.Valid = res.Valid && quota.Passed
	return res, nil
}

// validateRules runs the pure rules R1-R7.
func (s *Service) validateRules(_ context.Context, cfg ConfigInput) ValidationResult {
	res := ValidationResult{Valid: true, Checks: make([]ValidationCheck, 0, 8)}
	fail := func(rule, format string, args ...any) {
		res.Checks = append(res.Checks, ValidationCheck{Rule: rule, Passed: false, Reason: fmt.Sprintf(format, args...)})
		res.Valid = false
	}
	pass := func(rule string) {
		res.Checks = append(res.Checks, ValidationCheck{Rule: rule, Passed: true})
	}

	// R1 transport: explicit value must be legal and consistent with the
	// command/url evidence; both present is ambiguous.
	transport := cfg.Transport
	if transport != "" && transport != m7flow.McpTransportStdio && transport != m7flow.McpTransportHTTPS {
		fail(RuleTransport, "transport %q must be stdio or https", transport)
	} else if cfg.Command != "" && cfg.URL != "" {
		fail(RuleTransport, "command and url are mutually exclusive")
	} else if cfg.Command == "" && cfg.URL == "" {
		fail(RuleTransport, "command or url required")
	} else {
		if transport == "" {
			if cfg.Command != "" {
				transport = m7flow.McpTransportStdio
			} else {
				transport = m7flow.McpTransportHTTPS
			}
		}
		if transport == m7flow.McpTransportStdio && cfg.URL != "" {
			fail(RuleTransport, "stdio transport cannot carry a url")
		}
		if transport == m7flow.McpTransportHTTPS && cfg.Command != "" {
			fail(RuleTransport, "https transport cannot carry a command")
		}
		if res.Valid {
			pass(RuleTransport)
		}
	}

	// R2/R3 stdio surface.
	if transport == m7flow.McpTransportStdio || (transport == "" && cfg.Command != "") {
		if cfg.Command == "" {
			fail(RuleStdioCommand, "stdio needs a command")
		} else if !m7flow.McpStdioCommandAllowed(cfg.Command) {
			fail(RuleStdioCommand, "command %q not whitelisted", cfg.Command)
		} else {
			pass(RuleStdioCommand)
		}
		if len(cfg.Args) == 0 {
			fail(RuleStdioArgs, "stdio needs at least one arg")
		} else if len(cfg.Args) > McMaxArgs {
			fail(RuleStdioArgs, "too many args (%d > %d)", len(cfg.Args), McMaxArgs)
		} else if !m7flow.McpArgsSafe(cfg.Args) {
			fail(RuleStdioArgs, "args contain shell metacharacters")
		} else {
			pass(RuleStdioArgs)
		}
	}

	// R4/R5 https surface.
	if transport == m7flow.McpTransportHTTPS || (transport == "" && cfg.URL != "") {
		if !strings.HasPrefix(cfg.URL, "https://") || len(cfg.URL) > 2048 {
			fail(RuleHTTPSURL, "https url required (max 2048 chars)")
		} else if _, err := url.Parse(cfg.URL); err != nil {
			fail(RuleHTTPSURL, "url unparseable: %v", err)
		} else {
			pass(RuleHTTPSURL)
		}
		if ok, reason := ssrfCheck(cfg.URL); !ok {
			fail(RuleSSRF, "%s", reason)
		} else {
			pass(RuleSSRF)
		}
	}

	// R6 env secret refs.
	envOK := true
	if len(cfg.EnvSecretRefs) > McMaxEnv {
		fail(RuleEnvSecrets, "too many env refs (%d > %d)", len(cfg.EnvSecretRefs), McMaxEnv)
		envOK = false
	} else {
		for k, v := range cfg.EnvSecretRefs {
			if k == "" || v == "" {
				fail(RuleEnvSecrets, "env ref %q empty", k)
				envOK = false
				break
			}
			if strings.Contains(strings.ToLower(k), "key") && strings.Contains(v, "sk-") {
				fail(RuleEnvSecrets, "env ref %q carries a plaintext credential", k)
				envOK = false
				break
			}
		}
	}
	if envOK {
		pass(RuleEnvSecrets)
	}

	// R7 length bounds.
	if len(cfg.Command) > 512 {
		fail(RuleLengths, "command too long (%d > 512)", len(cfg.Command))
	} else {
		tooLong := false
		for _, a := range cfg.Args {
			if len(a) > 512 {
				fail(RuleLengths, "arg too long (%d > 512)", len(a))
				tooLong = true
				break
			}
		}
		if !tooLong {
			pass(RuleLengths)
		}
	}
	return res
}

// checkQuota is R8: endpoint cap + fingerprint duplicate (self excluded).
func (s *Service) checkQuota(ctx context.Context, cfg ConfigInput, selfID string) ValidationCheck {
	transport := cfg.Transport
	if transport == "" {
		if cfg.Command != "" {
			transport = m7flow.McpTransportStdio
		} else {
			transport = m7flow.McpTransportHTTPS
		}
	}
	check := ValidationCheck{Rule: RuleQuota, Passed: true}
	err := s.uow.TransactMc(ctx, func(tx Tx) error {
		if n, err := tx.CountMcpEndpoints(); err != nil {
			return err
		} else if n >= m7app.McpMaxEndpoints {
			check.Passed = false
			check.Reason = fmt.Sprintf("endpoint cap reached (%d)", m7app.McpMaxEndpoints)
			return nil
		}
		argsJSON, _ := json.Marshal(cfg.Args)
		if existing, err := tx.FindMcpEndpointByFingerprint(transport, cfg.Command, cfg.URL, string(argsJSON)); err == nil {
			if existing.EndpointID != selfID {
				check.Passed = false
				check.Reason = "transport target already registered: " + existing.EndpointID
			}
		}
		return nil
	})
	if err != nil {
		check.Passed = false
		check.Reason = "quota check unavailable"
	}
	return check
}

// ssrfCheck enforces the egress policy on one https url: allowed port,
// and every resolved address outside loopback/private/link-local/CGNAT
// ranges (hostname resolution failures fail closed).
func ssrfCheck(rawURL string) (bool, string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false, "url unparseable"
	}
	host := u.Hostname()
	if host == "" {
		return false, "url host missing"
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
			return false, "port " + port + " not allowed"
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		if m7flow.ToolSSRFReject(ip.String()) {
			return false, "address " + host + " is a forbidden network target"
		}
		return true, ""
	}
	// IPv6 literals keep their brackets out of Hostname(); resolve the name
	// and require every address to pass the IP policy.
	resolver := net.DefaultResolver
	addrs, err := resolver.LookupIPAddr(context.Background(), host)
	if err != nil || len(addrs) == 0 {
		return false, "host " + host + " does not resolve"
	}
	for _, a := range addrs {
		if m7flow.ToolSSRFReject(a.IP.String()) {
			return false, "host " + host + " resolves to forbidden address " + a.IP.String()
		}
	}
	return true, ""
}
