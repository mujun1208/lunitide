// Legacy S5 governance service coverage: credential lifecycle (CRD-001),
// integration state machine + optimistic lock, operation publish/enable
// (INT-001), mapping publish (MAP-001, allowlist + immutability) and the
// call-path authorization gate (HLT-001 / CRD-001 / INT-001 ordering).
package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/m6app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func newIntegrationService(t *testing.T) *m6app.IntegrationService {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "m6.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return m6app.NewIntegrationService(store.AgentRuntimeRepository())
}

func govCode(t *testing.T, err error) string {
	t.Helper()
	var gov *m6supply.GovernanceError
	if !errors.As(err, &gov) {
		t.Fatalf("want GovernanceError, got %v", err)
	}
	return gov.Code
}

const goodBindings = `{"development":{"endpoint":"https://api.dev.example"},"production":{"endpoint":"https://api.example"}}`

func goodCreateInput(name string) m6app.CreateIntegrationInput {
	return m6app.CreateIntegrationInput{
		Name: name, Kind: m6supply.IntegrationKindOpenAPI,
		BaseURL: "https://api.example",
		SpecDigest: strings.Repeat("a", 64), SpecVersion: "1.0.0",
		AuthType: m6supply.AuthTypeNone, Direction: m6supply.DirectionOutbound,
		Role: m6supply.RoleClient, EnvironmentBindings: goodBindings,
	}
}

