package app

import (
	"context"
	"errors"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

// M7 slice-7 handlers (T-7.7.x): the seven gap tools. P1 read tools
// (http.request / db.query / document.parse) and the P2 write-effect group
// (http.download / archive.pack / archive.unpack / git.read). Workspace
// effects resolve the client-supplied absolute root through the shared
// canonicalWorkspaceRoot fence before any confinement check.
//
// Error mapping: SSRF contract -> M7-TOOL-001 (422), response over cap ->
// M7-TOOL-002 (413), SQL whitelist -> M7-TOOL-003 (422), connection
// refusal -> M7-TOOL-004 (403), parse guards -> M7-TOOL-005 (422),
// timeout -> M7-TOOL-006 (504).

// m7WorkspaceRoot resolves the toolgap workspace fence; empty means the
// call needs no workspace confinement.
func m7WorkspaceRoot(raw string) (string, bool) {
	if raw == "" {
		return "", true
	}
	root, err := canonicalWorkspaceRoot(raw)
	if err != nil {
		return "", false
	}
	return root, true
}

func handleHttpRequest(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunIDStr         string            `json:"runId"`
		Method           string            `json:"method"`
		URL              string            `json:"url"`
		Headers          map[string]string `json:"headers"`
		Body             string            `json:"body"`
		TimeoutMS        int64             `json:"timeoutMs"`
		MaxResponseBytes int64             `json:"maxResponseBytes"`
		Reviewed         bool              `json:"reviewed"`
		RequestID        string            `json:"requestId"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.RunIDStr) < 1 || len(p.RunIDStr) > 128 ||
		len(p.URL) < 8 || len(p.URL) > 2048 || p.TimeoutMS < 1 || p.MaxResponseBytes < 1 ||
		len(p.RequestID) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "http.request 参数无效", false)
	}
	if e.m7toolgap == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "工具运行时暂时不可用", true)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	res, err := e.m7toolgap.HTTPRequest(ctx, m7app.HTTPRequestInput{
		RunID:            p.RunIDStr,
		Method:           p.Method,
		URL:              p.URL,
		Headers:          p.Headers,
		Body:             p.Body,
		TimeoutMS:        p.TimeoutMS,
		MaxResponseBytes: p.MaxResponseBytes,
		Reviewed:         p.Reviewed,
		IdempotencyKey:   p.RequestID,
	})
	if err != nil {
		return m7ToolgapFailure(r, err, "http.request")
	}
	return bridge.Success(r.ID, struct {
		Status     int    `json:"status"`
		Body       string `json:"body,omitempty"`
		BodyDigest string `json:"bodyDigest"`
		Bytes      int64  `json:"bytes"`
		Truncated  bool   `json:"truncated"`
	}{res.Status, res.Body, res.BodyDigest, res.Bytes, res.Truncated})
}

func handleDbQuery(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID       string   `json:"runId"`
		Target      string   `json:"target"`
		SQL         string   `json:"sql"`
		Params      []any    `json:"params"`
		MaxRows     int64    `json:"maxRows"`
		TimeoutMS   int64    `json:"timeoutMs"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.RunID) < 1 || len(p.RunID) > 128 ||
		len(p.SQL) < 1 || len(p.SQL) > 16384 || p.MaxRows < 1 || p.TimeoutMS < 1 ||
		len(p.Target) > 512 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "db.query 参数无效", false)
	}
	if e.m7toolgap == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "工具运行时暂时不可用", true)
	}
	// target: "sqlite" keeps the legacy default file, "external:<id>" is the
	// registered connection, any other value is a workspace-relative sqlite
	// file path.
	connID := ""
	sqlitePath := p.Target
	if len(p.Target) > 9 && p.Target[:9] == "external:" {
		connID = p.Target[9:]
		sqlitePath = ""
	}
	res, err := e.m7toolgap.DBQuery(ctx, m7app.DBQueryInput{
		RunID:      p.RunID,
		ConnID:     connID,
		SQLitePath: sqlitePath,
		SQL:        p.SQL,
		Params:     p.Params,
		MaxRows:    p.MaxRows,
		TimeoutMS:  p.TimeoutMS,
	})
	if err != nil {
		return m7ToolgapFailure(r, err, "db.query")
	}
	rows := res.Rows
	if rows == nil {
		rows = [][]any{}
	}
	cols := res.Columns
	if cols == nil {
		cols = []string{}
	}
	return bridge.Success(r.ID, struct {
		Columns      []string `json:"columns"`
		Rows         [][]any  `json:"rows"`
		RowCount     int      `json:"rowCount"`
		Truncated    bool     `json:"truncated"`
		ResultDigest string   `json:"resultDigest"`
	}{cols, rows, res.RowCount, res.Truncated, res.ResultDigest})
}

