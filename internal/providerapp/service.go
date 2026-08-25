// Package providerapp coordinates provider mutations, idempotency, audit, and outbox.
// It deliberately has no dependency on credential storage.
package providerapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/secret"
)

var (
	ErrIdempotencyKeyRequired    = errors.New("idempotency key is required")
	ErrIdempotencyConflict       = errors.New("idempotency key reused with different request")
	ErrCredentialCleanupRequired = errors.New("provider credential must be removed before deletion")
	ErrStorageBusy               = errors.New("provider storage is busy")
)

const (
	modelSyncClaimTTL   = 10 * time.Second
	modelSyncClaimLimit = 128
)

type Record struct {
	Operation, Key, Digest string
	Response               []byte
	CreatedAt, ExpiresAt   time.Time
}
type Claim struct {
	Operation, Key, Digest, Owner string
	ExpiresAt                     time.Time
}
type Event struct {
	ID, Topic, AggregateID string
	Payload                []byte
	CreatedAt              time.Time
}
type Audit struct {
	ID, Action, AggregateID, Actor string
	Metadata                       []byte
	CreatedAt                      time.Time
}
type ClaimedEvent struct {
	Event
	Attempts   int
	LeaseUntil time.Time
}

type Tx interface {
	Get(context.Context, string) (provider.Provider, error)
	Create(context.Context, provider.Provider) (provider.Provider, error)
	Update(context.Context, provider.Provider, int64) (provider.Provider, error)
	Delete(context.Context, string, int64) error
	Idempotency(context.Context, string, string, time.Time) (Record, bool, error)
	PutIdempotency(context.Context, Record) error
	ClaimIdempotency(context.Context, Claim, time.Time, int) (bool, error)
	ReleaseIdempotencyClaim(context.Context, string, string, string) error
	PutAudit(context.Context, Audit) error
	PutOutbox(context.Context, Event) error
	PutCredentialAdoption(context.Context, secret.Ref, string, time.Time) error
}
type UnitOfWork interface {
	Do(context.Context, func(Tx) error) error
}
type Reader interface {
	Get(context.Context, string) (provider.Provider, error)
	List(context.Context, provider.Filter) ([]provider.Provider, error)
}
type Outbox interface {
	Claim(context.Context, string, time.Time, time.Duration, int) ([]ClaimedEvent, error)
	ClaimTopic(context.Context, string, string, time.Time, time.Duration, int) ([]ClaimedEvent, error)
	Retry(context.Context, string, string, time.Time, string) error
	Complete(context.Context, string, string, time.Time) error
}

const CredentialCleanupTopic = "credential.cleanup"

type CredentialCleanup struct {
	CredentialRef string `json:"credentialRef"`
	ProviderID    string `json:"providerId"`
	Origin        string `json:"origin"`
	Protocol      string `json:"protocol"`
}

func putCleanup(ctx context.Context, tx Tx, ref secret.Ref, now time.Time, digest string) error {
	if _, err := ref.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(CredentialCleanup{ref.CredentialRef, ref.ProviderID, ref.Origin, ref.Protocol})
	if err != nil {
		return err
	}
	return tx.PutOutbox(ctx, Event{ID: eventID(now, digest, "credential-cleanup-"+ref.CredentialRef), Topic: CredentialCleanupTopic, AggregateID: ref.ProviderID, Payload: payload, CreatedAt: now})
}

const MaxIdempotencyKeyBytes = 128

// ValidIdempotencyKey accepts 1-128 bytes of printable ASCII (U+0021-U+007E).
// Restricting keys to visible single-byte characters keeps Bridge, Go, and
// SQLite length units identical and avoids whitespace/control-character keys.
func ValidIdempotencyKey(k string) bool {
	if len(k) < 1 || len(k) > MaxIdempotencyKeyBytes {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < 0x21 || k[i] > 0x7e {
			return false
		}
	}
	return true
}

type Service struct {
	read  Reader
	uow   UnitOfWork
	clock Clock
}