func TestCredentialLifecycle(t *testing.T) {
	svc := newIntegrationService(t)
	ctx := context.Background()

	ref, err := svc.RegisterCredential(ctx, "acme", "vault://acme/prod", []string{"pets:read"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Version != 1 || ref.RevokedAt != nil {
		t.Fatalf("fresh credential: %+v", ref)
	}
	if err := ref.UsableAt(time.Now()); err != nil {
		t.Fatalf("fresh credential must be usable: %v", err)
	}

	revoked, err := svc.RevokeCredential(ctx, ref.ID, ref.Version)
	if err != nil {
		t.Fatal(err)
	}
	if revoked.RevokedAt == nil || revoked.Version != 2 {
		t.Fatalf("revoked credential: %+v", revoked)
	}
	// revoke is idempotent at the current version
	again, err := svc.RevokeCredential(ctx, ref.ID, revoked.Version)
	if err != nil {
		t.Fatalf("idempotent revoke: %v", err)
	}
	if again.RevokedAt == nil {
		t.Fatal("idempotent revoke lost revokedAt")
	}
	// a replay with any older version stays idempotent once revoked
	if _, err := svc.RevokeCredential(ctx, ref.ID, ref.Version); err != nil {
		t.Fatalf("replayed revoke must stay idempotent, got %v", err)
	}
	// revoked answers CRD-001
	if code := govCode(t, revoked.UsableAt(time.Now())); code != m6supply.CodeCredentialInvalid {
		t.Fatalf("revoked credential code: %s", code)
	}
	// unknown credential answers not-found
	if _, err := svc.RevokeCredential(ctx, "01AAAAAAAAAAAAAAAAAAAAAAAA", 1); !errors.Is(err, m6app.ErrCredentialNotFound) {
		t.Fatalf("unknown credential: %v", err)
	}
}

func TestCredentialExpiry(t *testing.T) {
	svc := newIntegrationService(t)
	past := time.Now().UTC().Add(-time.Hour)
	ref, err := svc.RegisterCredential(context.Background(), "acme", "vault://acme/exp", nil, &past)
	if err != nil {
		t.Fatal(err)
	}
	if code := govCode(t, ref.UsableAt(time.Now())); code != m6supply.CodeCredentialInvalid {
		t.Fatalf("expired credential code: %s", code)
	}
	// future expiry stays usable
	future := time.Now().UTC().Add(time.Hour)
	ok, err := svc.RegisterCredential(context.Background(), "acme", "vault://acme/fut", nil, &future)
	if err != nil {
		t.Fatal(err)
	}
	if err := ok.UsableAt(time.Now()); err != nil {
		t.Fatalf("future credential must be usable: %v", err)
	}
}

func TestCreateIntegrationCredentialGate(t *testing.T) {
	svc := newIntegrationService(t)
	ctx := context.Background()

	// authType != none without a credentialRef refuses at creation
	in := goodCreateInput("pets")
	in.AuthType = "bearerToken"
	_, err := svc.Create(ctx, in)
	if code := govCode(t, err); code != m6supply.CodeCredentialInvalid {
		t.Fatalf("missing credential at creation: %v", err)
	}

	// a revoked credential refuses at creation
	ref, err := svc.RegisterCredential(ctx, "acme", "vault://acme/pets", []string{"pets:read"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RevokeCredential(ctx, ref.ID, ref.Version); err != nil {
		t.Fatal(err)
	}
	in.CredentialRefID = ref.ID
	_, err = svc.Create(ctx, in)
	if code := govCode(t, err); code != m6supply.CodeCredentialInvalid {
		t.Fatalf("revoked credential at creation: %v", err)
	}

	// a usable credential passes; authType=none needs no credential
	ok, err := svc.Create(ctx, goodCreateInput("pets-none"))
	if err != nil {
		t.Fatal(err)
	}
	if ok.State != m6supply.IntegrationDraft || ok.Version != 1 {
		t.Fatalf("created integration: %+v", ok)
	}

	// (name, specVersion) is unique
	if _, err := svc.Create(ctx, goodCreateInput("pets-none")); !errors.Is(err, m6app.ErrIntegrationExists) {
		t.Fatalf("duplicate name+version: %v", err)
	}

	// environmentBindings must be explicit and secret-free
	bad := goodCreateInput("pets-bad")
	bad.EnvironmentBindings = `{"development":{"password":"hunter2"}}`
	if _, err := svc.Create(ctx, bad); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("secret-bearing binding must be refused: %v", err)
	}
	worse := goodCreateInput("pets-worse")
	worse.EnvironmentBindings = `{"staging":{}}`
	if _, err := svc.Create(ctx, worse); err == nil || !strings.Contains(err.Error(), "environment") {
		t.Fatalf("unknown environment must be refused: %v", err)
	}
}

func TestIntegrationStateMachine(t *testing.T) {
	svc := newIntegrationService(t)
	ctx := context.Background()
	ig, err := svc.Create(ctx, goodCreateInput("sm"))
	if err != nil {
		t.Fatal(err)
	}

	// draft -> active is illegal (must validate first)
	if _, err := svc.Transition(ctx, ig.ID, ig.Version, m6supply.IntegrationActive); !errors.Is(err, m6supply.ErrInvalidTransition) {
		t.Fatalf("draft->active must be refused: %v", err)
	}
	validating, err := svc.Transition(ctx, ig.ID, ig.Version, m6supply.IntegrationValidating)
	if err != nil {
		t.Fatal(err)
	}
	if validating.State != m6supply.IntegrationValidating || validating.Version != 2 {
		t.Fatalf("validating: %+v", validating)
	}
	active, err := svc.Transition(ctx, validating.ID, validating.Version, m6supply.IntegrationActive)
	if err != nil {
		t.Fatal(err)
	}
	paused, err := svc.Transition(ctx, active.ID, active.Version, m6supply.IntegrationPaused)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := svc.Transition(ctx, paused.ID, paused.Version, m6supply.IntegrationActive)
	if err != nil {
		t.Fatalf("paused->active: %v", err)
	}

	// optimistic lock: replaying the paused transition with the old version
	if _, err := svc.Transition(ctx, active.ID, active.Version, m6supply.IntegrationPaused); !errors.Is(err, m6supply.ErrVersionConflict) {
		t.Fatalf("stale version must conflict: %v", err)
	}

	// revoked is terminal
	fresh, err := svc.Transition(ctx, resumed.ID, resumed.Version, m6supply.IntegrationRevoked)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, fresh.ID, fresh.Version, m6supply.IntegrationActive); !errors.Is(err, m6supply.ErrInvalidTransition) {
		t.Fatalf("revoked is terminal: %v", err)
	}

	// same-state transition is idempotent
	same, err := svc.Transition(ctx, fresh.ID, fresh.Version, m6supply.IntegrationRevoked)
	if err != nil {
		t.Fatal(err)
	}
	if same.Version != fresh.Version {
		t.Fatalf("same-state transition must not bump version: %+v", same)
	}

	// unknown integration
	if _, err := svc.Transition(ctx, "01AAAAAAAAAAAAAAAAAAAAAAAA", 1, m6supply.IntegrationActive); !errors.Is(err, m6app.ErrIntegrationNotFound) {
		t.Fatalf("unknown integration: %v", err)
	}
}

func opInput(integrationID string) m6app.OperationInput {
	return m6app.OperationInput{
		IntegrationID: integrationID, OperationID: "listPets", Method: "GET",
		PathTemplate: "/pets", Risk: m6supply.OperationRiskLow,
		InputSchemaJSON:  `{"type":"object"}`,
		OutputSchemaJSON: `{"type":"array"}`,
		PaginationSpecJSON:  `{"type":"cursor","terminalField":"next"}`,
		RetrySpecJSON:       `{"maxAttempts":3,"backoffMs":100,"jitter":true,"retryOnStatus":["5xx"],"deadlineMs":1000}`,
		IdempotencySpecJSON: `{"required":true,"header":"Idempotency-Key","keyScope":"operation","ttlSeconds":86400,"replayOutcome":"original"}`,
	}
}

func TestPublishOperationSpecValidation(t *testing.T) {
	svc := newIntegrationService(t)
	ctx := context.Background()
	ig, err := svc.Create(ctx, goodCreateInput("ops"))
	if err != nil {
		t.Fatal(err)
	}

	op, err := svc.PublishOperation(ctx, opInput(ig.ID))
	if err != nil {
		t.Fatal(err)
	}
	if op.Enabled {
		t.Fatal("operations publish disabled")
	}

	// duplicate operationId is refused, not overwritten
	if _, err := svc.PublishOperation(ctx, opInput(ig.ID)); !errors.Is(err, m6app.ErrOperationExists) {
		t.Fatalf("duplicate operation: %v", err)
	}

	// pagination without a termination condition
	noTerminal := opInput(ig.ID)
	noTerminal.OperationID = "walkPets"
	noTerminal.PaginationSpecJSON = `{"type":"cursor"}`
	if _, err := svc.PublishOperation(ctx, noTerminal); err == nil || !strings.Contains(err.Error(), "termination") {
		t.Fatalf("pagination without termination: %v", err)
	}

	// retry plan overshooting its deadline
	overshoot := opInput(ig.ID)
	overshoot.OperationID = "slowPets"
	overshoot.RetrySpecJSON = `{"maxAttempts":5,"backoffMs":300,"jitter":false,"retryOnStatus":["5xx"],"deadlineMs":500}`
	if _, err := svc.PublishOperation(ctx, overshoot); err == nil || !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("retry over deadline: %v", err)
	}

	// idempotency without header
	noHeader := opInput(ig.ID)
	noHeader.OperationID = "idemPets"
	noHeader.IdempotencySpecJSON = `{"required":true,"keyScope":"operation","ttlSeconds":60,"replayOutcome":"original"}`
	if _, err := svc.PublishOperation(ctx, noHeader); err == nil || !strings.Contains(err.Error(), "header") {
		t.Fatalf("idempotency without header: %v", err)
	}

	// schemas must be JSON objects
	badSchema := opInput(ig.ID)
	badSchema.OperationID = "badPets"
	badSchema.OutputSchemaJSON = `[1,2]`
	if _, err := svc.PublishOperation(ctx, badSchema); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatalf("non-object schema: %v", err)
	}

	// revoked integrations accept no new operations
	revoked, err := svc.Transition(ctx, ig.ID, ig.Version, m6supply.IntegrationRevoked)
	if err != nil {
		t.Fatal(err)
	}
	late := opInput(ig.ID)
	late.OperationID = "latePets"
	if _, err := svc.PublishOperation(ctx, late); !errors.Is(err, m6supply.ErrInvalidTransition) {
		t.Fatalf("publish onto revoked integration: %v", err)
	}
	_ = revoked
}