func handleDocumentParse(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID          string `json:"runId"`
		FileRef        string `json:"fileRef"`
		WorkspaceRoot  string `json:"workspaceRoot"`
		Format         string `json:"format"`
		PageRange      string `json:"pageRange"`
		MaxOutputBytes int64  `json:"maxOutputBytes"`
		ExpectedDigest string `json:"expectedDigest"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.RunID) < 1 || len(p.RunID) > 128 ||
		len(p.FileRef) < 1 || len(p.FileRef) > 512 || len(p.PageRange) > 32 ||
		p.MaxOutputBytes < 1 || (p.ExpectedDigest != "" && len(p.ExpectedDigest) != 64) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "document.parse 参数无效", false)
	}
	if e.m7toolgap == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "工具运行时暂时不可用", true)
	}
	root, ok := m7WorkspaceRoot(p.WorkspaceRoot)
	if !ok {
		return bridge.Failure(r.ID, r.TraceID, "WORKSPACE_ROOT_INVALID", "工作区根必须是存在的本地目录绝对路径", false)
	}
	path := p.FileRef
	if root != "" {
		confined, cerr := m7flow.ToolConfinePath(root, p.FileRef)
		if cerr != nil {
			return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "fileRef 越界工作区", false)
		}
		path = confined
	}
	res, err := e.m7toolgap.DocumentParse(ctx, m7app.DocumentParseInput{
		RunID:          p.RunID,
		Path:           path,
		Format:         p.Format,
		PageRange:      p.PageRange,
		MaxOutputBytes: p.MaxOutputBytes,
		ExpectedDigest: p.ExpectedDigest,
	})
	if err != nil {
		return m7ToolgapFailure(r, err, "document.parse")
	}
	return bridge.Success(r.ID, struct {
		PageCount    int               `json:"pageCount"`
		Blocks       []m7ParseBlockDTO `json:"blocks"`
		OutputDigest string            `json:"outputDigest"`
		Truncated    bool              `json:"truncated"`
	}{res.PageCount, m7ParseBlocks(res.Blocks), res.OutputDigest, res.Truncated})
}

func handleHttpDownload(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID         string `json:"runId"`
		URL           string `json:"url"`
		DestPath      string `json:"destPath"`
		WorkspaceRoot string `json:"workspaceRoot"`
		ExpectedSha   string `json:"expectedSha256"`
		MaxBytes      int64  `json:"maxBytes"`
		RequestID     string `json:"requestId"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.RunID) < 1 || len(p.RunID) > 128 ||
		len(p.URL) < 8 || len(p.URL) > 2048 || len(p.DestPath) < 1 || len(p.DestPath) > 512 ||
		p.MaxBytes < 1 || len(p.RequestID) < 1 || len(p.RequestID) > 128 ||
		(p.ExpectedSha != "" && len(p.ExpectedSha) != 64) {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "http.download 参数无效", false)
	}
	if e.m7toolgap == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "工具运行时暂时不可用", true)
	}
	root, ok := m7WorkspaceRoot(p.WorkspaceRoot)
	if !ok || root == "" {
		return bridge.Failure(r.ID, r.TraceID, "WORKSPACE_ROOT_INVALID", "http.download 需要有效工作区根", false)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	res, err := e.m7toolgap.Download(ctx, m7app.DownloadInput{
		RunID:          p.RunID,
		URL:            p.URL,
		DestPath:       p.DestPath,
		WorkspaceRoot:  root,
		ExpectedSHA256: p.ExpectedSha,
		MaxBytes:       p.MaxBytes,
		IdempotencyKey: p.RequestID,
	})
	if err != nil {
		return m7ToolgapFailure(r, err, "http.download")
	}
	return bridge.Success(r.ID, struct {
		TaskID string `json:"taskId"`
		State  string `json:"state"`
		SHA256 string `json:"sha256"`
		Bytes  int64  `json:"bytes"`
	}{p.RequestID, "completed", res.SHA256, res.Bytes})
}