// Clock makes transaction boundaries and idempotency expiry deterministic in
// tests. The sampled time is passed into Tx so expiry reclamation and record
// creation use one instant under the same transaction lock.
type Clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

func New(read Reader, uow UnitOfWork) *Service {
	return NewWithClock(read, uow, systemClock{})
}
func NewWithClock(read Reader, uow UnitOfWork, clock Clock) *Service {
	if clock == nil {
		clock = systemClock{}
	}
	return &Service{read: read, uow: uow, clock: clock}
}
func (s *Service) Get(ctx context.Context, id string) (provider.Provider, error) {
	return s.read.Get(ctx, id)
}
func (s *Service) List(ctx context.Context, f provider.Filter) ([]provider.Provider, error) {
	return s.read.List(ctx, f)
}
func (s *Service) IsCredentialReferenceAdopted(ctx context.Context, ref secret.Ref) (bool, error) {
	resolver, ok := s.read.(interface {
		IsCredentialReferenceAdopted(context.Context, secret.Ref) (bool, error)
	})
	if !ok {
		return false, errors.New("credential adoption resolver unavailable")
	}
	return resolver.IsCredentialReferenceAdopted(ctx, ref)
}
func (s *Service) ResolveCredentialBinding(ctx context.Context, id string) (secret.Ref, bool, error) {
	resolver, ok := s.read.(interface {
		ResolveCredentialBinding(context.Context, string) (secret.Ref, bool, error)
	})
	if !ok {
		return secret.Ref{}, false, errors.New("credential binding resolver unavailable")
	}
	return resolver.ResolveCredentialBinding(ctx, id)
}

