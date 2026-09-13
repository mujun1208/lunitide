// Package projectapp coordinates project queries and atomic idempotent creation.
package projectapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

var (
	ErrIdempotencyKeyRequired = errors.New("idempotency key is required")
	ErrIdempotencyConflict    = errors.New("idempotency key reused with different request")
	ErrProjectCapacityReached = errors.New("project capacity reached")
	ErrProjectVersionConflict = errors.New("project version conflict")
	ErrInvalidTransition      = errors.New("project lifecycle transition is invalid")
	ErrRootRequired           = errors.New("project root path is required")
	ErrRootInvalid            = errors.New("project root path is invalid")
	ErrRootBusy               = errors.New("project root path is busy")
	ErrRootReadonly           = errors.New("project root path is not writable")
	ErrTreeInvalid            = errors.New("project tree is invalid")
	ErrTreeFailed             = errors.New("project tree materialize failed")
	ErrTreeRequired           = errors.New("project tree is required")
	ErrTaskNotFound           = errors.New("project task not found")
	ErrTaskPhase              = errors.New("project task phase is invalid")
	ErrExecutorUnavailable    = errors.New("project executor is unavailable")
	ErrDevIncomplete          = errors.New("project development is incomplete")
	ErrTestOpen               = errors.New("project test items remain open")
	ErrTestReasonRequired     = errors.New("project test failure reason is required")
	ErrTestNoSource           = errors.New("project test item has no source")
	ErrGenerateEmpty          = errors.New("generated deliverable is empty")
	ErrGenerateSkipApproved   = errors.New("approved deliverable cannot be overwritten")
	ErrTemplateMissing        = errors.New("project template is missing")
	ErrRulesFailed            = errors.New("project rules materialize failed")
	ErrRulesStale             = errors.New("project rules are stale")
	ErrSchemaInvalid          = errors.New("project database schema is invalid")
	ErrDBBindInvalid          = errors.New("project database path is invalid")
	ErrDBFailed               = errors.New("project database materialize failed")
	ErrDBIncomplete           = errors.New("project database tables are incomplete")
	ErrDBRequired             = errors.New("project database must be ready")
	ErrInterfaceRequired      = errors.New("project interface phase must be confirmed")
	ErrBoardSourceInvalid     = errors.New("project board source is invalid")
	ErrBoardDirty             = errors.New("project board still has items to reprocess")
	ErrSelfTestRequired       = errors.New("project item self-test is required")
	ErrTestKindUnsupported    = errors.New("project test kind is unsupported")
	ErrIntegrationNotReady    = errors.New("project integration scenario is not ready")
	ErrSyncInvalid            = errors.New("project release sync destination is invalid")
	ErrSyncRequired           = errors.New("project release sync is required")
	ErrAttachmentRequired     = errors.New("project deliverable attachment is required")
)

func IsRootBusy(err error) bool { return errors.Is(err, ErrRootBusy) }

type Tx interface {
	CreateProject(context.Context, project.Project) (project.Project, error)
	GetProject(context.Context, string) (project.Project, error)
	UpdateProject(context.Context, string, int64, func(*project.Project) error) (project.Project, error)
	Idempotency(context.Context, string, string, time.Time) (providerapp.Record, bool, error)
	PutIdempotency(context.Context, providerapp.Record) error
	PutAudit(context.Context, providerapp.Audit) error
}
type UnitOfWork interface {
	DoProject(context.Context, func(Tx) error) error
}

// PhaseCompletionTx performs evidence checks, freezes deliverables, completes
// the stage and advances the project on the same project transaction.
type PhaseCompletionTx interface {
	CompleteProjectPhase(context.Context, string, int64, int) (project.Project, error)
}
type Reader interface {
	ListProjects(context.Context, project.Filter) ([]project.Project, error)
	GetProject(context.Context, string) (project.Project, error)
}
type ArtifactChecker interface {
	ProjectHasArtifacts(context.Context, string) (bool, error)
}

// Deleter removes a project and all its dependent records.
type Deleter interface {
	DeleteProject(context.Context, string) error
}
type Clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type Service struct {
	read      Reader
	uow       UnitOfWork
	deleter   Deleter
	artifacts ArtifactChecker
	clock     Clock
}

