package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/connector"
	"github.com/lunitide/lunitide/internal/delegation"
	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/extension"
	"github.com/lunitide/lunitide/internal/m6app"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/merge"
)

// M6 slice-1 handlers (T-6.1.x): extension.search / extension.install /
// extension.lifecycle and mcp6.register / mcp6.invoke / mcp6.revoke.
//
// Error mapping follows M6_ERROR_CATALOG_V2: mcp6 lifecycle failures carry
// the M6-MCP-00x codes verbatim; extension failures use the extension
// family. Quarantined installs are a success response (state=quarantined)
// with the verdict code, because the quarantine itself is durable state.

// m6Subject is the local single-user subject for slice 1; multi-subject
// arrives with the project-scope governance slice.
const m6Subject = "local-user"

func handleExtensionSearch(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Query   string `json:"query"`
		Scope   string `json:"scope"`
		Filters *struct {
			Publisher string `json:"publisher"`
			Category  string `json:"category"`
			MaxRisk   string `json:"maxRisk"`
		} `json:"filters"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.Query) < 1 || len(p.Query) > 256 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "extension.search 参数无效", false)
	}
	if p.Scope != "personal" && p.Scope != "project" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "extension.search 参数无效", false)
	}
	maxRisk := "medium"
	publisher := ""
	if p.Filters != nil {
		if p.Filters.MaxRisk != "" {
			maxRisk = p.Filters.MaxRisk
		}
		publisher = p.Filters.Publisher
	}
	if maxRisk != "low" && maxRisk != "medium" {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "extension.search 参数无效", false)
	}
	if e.m6ext == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "扩展目录暂时不可用", true)
	}
	items, err := e.m6ext.Search(ctx, strings.ToLower(p.Query), strings.ToLower(publisher), maxRisk)
	if err != nil {
		return m6ExtensionFailure(r, err)
	}
	if items == nil {
		items = []m6app.SearchItem{}
	}
	return bridge.Success(r.ID, struct {
		Items []m6app.SearchItem `json:"items"`
	}{items})
}

func handleExtensionInstall(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ArtifactID      string `json:"artifactId"`
		Version         string `json:"version"`
		PermissionGrant struct {
			Granted              []string `json:"granted"`
			ConfirmedDeltaDigest string   `json:"confirmedDeltaDigest"`
		} `json:"permissionGrant"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.ArtifactID) || p.Version == "" ||
		len(p.PermissionGrant.Granted) < 1 || len(p.PermissionGrant.Granted) > 64 ||
		!validLowerHexDigest(p.PermissionGrant.ConfirmedDeltaDigest) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "extension.install 参数无效", false)
	}
	if e.m6ext == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "扩展服务暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	result, err := e.m6ext.Install(ctx, r.IdempotencyKey, agentRunMutationActor, m6Subject, "personal",
		p.ArtifactID, p.Version,
		extension.GrantDecision{Granted: p.PermissionGrant.Granted, ConfirmedDeltaDigest: p.PermissionGrant.ConfirmedDeltaDigest})
	if err != nil {
		return m6ExtensionFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		InstallID    string `json:"installId"`
		State        string `json:"state"`
		AuditEventID string `json:"auditEventId"`
	}{result.InstallID, result.State, result.AuditEventID})
}

