// Legacy S5 governance application service (migration 0053): the
// Integration / ApiOperation / FieldMapping lifecycle plus the
// CredentialRef gate. All mutations share the agent-runtime single-writer
// transaction so the entity row and its audit record commit atomically.
//
// Wire semantics (M6_ERROR_CATALOG_V2):
//
//	M6-CRD-001  a credentialRef that is missing, expired or revoked never
//	            reaches a call — creation and authorization both refuse,
//	            the circuit breaks and re-authorization is required; there
//	            is never a fallback to an older secret.
//	M6-INT-001  an operation that is not enabled is not authorized — the
//	            call path blocks before any remote I/O.
//	M6-MAP-001  an illegal mapping (transform outside the allowlist or a
//	            defaultValue that breaks the transform's type contract) is
//	            refused and the previously published row stays in force.
//	M6-HLT-001  a paused or otherwise non-active integration blocks
//	            scheduling.
//
// Optimistic concurrency: every lifecycle mutation carries the row version
// the caller read; a lost race answers m6supply.ErrVersionConflict and the
// caller must re-read — nothing is silently overwritten.
package m6app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

var (
	// ErrIntegrationNotFound: the integration row does not exist.
	ErrIntegrationNotFound = errors.New("m6app: integration not found")
	// ErrIntegrationExists: (name, specVersion) already registered.
	ErrIntegrationExists = errors.New("m6app: integration already registered")
	// ErrOperationNotFound: the operation row does not exist.
	ErrOperationNotFound = errors.New("m6app: api operation not found")
	// ErrOperationExists: the operationId is already published under the
	// integration.
	ErrOperationExists = errors.New("m6app: api operation already published")
	// ErrMappingExists: the (operation, source, target, direction) tuple is
	// already published; the old version stays in force.
	ErrMappingExists = errors.New("m6app: field mapping already published")
	// ErrSchemaVersionNotMonotonic: the mapping schemaVersion must exceed
	// every published schemaVersion of the same operation.
	ErrSchemaVersionNotMonotonic = errors.New("m6app: mapping schemaVersion must be monotonic")
	// ErrCredentialNotFound: the credential row does not exist.
	ErrCredentialNotFound = errors.New("m6app: credential ref not found")
)

// IntegrationService implements the S5 governance use cases.
type IntegrationService struct {
	uow   UnitOfWork
	clock Clock
}

func NewIntegrationService(uow UnitOfWork) *IntegrationService {
	return &IntegrationService{uow: uow, clock: systemClock{}}
}

// SetClock substitutes the wall clock (tests).
func (s *IntegrationService) SetClock(c Clock) { s.clock = c }

func (s *IntegrationService) available() error {
	if s == nil || s.uow == nil {
		return ErrServiceUnavailable
	}
	return nil
}

// ── CredentialRef ───────────────────────────────────────────────────────────

// RegisterCredential records a handle-only credential reference. The
// secret value itself never enters this call — only the store handle.
func (s *IntegrationService) RegisterCredential(ctx context.Context, provider, secretHandle string, scopes []string, expiresAt *time.Time) (m6supply.CredentialRef, error) {
	if err := s.available(); err != nil {
		return m6supply.CredentialRef{}, err
	}
	if err := m6supply.ValidateCredentialInput(provider, secretHandle, scopes); err != nil {
		return m6supply.CredentialRef{}, err
	}
	scopesJSON, err := json.Marshal(scopes)
	if err != nil {
		return m6supply.CredentialRef{}, err
	}
	var out m6supply.CredentialRef
	err = s.uow.TransactM6(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		out = m6supply.CredentialRef{
			ID: ulid.Make().String(), Provider: provider, SecretHandle: secretHandle,
			ScopesJSON: string(scopesJSON), ExpiresAt: expiresAt,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		return tx.PutM6CredentialRef(out)
	})
	return out, err
}

// RevokeCredential stamps revoked_at (idempotent at the current version)
// and audits credential.revoked. From this transaction on, every
// authorization against the ref is M6-CRD-001.
func (s *IntegrationService) RevokeCredential(ctx context.Context, id string, expectedVersion int64) (m6supply.CredentialRef, error) {
	if err := s.available(); err != nil {
		return m6supply.CredentialRef{}, err
	}
	var out m6supply.CredentialRef
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		ref, err := tx.RevokeM6CredentialRef(id, expectedVersion, now)
		if errors.Is(err, m6supply.ErrNotFound) {
			return ErrCredentialNotFound
		}
		if err != nil {
			return err
		}
		if ref.RevokedAt == nil {
			return errors.New("m6app: credential revocation did not persist")
		}
		out = ref
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "credential.revoked",
			AggregateID: id, Actor: delegationActor, CreatedAt: now,
			Metadata: marshalJSON(struct {
				CredentialRefID string `json:"credentialRefId"`
				Provider        string `json:"provider"`
			}{CredentialRefID: id, Provider: ref.Provider}),
		})
	})
	return out, err
}