func TestPublishMappingGovernance(t *testing.T) {
	svc := newIntegrationService(t)
	ctx := context.Background()
	ig, err := svc.Create(ctx, goodCreateInput("maps"))
	if err != nil {
		t.Fatal(err)
	}
	op, err := svc.PublishOperation(ctx, opInput(ig.ID))
	if err != nil {
		t.Fatal(err)
	}

	mapping := m6app.MappingInput{
		OperationRowID: op.ID, Source: "data.items", Target: "pets", Direction: m6supply.MappingResponse,
		TransformID: "identity", SchemaVersion: 1,
	}
	first, err := svc.PublishMapping(ctx, mapping)
	if err != nil {
		t.Fatal(err)
	}
	if first.SchemaVersion != 1 {
		t.Fatalf("mapping: %+v", first)
	}

	// same tuple never overwrites the published version
	if _, err := svc.PublishMapping(ctx, mapping); !errors.Is(err, m6app.ErrMappingExists) {
		t.Fatalf("duplicate mapping tuple: %v", err)
	}

	// transform outside the allowlist is M6-MAP-001
	evil := mapping
	evil.Source, evil.Target = "data.evil", "pets.evil"
	evil.TransformID = "eval"
	if _, err := svc.PublishMapping(ctx, evil); err == nil {
		t.Fatal("allowlist-violating transform must be refused")
	} else if code := govCode(t, err); code != m6supply.CodeMappingInvalid {
		t.Fatalf("allowlist transform: %s", code)
	}

	// defaultValue breaking the transform's type contract is M6-MAP-001
	typed := mapping
	typed.Source, typed.Target = "data.limit", "query.limit"
	typed.TransformID = "toNumber"
	typed.DefaultValueJSON = `"20"`
	if _, err := svc.PublishMapping(ctx, typed); err == nil {
		t.Fatal("type-breaking defaultValue must be refused")
	} else if code := govCode(t, err); code != m6supply.CodeMappingInvalid {
		t.Fatalf("type contract: %s", code)
	}
	// the matching type passes and bumps the schemaVersion monotonically
	typed.DefaultValueJSON = `20`
	typed.SchemaVersion = 2
	if _, err := svc.PublishMapping(ctx, typed); err != nil {
		t.Fatalf("typed mapping must publish: %v", err)
	}
	// non-monotonic schemaVersion is refused
	flat := mapping
	flat.Source, flat.Target = "data.q", "query.q"
	flat.SchemaVersion = 1
	if _, err := svc.PublishMapping(ctx, flat); !errors.Is(err, m6app.ErrSchemaVersionNotMonotonic) {
		t.Fatalf("non-monotonic schemaVersion: %v", err)
	}

	// malformed path
	badPath := mapping
	badPath.Source = ".leading"
	badPath.Target = "x"
	badPath.SchemaVersion = 3
	if _, err := svc.PublishMapping(ctx, badPath); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("bad path: %v", err)
	}

	// unknown operation row
	ghost := mapping
	ghost.OperationRowID = "01AAAAAAAAAAAAAAAAAAAAAAAA"
	if _, err := svc.PublishMapping(ctx, ghost); !errors.Is(err, m6app.ErrOperationNotFound) {
		t.Fatalf("unknown operation: %v", err)
	}
}

