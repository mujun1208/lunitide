package m7flow

import "strings"

// M7 slice 8 (T-7.8.x): the MCP settings-plane state machine.
//
// probe -> ready <-> degraded -> revoked is the lifecycle; quarantined is
// the fail-closed side state reached from drift (M7-MCP-003) or unconfirmed
// source trust (M7-MCP-002). revoked is terminal - re-adding an endpoint is
// the only path back.

// McpEndpoint states.
const (
	McpStateProbe       = "probe"
	McpStateReady       = "ready"
	McpStateDegraded    = "degraded"
	McpStateRevoked     = "revoked"
	McpStateQuarantined = "quarantined"
)

// mcpTransitions is the legal settings-plane state machine.
var mcpTransitions = map[string]map[string]bool{
	McpStateProbe:       {McpStateReady: true, McpStateDegraded: true, McpStateQuarantined: true, McpStateRevoked: true},
	McpStateReady:       {McpStateDegraded: true, McpStateQuarantined: true, McpStateRevoked: true},
	McpStateDegraded:    {McpStateReady: true, McpStateQuarantined: true, McpStateRevoked: true},
	McpStateQuarantined: {McpStateReady: true, McpStateRevoked: true},
	McpStateRevoked:     {},
}

// McpTransitionAllowed reports whether from -> to is legal.
func McpTransitionAllowed(from, to string) bool { return mcpTransitions[from][to] }

// McpTransports.
const (
	McpTransportStdio = "stdio"
	McpTransportHTTPS = "https"
)

// McpOrigins / source trust values.
const (
	McpOriginMarket  = "market"
	McpOriginManual  = "manual"
	McpTrustSigned   = "signed"
	McpTrustVerified = "verified"
	McpTrustUnknown  = "unknown"
)

// mcpStdioCommandWhitelist: stdio transports may only launch npx/uvx or a
// product-signed executable (the latter injected via config at deploy time;
// the frozen M7 set is npx/uvx).
var mcpStdioCommandWhitelist = map[string]bool{"npx": true, "uvx": true, "node": true}

// McpStdioCommandAllowed reports whether a stdio command is launchable.
func McpStdioCommandAllowed(command string) bool { return mcpStdioCommandWhitelist[command] }

// McpArgsSafe rejects shell metacharacters anywhere in one argv element.
func McpArgsSafe(args []string) bool {
	for _, a := range args {
		if strings.ContainsAny(a, "&|;<>()$`\\\"'\n\r") {
			return false
		}
	}
	return true
}

// McpEndpointConfig is one settings-plane row.
type McpEndpointConfig struct {
	EndpointID       string
	Transport        string
	Command          string
	ArgsJSON         string
	URL              string
	Origin           string
	SourceTrust      string
	Enabled          bool
	State            string
	CapabilityDigest string
	PinnedDigest     string
	LastHealthAt     string
	CreatedAt        string
}

// McpMarketItem is one read-only catalog cache row.
type McpMarketItem struct {
	ID                string
	Name              string
	Publisher         string
	Description       string
	TransportHint     string
	InstallConfigJSON string
	CatalogDigest     string
	Signature         string
	FetchedAt         string
}