func handleArchivePack(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID         string   `json:"runId"`
		Sources       []string `json:"sources"`
		DestPath      string   `json:"destPath"`
		WorkspaceRoot string   `json:"workspaceRoot"`
		Format        string   `json:"format"`
		MaxEntries    int64    `json:"maxEntries"`
		MaxBytes      int64    `json:"maxBytes"`
		RequestID     string   `json:"requestId"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.RunID) < 1 || len(p.RunID) > 128 ||
		len(p.Sources) < 1 || len(p.Sources) > 1000 || len(p.DestPath) < 1 || len(p.DestPath) > 512 ||
		(p.MaxEntries < 1) || (p.MaxBytes < 1) || len(p.RequestID) < 1 || len(p.RequestID) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "archive.pack 参数无效", false)
	}
	if e.m7toolgap == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "工具运行时暂时不可用", true)
	}
	root, ok := m7WorkspaceRoot(p.WorkspaceRoot)
	if !ok || root == "" {
		return bridge.Failure(r.ID, r.TraceID, "WORKSPACE_ROOT_INVALID", "archive.pack 需要有效工作区根", false)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	res, err := e.m7toolgap.Pack(ctx, m7app.ArchivePackInput{
		RunID:          p.RunID,
		Sources:        p.Sources,
		DestPath:       p.DestPath,
		WorkspaceRoot:  root,
		Format:         p.Format,
		MaxEntries:     p.MaxEntries,
		MaxBytes:       p.MaxBytes,
		IdempotencyKey: p.RequestID,
	})
	if err != nil {
		return m7ToolgapFailure(r, err, "archive.pack")
	}
	return bridge.Success(r.ID, struct {
		ArchivePath string `json:"archivePath"`
		EntryCount  int    `json:"entryCount"`
		SHA256      string `json:"sha256"`
		Bytes       int64  `json:"bytes"`
	}{res.ArchivePath, res.EntryCount, res.SHA256, res.Bytes})
}

func handleArchiveUnpack(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID         string `json:"runId"`
		ArchivePath   string `json:"archivePath"`
		DestDir       string `json:"destDir"`
		WorkspaceRoot string `json:"workspaceRoot"`
		MaxEntries    int64  `json:"maxEntries"`
		MaxBytes      int64  `json:"maxBytes"`
		RequestID     string `json:"requestId"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.RunID) < 1 || len(p.RunID) > 128 ||
		len(p.ArchivePath) < 1 || len(p.ArchivePath) > 512 || len(p.DestDir) < 1 || len(p.DestDir) > 512 ||
		p.MaxEntries < 1 || p.MaxBytes < 1 || len(p.RequestID) < 1 || len(p.RequestID) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "archive.unpack 参数无效", false)
	}
	if e.m7toolgap == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "工具运行时暂时不可用", true)
	}
	root, ok := m7WorkspaceRoot(p.WorkspaceRoot)
	if !ok || root == "" {
		return bridge.Failure(r.ID, r.TraceID, "WORKSPACE_ROOT_INVALID", "archive.unpack 需要有效工作区根", false)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	res, err := e.m7toolgap.Unpack(ctx, m7app.ArchiveUnpackInput{
		RunID:          p.RunID,
		ArchivePath:    p.ArchivePath,
		DestDir:        p.DestDir,
		WorkspaceRoot:  root,
		MaxEntries:     p.MaxEntries,
		MaxBytes:       p.MaxBytes,
		IdempotencyKey: p.RequestID,
	})
	if err != nil {
		return m7ToolgapFailure(r, err, "archive.unpack")
	}
	return bridge.Success(r.ID, struct {
		EntryCount  int      `json:"entryCount"`
		TotalBytes  int64    `json:"totalBytes"`
		DestDir     string   `json:"destDir,omitempty"`
		Rejected    []string `json:"rejectedEntries,omitempty"`
	}{res.EntryCount, res.TotalBytes, res.DestDir, nil})
}