func handleExtensionLifecycle(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		InstallID     string `json:"installId"`
		Op            string `json:"op"`
		TargetVersion string `json:"targetVersion"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.InstallID) || !m6supply.ValidLifecycleOp(p.Op) ||
		((p.Op == "upgrade" || p.Op == "rollback") && (p.TargetVersion == "" || len(p.TargetVersion) > 64)) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "extension.lifecycle 参数无效", false)
	}
	if e.m6ext == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "扩展服务暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	result, err := e.m6ext.Lifecycle(ctx, r.IdempotencyKey, agentRunMutationActor, p.InstallID, p.Op, p.TargetVersion)
	if err != nil {
		return m6ExtensionFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		InstallID    string `json:"installId"`
		State        string `json:"state"`
		AuditEventID string `json:"auditEventId"`
	}{result.InstallID, result.State, result.AuditEventID})
}

func handleMcp6Register(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		Endpoint struct {
			Transport string   `json:"transport"`
			URL       string   `json:"url"`
			AuthRef   string   `json:"authRef"`
			Command   string   `json:"command"`
			Args      []string `json:"args"`
		} `json:"endpoint"`
		CapabilityPin struct {
			ServerIdentityDigest string            `json:"serverIdentityDigest"`
			ToolSchemaDigests    map[string]string `json:"toolSchemaDigests"`
		} `json:"capabilityPin"`
	}
	if decodePayload(r.Payload, &p) != nil || p.Endpoint.Transport == "" ||
		len(p.CapabilityPin.ToolSchemaDigests) < 1 || len(p.CapabilityPin.ToolSchemaDigests) > 256 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mcp6.register 参数无效", false)
	}
	// per-transport shape: https needs url+authRef, stdio needs command+args.
	switch p.Endpoint.Transport {
	case "https":
		if p.Endpoint.URL == "" || p.Endpoint.AuthRef == "" {
			return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "https 端点需要 url 与 authRef", false)
		}
	case "stdio":
		if p.Endpoint.Command == "" || len(p.Endpoint.Args) == 0 || len(p.Endpoint.Args) > 16 {
			return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "stdio 端点需要 command 与 1-16 个 args", false)
		}
	default:
		return bridge.Failure(r.ID, r.TraceID, mcp6.CodeStdioDisabled, "传输仅支持 https 或 stdio", false)
	}
	if e.mcp6Registry == nil {
		return bridge.Failure(r.ID, r.TraceID, "FEATURE_DISABLED", "MCP 网关尚未启用", false)
	}
	endpoint, err := e.mcp6Registry.Register(ctx, mcp6.EndpointInput{
		Transport: p.Endpoint.Transport,
		URL:       p.Endpoint.URL,
		AuthRef:   p.Endpoint.AuthRef,
		Command:   p.Endpoint.Command,
		Args:      p.Endpoint.Args,
		Pin: mcp6.CapabilityPin{ServerIdentityDigest: p.CapabilityPin.ServerIdentityDigest,
			ToolSchemaDigests: p.CapabilityPin.ToolSchemaDigests},
	})
	if err != nil {
		resp := m6McpFailure(r, err)
		// A degraded registration is still a durable registration: persist
		// it before surfacing M6-MCP-001.
		if errors.Is(err, mcp6.ErrHealthCheckFailed) && endpoint != nil && e.mcp6Endpoints != nil {
			_ = e.mcp6Endpoints.PersistRegister(ctx, endpoint)
		}
		return resp
	}
	if e.mcp6Endpoints != nil {
		if err := e.mcp6Endpoints.PersistRegister(ctx, endpoint); err != nil {
			return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "端点持久化失败", true)
		}
	}
	return bridge.Success(r.ID, struct {
		EndpointID string `json:"endpointId"`
		State      string `json:"state"`
	}{endpoint.ID, endpoint.State})
}

func handleMcp6Invoke(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		EndpointID     string         `json:"endpointId"`
		Tool           string         `json:"tool"`
		Args           map[string]any `json:"args"`
		IdempotencyKey string         `json:"idempotencyKey"`
		DeadlineMS     int64          `json:"deadlineMs"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.EndpointID) || len(p.Tool) < 1 || len(p.Tool) > 128 ||
		p.Args == nil || len(p.Args) > 64 || p.IdempotencyKey == "" || len(p.IdempotencyKey) > 256 ||
		p.DeadlineMS < 100 || p.DeadlineMS > 30000 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mcp6.invoke 参数无效", false)
	}
	if e.mcp6Registry == nil {
		return bridge.Failure(r.ID, r.TraceID, "FEATURE_DISABLED", "MCP 网关尚未启用", false)
	}
	invokeCtx, cancel := context.WithTimeout(ctx, time.Duration(p.DeadlineMS)*time.Millisecond)
	defer cancel()
	result, err := e.mcp6Registry.Invoke(invokeCtx, p.EndpointID, p.Tool, p.Args)
	if err != nil {
		// Mirror lifecycle changes (drift/revocation) durably; best effort —
		// the in-memory registry stays authoritative for the response.
		if e.mcp6Endpoints != nil {
			if errors.Is(err, mcp6.ErrCapabilityDrift) {
				_ = e.mcp6Endpoints.PersistState(ctx, p.EndpointID, mcp6.StateDegraded)
			} else if errors.Is(err, mcp6.ErrCredentialRevoked) {
				_ = e.mcp6Endpoints.PersistState(ctx, p.EndpointID, mcp6.StateRevoked)
			}
		}
		return m6McpFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		Result     map[string]any `json:"result"`
		Bytes      int            `json:"bytes"`
		DurationMS int64          `json:"durationMs"`
		TraceID    string         `json:"traceId"`
	}{result.Result, result.Bytes, result.DurationMS, result.TraceID})
}