func digest(v any) (string, []byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", nil, err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), b, nil
}
func key(k string) error {
	if !ValidIdempotencyKey(k) {
		return ErrIdempotencyKeyRequired
	}
	return nil
}
func (s *Service) mutate(ctx context.Context, op, k, actor string, request any, fn func(Tx) (provider.Provider, error)) (provider.Provider, error) {
	if err := key(k); err != nil {
		return provider.Provider{}, err
	}
	d, _, err := digest(request)
	if err != nil {
		return provider.Provider{}, err
	}
	var result provider.Provider
	err = s.uow.Do(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		r, found, e := tx.Idempotency(ctx, op, k, now)
		if e != nil {
			return e
		}
		if found {
			if r.Digest != d {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal(r.Response, &result)
		}
		result, e = fn(tx)
		if e != nil {
			return e
		}
		replayResult := result
		replayResult.LegacyID = ""
		response, e := json.Marshal(replayResult)
		if e != nil {
			return e
		}
		action := map[string]string{"provider.create": "provider.created", "provider.update": "provider.updated", "provider.model.sync": "provider.models.synced", "provider.delete": "provider.deleted"}[op]
		meta, _ := json.Marshal(map[string]any{"version": result.Version})
		if e = tx.PutAudit(ctx, Audit{ID: eventID(now, d, "audit"), Action: action, AggregateID: result.ID, Actor: actor, Metadata: meta, CreatedAt: now}); e != nil {
			return e
		}
		// Never serialize the internal Provider into an integration event: it
		// contains legacy identity and credential-related fields.
		payload, e := json.Marshal(struct {
			ProviderID string `json:"providerId"`
			Version    int64  `json:"version"`
		}{result.ID, result.Version})
		if e != nil {
			return e
		}
		if e = tx.PutOutbox(ctx, Event{ID: eventID(now, d, "outbox"), Topic: action, AggregateID: result.ID, Payload: payload, CreatedAt: now}); e != nil {
			return e
		}
		if result.CredentialRef != "" {
			origin, originErr := provider.NormalizeOrigin(result.BaseURL)
			if originErr != nil {
				return originErr
			}
			ref := secret.Ref{CredentialRef: result.CredentialRef, ProviderID: result.ID, Origin: origin, Protocol: string(result.Protocol)}
			// Legacy imported references are not adoption-capable structured refs.
			// New coordinator references validate and always receive a receipt.
			if _, valid := ref.Validate(); valid == nil {
				e = tx.PutCredentialAdoption(ctx, ref, eventID(now, d, "adoption"), now)
			}
			if e != nil {
				return e
			}
		}
		return tx.PutIdempotency(ctx, Record{Operation: op, Key: k, Digest: d, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}
func eventID(t time.Time, digest, salt string) string {
	h := sha256.Sum256([]byte(salt + digest))
	return t.Format("20060102150405.000000000") + "-" + hex.EncodeToString(h[:8])
}
func (s *Service) Create(ctx context.Context, k, actor string, p provider.Provider) (provider.Provider, error) {
	return s.mutate(ctx, "provider.create", k, actor, p, func(tx Tx) (provider.Provider, error) { return tx.Create(ctx, p) })
}

// CreateRequest hashes the canonical transport request while allowing storage
// to generate the aggregate ID inside the idempotent transaction.
func (s *Service) CreateRequest(ctx context.Context, k, actor string, request any, p provider.Provider) (provider.Provider, error) {
	return s.mutate(ctx, "provider.create", k, actor, request, func(tx Tx) (provider.Provider, error) { return tx.Create(ctx, p) })
}

type updateRequest struct {
	Item            provider.Provider `json:"item"`
	ExpectedVersion int64             `json:"expectedVersion"`
}

func (s *Service) Update(ctx context.Context, k, actor string, p provider.Provider, expected int64) (provider.Provider, error) {
	r := updateRequest{p, expected}
	return s.mutate(ctx, "provider.update", k, actor, r, func(tx Tx) (provider.Provider, error) { return tx.Update(ctx, p, expected) })
}

// UpdateRequest checks idempotency before reading current state, then applies
// the public patch under the same transaction lock after CAS verification.
func (s *Service) UpdateRequest(ctx context.Context, k, actor string, request any, id string, expected int64, apply func(provider.Provider) (provider.Provider, error)) (provider.Provider, error) {
	return s.updateRequest(ctx, "provider.update", k, actor, request, id, expected, apply)
}

func (s *Service) UpdateCredentialRequest(ctx context.Context, k, actor string, request any, id string, expected int64, apply func(provider.Provider) (provider.Provider, error)) (provider.Provider, error) {
	d, _, err := digest(request)
	if err != nil {
		return provider.Provider{}, err
	}
	return s.mutate(ctx, "provider.update", k, actor, request, func(tx Tx) (provider.Provider, error) {
		current, err := tx.Get(ctx, id)
		if err != nil {
			return current, err
		}
		if current.Version != expected {
			return current, provider.ErrConflict
		}
		updated, err := apply(current)
		if err != nil {
			return current, err
		}
		updated, err = tx.Update(ctx, updated, expected)
		if err != nil {
			return current, err
		}
		if current.CredentialRef != "" && current.CredentialRef != updated.CredentialRef {
			origin, e := provider.NormalizeOrigin(current.BaseURL)
			if e != nil {
				return current, e
			}
			if e = putCleanup(ctx, tx, secret.Ref{CredentialRef: current.CredentialRef, ProviderID: current.ID, Origin: origin, Protocol: string(current.Protocol)}, s.clock.Now().UTC(), d); e != nil {
				return current, e
			}
		}
		return updated, nil
	})
}

// SyncModelsRequest gives discovery its own idempotency namespace/audit topic
// while retaining the exact same SQLite CAS transaction boundary.
func (s *Service) SyncModelsRequest(ctx context.Context, k, actor string, request any, id string, expected int64, apply func(provider.Provider) (provider.Provider, error)) (provider.Provider, error) {
	return s.updateRequest(ctx, "provider.model.sync", k, actor, request, id, expected, apply)
}

func (s *Service) updateRequest(ctx context.Context, operation, k, actor string, request any, id string, expected int64, apply func(provider.Provider) (provider.Provider, error)) (provider.Provider, error) {
	return s.mutate(ctx, operation, k, actor, request, func(tx Tx) (provider.Provider, error) {
		current, err := tx.Get(ctx, id)
		if err != nil {
			return current, err
		}
		if current.Version != expected {
			return current, provider.ErrConflict
		}
		updated, err := apply(current)
		if err != nil {
			return current, err
		}
		return tx.Update(ctx, updated, expected)
	})
}

type deleteRequest struct {
	ID              string `json:"id"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

func (s *Service) Delete(ctx context.Context, k, actor, id string, expected int64) (provider.Provider, error) {
	r := deleteRequest{id, expected}
	return s.mutate(ctx, "provider.delete", k, actor, r, func(tx Tx) (provider.Provider, error) {
		p, e := tx.Get(ctx, id)
		if e != nil {
			return p, e
		}
		if e = tx.Delete(ctx, id, expected); e != nil {
			return p, e
		}
		p.Version = expected + 1
		return p, nil
	})
}

// DeleteRequest adds the public Bridge safety policy without changing the
// lower-level delete operation used by trusted internal maintenance workflows.
func (s *Service) DeleteRequest(ctx context.Context, k, actor string, request any, id string, expected int64) (provider.Provider, error) {
	return s.mutate(ctx, "provider.delete", k, actor, request, func(tx Tx) (provider.Provider, error) {
		p, err := tx.Get(ctx, id)
		if err != nil {
			return p, err
		}
		if p.Version != expected {
			return p, provider.ErrConflict
		}
		if p.CredentialRef != "" {
			return p, ErrCredentialCleanupRequired
		}
		if err = tx.Delete(ctx, id, expected); err != nil {
			return p, err
		}
		p.Version = expected + 1
		return p, nil
	})
}

func (s *Service) DeleteCoordinatedRequest(ctx context.Context, k, actor string, request any, id string, expected int64, expectedRef *secret.Ref) (provider.Provider, error) {
	d, _, err := digest(request)
	if err != nil {
		return provider.Provider{}, err
	}
	return s.mutate(ctx, "provider.delete", k, actor, request, func(tx Tx) (provider.Provider, error) {
		p, err := tx.Get(ctx, id)
		if err != nil {
			return p, err
		}
		if p.Version != expected {
			return p, provider.ErrConflict
		}
		if p.CredentialRef == "" {
			if expectedRef != nil {
				return p, provider.ErrConflict
			}
		} else {
			origin, e := provider.NormalizeOrigin(p.BaseURL)
			if e != nil {
				return p, e
			}
			actual := secret.Ref{CredentialRef: p.CredentialRef, ProviderID: p.ID, Origin: origin, Protocol: string(p.Protocol)}
			// A missing expectedRef is used by the private idempotent delete path:
			// derive the authoritative binding in this transaction so replay after
			// a lost response does not depend on resolving a soft-deleted provider.
			if expectedRef != nil && *expectedRef != actual {
				return p, provider.ErrConflict
			}
			if e = putCleanup(ctx, tx, actual, s.clock.Now().UTC(), d); e != nil {
				return p, e
			}
		}
		if err = tx.Delete(ctx, id, expected); err != nil {
			return p, err
		}
		p.Version = expected + 1
		return p, nil
	})
}

func (s *Service) ClaimCredentialCleanup(ctx context.Context, owner string, now time.Time, lease time.Duration, limit int) ([]ClaimedEvent, error) {
	o, ok := s.read.(Outbox)
	if !ok {
		return nil, errors.New("credential cleanup outbox unavailable")
	}
	return o.ClaimTopic(ctx, CredentialCleanupTopic, owner, now, lease, limit)
}
func (s *Service) CompleteCredentialCleanup(ctx context.Context, id, owner string, now time.Time) error {
	o, ok := s.read.(Outbox)
	if !ok {
		return errors.New("credential cleanup outbox unavailable")
	}
	return o.Complete(ctx, id, owner, now)
}
func (s *Service) RetryCredentialCleanup(ctx context.Context, id, owner string, at time.Time, message string) error {
	o, ok := s.read.(Outbox)
	if !ok {
		return errors.New("credential cleanup outbox unavailable")
	}
	return o.Retry(ctx, id, owner, at, message)
}

// SyncModelsDiscovery reserves the key before upstream work. Claims are bounded,
// persisted, and expire after a crashed Engine.
func (s *Service) SyncModelsDiscovery(ctx context.Context, k, actor string, request any, id string, expected int64, discover func(provider.Provider) ([]provider.Model, string, error)) (provider.Provider, string, error) {
	if err := key(k); err != nil {
		return provider.Provider{}, "", err
	}
	d, _, err := digest(request)
	if err != nil {
		return provider.Provider{}, "", err
	}
	type syncReplay struct {
		Provider provider.Provider `json:"provider"`
		Warning  string            `json:"warning,omitempty"`
	}
	owner := eventID(s.clock.Now().UTC(), d, "claim")
	var replay syncReplay
	for {
		acquired, found := false, false
		err = s.uow.Do(ctx, func(tx Tx) error {
			now := s.clock.Now().UTC()
			r, ok, e := tx.Idempotency(ctx, "provider.model.sync", k, now)
			if e != nil {
				return e
			}
			if ok {
				if r.Digest != d {
					return ErrIdempotencyConflict
				}
				found = true
				return json.Unmarshal(r.Response, &replay)
			}
			acquired, e = tx.ClaimIdempotency(ctx, Claim{Operation: "provider.model.sync", Key: k, Digest: d, Owner: owner, ExpiresAt: now.Add(modelSyncClaimTTL)}, now, modelSyncClaimLimit)
			return e
		})
		if err != nil || found {
			return replay.Provider, replay.Warning, err
		}
		if acquired {
			break
		}
		select {
		case <-ctx.Done():
			return provider.Provider{}, "", ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	release := func() {
		_ = s.uow.Do(context.Background(), func(tx Tx) error {
			return tx.ReleaseIdempotencyClaim(context.Background(), "provider.model.sync", k, owner)
		})
	}
	var current provider.Provider
	err = s.uow.Do(ctx, func(tx Tx) error {
		var e error
		current, e = tx.Get(ctx, id)
		if e == nil && current.Version != expected {
			e = provider.ErrConflict
		}
		return e
	})
	if err != nil {
		release()
		return provider.Provider{}, "", err
	}
	models, warning, err := discover(current)
	if err != nil {
		release()
		return provider.Provider{}, "", err
	}
	err = s.uow.Do(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		r, found, e := tx.Idempotency(ctx, "provider.model.sync", k, now)
		if e != nil {
			return e
		}
		if found {
			if r.Digest != d {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal(r.Response, &replay)
		}
		current, e = tx.Get(ctx, id)
		if e != nil {
			return e
		}
		if current.Version != expected {
			return provider.ErrConflict
		}
		current.Models = models
		replay.Provider, e = tx.Update(ctx, current, expected)
		if e != nil {
			return e
		}
		replay.Warning = warning
		response, e := json.Marshal(replay)
		if e != nil {
			return e
		}
		meta, _ := json.Marshal(map[string]any{"version": replay.Provider.Version})
		if e = tx.PutAudit(ctx, Audit{ID: eventID(now, d, "audit"), Action: "provider.models.synced", AggregateID: id, Actor: actor, Metadata: meta, CreatedAt: now}); e != nil {
			return e
		}
		payload, e := json.Marshal(map[string]any{"providerId": id, "version": replay.Provider.Version})
		if e != nil {
			return e
		}
		if e = tx.PutOutbox(ctx, Event{ID: eventID(now, d, "outbox"), Topic: "provider.models.synced", AggregateID: id, Payload: payload, CreatedAt: now}); e != nil {
			return e
		}
		if e = tx.PutIdempotency(ctx, Record{Operation: "provider.model.sync", Key: k, Digest: d, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}); e != nil {
			return e
		}
		return tx.ReleaseIdempotencyClaim(ctx, "provider.model.sync", k, owner)
	})
	if err != nil {
		release()
	}
	return replay.Provider, replay.Warning, err
}
