package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/canonpath"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
)

// M4-C handlers: review.decide and workspace register/grant/lease. The
// workspace methods bind durable identities used by the M4-D+ tools; all
// mutations go through agentrunapp.Service in one transaction with
// idempotency + audit.

type runReviewDTO struct {
	ID             string    `json:"id"`
	RunID          string    `json:"runId"`
	ApprovalDigest string    `json:"approvalDigest"`
	Decision       string    `json:"decision"`
	DecidedBy      string    `json:"decidedBy"`
	DecidedAt      time.Time `json:"decidedAt"`
	CreatedAt      time.Time `json:"createdAt"`
}

func newRunReviewDTO(v agentrun.RunReview) runReviewDTO {
	return runReviewDTO{
		ID:             v.ID,
		RunID:          v.RunID,
		ApprovalDigest: v.ApprovalDigest,
		Decision:       string(v.Decision),
		DecidedBy:      v.DecidedBy,
		DecidedAt:      v.DecidedAt,
		CreatedAt:      v.CreatedAt,
	}
}

func validLowerHexDigest(v string) bool {
	if len(v) != 64 {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}

func handleReviewDecide(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID           string `json:"runId"`
		ExpectedVersion int64  `json:"expectedVersion"`
		ApprovalDigest  string `json:"approvalDigest"`
		Decision        string `json:"decision"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RunID) || p.ExpectedVersion < 1 ||
		!validLowerHexDigest(p.ApprovalDigest) ||
		(p.Decision != string(agentrun.ReviewApproved) && p.Decision != string(agentrun.ReviewRejected)) {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "review.decide 参数无效", false)
	}
	if e.agentRuns == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "Agent 运行数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	result, err := e.agentRuns.ReviewDecide(ctx, r.IdempotencyKey, agentRunMutationActor, p, p.RunID, p.ExpectedVersion, p.ApprovalDigest, agentrun.ReviewDecision(p.Decision))
	if err != nil {
		if errors.Is(err, agentrun.ErrReviewDigestMismatch) {
			return r.Fail("REVIEW_DIGEST_MISMATCH", "审批摘要与待审摘要不一致", false)
		}
		return agentRunFailure(r, err)
	}
	return r.Ok(struct {
		Run    agentRunDTO  `json:"run"`
		Review runReviewDTO `json:"review"`
	}{newAgentRunDTO(result.Run), newRunReviewDTO(result.Review)})
}

type workspaceRegistrationDTO struct {
	ID            string    `json:"id"`
	CanonicalRoot string    `json:"canonicalRoot"`
	RootDigest    string    `json:"rootDigest"`
	Status        string    `json:"status"`
	Version       int64     `json:"version"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type workspaceScopeDTO struct {
	Paths      []string `json:"paths"`
	Operations []string `json:"operations"`
}

type workspaceGrantDTO struct {
	ID             string            `json:"id"`
	RegistrationID string            `json:"registrationId"`
	Scope          workspaceScopeDTO `json:"scope"`
	ExpiresAt      time.Time         `json:"expiresAt"`
	Status         string            `json:"status"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

type workspaceLeaseDTO struct {
	ID           string    `json:"id"`
	GrantID      string    `json:"grantId"`
	FencingToken int64     `json:"fencingToken"`
	ExpiresAt    time.Time `json:"expiresAt"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

func workspaceFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, agentrunapp.ErrIdempotencyKeyRequired):
		return r.Fail("IDEMPOTENCY_KEY_REQUIRED", "写操作需要幂等键", false)
	case errors.Is(err, agentrunapp.ErrIdempotencyConflict):
		return r.Fail("IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求", false)
	case errors.Is(err, agentrun.ErrNotFound):
		return r.Fail("WORKSPACE_NOT_FOUND", "工作区注册或授权不存在", false)
	case errors.Is(err, agentrun.ErrWorkspaceInactive):
		return r.Fail("WORKSPACE_INACTIVE", "工作区注册或授权已失效", false)
	case errors.Is(err, agentrun.ErrInvalid):
		return r.Fail("BRIDGE_SCHEMA_INVALID", "工作区参数无效", false)
	default:
		return r.Fail("STORAGE_UNAVAILABLE", "工作区数据暂时不可用", true)
	}
}

// canonicalWorkspaceRoot resolves a client-supplied root to its canonical
// absolute, symlink-resolved directory path. Volume/file-ID binding is
// deferred to the M4-D handle layer which opens pinned directory handles.
func canonicalWorkspaceRoot(path string) (string, error) {
	if path == "" || len(path) > 1024 || !filepath.IsAbs(path) {
		return "", errors.New("root must be an absolute path")
	}
	resolved, err := canonpath.Canonical(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", errors.New("root must be an existing directory")
	}
	return resolved, nil
}