// credentialTx is the CRD-001 gate shared by creation and authorization:
// none needs no credential; every other auth type requires a usable ref.
func credentialTx(tx Tx, refID, authType string, now time.Time) error {
	if authType == m6supply.AuthTypeNone {
		return nil
	}
	if refID == "" {
		return &m6supply.GovernanceError{Code: m6supply.CodeCredentialInvalid, Detail: "credentialRef required for an authenticated integration"}
	}
	ref, err := tx.GetM6CredentialRef(refID)
	if errors.Is(err, m6supply.ErrNotFound) {
		return &m6supply.GovernanceError{Code: m6supply.CodeCredentialInvalid, Detail: "credentialRef not found"}
	}
	if err != nil {
		return err
	}
	return ref.UsableAt(now)
}

// ── Integration lifecycle ───────────────────────────────────────────────────

// CreateIntegrationInput is the creation payload; every field is explicit
// (direction/role/environmentBindings have no defaults).
type CreateIntegrationInput struct {
	Name                string
	Kind                string
	BaseURL             string
	SpecDigest          string
	SpecVersion         string
	AuthType            string
	CredentialRefID     string
	Direction           string
	Role                string
	EnvironmentBindings string // canonical JSON object keyed by environment
}

// Create registers an integration in draft. The credential gate runs at
// creation (M6-CRD-001) and again at every call — a ref that expires or is
// revoked later breaks the circuit without touching the draft row.
func (s *IntegrationService) Create(ctx context.Context, in CreateIntegrationInput) (m6supply.Integration, error) {
	if err := s.available(); err != nil {
		return m6supply.Integration{}, err
	}
	if err := m6supply.ValidateIntegrationInput(in.Name, in.Kind, in.BaseURL, in.SpecDigest,
		in.SpecVersion, in.AuthType, in.Direction, in.Role, in.EnvironmentBindings); err != nil {
		return m6supply.Integration{}, err
	}
	var out m6supply.Integration
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		if err := credentialTx(tx, in.CredentialRefID, in.AuthType, now); err != nil {
			return err
		}
		if _, err := tx.FindM6IntegrationByName(in.Name, in.SpecVersion); err == nil {
			return ErrIntegrationExists
		} else if !errors.Is(err, m6supply.ErrNotFound) {
			return err
		}
		out = m6supply.Integration{
			ID: ulid.Make().String(), Name: in.Name, Kind: in.Kind, BaseURL: in.BaseURL,
			SpecDigest: in.SpecDigest, SpecVersion: in.SpecVersion, AuthType: in.AuthType,
			CredentialRefID: in.CredentialRefID, Direction: in.Direction, Role: in.Role,
			EnvironmentBindings: in.EnvironmentBindings,
			State:               m6supply.IntegrationDraft, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.PutM6Integration(out); err != nil {
			return err
		}
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "integration.state.changed",
			AggregateID: out.ID, Actor: delegationActor, CreatedAt: now,
			Metadata: integrationAuditMeta(out.ID, "", out.State),
		})
	})
	return out, err
}

// Transition moves the integration along the state machine
// (draft→validating→active⇄paused, revoked/failed exits) under the version
// CAS and audits integration.state.changed.
func (s *IntegrationService) Transition(ctx context.Context, id string, expectedVersion int64, to string) (m6supply.Integration, error) {
	if err := s.available(); err != nil {
		return m6supply.Integration{}, err
	}
	var out m6supply.Integration
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		cur, err := tx.GetM6Integration(id)
		if errors.Is(err, m6supply.ErrNotFound) {
			return ErrIntegrationNotFound
		}
		if err != nil {
			return err
		}
		if cur.State == to {
			out = cur
			return nil
		}
		if !m6supply.IntegrationTransitionAllowed(cur.State, to) {
			return m6supply.ErrInvalidTransition
		}
		now := s.clock.Now().UTC()
		next, err := tx.TransitionM6Integration(id, expectedVersion, to, now)
		if errors.Is(err, m6supply.ErrVersionConflict) {
			return err
		}
		if err != nil {
			return err
		}
		out = next
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "integration.state.changed",
			AggregateID: id, Actor: delegationActor, CreatedAt: now,
			Metadata: integrationAuditMeta(id, cur.State, to),
		})
	})
	return out, err
}