func New(read Reader, uow UnitOfWork) *Service {
	return &Service{read: read, uow: uow, clock: systemClock{}}
}
func NewWithClock(read Reader, uow UnitOfWork, clock Clock) *Service {
	return &Service{read: read, uow: uow, clock: clock}
}
func (s *Service) SetDeleter(d Deleter)                 { s.deleter = d }
func (s *Service) SetArtifactChecker(c ArtifactChecker) { s.artifacts = c }
func (s *Service) Get(ctx context.Context, id string) (project.Project, error) {
	if s == nil || s.read == nil {
		return project.Project{}, errors.New("project reader is unavailable")
	}
	return s.read.GetProject(ctx, id)
}
func (s *Service) HasArtifacts(ctx context.Context, id string) (bool, error) {
	if s == nil || s.artifacts == nil {
		return false, nil
	}
	return s.artifacts.ProjectHasArtifacts(ctx, id)
}
func (s *Service) Delete(ctx context.Context, id string) error {
	if s == nil || s.deleter == nil {
		return errors.New("project deleter unavailable")
	}
	return s.deleter.DeleteProject(ctx, id)
}
func (s *Service) List(ctx context.Context, f project.Filter) ([]project.Project, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("project reader is unavailable")
	}
	return s.read.ListProjects(ctx, f)
}