func handleWorkspaceRegister(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RootPath string `json:"rootPath"`
	}
	if decodePayload(r.Payload, &p) != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "workspace.register 参数无效", false)
	}
	if e.agentRuns == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "工作区数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	root, err := canonicalWorkspaceRoot(p.RootPath)
	if err != nil {
		return r.Fail("WORKSPACE_ROOT_INVALID", "工作区根必须是存在的本地目录绝对路径", false)
	}
	registration, err := e.agentRuns.WorkspaceRegister(ctx, r.IdempotencyKey, agentRunMutationActor, p, root)
	if err != nil {
		return workspaceFailure(r, err)
	}
	return r.Ok(workspaceRegistrationDTO{
		ID:            registration.ID,
		CanonicalRoot: registration.CanonicalRoot,
		RootDigest:    registration.RootDigest,
		Status:        string(registration.Status),
		Version:       registration.Version,
		CreatedAt:     registration.CreatedAt,
		UpdatedAt:     registration.UpdatedAt,
	})
}

// validScopePath reports whether a grant scope path is a relative,
// forward-slash path that cannot escape the workspace root.
func validScopePath(p string) bool {
	if p == "" || len(p) > 512 || strings.ContainsRune(p, '\\') || strings.ContainsRune(p, ':') {
		return false
	}
	if strings.HasPrefix(p, "/") {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func validScopeOperation(op string) bool {
	return op == "read" || op == "write" || op == "execute"
}

func handleWorkspaceGrant(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RegistrationID string `json:"registrationId"`
		Scope          struct {
			Paths      []string `json:"paths"`
			Operations []string `json:"operations"`
		} `json:"scope"`
		TTLSeconds int64 `json:"ttlSeconds"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RegistrationID) ||
		p.TTLSeconds < 60 || p.TTLSeconds > 86400 ||
		len(p.Scope.Paths) < 1 || len(p.Scope.Paths) > 64 ||
		len(p.Scope.Operations) < 1 || len(p.Scope.Operations) > 3 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "workspace.grant 参数无效", false)
	}
	seen := map[string]bool{}
	for _, path := range p.Scope.Paths {
		if !validScopePath(path) {
			return r.Fail("WORKSPACE_SCOPE_INVALID", "授权范围路径必须是工作区内相对路径", false)
		}
	}
	for _, op := range p.Scope.Operations {
		if !validScopeOperation(op) || seen[op] {
			return r.Fail("WORKSPACE_SCOPE_INVALID", "授权范围操作无效或重复", false)
		}
		seen[op] = true
	}
	if e.agentRuns == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "工作区数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	// Canonical scope JSON: sorted keys, no whitespace (map marshal).
	scope, err := json.Marshal(map[string]any{"operations": p.Scope.Operations, "paths": p.Scope.Paths})
	if err != nil {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "workspace.grant 参数无效", false)
	}
	grant, err := e.agentRuns.WorkspaceGrant(ctx, r.IdempotencyKey, agentRunMutationActor, p, p.RegistrationID, scope, p.TTLSeconds)
	if err != nil {
		return workspaceFailure(r, err)
	}
	return r.Ok(workspaceGrantDTO{
		ID:             grant.ID,
		RegistrationID: grant.RegistrationID,
		Scope:          workspaceScopeDTO{Paths: p.Scope.Paths, Operations: p.Scope.Operations},
		ExpiresAt:      grant.ExpiresAt,
		Status:         string(grant.Status),
		CreatedAt:      grant.CreatedAt,
		UpdatedAt:      grant.UpdatedAt,
	})
}

func handleWorkspaceLease(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		GrantID    string `json:"grantId"`
		TTLSeconds int64  `json:"ttlSeconds"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.GrantID) || p.TTLSeconds < 60 || p.TTLSeconds > 3600 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "workspace.lease 参数无效", false)
	}
	if e.agentRuns == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "工作区数据暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	lease, err := e.agentRuns.AcquireLease(ctx, r.IdempotencyKey, agentRunMutationActor, p, p.GrantID, p.TTLSeconds)
	if err != nil {
		return workspaceFailure(r, err)
	}
	return r.Ok(workspaceLeaseDTO{
		ID:           lease.ID,
		GrantID:      lease.GrantID,
		FencingToken: lease.FencingToken,
		ExpiresAt:    lease.ExpiresAt,
		Status:       string(lease.Status),
		CreatedAt:    lease.CreatedAt,
		UpdatedAt:    lease.UpdatedAt,
	})
}