func handleMcp6Revoke(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		EndpointID string `json:"endpointId"`
		Reason     string `json:"reason"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.EndpointID) ||
		(p.Reason != "credential" && p.Reason != "drift" && p.Reason != "policy" && p.Reason != "manual") {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mcp6.revoke 参数无效", false)
	}
	if e.mcp6Registry == nil {
		return bridge.Failure(r.ID, r.TraceID, "FEATURE_DISABLED", "MCP 网关尚未启用", false)
	}
	endpoint, err := e.mcp6Registry.Revoke(p.EndpointID, p.Reason)
	if err != nil {
		return m6McpFailure(r, err)
	}
	if e.mcp6Endpoints != nil {
		if err := e.mcp6Endpoints.PersistState(ctx, p.EndpointID, mcp6.StateRevoked); err != nil {
			return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "端点状态持久化失败", true)
		}
	}
	return bridge.Success(r.ID, struct {
		EndpointID   string `json:"endpointId"`
		State        string `json:"state"`
		PoolsCleared bool   `json:"poolsCleared"`
	}{endpoint.ID, endpoint.State, true})
}

// handleMcp6PresetsList answers the curated free-official-server catalog
// (task c3-mcp). The catalog is static and validated at test time against
// the unchanged m7flow stdio whitelist, so no runtime gate is needed; a
// preset registration still flows through mcp.add / mcp6.register and their
// fail-closed admission.
func handleMcp6PresetsList(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct{}
	if decodePayload(r.Payload, &p) != nil {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "mcp6.presets.list 参数无效", false)
	}
	items := mcp6.Presets()
	for i := range items {
		if !items[i].NeedsArgs {
			continue
		}
		items[i].ArgDefault = mcp6.PrepareSandbox(items[i].ID)
		if items[i].ID == "git" {
			initGitSandbox(items[i].ArgDefault)
		}
	}
	return bridge.Success(r.ID, struct {
		Items []mcp6.Preset `json:"items"`
	}{items})
}

func initGitSandbox(dir string) {
	if dir == "" {
		return
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = filepath.FromSlash(dir)
	_ = cmd.Run()
}

// m6ExtensionFailure maps extension-service errors onto wire codes.
func m6ExtensionFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, m6app.ErrArtifactNotFound):
		return bridge.Failure(r.ID, r.TraceID, "EXTENSION_ARTIFACT_NOT_FOUND", "扩展制品不存在", false)
	case errors.Is(err, m6app.ErrVersionMismatch):
		return bridge.Failure(r.ID, r.TraceID, "EXTENSION_VERSION_MISMATCH", "制品版本不一致", false)
	case errors.Is(err, m6app.ErrArtifactUnverified):
		return bridge.Failure(r.ID, r.TraceID, "M6-EXT-002", "制品签名状态不可安装", false)
	case errors.Is(err, m6app.ErrInstallNotFound):
		return bridge.Failure(r.ID, r.TraceID, "EXTENSION_INSTALL_NOT_FOUND", "安装记录不存在", false)
	case errors.Is(err, m6app.ErrBadLifecycleOp), errors.Is(err, m6app.ErrTargetRequired):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "生命周期操作对当前状态无效", false)
	case errors.Is(err, m6app.ErrIdempotencyConflict):
		return bridge.Failure(r.ID, r.TraceID, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求", false)
	case errors.Is(err, m6supply.ErrVersionConflict):
		return bridge.Failure(r.ID, r.TraceID, "EXTENSION_VERSION_CONFLICT", "安装记录版本已变化，请重试", false)
	case errors.Is(err, m6app.ErrSubjectRequired), errors.Is(err, m6app.ErrServiceUnavailable):
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "扩展服务暂时不可用", true)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "扩展服务暂时不可用", true)
	}
}

// m6McpFailure maps mcp6 registry errors onto the frozen M6-MCP-00x codes.
func m6McpFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, mcp6.ErrStdioDisabled):
		return bridge.Failure(r.ID, r.TraceID, mcp6.CodeStdioDisabled, "stdio 端点被拒绝：命令不在白名单或参数含非法字符", false)
	case errors.Is(err, mcp6.ErrEndpointNotFound):
		return bridge.Failure(r.ID, r.TraceID, "MCP6_ENDPOINT_NOT_FOUND", "端点不存在", false)
	case errors.Is(err, mcp6.ErrEndpointRevoked):
		return bridge.Failure(r.ID, r.TraceID, "MCP6_ENDPOINT_REVOKED", "端点已撤销", false)
	case errors.Is(err, mcp6.ErrCredentialRevoked):
		return bridge.Failure(r.ID, r.TraceID, mcp6.CodeCredentialRevoked, "上游拒绝凭据，端点已撤销并清空连接池", false)
	case errors.Is(err, mcp6.ErrCapabilityDrift):
		return bridge.Failure(r.ID, r.TraceID, mcp6.CodeCapabilityDrift, "能力固定摘要漂移，授权已失效", false)
	case errors.Is(err, mcp6.ErrCircuitOpen):
		return bridge.Failure(r.ID, r.TraceID, mcp6.CodeCircuitOpen, "熔断窗口开启，请稍后重试", true)
	case errors.Is(err, mcp6.ErrNotReady), errors.Is(err, mcp6.ErrHealthCheckFailed):
		return bridge.Failure(r.ID, r.TraceID, mcp6.CodeHealthCheckFailed, "端点未就绪或健康检查失败", false)
	case errors.Is(err, mcp6.ErrTransport):
		return bridge.Failure(r.ID, r.TraceID, mcp6.CodeCircuitOpen, "上游传输失败，已计入熔断", true)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "MCP 网关暂时不可用", true)
	}
}

// M6 slice-2 handlers (T-6.2.x): connector.snapshot and worker.dispatch.
//
// Error mapping follows the M6_ERROR_CATALOG_V2 method x error matrix:
// connector.snapshot maps metadataScope violations to M6-DB-002 (the exact
// parameter-column code) and upstream fetch failures to M6-HLT-001;
// worker.dispatch maps capability-token failures to M6-DLG-001 (envelope not
// authorized) and storage faults to STORAGE_UNAVAILABLE. Same-key dispatch
// replays return the original task (M6-TSK-001 same-key-same-digest
// semantics) without creating a new worker.

// handleConnectorSnapshot validates the connector identifier and the frozen
// metadata scope enum, then runs the catalog snapshot use case inside the
// single-writer transaction (per-connector monotonic snapshot_version).
func handleConnectorSnapshot(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		ConnectorID   string `json:"connectorId"`
		MetadataScope string `json:"metadataScope"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.ConnectorID) < 1 || len(p.ConnectorID) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "connector.snapshot 参数无效", false)
	}
	if !connector.ConnectorIDPattern.MatchString(p.ConnectorID) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "connector.snapshot 参数无效", false)
	}
	if !connector.MetadataScopes[p.MetadataScope] {
		return bridge.Failure(r.ID, r.TraceID, "M6-DB-002", "元数据范围越界，请缩小范围", false)
	}
	if e.m6catalog == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "连接器目录暂时不可用", true)
	}
	snapshot, err := e.m6catalog.Snapshot(ctx, p.ConnectorID, p.MetadataScope)
	if err != nil {
		switch {
		case errors.Is(err, m6app.ErrScopeDenied):
			return bridge.Failure(r.ID, r.TraceID, "M6-DB-002", "元数据范围越界，请缩小范围", false)
		case errors.Is(err, m6app.ErrConnectorIDBad):
			return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "connector.snapshot 参数无效", false)
		default:
			// Upstream fetch failed: the credential may be gone (M6-CRD-001)
			// or the integration unhealthy (M6-HLT-001); without probing we
			// report the integration as unavailable and retryable.
			return bridge.Failure(r.ID, r.TraceID, "M6-HLT-001", "该集成已暂停或不可用", true)
		}
	}
	var objects map[string]any
	if err := json.Unmarshal([]byte(snapshot.ObjectsJSON), &objects); err != nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "快照产物损坏", true)
	}
	if objects == nil {
		objects = map[string]any{}
	}
	return bridge.Success(r.ID, struct {
		SnapshotVersion int64          `json:"snapshotVersion"`
		Objects         map[string]any `json:"objects"`
		FetchedAt       string         `json:"fetchedAt"`
	}{snapshot.SnapshotVersion, objects, snapshot.FetchedAt.Format(time.RFC3339Nano)})
}