// ── ApiOperation ────────────────────────────────────────────────────────────

// OperationInput is the publish payload for one governed operation.
type OperationInput struct {
	IntegrationID       string
	OperationID         string
	Method              string
	PathTemplate        string
	InputSchemaJSON     string
	OutputSchemaJSON    string
	Risk                string
	PaginationSpecJSON  string
	RetrySpecJSON       string
	IdempotencySpecJSON string
}

// PublishOperation registers an operation under a non-revoked
// integration, disabled by default — the enable gate is a separate,
// audited decision. Duplicate operationIds are refused, not overwritten.
func (s *IntegrationService) PublishOperation(ctx context.Context, in OperationInput) (m6supply.ApiOperation, error) {
	if err := s.available(); err != nil {
		return m6supply.ApiOperation{}, err
	}
	if err := m6supply.ValidateOperationInput(in.Method, in.PathTemplate, in.Risk,
		in.PaginationSpecJSON, in.RetrySpecJSON, in.IdempotencySpecJSON); err != nil {
		return m6supply.ApiOperation{}, err
	}
	if !jsonObject(in.InputSchemaJSON) || !jsonObject(in.OutputSchemaJSON) {
		return m6supply.ApiOperation{}, errors.New("m6app: input/output schema must be JSON objects")
	}
	var out m6supply.ApiOperation
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		ig, err := tx.GetM6Integration(in.IntegrationID)
		if errors.Is(err, m6supply.ErrNotFound) {
			return ErrIntegrationNotFound
		}
		if err != nil {
			return err
		}
		if ig.State == m6supply.IntegrationRevoked {
			return m6supply.ErrInvalidTransition
		}
		if _, err := tx.FindM6ApiOperationByOperationID(in.IntegrationID, in.OperationID); err == nil {
			return ErrOperationExists
		} else if !errors.Is(err, m6supply.ErrNotFound) {
			return err
		}
		now := s.clock.Now().UTC()
		out = m6supply.ApiOperation{
			ID: ulid.Make().String(), IntegrationID: in.IntegrationID,
			OperationID: in.OperationID, Method: in.Method, PathTemplate: in.PathTemplate,
			InputSchemaJSON: in.InputSchemaJSON, OutputSchemaJSON: in.OutputSchemaJSON,
			Risk: in.Risk, Enabled: false,
			PaginationSpecJSON: in.PaginationSpecJSON, RetrySpecJSON: in.RetrySpecJSON,
			IdempotencySpecJSON: in.IdempotencySpecJSON,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		return tx.PutM6ApiOperation(out)
	})
	return out, err
}

// EnableOperation CAS-flips the enabled gate (the INT-001 authorization
// boundary for calls).
func (s *IntegrationService) EnableOperation(ctx context.Context, id string, expectedVersion int64, enabled bool) (m6supply.ApiOperation, error) {
	if err := s.available(); err != nil {
		return m6supply.ApiOperation{}, err
	}
	var out m6supply.ApiOperation
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		if _, err := tx.GetM6ApiOperation(id); err != nil {
			if errors.Is(err, m6supply.ErrNotFound) {
				return ErrOperationNotFound
			}
			return err
		}
		var err error
		out, err = tx.SetM6ApiOperationEnabled(id, expectedVersion, enabled, s.clock.Now().UTC())
		return err
	})
	return out, err
}

// ── FieldMapping ────────────────────────────────────────────────────────────

// MappingInput is the publish payload for one immutable mapping row.
type MappingInput struct {
	OperationRowID   string // m6_api_operation.id
	Source           string
	Target           string
	Direction        string
	Required         bool
	TransformID      string
	DefaultValueJSON string
	SchemaVersion    int64
}