func (s *Service) Create(ctx context.Context, key, actor string, request any, p project.Project) (project.Project, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return project.Project{}, ErrIdempotencyKeyRequired
	}
	if s == nil || s.uow == nil || s.clock == nil {
		return project.Project{}, errors.New("project unit of work is unavailable")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return project.Project{}, err
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	var result project.Project
	err = s.uow.DoProject(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency(ctx, "project.create", key, now)
		if err != nil {
			return err
		}
		if found {
			if record.Digest != digest {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal(record.Response, &result)
		}
		result, err = tx.CreateProject(ctx, p)
		if err != nil {
			return err
		}
		// Keep the durable replay representation deliberately narrower than the
		// domain object so future internal fields cannot become public by accident.
		response, err := json.Marshal(projectReplayDTOFrom(result))
		if err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"version": result.Version})
		eventSum := sha256.Sum256([]byte("project-audit\x00" + digest + "\x00" + result.ID))
		var eventULID ulid.ULID
		copy(eventULID[:], eventSum[:16])
		eventID := eventULID.String()
		if err = tx.PutAudit(ctx, providerapp.Audit{ID: eventID, Action: "project.created", AggregateID: result.ID, Actor: actor, Metadata: meta, CreatedAt: now}); err != nil {
			return err
		}
		return tx.PutIdempotency(ctx, providerapp.Record{Operation: "project.create", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}

type projectReplayDTO struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	ProjectCode       string         `json:"projectCode"`
	Type              project.Type   `json:"type"`
	Description       string         `json:"description"`
	Summary           string         `json:"summary"`
	Objective         string         `json:"objective"`
	Client            string         `json:"client"`
	ContractNo        string         `json:"contractNo"`
	Amount            float64        `json:"amount"`
	Budget            float64        `json:"budget"`
	PlanStart         string         `json:"planStart"`
	PlanEnd           string         `json:"planEnd"`
	Remark            string         `json:"remark"`
	CloseReason       string         `json:"closeReason"`
	StatusBeforeClose project.Status `json:"statusBeforeClose"`
	ReopenReason      string         `json:"reopenReason"`
	OrgID             string         `json:"orgId,omitempty"`
	SpaceID           string         `json:"spaceId,omitempty"`
	Status            project.Status `json:"status"`
	RootPath          string           `json:"rootPath,omitempty"`
	TreeStatus        project.TreeStatus `json:"treeStatus,omitempty"`
	TreeDigest        string           `json:"treeDigest,omitempty"`
	TreeGeneratedAt   string           `json:"treeGeneratedAt,omitempty"`
	DefaultExecutor      project.Executor   `json:"defaultExecutor,omitempty"`
	RulesDigest          string             `json:"rulesDigest,omitempty"`
	RulesMaterializedAt  string             `json:"rulesMaterializedAt,omitempty"`
	DBStatus             project.DBStatus   `json:"dbStatus,omitempty"`
	DBPath               string             `json:"dbPath,omitempty"`
	DBDigest             string             `json:"dbDigest,omitempty"`
	DBVerifiedAt         string             `json:"dbVerifiedAt,omitempty"`
	CreatedAt            time.Time          `json:"createdAt"`
	UpdatedAt            time.Time          `json:"updatedAt"`
	Version              int64              `json:"version"`
}

func projectReplayDTOFrom(p project.Project) projectReplayDTO {
	return projectReplayDTO{ID: p.ID, Name: p.Name, ProjectCode: p.ProjectCode, Type: p.Type, Description: p.Description, Summary: p.Summary, Objective: p.Objective, Client: p.Client, ContractNo: p.ContractNo, Amount: p.Amount, Budget: p.Budget, PlanStart: p.PlanStart, PlanEnd: p.PlanEnd, Remark: p.Remark, CloseReason: p.CloseReason, StatusBeforeClose: p.StatusBeforeClose, ReopenReason: p.ReopenReason, OrgID: p.OrgID, SpaceID: p.SpaceID, Status: p.Status, RootPath: p.RootPath, TreeStatus: p.TreeStatus, TreeDigest: p.TreeDigest, TreeGeneratedAt: p.TreeGeneratedAt, DefaultExecutor: p.DefaultExecutor, RulesDigest: p.RulesDigest, RulesMaterializedAt: p.RulesMaterializedAt, DBStatus: p.DBStatus, DBPath: p.DBPath, DBDigest: p.DBDigest, DBVerifiedAt: p.DBVerifiedAt, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, Version: p.Version}
}

// Mutate applies an optimistic-locking lifecycle mutation (update / publish /
// close / reopen) inside one project unit of work with audit trail.
func (s *Service) Mutate(ctx context.Context, key, actor, action string, id string, version int64, request any, mutate func(*project.Project) error) (project.Project, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return project.Project{}, ErrIdempotencyKeyRequired
	}
	if s == nil || s.uow == nil || s.clock == nil {
		return project.Project{}, errors.New("project unit of work is unavailable")
	}
	// Callers pass the decoded full payload, not just id/version. The closure
	// is applied only after this durable request identity has been checked.
	digest, err := mutationDigest(actor, action, id, version, request)
	if err != nil {
		return project.Project{}, err
	}
	var result project.Project
	err = s.uow.DoProject(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency(ctx, action, key, now)
		if err != nil {
			return err
		}
		if found {
			if record.Digest != digest {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal(record.Response, &result)
		}
		if action == "project.advanceStatus" {
			var phaseRequest struct {
				Phase int `json:"phase"`
			}
			raw, marshalErr := json.Marshal(request)
			if marshalErr != nil {
				return marshalErr
			}
			if err := json.Unmarshal(raw, &phaseRequest); err != nil {
				return err
			}
			phaseTx, ok := tx.(PhaseCompletionTx)
			if !ok {
				return ErrInvalidTransition
			}
			result, err = phaseTx.CompleteProjectPhase(ctx, id, version, phaseRequest.Phase)
		} else {
			result, err = tx.UpdateProject(ctx, id, version, mutate)
		}
		if err != nil {
			return err
		}
		response, err := json.Marshal(projectReplayDTOFrom(result))
		if err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"version": result.Version, "status": result.Status})
		eventSum := sha256.Sum256([]byte("project-audit\x00" + action + "\x00" + result.ID + "\x00" + key + "\x00" + digest))
		var eventULID ulid.ULID
		copy(eventULID[:], eventSum[:16])
		if err = tx.PutAudit(ctx, providerapp.Audit{ID: eventULID.String(), Action: projectAuditAction(action), AggregateID: result.ID, Actor: actor, Metadata: meta, CreatedAt: now}); err != nil {
			return err
		}
		return tx.PutIdempotency(ctx, providerapp.Record{Operation: action, Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}

func mutationDigest(actor, action, id string, version int64, request any) (string, error) {
	body, err := json.Marshal(struct {
		Actor   string `json:"actor"`
		Action  string `json:"action"`
		ID      string `json:"id"`
		Version int64  `json:"version"`
		Payload any    `json:"payload"`
	}{actor, action, id, version, request})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

// projectAuditAction maps bridge methods onto the frozen audit_events
// CHECK list (project.updated / published / closed / reopened).
func projectAuditAction(action string) string {
	switch action {
	case "project.update":
		return "project.updated"
	case "project.publish":
		return "project.published"
	case "project.close":
		return "project.closed"
	case "project.reopen":
		return "project.reopened"
	case "project.advanceStatus":
		return "project.advanced"
	case "project.delete":
		return "project.deleted"
	default:
		return action
	}
}