// handleWorkerDispatch verifies the capability token, acquires the fencing
// lease and persists the cloud task (state leased). Replays with the same
// (jobSpecDigest, budgetLeaseId) return the original worker/task pair.
func handleWorkerDispatch(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		JobSpecDigest   string `json:"jobSpecDigest"`
		CapabilityToken string `json:"capabilityToken"`
		BudgetLeaseID   string `json:"budgetLeaseId"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.JobSpecDigest) != 64 || !validLowerHexDigest(p.JobSpecDigest) ||
		p.CapabilityToken == "" || len(p.CapabilityToken) > 4096 ||
		p.BudgetLeaseID == "" || len(p.BudgetLeaseID) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "worker.dispatch 参数无效", false)
	}
	if e.m6dispatch == nil {
		return bridge.Failure(r.ID, r.TraceID, "FEATURE_DISABLED", "Worker 运行时尚未启用", false)
	}
	result, err := e.m6dispatch.Dispatch(ctx, p.JobSpecDigest, p.CapabilityToken, p.BudgetLeaseID)
	if err != nil {
		switch {
		case errors.Is(err, m6app.ErrWorkerNotVerified):
			return bridge.Failure(r.ID, r.TraceID, "M6-DLG-001", "委派信封校验失败，已拒绝", false)
		case errors.Is(err, m6app.ErrTaskExists):
			return bridge.Failure(r.ID, r.TraceID, "M6-TSK-001", "任务重复提交，已返回原结果", false)
		default:
			return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "Worker 调度暂时不可用", true)
		}
	}
	if result.WorktreeRef == "" {
		result.WorktreeRef = "pending"
	}
	return bridge.Success(r.ID, struct {
		WorkerID    string `json:"workerId"`
		TaskID      string `json:"taskId"`
		WorktreeRef string `json:"worktreeRef"`
	}{result.WorkerID, result.TaskID, result.WorktreeRef})
}

// M6 slice-3 handlers (T-6.3.x): delegation.create / delegation.settle /
// barrier.arrive.
//
// Error mapping follows the M6_ERROR_CATALOG_V2 method x error matrix:
//
//	delegation.create  DLG-001 envelope chain failed, DLG-002 depth/fan-out/
//	                   tree caps, BGT-001 budget cap refused (nothing frozen)
//	delegation.settle  JOIN-001 late/duplicate arrival, BGT-001 usage over
//	                   the reservation, BGT-002 conservation drift (rolled
//	                   back, tree frozen)
//	barrier.arrive     duplicate arrivals answer the existing settlement
//	                   (alreadySettled=true) per JOIN-001
//
// delegation.settle replays answer the original settlement through the
// idempotency record, so a retry after a network blip never double-consumes
// the budget.

// handleDelegationCreate validates the wire shape, then runs the governed
// fan-out: caps check -> envelope sign + static verify -> budget reserve ->
// durable delegation row, all in one single-writer transaction.
func handleDelegationCreate(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RootID        string   `json:"rootId"`
		ParentID      string   `json:"parentId"`
		Objective     string   `json:"objective"`
		InputDigests  []string `json:"inputDigests"`
		CapabilitySet []string `json:"capabilitySet"`
		BudgetGrant   struct {
			CPUSeconds  int64 `json:"cpuSeconds"`
			Tokens      int64 `json:"tokens"`
			Cost        int64 `json:"cost"`
			WallClockMs int64 `json:"wallClockMs"`
		} `json:"budgetGrant"`
		DeadlineMS int64 `json:"deadlineMs"`
		Depth      int   `json:"depth"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RootID) ||
		p.ParentID == "" || len(p.ParentID) > 256 ||
		p.Objective == "" || len(p.Objective) > 8192 ||
		len(p.InputDigests) == 0 || len(p.InputDigests) > 64 ||
		len(p.CapabilitySet) == 0 || len(p.CapabilitySet) > 64 ||
		p.DeadlineMS < 1000 || p.DeadlineMS > 86400000 ||
		p.Depth < 0 || p.Depth > 16 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "delegation.create 参数无效", false)
	}
	for _, d := range p.InputDigests {
		if len(d) != 64 || !validLowerHexDigest(d) {
			return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "delegation.create 参数无效", false)
		}
	}
	for _, c := range p.CapabilitySet {
		if c == "" || len(c) > 128 {
			return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "delegation.create 参数无效", false)
		}
	}
	if e.m6delegation == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "委派服务暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	result, err := e.m6delegation.Create(ctx, r.IdempotencyKey, m6app.CreateDelegationRequest{
		RootID:        p.RootID,
		ParentID:      p.ParentID,
		Objective:     p.Objective,
		InputDigests:  p.InputDigests,
		CapabilitySet: p.CapabilitySet,
		Grant: delegation.BudgetGrant{
			CPUSeconds: p.BudgetGrant.CPUSeconds, Tokens: p.BudgetGrant.Tokens,
			Cost: p.BudgetGrant.Cost, WallClockMs: p.BudgetGrant.WallClockMs,
		},
		DeadlineMS: p.DeadlineMS,
		Depth:      p.Depth,
	})
	if err != nil {
		return m6DelegationFailure(r, err)
	}
	return bridge.Success(r.ID, struct {
		DelegationID      string `json:"delegationId"`
		EnvelopeSignature string `json:"envelopeSignature"`
	}{result.DelegationID, result.EnvelopeSignature})
}

