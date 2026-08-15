// T-6.2.1 application service: the connector metadata snapshot. Runs inside
// one agent-runtime transaction: max(version)+1 and the snapshot row commit
// atomically, so concurrent snapshots can never both claim the same
// snapshot_version (the 0045 UNIQUE backs it up as a hard stop).
package m6app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/connector"
	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/extension"
	"github.com/lunitide/lunitide/internal/worker"
	"github.com/oklog/ulid/v2"
)

var (
	ErrScopeDenied       = fmt.Errorf("m6app: %w", connector.ErrScopeDenied)
	ErrConnectorIDBad    = fmt.Errorf("m6app: %w", connector.ErrInvalidConnectorID)
	ErrWorkerNotVerified = errors.New("m6app: capability token verification failed")
	ErrTaskExists        = errors.New("m6app: cloud task idempotency conflict")
)

// Fetcher pulls metadata through a read-only driver connection; it must
// route every driver call through connector.CheckAccess first.
type Fetcher func(ctx context.Context, connectorID, metadataScope string) (map[string]any, error)

type CatalogService struct {
	uow   UnitOfWork
	fetch Fetcher
	clock Clock
}

func NewCatalogService(uow UnitOfWork, fetch Fetcher) *CatalogService {
	return &CatalogService{uow: uow, fetch: fetch, clock: systemClock{}}
}

// Snapshot validates the scope (DB-002), fetches through the read-only
// adapter, canonicalizes the object map and persists with a
// connector-scoped monotonic snapshot_version — all in one transaction.
func (s *CatalogService) Snapshot(ctx context.Context, connectorID, metadataScope string) (m6supply.ConnectorSnapshot, error) {
	if s == nil || s.uow == nil {
		return m6supply.ConnectorSnapshot{}, ErrServiceUnavailable
	}
	if !connector.ConnectorIDPattern.MatchString(connectorID) {
		return m6supply.ConnectorSnapshot{}, ErrConnectorIDBad
	}
	if !connector.MetadataScopes[metadataScope] {
		return m6supply.ConnectorSnapshot{}, ErrScopeDenied
	}
	var out m6supply.ConnectorSnapshot
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		objects, err := s.fetch(ctx, connectorID, metadataScope)
		if err != nil {
			return err
		}
		canonical, err := token.CanonicalJSON(objects)
		if err != nil {
			return err
		}
		version, err := tx.MaxM6ConnectorSnapshotVersion(connectorID)
		if err != nil {
			return err
		}
		version++
		now := s.clock.Now().UTC()
		out = m6supply.ConnectorSnapshot{
			ID: ulid.Make().String(), ConnectorID: connectorID, Scope: "personal",
			MetadataScope: metadataScope, SnapshotVersion: version,
			ObjectsJSON: string(canonical), FetchedAt: now,
		}
		return tx.PutM6ConnectorSnapshot(out)
	})
	return out, err
}

// DispatchResult is the worker.dispatch outcome (T-6.2.2 wiring).
type DispatchResult struct {
	WorkerID     string
	TaskID       string
	WorktreeRef  string
	FencingToken uint64
}

// CapabilityVerifier verifies the capability token bound to one dispatch.
type CapabilityVerifier func(jobSpecDigest, budgetLeaseID, token string) bool

// DispatchService creates the sandbox profile, acquires the fencing lease
// and persists the cloud task row (state leased) in one transaction. The
// same (jobSpecDigest, budgetLeaseID) dispatch replays to the same task.
type DispatchService struct {
	uow        UnitOfWork
	leases     *worker.LeaseManager
	verify     CapabilityVerifier
	clock      Clock
	ttl        time.Duration
	rootForJob func(jobSpecDigest string) string
}

// NewDispatchService wires the dispatch service; ttl bounds the worker
// lease (the Reaper reclaims after ttl without heartbeats).
func NewDispatchService(uow UnitOfWork, verify CapabilityVerifier, ttl time.Duration) *DispatchService {
	return &DispatchService{uow: uow, leases: worker.NewLeaseManager(), verify: verify, clock: systemClock{}, ttl: ttl}
}

// SetSandboxRoot installs the root-derivation hook (the runtime derives an
// independent temporary directory per job; tests inject temp dirs).
func (s *DispatchService) SetSandboxRoot(fn func(jobSpecDigest string) string) { s.rootForJob = fn }

// Dispatch verifies the capability token, then creates worker + lease +
// durable task. jobSpecDigest doubles as the payload digest; the
// idempotency key is derived from (jobSpecDigest, budgetLeaseID) so replays
// return the original task.
func (s *DispatchService) Dispatch(ctx context.Context, jobSpecDigest, capabilityToken, budgetLeaseID string) (DispatchResult, error) {
	if s == nil || s.uow == nil {
		return DispatchResult{}, ErrServiceUnavailable
	}
	if !s.verify(jobSpecDigest, budgetLeaseID, capabilityToken) {
		return DispatchResult{}, ErrWorkerNotVerified
	}
	workerID := ulid.Make().String()
	lease := s.leases.Acquire(workerID, s.ttl)
	root := ""
	if s.rootForJob != nil {
		root = s.rootForJob(jobSpecDigest)
	}
	var result DispatchResult
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		key := dispatchKey(jobSpecDigest, budgetLeaseID)
		if existing, err := tx.GetM6CloudTaskByIdempotencyKey(key); err == nil {
			result = DispatchResult{
				WorkerID: existing.LeaseOwner, TaskID: existing.ID,
				WorktreeRef: existing.ResultRef,
			}
			return nil
		} else if !errors.Is(err, m6supply.ErrNotFound) {
			return err
		}
		now := s.clock.Now().UTC()
		expires := lease.ExpiresAt
		task := m6supply.CloudTask{
			ID:             ulid.Make().String(),
			IdempotencyKey: key,
			PayloadDigest:  jobSpecDigest,
			LeaseOwner:     workerID,
			LeaseExpiresAt: &expires,
			State:          "leased", Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.PutM6CloudTask(task); err != nil {
			return err
		}
		result = DispatchResult{WorkerID: workerID, TaskID: task.ID, WorktreeRef: root, FencingToken: lease.FencingToken}
		return nil
	})
	return result, err
}

func dispatchKey(jobSpecDigest, budgetLeaseID string) string {
	return "dispatch-" + extension.DeltaDigest([]string{jobSpecDigest, budgetLeaseID})
}
