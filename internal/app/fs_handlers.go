package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/bridge"
)

// M4-D handlers: fs.tree/stat/read/readMany/glob/grep. All six are read-only
// and authorized by a fenced workspace lease; failures map to stable bridge
// error codes so the renderer can distinguish a stale handle (re-acquire the
// lease) from a scope denial (do not retry).

func fsFailure(r bridge.Request, err error) bridge.Response {
	switch {
	case errors.Is(err, agentrunapp.ErrFsLeaseInvalid):
		return r.Fail("FS_LEASE_INVALID", "工作区租约不存在或已过期", false)
	case errors.Is(err, agentrunapp.ErrFsFencingStale):
		return r.Fail("FS_FENCING_STALE", "租约 fencing token 已失效，请重新获取租约", false)
	case errors.Is(err, agentrunapp.ErrFsScopeDenied):
		return r.Fail("FS_SCOPE_DENIED", "路径不在工作区授权范围内", false)
	case errors.Is(err, agentrunapp.ErrFsPathInvalid):
		return r.Fail("FS_PATH_INVALID", "路径或匹配模式无效", false)
	case errors.Is(err, agentrunapp.ErrFsNotFound):
		return r.Fail("FS_NOT_FOUND", "路径不存在", false)
	case errors.Is(err, agentrunapp.ErrFsNotAFile):
		return r.Fail("FS_NOT_A_FILE", "目标不是常规文件", false)
	case errors.Is(err, agentrunapp.ErrFsBinary):
		return r.Fail("FS_BINARY", "文件不是 UTF-8 文本", false)
	case errors.Is(err, agentrunapp.ErrFsTooLarge):
		return r.Fail("FS_TOO_LARGE", "文件超出可读取大小上限", false)
	default:
		return r.Fail("FS_READ_FAILED", "文件系统读取暂时不可用", true)
	}
}

// validFsLease validates the lease identity shared by all fs.* payloads.
func validFsLease(leaseID string, fencingToken int64) bool {
	return validCanonicalULID(leaseID) && fencingToken >= 1
}

func handleFsTree(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		LeaseID      string `json:"leaseId"`
		FencingToken int64  `json:"fencingToken"`
		Path         string `json:"path"`
		MaxDepth     int    `json:"maxDepth"`
		MaxEntries   int    `json:"maxEntries"`
	}
	if decodePayload(r.Payload, &p) != nil || !validFsLease(p.LeaseID, p.FencingToken) ||
		p.MaxDepth > 8 || p.MaxEntries > 2048 || p.MaxDepth < 0 || p.MaxEntries < 0 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "fs.tree 参数无效", false)
	}
	if e.agentRuns == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "工作区数据暂时不可用", true)
	}
	result, err := e.agentRuns.FsTree(ctx, p.LeaseID, p.FencingToken, p.Path, p.MaxDepth, p.MaxEntries)
	if err != nil {
		return fsFailure(r, err)
	}
	return r.Ok(result)
}

func handleFsStat(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		LeaseID      string `json:"leaseId"`
		FencingToken int64  `json:"fencingToken"`
		Path         string `json:"path"`
	}
	if decodePayload(r.Payload, &p) != nil || !validFsLease(p.LeaseID, p.FencingToken) || p.Path == "" {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "fs.stat 参数无效", false)
	}
	if e.agentRuns == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "工作区数据暂时不可用", true)
	}
	result, err := e.agentRuns.FsStat(ctx, p.LeaseID, p.FencingToken, p.Path)
	if err != nil {
		return fsFailure(r, err)
	}
	return r.Ok(result)
}

func handleFsRead(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		LeaseID      string `json:"leaseId"`
		FencingToken int64  `json:"fencingToken"`
		Path         string `json:"path"`
		MaxBytes     int    `json:"maxBytes"`
	}
	if decodePayload(r.Payload, &p) != nil || !validFsLease(p.LeaseID, p.FencingToken) ||
		p.Path == "" || p.MaxBytes < 0 || p.MaxBytes > 1048576 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "fs.read 参数无效", false)
	}
	if e.agentRuns == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "工作区数据暂时不可用", true)
	}
	result, err := e.agentRuns.FsRead(ctx, p.LeaseID, p.FencingToken, p.Path, p.MaxBytes)
	if err != nil {
		return fsFailure(r, err)
	}
	return r.Ok(result)
}

func handleFsReadMany(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		LeaseID      string   `json:"leaseId"`
		FencingToken int64    `json:"fencingToken"`
		Paths        []string `json:"paths"`
		MaxBytes     int      `json:"maxBytes"`
	}
	if decodePayload(r.Payload, &p) != nil || !validFsLease(p.LeaseID, p.FencingToken) ||
		len(p.Paths) < 1 || len(p.Paths) > 32 || p.MaxBytes < 0 || p.MaxBytes > 1048576 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "fs.readMany 参数无效", false)
	}
	if e.agentRuns == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "工作区数据暂时不可用", true)
	}
	result, err := e.agentRuns.FsReadMany(ctx, p.LeaseID, p.FencingToken, p.Paths, p.MaxBytes)
	if err != nil {
		return fsFailure(r, err)
	}
	return r.Ok(result)
}

func handleFsGlob(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		LeaseID      string `json:"leaseId"`
		FencingToken int64  `json:"fencingToken"`
		Pattern      string `json:"pattern"`
		MaxResults   int    `json:"maxResults"`
	}
	if decodePayload(r.Payload, &p) != nil || !validFsLease(p.LeaseID, p.FencingToken) ||
		p.Pattern == "" || p.MaxResults < 0 || p.MaxResults > 1024 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "fs.glob 参数无效", false)
	}
	if e.agentRuns == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "工作区数据暂时不可用", true)
	}
	result, err := e.agentRuns.FsGlob(ctx, p.LeaseID, p.FencingToken, p.Pattern, p.MaxResults)
	if err != nil {
		return fsFailure(r, err)
	}
	return r.Ok(result)
}

func handleFsGrep(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		LeaseID      string `json:"leaseId"`
		FencingToken int64  `json:"fencingToken"`
		Pattern      string `json:"pattern"`
		Path         string `json:"path"`
		MaxResults   int    `json:"maxResults"`
	}
	if decodePayload(r.Payload, &p) != nil || !validFsLease(p.LeaseID, p.FencingToken) ||
		p.Pattern == "" || len(p.Pattern) > 256 || p.MaxResults < 0 || p.MaxResults > 500 {
		return r.Fail("BRIDGE_SCHEMA_INVALID", "fs.grep 参数无效", false)
	}
	if e.agentRuns == nil {
		return r.Fail("STORAGE_UNAVAILABLE", "工作区数据暂时不可用", true)
	}
	result, err := e.agentRuns.FsGrep(ctx, p.LeaseID, p.FencingToken, p.Pattern, p.Path, p.MaxResults)
	if err != nil {
		return fsFailure(r, err)
	}
	return r.Ok(result)
}