// handleDelegationSettle validates the ResultBundle, consumes the reported
// usage, settles the child into the root's open barrier and audits
// conservation in one transaction.
func handleDelegationSettle(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		DelegationID string `json:"delegationId"`
		ResultBundle struct {
			Claims           map[string]any `json:"claims"`
			PatchDigest      string         `json:"patchDigest"`
			TestEvidenceRefs []string       `json:"testEvidenceRefs"`
			Usage            map[string]any `json:"usage"`
			ResultDigest     string         `json:"resultDigest"`
		} `json:"resultBundle"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.DelegationID) ||
		len(p.ResultBundle.PatchDigest) != 64 || !validLowerHexDigest(p.ResultBundle.PatchDigest) ||
		len(p.ResultBundle.ResultDigest) != 64 || !validLowerHexDigest(p.ResultBundle.ResultDigest) ||
		len(p.ResultBundle.Claims) == 0 || len(p.ResultBundle.TestEvidenceRefs) > 64 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "delegation.settle 参数无效", false)
	}
	for _, ref := range p.ResultBundle.TestEvidenceRefs {
		if ref == "" || len(ref) > 512 {
			return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "delegation.settle 参数无效", false)
		}
	}
	if e.m6delegation == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "委派服务暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	result, err := e.m6delegation.Settle(ctx, r.IdempotencyKey, p.DelegationID, m6app.ResultBundle{
		Claims:           p.ResultBundle.Claims,
		PatchDigest:      p.ResultBundle.PatchDigest,
		TestEvidenceRefs: p.ResultBundle.TestEvidenceRefs,
		Usage:            p.ResultBundle.Usage,
		ResultDigest:     p.ResultBundle.ResultDigest,
	})
	if err != nil {
		return m6DelegationFailure(r, err)
	}
	if result.BudgetConsumed == nil {
		result.BudgetConsumed = map[string]int64{}
	}
	return bridge.Success(r.ID, struct {
		SettledAt      string           `json:"settledAt"`
		BudgetConsumed map[string]int64 `json:"budgetConsumed"`
		BarrierState   string           `json:"barrierState"`
	}{result.SettledAt.Format(time.RFC3339Nano), result.BudgetConsumed, result.BarrierState})
}

// handleBarrierArrive settles one child outcome; duplicates answer the
// existing settlement (alreadySettled) without touching the budget twice.
func handleBarrierArrive(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		BarrierID    string `json:"barrierId"`
		ChildID      string `json:"childId"`
		Attempt      int64  `json:"attempt"`
		ResultDigest string `json:"resultDigest"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.BarrierID) ||
		p.ChildID == "" || len(p.ChildID) > 256 || p.Attempt < 0 || p.Attempt > 10 ||
		len(p.ResultDigest) != 64 || !validLowerHexDigest(p.ResultDigest) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "barrier.arrive 参数无效", false)
	}
	if e.m6barriers == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "汇合服务暂时不可用", true)
	}
	// The wire schema carries no outcome field; a direct barrier.arrive is
	// a success-shaped settlement — failure-shaped arrivals flow through
	// delegation.settle with the envelope's verdict.
	result, err := e.m6barriers.Arrive(ctx, p.BarrierID, p.ChildID, p.Attempt, "succeeded", p.ResultDigest)
	if err != nil {
		switch {
		case errors.Is(err, m6app.ErrBarrierNotFound):
			return bridge.Failure(r.ID, r.TraceID, "M6_BARRIER_NOT_FOUND", "汇合点不存在", false)
		default:
			return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "汇合服务暂时不可用", true)
		}
	}
	return bridge.Success(r.ID, struct {
		BarrierState   string `json:"barrierState"`
		AlreadySettled bool   `json:"alreadySettled"`
	}{result.State, result.AlreadySettled})
}