// PublishMapping type-checks and appends one mapping row. A published
// (operation, source, target, direction) tuple is never overwritten — the
// old version stays in force; the next schemaVersion must be strictly
// higher (M6-MAP-001 semantics: refuse and keep the old version).
func (s *IntegrationService) PublishMapping(ctx context.Context, in MappingInput) (m6supply.FieldMapping, error) {
	if err := s.available(); err != nil {
		return m6supply.FieldMapping{}, err
	}
	if err := m6supply.ValidateMappingInput(in.Source, in.Target, in.Direction, in.TransformID, in.DefaultValueJSON); err != nil {
		return m6supply.FieldMapping{}, err
	}
	var out m6supply.FieldMapping
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		op, err := tx.GetM6ApiOperation(in.OperationRowID)
		if errors.Is(err, m6supply.ErrNotFound) {
			return ErrOperationNotFound
		}
		if err != nil {
			return err
		}
		if in.SchemaVersion < 1 {
			return ErrSchemaVersionNotMonotonic
		}
		existing, err := tx.ListM6FieldMappings(op.ID)
		if err != nil {
			return err
		}
		var maxVersion int64
		for _, m := range existing {
			if m.Source == in.Source && m.Target == in.Target && m.Direction == in.Direction {
				return ErrMappingExists
			}
			if m.SchemaVersion > maxVersion {
				maxVersion = m.SchemaVersion
			}
		}
		if in.SchemaVersion <= maxVersion {
			return ErrSchemaVersionNotMonotonic
		}
		now := s.clock.Now().UTC()
		out = m6supply.FieldMapping{
			ID: ulid.Make().String(), OperationRowID: op.ID,
			Source: in.Source, Target: in.Target, Direction: in.Direction,
			Required: in.Required, TransformID: in.TransformID,
			DefaultValueJSON: in.DefaultValueJSON, SchemaVersion: in.SchemaVersion,
			CreatedAt: now,
		}
		if err := tx.PutM6FieldMapping(out); err != nil {
			return err
		}
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "mapping.published",
			AggregateID: op.ID, Actor: delegationActor, CreatedAt: now,
			Metadata: marshalJSON(struct {
				OperationID   string `json:"operationId"`
				IntegrationID string `json:"integrationId"`
				Source        string `json:"source"`
				Target        string `json:"target"`
				Direction     string `json:"direction"`
				TransformID   string `json:"transformId"`
				SchemaVersion int64  `json:"schemaVersion"`
			}{
				OperationID: op.OperationID, IntegrationID: op.IntegrationID,
				Source: in.Source, Target: in.Target, Direction: in.Direction,
				TransformID: in.TransformID, SchemaVersion: in.SchemaVersion,
			}),
		})
	})
	return out, err
}

// ── Call-path authorization ─────────────────────────────────────────────────

// AuthorizeCallTx is the pre-call governance gate (shared with the bridge
// wiring): the integration must be active (paused answers HLT-001), the
// credential must be usable (CRD-001) and the operation must be enabled
// (INT-001). No remote I/O happens before this gate passes.
func (s *IntegrationService) AuthorizeCallTx(tx Tx, integrationID, operationID string, now time.Time) error {
	ig, err := tx.GetM6Integration(integrationID)
	if errors.Is(err, m6supply.ErrNotFound) {
		return ErrIntegrationNotFound
	}
	if err != nil {
		return err
	}
	if ig.State == m6supply.IntegrationPaused {
		return &m6supply.GovernanceError{Code: m6supply.CodeHealthBlocked, Detail: "integration paused; scheduling blocked pending manual recovery"}
	}
	if ig.State != m6supply.IntegrationActive {
		return &m6supply.GovernanceError{Code: m6supply.CodeHealthBlocked, Detail: "integration not active"}
	}
	if err := credentialTx(tx, ig.CredentialRefID, ig.AuthType, now); err != nil {
		return err
	}
	op, err := tx.FindM6ApiOperationByOperationID(integrationID, operationID)
	if errors.Is(err, m6supply.ErrNotFound) {
		return ErrOperationNotFound
	}
	if err != nil {
		return err
	}
	if !op.Enabled {
		return &m6supply.GovernanceError{Code: m6supply.CodeOperationDenied, Detail: "operation not enabled; complete authorization first"}
	}
	return nil
}

// AuthorizeCall runs the gate in its own read-only-style transaction.
func (s *IntegrationService) AuthorizeCall(ctx context.Context, integrationID, operationID string) error {
	if err := s.available(); err != nil {
		return err
	}
	return s.uow.TransactM6(ctx, func(tx Tx) error {
		return s.AuthorizeCallTx(tx, integrationID, operationID, s.clock.Now().UTC())
	})
}

// integrationAuditMeta shapes the audit metadata for integration state
// rows. from is empty on creation.
func integrationAuditMeta(id, from, to string) []byte {
	type meta struct {
		IntegrationID string `json:"integrationId"`
		From          string `json:"from,omitempty"`
		To            string `json:"to"`
	}
	return marshalJSON(meta{IntegrationID: id, From: from, To: to})
}

// jsonObject reports whether raw is a non-empty JSON object.
func jsonObject(raw string) bool {
	if len(raw) < 2 {
		return false
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return false
	}
	return len(doc) > 0
}