func TestAuthorizeCallGate(t *testing.T) {
	svc := newIntegrationService(t)
	ctx := context.Background()

	ref, err := svc.RegisterCredential(ctx, "acme", "vault://acme/gate", []string{"pets:read"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	in := goodCreateInput("gate")
	in.AuthType = "bearerToken"
	in.CredentialRefID = ref.ID
	ig, err := svc.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	op, err := svc.PublishOperation(ctx, opInput(ig.ID))
	if err != nil {
		t.Fatal(err)
	}

	// not active -> HLT-001
	if code := govCode(t, svc.AuthorizeCall(ctx, ig.ID, op.OperationID)); code != m6supply.CodeHealthBlocked {
		t.Fatalf("draft integration: %s", code)
	}
	validating, err := svc.Transition(ctx, ig.ID, ig.Version, m6supply.IntegrationValidating)
	if err != nil {
		t.Fatal(err)
	}
	active, err := svc.Transition(ctx, validating.ID, validating.Version, m6supply.IntegrationActive)
	if err != nil {
		t.Fatal(err)
	}

	// disabled operation -> INT-001 (no remote I/O)
	if code := govCode(t, svc.AuthorizeCall(ctx, ig.ID, op.OperationID)); code != m6supply.CodeOperationDenied {
		t.Fatalf("disabled operation: %s", code)
	}
	enabled, err := svc.EnableOperation(ctx, op.ID, op.Version, true)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.Enabled || enabled.Version != 2 {
		t.Fatalf("enabled: %+v", enabled)
	}
	if err := svc.AuthorizeCall(ctx, ig.ID, op.OperationID); err != nil {
		t.Fatalf("authorized call must pass: %v", err)
	}

	// paused -> HLT-001 takes precedence over everything else
	paused, err := svc.Transition(ctx, active.ID, active.Version, m6supply.IntegrationPaused)
	if err != nil {
		t.Fatal(err)
	}
	if code := govCode(t, svc.AuthorizeCall(ctx, ig.ID, op.OperationID)); code != m6supply.CodeHealthBlocked {
		t.Fatalf("paused integration: %s", code)
	}
	resumed, err := svc.Transition(ctx, paused.ID, paused.Version, m6supply.IntegrationActive)
	if err != nil {
		t.Fatal(err)
	}

	// revoking the credential mid-flight breaks the circuit (CRD-001)
	revoked, err := svc.RevokeCredential(ctx, ref.ID, ref.Version)
	if err != nil {
		t.Fatal(err)
	}
	if code := govCode(t, svc.AuthorizeCall(ctx, ig.ID, op.OperationID)); code != m6supply.CodeCredentialInvalid {
		t.Fatalf("revoked credential at call time: %s", code)
	}
	_ = resumed
	_ = revoked

	// unknown operation
	if _, err := svc.EnableOperation(ctx, "01AAAAAAAAAAAAAAAAAAAAAAAA", 1, true); !errors.Is(err, m6app.ErrOperationNotFound) {
		t.Fatalf("unknown operation enable: %v", err)
	}
	// stale enable version
	if _, err := svc.EnableOperation(ctx, op.ID, op.Version, false); !errors.Is(err, m6supply.ErrVersionConflict) {
		t.Fatalf("stale enable: %v", err)
	}
}