// m6DelegationFailure maps delegation-service errors onto wire codes.
func m6DelegationFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, m6app.ErrLimitsExceeded):
		return bridge.Failure(r.ID, r.TraceID, "M6-DLG-002", "委派深度或数量超出硬上限，已拒绝", false)
	case errors.Is(err, m6app.ErrEnvelopeRejected):
		return bridge.Failure(r.ID, r.TraceID, "M6-DLG-001", "委派信封校验失败，已拒绝", false)
	case errors.Is(err, m6app.ErrBudgetInsufficient), errors.Is(err, m6app.ErrBudgetOverconsume):
		return bridge.Failure(r.ID, r.TraceID, "M6-BGT-001", "预算不足，已拒绝划拨", false)
	case errors.Is(err, m6app.ErrBudgetDrift):
		return bridge.Failure(r.ID, r.TraceID, "M6-BGT-002", "预算账本异常，已冻结并告警", false)
	case errors.Is(err, m6app.ErrDelegationNotFound):
		return bridge.Failure(r.ID, r.TraceID, "M6_DELEGATION_NOT_FOUND", "委派记录不存在", false)
	case errors.Is(err, m6app.ErrDelegationSettled):
		return bridge.Failure(r.ID, r.TraceID, "M6-JOIN-001", "汇合已封闭或重复到达，已按既有结果处理", false)
	case errors.Is(err, m6app.ErrDelegationLate):
		return bridge.Failure(r.ID, r.TraceID, "M6-JOIN-001", "汇合已封闭或重复到达，已按既有结果处理", false)
	case errors.Is(err, m6app.ErrBundleInvalid):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "delegation 参数无效", false)
	case errors.Is(err, m6app.ErrIdempotencyConflict):
		return bridge.Failure(r.ID, r.TraceID, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求", false)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "委派服务暂时不可用", true)
	}
}

