// Legacy S5 governance domain values (migration 0053): CredentialRef,
// Integration, ApiOperation and FieldMapping. All enums mirror the CHECK
// constraints of m6_credential_ref / m6_integration / m6_api_operation /
// m6_field_mapping; the wire codes mirror M6_ERROR_CATALOG_V2:
//
//	M6-CRD-001  CredentialRef missing / expired / revoked
//	M6-INT-001  ApiOperation not enabled / not authorized
//	M6-MAP-001  FieldMapping type or transform invalid
//	M6-HLT-001  integration paused / unhealthy blocks scheduling
//
// Invariants (M6/02 Legacy S5 Contract):
//
//	Integration  direction/role/environmentBindings explicit; state machine
//	             draft→validating→active⇄paused with revoked/failed exits;
//	             credentials and grants never inherit across environments.
//	ApiOperation pagination carries a termination condition; retry carries
//	             cap/backoff/jitter/status and never exceeds its deadline;
//	             idempotency carries required/header/keyScope/ttl/replay.
//	FieldMapping transform allowlist only; type-checked before publish;
//	             a published (operation, source, target, direction) row is
//	             immutable — corrections land as new rows with a higher
//	             schemaVersion.
//	CredentialRef stores a handle only; revocation blocks immediately and
//	             nothing ever falls back to an older secret.
package m6supply

import (
	"encoding/json"
	"fmt"
	"time"
)

// Wire codes (M6_ERROR_CATALOG_V2, 04 §Dev-Ready error matrix).
const (
	CodeCredentialInvalid = "M6-CRD-001"
	CodeOperationDenied   = "M6-INT-001"
	CodeMappingInvalid    = "M6-MAP-001"
	CodeHealthBlocked     = "M6-HLT-001"
)

// GovernanceError carries a governance wire code up to the bridge envelope.
type GovernanceError struct {
	Code   string
	Detail string
}

func (e *GovernanceError) Error() string {
	return fmt.Sprintf("m6supply: %s: %s", e.Code, e.Detail)
}

// ── CredentialRef ───────────────────────────────────────────────────────────