func handleGitRead(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	var p struct {
		RunID          string `json:"runId"`
		RepoPath       string `json:"repoPath"`
		WorkspaceRoot  string `json:"workspaceRoot"`
		Op             string `json:"op"`
		Ref            string `json:"ref"`
		MaxOutputBytes int64  `json:"maxOutputBytes"`
		RequestID      string `json:"requestId"`
	}
	if decodePayload(r.Payload, &p) != nil || len(p.RunID) < 1 || len(p.RunID) > 128 ||
		len(p.RepoPath) < 1 || len(p.RepoPath) > 512 || len(p.Ref) > 256 ||
		p.MaxOutputBytes < 1 || len(p.RequestID) < 1 || len(p.RequestID) > 128 {
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "git.read 参数无效", false)
	}
	if e.m7toolgap == nil {
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "工具运行时暂时不可用", true)
	}
	root, ok := m7WorkspaceRoot(p.WorkspaceRoot)
	if !ok || root == "" {
		return bridge.Failure(r.ID, r.TraceID, "WORKSPACE_ROOT_INVALID", "git.read 需要有效工作区根", false)
	}
	if failure := requireIdempotency(r); failure != nil {
		return *failure
	}
	res, err := e.m7toolgap.GitRead(ctx, m7app.GitReadInput{
		RunID:           p.RunID,
		RepoPath:        p.RepoPath,
		WorkspaceRoot:   root,
		Op:              p.Op,
		Ref:             p.Ref,
		MaxOutputBytes:  p.MaxOutputBytes,
		IdempotencyKey:  p.RequestID,
	})
	if err != nil {
		return m7ToolgapFailure(r, err, "git.read")
	}
	return bridge.Success(r.ID, struct {
		Output       string `json:"output"`
		OutputDigest string `json:"outputDigest"`
		Bytes        int64  `json:"bytes"`
	}{res.Output, res.Digest, res.Bytes})
}

// m7ParseBlockDTO is the wire form of one parsed block (digest computed at
// the wire boundary since the parser port returns provenance blocks only).
type m7ParseBlockDTO struct {
	Kind   string `json:"kind"`
	Page   int    `json:"page,omitempty"`
	Text   string `json:"text"`
	Digest string `json:"digest"`
}

func m7ParseBlocks(blocks []m7flow.ParseBlock) []m7ParseBlockDTO {
	out := make([]m7ParseBlockDTO, 0, len(blocks))
	for _, b := range blocks {
		out = append(out, m7ParseBlockDTO{
			Kind: b.Kind, Page: b.Page, Text: b.Text,
			Digest: m7flow.SHA256Hex([]byte(b.Text)),
		})
	}
	return out
}

// m7ToolgapFailure maps m7app slice-7 errors onto the M7 wire family.
func m7ToolgapFailure(r bridge.Request, err error, method string) bridge.Response {
	switch {
	case errors.Is(err, m7app.ErrToolSSRF):
		return bridge.Failure(r.ID, r.TraceID, "M7-TOOL-001", "SSRF 合同拒绝该网络目标", false)
	case errors.Is(err, m7app.ErrToolResponseOverLimit):
		return bridge.Failure(r.ID, r.TraceID, "M7-TOOL-002", "响应超过 maxResponseBytes 已截断", false)
	case errors.Is(err, m7app.ErrToolSQL):
		return bridge.Failure(r.ID, r.TraceID, "M7-TOOL-003", "语句未过只读白名单解析器", false)
	case errors.Is(err, m7app.ErrToolDBConn):
		return bridge.Failure(r.ID, r.TraceID, "M7-TOOL-004", "外部连接未登记或只读探针失败", false)
	case errors.Is(err, m7app.ErrToolParse):
		return bridge.Failure(r.ID, r.TraceID, "M7-TOOL-005", "格式不支持、摘要不匹配或输出超限", false)
	case errors.Is(err, m7app.ErrToolTimeout):
		return bridge.Failure(r.ID, r.TraceID, "M7-TOOL-006", "工具执行超时", true)
	case errors.Is(err, m7app.ErrToolSchema):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", method+" 参数或路径越界", false)
	case errors.Is(err, m7app.ErrToolPolicy):
		return bridge.Failure(r.ID, r.TraceID, "FORBIDDEN_BY_POLICY", method+" 写语义未过审批", false)
	case errors.Is(err, m7app.ErrToolQuota):
		return bridge.Failure(r.ID, r.TraceID, "RATE_LIMITED", "同 Run 工具配额或并发超限", true)
	case errors.Is(err, m7app.ErrToolUnreachable):
		return bridge.Failure(r.ID, r.TraceID, "UPSTREAM_UNAVAILABLE", "下游目标不可达", true)
	case errors.Is(err, m7app.ErrToolNotFound):
		return bridge.Failure(r.ID, r.TraceID, "NOT_FOUND", "引用的资源不存在", false)
	case errors.Is(err, m7app.ErrToolNotInManifest):
		return bridge.Failure(r.ID, r.TraceID, "BRIDGE_SCHEMA_INVALID", "工具不在冻结 manifest 中", false)
	case errors.Is(err, m7app.ErrServiceUnavailable):
		return bridge.Failure(r.ID, r.TraceID, "STORAGE_UNAVAILABLE", "工具运行时暂时不可用", true)
	}
	return bridge.Failure(r.ID, r.TraceID, "INTERNAL_ERROR", method+" 执行失败", false)
}