// M6 slice-4 handlers (T-6.4.x): merge.submit.
//
// Error mapping (M6_ERROR_CATALOG_V2):
//
//	merge.submit        MRG-001 stale verdicts are SUCCESS answers with
//	                    state=stale (the requeue path, not an error);
//	                    sequence slot conflicts answer M6_MERGE_SEQUENCE_
//	                    CONFLICT; fenced writers answer M6-MRG-002.

// handleMergeSubmit validates the wire shape (rootId/sequence/intent),
// then lands the intent through the total-order + fast-fail CAS walk.
func handleMergeSubmit(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RootID   string `json:"rootId"`
		Sequence int64  `json:"sequence"`
		Intent   struct {
			ChildID      string `json:"childId"`
			ExpectedHead string `json:"expectedHead"`
			PatchDigest  string `json:"patchDigest"`
			TestsRef     string `json:"testsRef"`
		} `json:"intent"`
	}
	if decodePayload(r.Payload, &p) != nil || !validCanonicalULID(p.RootID) ||
		p.Sequence < 1 || p.Sequence > 1000000 ||
		p.Intent.ChildID == "" || len(p.Intent.ChildID) > 256 ||
		p.Intent.ExpectedHead == "" || len(p.Intent.ExpectedHead) > 256 ||
		!validLowerHexDigest(p.Intent.PatchDigest) ||
		p.Intent.TestsRef == "" || len(p.Intent.TestsRef) > 512 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "merge.submit 参数无效", false)
	}
	if e.m6merge == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "合并服务暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	result, err := e.m6merge.Submit(ctx, r.IdempotencyKey, m6app.SubmitMergeRequest{
		RootID: p.RootID, Sequence: p.Sequence,
		ChildID: p.Intent.ChildID, ExpectedHead: p.Intent.ExpectedHead,
		PatchDigest: p.Intent.PatchDigest, TestsRef: p.Intent.TestsRef,
	})
	if err != nil {
		return m6MergeFailure(r, err)
	}
	return bridge.Success(r.ID, result)
}