// CredentialRef is a handle-only pointer at a secret held by the secret
// store. The secret value never passes through this layer and never lands
// in a log, audit row or event payload.
type CredentialRef struct {
	ID           string
	Provider     string
	SecretHandle string
	ScopesJSON   string
	ExpiresAt    *time.Time
	RevokedAt    *time.Time
	Version      int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// UsableAt validates the credential for a call at time now. A missing
// reference is expressed by the caller (an empty CredentialRefID); expiry
// and revocation are judged here. Every failure is M6-CRD-001 — the caller
// must break the circuit and require re-authorization, never fall back to
// an older secret.
func (c CredentialRef) UsableAt(now time.Time) error {
	if c.RevokedAt != nil {
		return &GovernanceError{Code: CodeCredentialInvalid, Detail: "credential revoked; re-authorization required"}
	}
	if c.ExpiresAt != nil && !now.Before(*c.ExpiresAt) {
		return &GovernanceError{Code: CodeCredentialInvalid, Detail: "credential expired; re-authorization required"}
	}
	return nil
}

// ValidateCredentialInput checks the register payload shape.
func ValidateCredentialInput(provider, secretHandle string, scopes []string) error {
	if len(provider) < 1 || len(provider) > 128 {
		return fmt.Errorf("provider length must be 1..128")
	}
	if len(secretHandle) < 1 || len(secretHandle) > 256 {
		return fmt.Errorf("secretHandle length must be 1..256")
	}
	if len(scopes) > 64 {
		return fmt.Errorf("scopes count must be <= 64")
	}
	for _, s := range scopes {
		if len(s) < 1 || len(s) > 128 {
			return fmt.Errorf("scope length must be 1..128")
		}
	}
	return nil
}

// ── Integration ─────────────────────────────────────────────────────────────

// Integration states (m6_integration.state CHECK set, 0053).
const (
	IntegrationDraft      = "draft"
	IntegrationValidating = "validating"
	IntegrationActive     = "active"
	IntegrationPaused     = "paused"
	IntegrationRevoked    = "revoked"
	IntegrationFailed     = "failed"
)

// integrationTransitions is the explicit state machine. revoked is the
// only terminal state; failed may re-enter validating after a fix; paused
// only resumes to active.
var integrationTransitions = map[string]map[string]bool{
	IntegrationDraft:      {IntegrationValidating: true, IntegrationRevoked: true},
	IntegrationValidating: {IntegrationActive: true, IntegrationFailed: true, IntegrationRevoked: true},
	IntegrationActive:     {IntegrationPaused: true, IntegrationFailed: true, IntegrationRevoked: true},
	IntegrationPaused:     {IntegrationActive: true, IntegrationRevoked: true},
	IntegrationFailed:     {IntegrationValidating: true, IntegrationRevoked: true},
	IntegrationRevoked:    {},
}

// IntegrationTransitionAllowed guards the lifecycle CAS.
func IntegrationTransitionAllowed(from, to string) bool {
	if _, ok := integrationTransitions[from]; !ok {
		return false
	}
	return integrationTransitions[from][to]
}

// Integration kinds / directions / roles (CHECK sets, 0053).
const (
	IntegrationKindOpenAPI  = "openapi"
	IntegrationKindDatabase = "database"

	DirectionInbound       = "inbound"
	DirectionOutbound      = "outbound"
	DirectionBidirectional = "bidirectional"

	RoleClient = "client"
	RoleServer = "server"
)

// AuthTypeNone is the only auth value that tolerates an empty
// credentialRefId; the other five mirror the openapi consumer enum.
const AuthTypeNone = "none"

// EnvironmentBindingKeys are the deployment environments a binding may
// address (m6_call_log.environment CHECK set). Credentials and grants are
// per-environment and never inherited across these keys.
var EnvironmentBindingKeys = []string{"development", "test", "production"}

// sensitiveBindingFields are field names that must never appear inside an
// environment binding object — bindings reference handles, not secrets.
var sensitiveBindingFields = map[string]bool{
	"secret": true, "secrets": true, "password": true, "passwd": true,
	"apikey": true, "api_key": true, "token": true, "accesstoken": true,
	"access_token": true, "authorization": true,
}

// ValidateIntegrationInput checks the creation payload shape: enums,
// digest form, specVersion length and the environmentBindings JSON
// contract. environmentBindings must be a JSON object whose keys are a
// non-empty subset of the known environments with object values free of
// secret-like fields.
func ValidateIntegrationInput(name, kind, baseURL, specDigest, specVersion, authType, direction, role, environmentBindings string) error {
	if len(name) < 1 || len(name) > 128 {
		return fmt.Errorf("name length must be 1..128")
	}
	switch kind {
	case IntegrationKindOpenAPI, IntegrationKindDatabase:
	default:
		return fmt.Errorf("kind must be openapi|database")
	}
	if baseURL != "" && len(baseURL) > 2048 {
		return fmt.Errorf("baseUrl length must be <= 2048")
	}
	if len(specDigest) != 64 || !isLowerHex(specDigest) {
		return fmt.Errorf("specDigest must be a 64-char lowercase hex sha-256")
	}
	if len(specVersion) < 1 || len(specVersion) > 64 {
		return fmt.Errorf("specVersion length must be 1..64")
	}
	if !ValidAuthType(authType) {
		return fmt.Errorf("authType must be one of the six consumer enum values")
	}
	switch direction {
	case DirectionInbound, DirectionOutbound, DirectionBidirectional:
	default:
		return fmt.Errorf("direction must be inbound|outbound|bidirectional")
	}
	switch role {
	case RoleClient, RoleServer:
	default:
		return fmt.Errorf("role must be client|server")
	}
	return validateEnvironmentBindings(environmentBindings)
}

// ValidAuthType mirrors the openapi consumer auth enum (0053 CHECK).
func ValidAuthType(a string) bool {
	switch a {
	case "none", "apiKeyHeader", "apiKeyQuery", "bearerToken", "basic", "oauth2ClientCredentials":
		return true
	}
	return false
}

func validateEnvironmentBindings(raw string) error {
	if raw == "" {
		return fmt.Errorf("environmentBindings required (explicit per-environment binding)")
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return fmt.Errorf("environmentBindings must be a JSON object: %v", err)
	}
	if len(doc) == 0 {
		return fmt.Errorf("environmentBindings must declare at least one environment")
	}
	allowed := map[string]bool{}
	for _, k := range EnvironmentBindingKeys {
		allowed[k] = true
	}
	for env, body := range doc {
		if !allowed[env] {
			return fmt.Errorf("environmentBindings key %q is not a known environment", env)
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(body, &obj); err != nil {
			return fmt.Errorf("environmentBindings[%s] must be an object", env)
		}
		for f := range obj {
			if sensitiveBindingFields[normalizeLower(f)] {
				return fmt.Errorf("environmentBindings[%s].%s looks like a secret value; bind a handle instead", env, f)
			}
		}
	}
	return nil
}

func normalizeLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

func isLowerHex(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Integration is one governed integration row.
type Integration struct {
	ID                  string
	Name                string
	Kind                string
	BaseURL             string
	SpecDigest          string
	SpecVersion         string
	AuthType            string
	CredentialRefID     string
	Direction           string
	Role                string
	EnvironmentBindings string
	State               string
	Version             int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// ── ApiOperation ────────────────────────────────────────────────────────────

// Operation risk tiers (m6_api_operation.risk CHECK set).
const (
	OperationRiskLow    = "low"
	OperationRiskMedium = "medium"
	OperationRiskHigh   = "high"
)

// ApiOperation is one governed operation under an integration. The three
// spec payloads are canonical JSON objects validated below; operations are
// published disabled and become callable only after an explicit enable.
type ApiOperation struct {
	ID                  string
	IntegrationID       string
	OperationID         string
	Method              string
	PathTemplate        string
	InputSchemaJSON     string
	OutputSchemaJSON    string
	Risk                string
	Enabled             bool
	PaginationSpecJSON  string
	RetrySpecJSON       string
	IdempotencySpecJSON string
	Version             int64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// paginationSpec is the consumed shape of PaginationSpecJSON.
type paginationSpec struct {
	Type          string `json:"type"`
	MaxPages      *int   `json:"maxPages,omitempty"`
	TerminalField string `json:"terminalField,omitempty"`
}

// retrySpec is the consumed shape of RetrySpecJSON.
type retrySpec struct {
	MaxAttempts   int      `json:"maxAttempts"`
	BackoffMS     int      `json:"backoffMs"`
	Jitter        bool     `json:"jitter"`
	RetryOnStatus []string `json:"retryOnStatus"`
	DeadlineMS    int      `json:"deadlineMs"`
}

// idempotencySpec is the consumed shape of IdempotencySpecJSON.
type idempotencySpec struct {
	Required      bool   `json:"required"`
	Header        string `json:"header"`
	KeyScope      string `json:"keyScope"`
	TTLSeconds    int    `json:"ttlSeconds"`
	ReplayOutcome string `json:"replayOutcome"`
}

// ValidateOperationInput checks the operation payload: enums and the three
// spec contracts. Pagination must carry a termination condition; retry
// must carry cap/backoff/jitter/status and never exceed its deadline;
// idempotency must carry header/keyScope/ttl/replayOutcome.
func ValidateOperationInput(method, pathTemplate, risk, paginationSpecJSON, retrySpecJSON, idempotencySpecJSON string) error {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
	default:
		return fmt.Errorf("method must be an uppercase HTTP verb")
	}
	if len(pathTemplate) < 1 || len(pathTemplate) > 1024 {
		return fmt.Errorf("pathTemplate length must be 1..1024")
	}
	switch risk {
	case OperationRiskLow, OperationRiskMedium, OperationRiskHigh:
	default:
		return fmt.Errorf("risk must be low|medium|high")
	}
	if paginationSpecJSON != "" {
		var p paginationSpec
		if err := json.Unmarshal([]byte(paginationSpecJSON), &p); err != nil {
			return fmt.Errorf("paginationSpec: %v", err)
		}
		switch p.Type {
		case "cursor", "offset", "page":
		default:
			return fmt.Errorf("paginationSpec.type must be cursor|offset|page")
		}
		if p.MaxPages == nil && p.TerminalField == "" {
			return fmt.Errorf("paginationSpec needs a termination condition (maxPages or terminalField)")
		}
		if p.MaxPages != nil && *p.MaxPages < 1 {
			return fmt.Errorf("paginationSpec.maxPages must be >= 1")
		}
	}
	if retrySpecJSON != "" {
		var r retrySpec
		if err := json.Unmarshal([]byte(retrySpecJSON), &r); err != nil {
			return fmt.Errorf("retrySpec: %v", err)
		}
		if r.MaxAttempts < 1 || r.MaxAttempts > 10 {
			return fmt.Errorf("retrySpec.maxAttempts must be 1..10")
		}
		if r.BackoffMS < 0 {
			return fmt.Errorf("retrySpec.backoffMs must be >= 0")
		}
		if len(r.RetryOnStatus) == 0 {
			return fmt.Errorf("retrySpec.retryOnStatus must list the retriable status classes")
		}
		for _, s := range r.RetryOnStatus {
			switch s {
			case "4xx", "5xx":
			default:
				return fmt.Errorf("retrySpec.retryOnStatus entries must be 4xx|5xx")
			}
		}
		if r.DeadlineMS < 0 {
			return fmt.Errorf("retrySpec.deadlineMs must be >= 0")
		}
		// the retry plan must never overshoot its own deadline: worst
		// case is attempt 1 plus maxAttempts-1 waits of backoff each.
		if r.DeadlineMS > 0 && r.BackoffMS*(r.MaxAttempts-1) > r.DeadlineMS {
			return fmt.Errorf("retrySpec backoff plan exceeds deadlineMs")
		}
	}
	if idempotencySpecJSON != "" {
		var i idempotencySpec
		if err := json.Unmarshal([]byte(idempotencySpecJSON), &i); err != nil {
			return fmt.Errorf("idempotencySpec: %v", err)
		}
		if i.Header == "" {
			return fmt.Errorf("idempotencySpec.header required")
		}
		if i.KeyScope == "" {
			return fmt.Errorf("idempotencySpec.keyScope required")
		}
		if i.TTLSeconds < 1 {
			return fmt.Errorf("idempotencySpec.ttlSeconds must be >= 1")
		}
		switch i.ReplayOutcome {
		case "original", "rejected":
		default:
			return fmt.Errorf("idempotencySpec.replayOutcome must be original|rejected")
		}
	}
	return nil
}

// ── FieldMapping ────────────────────────────────────────────────────────────

// Mapping directions (m6_field_mapping.direction CHECK set).
const (
	MappingRequest  = "request"
	MappingResponse = "response"
)

// TransformAllowlist is the closed set of transform ids a mapping may
// reference (M6/02: "transform 仅 allowlist"). Anything else is
// M6-MAP-001.
var TransformAllowlist = []string{
	"identity", "toString", "toNumber", "toInteger", "toBool",
	"trim", "lowercase", "uppercase", "dateFormat",
}

// FieldMapping is one immutable published mapping row. A published
// (operation, source, target, direction) tuple is never overwritten — the
// next schemaVersion lands as a new row.
type FieldMapping struct {
	ID               string
	OperationRowID   string
	Source           string
	Target           string
	Direction        string
	Required         bool
	TransformID      string
	DefaultValueJSON string
	SchemaVersion    int64
	CreatedAt        time.Time
}

// ValidateMappingInput checks payload shape: path form, direction enum,
// transform allowlist and the defaultValue type contract implied by the
// transform (type-checked before publish — failures are M6-MAP-001).
func ValidateMappingInput(source, target, direction, transformID, defaultValueJSON string) error {
	if err := validateFieldPath(source); err != nil {
		return fmt.Errorf("source: %v", err)
	}
	if err := validateFieldPath(target); err != nil {
		return fmt.Errorf("target: %v", err)
	}
	switch direction {
	case MappingRequest, MappingResponse:
	default:
		return fmt.Errorf("direction must be request|response")
	}
	if !transformAllowed(transformID) {
		return &GovernanceError{Code: CodeMappingInvalid, Detail: fmt.Sprintf("transform %q is not in the allowlist", transformID)}
	}
	if defaultValueJSON != "" {
		var v any
		if err := json.Unmarshal([]byte(defaultValueJSON), &v); err != nil {
			return &GovernanceError{Code: CodeMappingInvalid, Detail: "defaultValue is not valid JSON"}
		}
		if err := defaultValueMatches(v, transformID); err != nil {
			return err
		}
	}
	return nil
}

func transformAllowed(id string) bool {
	for _, t := range TransformAllowlist {
		if t == id {
			return true
		}
	}
	return false
}

// defaultValueMatches enforces the type contract each transform implies.
func defaultValueMatches(v any, transformID string) error {
	switch transformID {
	case "toString", "trim", "lowercase", "uppercase", "dateFormat":
		if _, ok := v.(string); !ok {
			return &GovernanceError{Code: CodeMappingInvalid, Detail: "defaultValue must be a string for this transform"}
		}
	case "toNumber":
		if _, ok := v.(float64); !ok {
			return &GovernanceError{Code: CodeMappingInvalid, Detail: "defaultValue must be a number for toNumber"}
		}
	case "toInteger":
		f, ok := v.(float64)
		if !ok || f != float64(int64(f)) {
			return &GovernanceError{Code: CodeMappingInvalid, Detail: "defaultValue must be an integer for toInteger"}
		}
	case "toBool":
		if _, ok := v.(bool); !ok {
			return &GovernanceError{Code: CodeMappingInvalid, Detail: "defaultValue must be a boolean for toBool"}
		}
	}
	return nil
}

// validateFieldPath accepts dotted JSON paths: segments 1..64 chars, no
// empty segment, no leading/trailing dot.
func validateFieldPath(p string) error {
	if len(p) < 1 || len(p) > 512 {
		return fmt.Errorf("length must be 1..512")
	}
	if p[0] == '.' || p[len(p)-1] == '.' {
		return fmt.Errorf("must not start or end with '.'")
	}
	for _, part := range splitDot(p) {
		if len(part) < 1 || len(part) > 64 {
			return fmt.Errorf("segment lengths must be 1..64")
		}
	}
	return nil
}

func splitDot(p string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(p); i++ {
		if i == len(p) || p[i] == '.' {
			out = append(out, p[start:i])
			start = i + 1
		}
	}
	return out
}