// m6MergeFailure maps merge service errors onto the wire.
func m6MergeFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, m6app.ErrIdempotencyConflict):
		return bridge.Failure(r.ID, r.TraceID, "IDEMPOTENCY_CONFLICT", "幂等键已用于不同请求", false)
	case errors.Is(err, m6app.ErrMergeSequenceConflict):
		return bridge.Failure(r.ID, r.TraceID, "M6_MERGE_SEQUENCE_CONFLICT", "合并序号已被占用（全序槽位冲突）", false)
	case errors.Is(err, merge.ErrWriterFenced):
		return bridge.Failure(r.ID, r.TraceID, "M6-MRG-002", "写入者已被围栏（旧纪元拒绝写入）", false)
	case errors.Is(err, m6app.ErrPatchUnavailable):
		return bridge.Failure(r.ID, r.TraceID, "M6_PATCH_UNAVAILABLE", "补丁不可用（摘要不匹配或工作树缺失）", false)
	case errors.Is(err, m6app.ErrApplyFailed):
		return bridge.Failure(r.ID, r.TraceID, "M6_MERGE_APPLY_FAILED", "补丁未能应用到最终树", false)
	case errors.Is(err, m6app.ErrMergeNotFound):
		return bridge.Failure(r.ID, r.TraceID, "M6_MERGE_NOT_FOUND", "合并意图不存在", false)
	case errors.Is(err, m6app.ErrMergeServiceUnavailable):
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "合并服务暂时不可用", true)
	default:
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "合并服务暂时不可用", true)
	}
}
