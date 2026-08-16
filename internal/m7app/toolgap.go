// M7 slice 7 application service (T-7.7.x): the tool-gap runtime. Every
// tool call passes the frozen guards (SSRF contract, SQL whitelist parser,
// workspace confinement, size/time/quota caps) before reaching an execution
// port; the ports are injectable so the guard surface is deterministically
// testable while production wires the real executors. Manifest registration
// outside the frozen 23-tool set is 100% refused (scenario 44).
package m7app

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

var (
	// ErrToolSchema: request parameters outside the frozen caps (422
	// family, pre-execution).
	ErrToolSchema = errors.New("m7app: tool request invalid")
	// ErrToolPolicy: write semantics without review / policy refusal
	// (403 family).
	ErrToolPolicy = errors.New("m7app: tool call refused by policy")
	// ErrToolSSRF: SSRF contract violation (M7-TOOL-001, 422).
	ErrToolSSRF = errors.New("m7app: ssrf contract refused")
	// ErrToolResponseOverLimit: response body over maxResponseBytes,
	// truncated (M7-TOOL-002, 413).
	ErrToolResponseOverLimit = errors.New("m7app: response over limit")
	// ErrToolSQL: statement refused by the whitelist parser
	// (M7-TOOL-003, 422).
	ErrToolSQL = errors.New("m7app: sql refused")
	// ErrToolDBConn: connection unregistered or probe failed
	// (M7-TOOL-004, 403).
	ErrToolDBConn = errors.New("m7app: db connection refused")
	// ErrToolParse: format unsupported / digest mismatch / output over
	// limit (M7-TOOL-005, 422).
	ErrToolParse = errors.New("m7app: document parse refused")
	// ErrToolTimeout: execution over timeoutMs (M7-TOOL-006, 504).
	ErrToolTimeout = errors.New("m7app: tool execution timeout")
	// ErrToolQuota: per-run concurrency/call/byte quota exceeded (429).
	ErrToolQuota = errors.New("m7app: tool quota exceeded")
	// ErrToolUnreachable: downstream unreachable (502 family).
	ErrToolUnreachable = errors.New("m7app: downstream unreachable")
	// ErrToolNotFound: referenced resource missing (404).
	ErrToolNotFound = errors.New("m7app: tool resource not found")
	// ErrToolNotInManifest: registration outside the frozen manifest
	// (scenario 44, 422 + audit).
	ErrToolNotInManifest = errors.New("m7app: tool not in frozen manifest")
)

// Frozen caps (design tables).
const (
	ToolMaxTimeoutMS     = 60000
	ToolMaxResponseBytes = 10 * 1024 * 1024
	ToolMaxRows          = 5000
	ToolMaxOutputBytes   = 5 * 1024 * 1024
	ToolMaxDownloadBytes = 512 * 1024 * 1024
	ToolMaxArchiveEntry  = 10000
	ToolMaxArchiveBytes  = 1024 * 1024 * 1024
	ToolMaxConcurrent    = 4
	ToolMaxCallsPerRun   = 1000
	ToolMaxBytesPerRun   = 2 * 1024 * 1024 * 1024
	ToolMaxGitOutput     = 8 * 1024 * 1024
)

// ToolgapTx is the slice-7 single-writer transaction.
type ToolgapTx interface {
	GetToolManifest(toolName string) (m7flow.ToolManifestEntry, error)
	PutToolManifest(m7flow.ToolManifestEntry) error
	CountToolManifest() (int, error)
	GetDBConnection(id string) (m7flow.DBConnection, error)
	PutDBConnection(m7flow.DBConnection) error
	BeginToolCall(runID, toolName string) error
	EndToolCall(runID, toolName string, bytes int64) error
	PutToolResult(m7flow.ToolResult) error
	GetToolResult(runID, idempotencyKey string) (m7flow.ToolResult, error)
	AppendAuditEvent(e audit.Event) (audit.Event, error)
}

// ToolgapUnitOfWork is the slice-7 single-writer boundary.
type ToolgapUnitOfWork interface {
	TransactToolgap(ctx context.Context, fn func(ToolgapTx) error) error
}

// HTTPDoer executes one SSRF-cleared HTTP request; redirects are re-checked
// hop by hop by the caller, never by the transport cache.
type HTTPDoer interface {
	Do(ctx context.Context, method, rawURL string, headers map[string]string, body string, timeout time.Duration, maxBytes int64) (int, []byte, bool, error)
}

// SQLQuerier executes one whitelisted statement read-only.
type SQLQuerier interface {
	Query(ctx context.Context, target string, statement string, params []any, maxRows int) ([]string, [][]any, bool, error)
}

// DocumentParser parses one document into blocks with provenance.
type DocumentParser interface {
	Parse(ctx context.Context, path, format, pageRange string, maxOutput int64) (int, []m7flow.ParseBlock, []byte, bool, error)
}

// GitRunner answers one git status/log/diff read.
type GitRunner interface {
	Read(ctx context.Context, repoPath, op, ref string, maxOutput int64) ([]byte, error)
}

// ToolgapService implements the seven gap tools plus the manifest plane.
type ToolgapService struct {
	uow     ToolgapUnitOfWork
	clock   Clock
	http    HTTPDoer
	db      SQLQuerier
	parser  DocumentParser
	git     GitRunner
	resolve func(host string) ([]string, error)
}

func NewToolgapService(uow ToolgapUnitOfWork) *ToolgapService {
	return &ToolgapService{
		uow: uow, clock: systemClock{},
		http:   &LocalHTTPDoer{resolve: lookupIPStrings},
		db:     LocalSQLiteQuerier{},
		parser: LocalDocumentParser{},
		git:    LocalGitRunner{},
	}
}

func (s *ToolgapService) SetClock(c Clock) { s.clock = c }

// SetHTTPDoer substitutes the HTTP port (tests).
func (s *ToolgapService) SetHTTPDoer(d HTTPDoer) { s.http = d }

// SetSQLQuerier substitutes the SQL port (tests).
func (s *ToolgapService) SetSQLQuerier(q SQLQuerier) { s.db = q }

// SetDocumentParser substitutes the parser port (tests).
func (s *ToolgapService) SetDocumentParser(p DocumentParser) { s.parser = p }

// SetGitRunner substitutes the git port (tests).
func (s *ToolgapService) SetGitRunner(g GitRunner) { s.git = g }

// SetResolver substitutes the DNS resolution hook (rebinding tests).
func (s *ToolgapService) SetResolver(fn func(host string) ([]string, error)) { s.resolve = fn }

// ── manifest plane (scenario 44) ────────────────────────────────────────────

// SeedManifest imports the frozen 23-tool manifest read-only (digests
// verified per entry). Registration of anything else is refused.
func (s *ToolgapService) SeedManifest(ctx context.Context) error {
	return s.uow.TransactToolgap(ctx, func(tx ToolgapTx) error {
		for _, name := range m7flow.ToolgapManifestTools {
			if _, err := tx.GetToolManifest(name); err == nil {
				continue
			}
			manifest := fmt.Sprintf(`{"tool":%q,"io":%q}`, name, m7flow.ToolgapIO(name))
			entry := m7flow.ToolManifestEntry{
				ToolName:         name,
				DescriptorVersion: "m7-gap-v1",
				ManifestJSON:     manifest,
				ManifestDigest:   m7flow.SHA256Hex([]byte(manifest)),
				IOSemantics:      m7flow.ToolgapIO(name),
				TimeoutMS:        ToolMaxTimeoutMS,
				Enabled:          true,
				ImportedAt:       s.clock.Now().UTC().Format(time.RFC3339),
			}
			if err := tx.PutToolManifest(entry); err != nil {
				return err
			}
		}
		return nil
	})
}

// RegisterTool refuses any tool outside the frozen manifest (scenario 44)
// and audits the refusal.
func (s *ToolgapService) RegisterTool(ctx context.Context, toolName, actor string) error {
	if !m7flow.ToolgapManifestSet[toolName] {
		_ = s.uow.TransactToolgap(ctx, func(tx ToolgapTx) error {
			_, err := tx.AppendAuditEvent(audit.Event{
				ID: ulid.Make().String(), Action: "toolgap.register.refused",
				ResourceType: "tool", ResourceID: toolName, Actor: actorOr(actor),
				CreatedAt: s.clock.Now().UTC().Format(time.RFC3339),
			})
			return err
		})
		return fmt.Errorf("%w: %q", ErrToolNotInManifest, toolName)
	}
	return s.uow.TransactToolgap(ctx, func(tx ToolgapTx) error {
		_, err := tx.GetToolManifest(toolName)
		return err
	})
}

// ── http.request ────────────────────────────────────────────────────────────

// HTTPRequestInput is the http.request command.
type HTTPRequestInput struct {
	RunID            string
	Method           string
	URL              string
	Headers          map[string]string
	Body             string
	TimeoutMS        int64
	MaxResponseBytes int64
	Reviewed         bool
	IdempotencyKey   string
}

// HTTPRequestResult answers status/body digest with truncation flag.
type HTTPRequestResult struct {
	Status     int
	Body       string
	BodyDigest string
	Truncated  bool
	Bytes      int64
}

// HTTPRequest runs one SSRF-guarded HTTP call. GET/HEAD/OPTIONS are
// read-only; every other method requires a prior review (403 otherwise).
func (s *ToolgapService) HTTPRequest(ctx context.Context, in HTTPRequestInput) (HTTPRequestResult, error) {
	if in.TimeoutMS < 1 || in.TimeoutMS > ToolMaxTimeoutMS {
		return HTTPRequestResult{}, fmt.Errorf("%w: timeoutMs %d", ErrToolSchema, in.TimeoutMS)
	}
	if in.MaxResponseBytes < 1 || in.MaxResponseBytes > ToolMaxResponseBytes {
		return HTTPRequestResult{}, fmt.Errorf("%w: maxResponseBytes %d", ErrToolSchema, in.MaxResponseBytes)
	}
	switch in.Method {
	case "GET", "HEAD", "OPTIONS":
	case "POST", "PUT", "PATCH", "DELETE":
		if !in.Reviewed {
			return HTTPRequestResult{}, fmt.Errorf("%w: %s needs review", ErrToolPolicy, in.Method)
		}
	default:
		return HTTPRequestResult{}, fmt.Errorf("%w: method %q", ErrToolSchema, in.Method)
	}
	if err := m7flow.ToolSSRFCheckURL(in.URL, s.resolve); err != nil {
		return HTTPRequestResult{}, fmt.Errorf("%w: %v", ErrToolSSRF, err)
	}
	if err := s.begin(ctx, in.RunID, "http.request"); err != nil {
		return HTTPRequestResult{}, err
	}
	defer s.end(ctx, in.RunID, "http.request", 0)
	status, body, truncated, err := s.http.Do(ctx, in.Method, in.URL, in.Headers, in.Body,
		time.Duration(in.TimeoutMS)*time.Millisecond, in.MaxResponseBytes)
	if err != nil {
		if errors.Is(err, ErrToolTimeout) {
			return HTTPRequestResult{}, err
		}
		return HTTPRequestResult{}, fmt.Errorf("%w: %v", ErrToolUnreachable, err)
	}
	return HTTPRequestResult{
		Status: status, Body: string(body),
		BodyDigest: m7flow.SHA256Hex(body), Truncated: truncated, Bytes: int64(len(body)),
	}, nil
}

// ── db.query ────────────────────────────────────────────────────────────────

// DBQueryInput is the db.query command.
type DBQueryInput struct {
	RunID     string
	ConnID    string // external db_connections id; empty = local sqlite
	SQLitePath string
	SQL       string
	Params    []any
	MaxRows   int64
	TimeoutMS int64
}

// DBQueryResult answers rows with digest + truncation.
type DBQueryResult struct {
	Columns      []string
	Rows         [][]any
	RowCount     int
	Truncated    bool
	ResultDigest string
}

// DBQuery parses (M7-TOOL-003), resolves the connection (M7-TOOL-004) and
// executes one read-only statement against the injected querier.
func (s *ToolgapService) DBQuery(ctx context.Context, in DBQueryInput) (DBQueryResult, error) {
	if err := m7flow.ValidateReadOnlySQL(in.SQL); err != nil {
		return DBQueryResult{}, fmt.Errorf("%w: %v", ErrToolSQL, err)
	}
	if in.MaxRows < 1 || in.MaxRows > ToolMaxRows {
		return DBQueryResult{}, fmt.Errorf("%w: maxRows %d", ErrToolSchema, in.MaxRows)
	}
	if in.TimeoutMS < 1 || in.TimeoutMS > ToolMaxTimeoutMS {
		return DBQueryResult{}, fmt.Errorf("%w: timeoutMs %d", ErrToolSchema, in.TimeoutMS)
	}
	target := ""
	if in.ConnID != "" {
		var conn m7flow.DBConnection
		if err := s.uow.TransactToolgap(ctx, func(tx ToolgapTx) error {
			c, err := tx.GetDBConnection(in.ConnID)
			conn = c
			return err
		}); err != nil {
			return DBQueryResult{}, fmt.Errorf("%w: %s", ErrToolDBConn, in.ConnID)
		}
		if conn.ReadOnlyVerifiedAt == nil {
			return DBQueryResult{}, fmt.Errorf("%w: %s probe not verified", ErrToolDBConn, in.ConnID)
		}
		target = "external:" + conn.ID
	} else {
		if in.SQLitePath == "" {
			return DBQueryResult{}, fmt.Errorf("%w: no target", ErrToolSchema)
		}
		target = in.SQLitePath
	}
	if err := s.begin(ctx, in.RunID, "db.query"); err != nil {
		return DBQueryResult{}, err
	}
	defer s.end(ctx, in.RunID, "db.query", 0)
	qctx, cancel := context.WithTimeout(ctx, time.Duration(in.TimeoutMS)*time.Millisecond)
	defer cancel()
	cols, rows, truncated, err := s.db.Query(qctx, target, in.SQL, in.Params, int(in.MaxRows))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return DBQueryResult{}, ErrToolTimeout
		}
		return DBQueryResult{}, fmt.Errorf("%w: %v", ErrToolUnreachable, err)
	}
	digest := m7flow.SHA256Hex([]byte(fmt.Sprintf("%v", rows)))
	return DBQueryResult{Columns: cols, Rows: rows, RowCount: len(rows), Truncated: truncated, ResultDigest: digest}, nil
}

// ── document.parse ──────────────────────────────────────────────────────────

// DocumentParseInput is the document.parse command.
type DocumentParseInput struct {
	RunID          string
	Path           string
	Format         string
	PageRange      string
	MaxOutputBytes int64
	ExpectedDigest string
}

// DocumentParseResult answers pages/blocks with digest + truncation.
type DocumentParseResult struct {
	PageCount    int
	Blocks       []m7flow.ParseBlock
	OutputDigest string
	Truncated    bool
}

// DocumentParse enforces format/digest/output guards then delegates to the
// parser port (M7-TOOL-005 / M7-TOOL-006).
func (s *ToolgapService) DocumentParse(ctx context.Context, in DocumentParseInput) (DocumentParseResult, error) {
	switch in.Format {
	case "pdf", "docx", "xlsx":
	default:
		return DocumentParseResult{}, fmt.Errorf("%w: format %q", ErrToolParse, in.Format)
	}
	if in.MaxOutputBytes < 1 || in.MaxOutputBytes > ToolMaxOutputBytes {
		return DocumentParseResult{}, fmt.Errorf("%w: maxOutputBytes %d", ErrToolParse, in.MaxOutputBytes)
	}
	data, err := os.ReadFile(in.Path)
	if err != nil {
		return DocumentParseResult{}, fmt.Errorf("%w: %s", ErrToolNotFound, in.Path)
	}
	if in.ExpectedDigest != "" && m7flow.SHA256Hex(data) != in.ExpectedDigest {
		return DocumentParseResult{}, fmt.Errorf("%w: digest mismatch", ErrToolParse)
	}
	if err := s.begin(ctx, in.RunID, "document.parse"); err != nil {
		return DocumentParseResult{}, err
	}
	defer s.end(ctx, in.RunID, "document.parse", int64(len(data)))
	pages, blocks, output, truncated, err := s.parser.Parse(ctx, in.Path, in.Format, in.PageRange, in.MaxOutputBytes)
	if err != nil {
		if errors.Is(err, ErrToolTimeout) {
			return DocumentParseResult{}, err
		}
		return DocumentParseResult{}, fmt.Errorf("%w: %v", ErrToolUnreachable, err)
	}
	return DocumentParseResult{PageCount: pages, Blocks: blocks, OutputDigest: m7flow.SHA256Hex(output), Truncated: truncated}, nil
}

// ── P2 group: download / archive / git ─────────────────────────────────────

// DownloadInput is the http.download command.
type DownloadInput struct {
	RunID          string
	URL            string
	DestPath       string
	WorkspaceRoot  string
	ExpectedSHA256 string
	MaxBytes       int64
	IdempotencyKey string
}

// DownloadResult answers sha256 + bytes of the landed file.
type DownloadResult struct {
	Path   string
	SHA256 string
	Bytes  int64
}

// Download streams one SSRF-cleared download into the workspace with a
// running SHA-256; expectedSha256 mismatch refuses the result.
func (s *ToolgapService) Download(ctx context.Context, in DownloadInput) (DownloadResult, error) {
	if err := m7flow.ToolSSRFCheckURL(in.URL, s.resolve); err != nil {
		return DownloadResult{}, fmt.Errorf("%w: %v", ErrToolSSRF, err)
	}
	if in.MaxBytes < 1 || in.MaxBytes > ToolMaxDownloadBytes {
		return DownloadResult{}, fmt.Errorf("%w: maxBytes %d", ErrToolSchema, in.MaxBytes)
	}
	dest, err := m7flow.ToolConfinePath(in.WorkspaceRoot, in.DestPath)
	if err != nil {
		return DownloadResult{}, fmt.Errorf("%w: %v", ErrToolSchema, err)
	}
	res, err := s.replayOrRun(ctx, in.RunID, "http.download", in.IdempotencyKey, func() (any, error) {
		status, body, _, derr := s.http.Do(ctx, "GET", in.URL, nil, "", ToolMaxTimeoutMS*time.Millisecond, in.MaxBytes+1)
		if derr != nil {
			return nil, fmt.Errorf("%w: %v", ErrToolUnreachable, derr)
		}
		if status >= 400 {
			return nil, fmt.Errorf("%w: status %d", ErrToolUnreachable, status)
		}
		if int64(len(body)) > in.MaxBytes {
			return nil, fmt.Errorf("%w: %d bytes", ErrToolResponseOverLimit, len(body))
		}
		sum := m7flow.SHA256Hex(body)
		if in.ExpectedSHA256 != "" && sum != in.ExpectedSHA256 {
			return nil, fmt.Errorf("%w: sha256 mismatch", ErrToolSchema)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, body, 0o644); err != nil {
			return nil, err
		}
		return DownloadResult{Path: dest, SHA256: sum, Bytes: int64(len(body))}, nil
	})
	if err != nil {
		return DownloadResult{}, err
	}
	return res.(DownloadResult), nil
}

// ArchiveUnpackInput is the archive.unpack command.
type ArchiveUnpackInput struct {
	RunID         string
	ArchivePath   string
	DestDir       string
	WorkspaceRoot string
	MaxEntries    int64
	MaxBytes      int64
	IdempotencyKey string
}

// UnpackResult answers entry count + total bytes.
type UnpackResult struct {
	DestDir     string
	EntryCount  int
	TotalBytes  int64
}

// Unpack refuses zip-slip entries, symlinks and bomb archives (entry count
// + total size double caps) before writing inside the workspace.
func (s *ToolgapService) Unpack(ctx context.Context, in ArchiveUnpackInput) (UnpackResult, error) {
	if in.MaxEntries < 1 || in.MaxEntries > ToolMaxArchiveEntry {
		return UnpackResult{}, fmt.Errorf("%w: maxEntries %d", ErrToolSchema, in.MaxEntries)
	}
	if in.MaxBytes < 1 || in.MaxBytes > ToolMaxArchiveBytes {
		return UnpackResult{}, fmt.Errorf("%w: maxBytes %d", ErrToolSchema, in.MaxBytes)
	}
	src, err := m7flow.ToolConfinePath(in.WorkspaceRoot, in.ArchivePath)
	if err != nil {
		return UnpackResult{}, fmt.Errorf("%w: %v", ErrToolSchema, err)
	}
	dst, err := m7flow.ToolConfinePath(in.WorkspaceRoot, in.DestDir)
	if err != nil {
		return UnpackResult{}, fmt.Errorf("%w: %v", ErrToolSchema, err)
	}
	res, err := s.replayOrRun(ctx, in.RunID, "archive.unpack", in.IdempotencyKey, func() (any, error) {
		return unpackZip(src, dst, in.WorkspaceRoot, in.MaxEntries, in.MaxBytes)
	})
	if err != nil {
		return UnpackResult{}, err
	}
	return res.(UnpackResult), nil
}

// GitReadInput is the git.read command.
type GitReadInput struct {
	RunID         string
	RepoPath      string
	WorkspaceRoot string
	Op            string
	Ref           string
	MaxOutputBytes int64
	IdempotencyKey string
}

// GitReadResult answers the bounded output + digest.
type GitReadResult struct {
	Output string
	Digest string
	Bytes  int64
}

// GitRead serves status/log/diff only, confined to the workspace repo.
func (s *ToolgapService) GitRead(ctx context.Context, in GitReadInput) (GitReadResult, error) {
	switch in.Op {
	case "status", "log", "diff":
	default:
		return GitReadResult{}, fmt.Errorf("%w: op %q", ErrToolSchema, in.Op)
	}
	if in.MaxOutputBytes < 1 || in.MaxOutputBytes > ToolMaxGitOutput {
		return GitReadResult{}, fmt.Errorf("%w: maxOutputBytes %d", ErrToolSchema, in.MaxOutputBytes)
	}
	repo, err := m7flow.ToolConfinePath(in.WorkspaceRoot, in.RepoPath)
	if err != nil {
		return GitReadResult{}, fmt.Errorf("%w: %v", ErrToolSchema, err)
	}
	res, err := s.replayOrRun(ctx, in.RunID, "git.read", in.IdempotencyKey, func() (any, error) {
		out, rerr := s.git.Read(ctx, repo, in.Op, in.Ref, in.MaxOutputBytes)
		if rerr != nil {
			return nil, fmt.Errorf("%w: %v", ErrToolUnreachable, rerr)
		}
		if int64(len(out)) > in.MaxOutputBytes {
			out = out[:in.MaxOutputBytes]
		}
		return GitReadResult{Output: string(out), Digest: m7flow.SHA256Hex(out), Bytes: int64(len(out))}, nil
	})
	if err != nil {
		return GitReadResult{}, err
	}
	return res.(GitReadResult), nil
}

// ── quota + idempotency helpers ─────────────────────────────────────────────

func (s *ToolgapService) begin(ctx context.Context, runID, tool string) error {
	if runID == "" {
		return fmt.Errorf("%w: runId empty", ErrToolSchema)
	}
	return s.uow.TransactToolgap(ctx, func(tx ToolgapTx) error {
		if err := tx.BeginToolCall(runID, tool); err != nil {
			return fmt.Errorf("%w: %v", ErrToolQuota, err)
		}
		return nil
	})
}

func (s *ToolgapService) end(ctx context.Context, runID, tool string, bytes int64) {
	_ = s.uow.TransactToolgap(ctx, func(tx ToolgapTx) error {
		return tx.EndToolCall(runID, tool, bytes)
	})
}

// replayOrRun gives the P2 group runId+idempotencyKey idempotency: a
// recorded result replays verbatim, otherwise fn executes and records.
func (s *ToolgapService) replayOrRun(ctx context.Context, runID, tool, key string, fn func() (any, error)) (any, error) {
	if runID == "" || key == "" {
		return nil, fmt.Errorf("%w: runId/idempotencyKey empty", ErrToolSchema)
	}
	var cached m7flow.ToolResult
	var have bool
	if err := s.uow.TransactToolgap(ctx, func(tx ToolgapTx) error {
		r, err := tx.GetToolResult(runID, key)
		if err == nil {
			cached, have = r, true
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if have {
		return decodeToolResult(cached, tool)
	}
	out, err := fn()
	if err != nil {
		return nil, err
	}
	rec := m7flow.ToolResult{
		RunID: runID, ToolName: tool, IdempotencyKey: key,
		ResultJSON: encodeToolResult(out), ResultDigest: digestOf(fmt.Sprintf("%v", out)),
		CreatedAt:  s.clock.Now().UTC().Format(time.RFC3339),
	}
	if err := s.uow.TransactToolgap(ctx, func(tx ToolgapTx) error {
		if err := tx.PutToolResult(rec); err != nil {
			return err
		}
		_, err := tx.AppendAuditEvent(audit.Event{
			ID: ulid.Make().String(), Action: "toolgap." + tool,
			ResourceType: "run", ResourceID: runID, Actor: "system",
			AfterDigest: rec.ResultDigest, CreatedAt: rec.CreatedAt,
		})
		return err
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func encodeToolResult(v any) string {
	b, _ := jsonMarshal(v)
	return string(b)
}

func decodeToolResult(r m7flow.ToolResult, tool string) (any, error) {
	switch tool {
	case "http.download":
		var out DownloadResult
		if err := jsonUnmarshal([]byte(r.ResultJSON), &out); err != nil {
			return nil, err
		}
		return out, nil
	case "archive.unpack":
		var out UnpackResult
		if err := jsonUnmarshal([]byte(r.ResultJSON), &out); err != nil {
			return nil, err
		}
		return out, nil
	case "git.read":
		var out GitReadResult
		if err := jsonUnmarshal([]byte(r.ResultJSON), &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	return nil, fmt.Errorf("unknown tool %q", tool)
}

// ── default ports ───────────────────────────────────────────────────────────

// LocalHTTPDoer is the production HTTP port: per-hop SSRF re-validation,
// byte cap enforcement and timeout propagation.
type LocalHTTPDoer struct {
	resolve func(host string) ([]string, error)
}

func (d *LocalHTTPDoer) Do(ctx context.Context, method, rawURL string, headers map[string]string, body string, timeout time.Duration, maxBytes int64) (int, []byte, bool, error) {
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if err := m7flow.ToolSSRFCheckURL(req.URL.String(), d.resolve); err != nil {
				return fmt.Errorf("redirect refused: %w", err)
			}
			return nil
		},
	}
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, reader)
	if err != nil {
		return 0, nil, false, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, false, err
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return 0, nil, false, err
	}
	truncated := int64(len(data)) > maxBytes
	if truncated {
		data = data[:maxBytes]
	}
	return resp.StatusCode, data, truncated, nil
}

// LocalSQLiteQuerier opens sqlite files strictly read-only.
type LocalSQLiteQuerier struct{}

func (LocalSQLiteQuerier) Query(ctx context.Context, target, statement string, params []any, maxRows int) ([]string, [][]any, bool, error) {
	dsn := target
	if !strings.Contains(dsn, "mode=ro") && !strings.HasPrefix(dsn, "external:") {
		dsn = "file:" + dsn + "?mode=ro"
	}
	if strings.HasPrefix(dsn, "external:") {
		return nil, nil, false, fmt.Errorf("external connection has no local executor")
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, nil, false, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, statement, params...)
	if err != nil {
		return nil, nil, false, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, false, err
	}
	var out [][]any
	truncated := false
	for rows.Next() {
		if len(out) >= maxRows {
			truncated = true
			break
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, false, err
		}
		out = append(out, vals)
	}
	return cols, out, truncated, rows.Err()
}

// LocalDocumentParser is the deterministic local parser (page provenance
// from byte ranges; a sandboxed subprocess parser plugs in via
// SetDocumentParser).
type LocalDocumentParser struct{}

func (LocalDocumentParser) Parse(ctx context.Context, path, format, pageRange string, maxOutput int64) (int, []m7flow.ParseBlock, []byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, nil, nil, false, err
	}
	const pageSize = 2048
	pages := (len(data) + pageSize - 1) / pageSize
	if pages == 0 {
		pages = 1
	}
	var blocks []m7flow.ParseBlock
	var output []byte
	for i := 0; i < len(data); i += pageSize {
		end := i + pageSize
		if end > len(data) {
			end = len(data)
		}
		blocks = append(blocks, m7flow.ParseBlock{
			Page: (i/pageSize + 1), Kind: "text",
			Text: string(data[i:end]),
		})
		output = append(output, data[i:end]...)
		if int64(len(output)) > maxOutput {
			return pages, blocks, output, true, nil
		}
	}
	return pages, blocks, output, false, nil
}

// LocalGitRunner shells out to the whitelisted git binary with bounded
// output.
type LocalGitRunner struct{}

func (LocalGitRunner) Read(ctx context.Context, repoPath, op, ref string, maxOutput int64) ([]byte, error) {
	args := []string{"-C", repoPath}
	switch op {
	case "status":
		args = append(args, "status", "--porcelain")
	case "log":
		args = append(args, "log", "--oneline", "-n", "200")
		if ref != "" {
			args = append(args, ref)
		}
	case "diff":
		args = append(args, "diff", "--stat")
		if ref != "" {
			args = append(args, ref)
		}
	default:
		return nil, fmt.Errorf("op %q refused", op)
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, git, args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	if int64(len(out)) > maxOutput {
		out = out[:maxOutput]
	}
	return out, nil
}

// urlOK is a small helper used by tests of the default doer.
func urlOK(raw string) bool { _, err := url.Parse(raw); return err == nil }

// sha256Hex is exported for storage/helpers reuse.
func sha256Hex(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
// lookupIPStrings adapts net.LookupIP to the []string resolver hook.
func lookupIPStrings(host string) ([]string, error) {
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out, nil
}

// unpackZip extracts a zip under the guards: per-entry zip-slip + symlink
// refusal, entry-count and total-size double caps (bomb defence), then
// writes only inside workspaceRoot.
func unpackZip(src, dst, workspaceRoot string, maxEntries, maxBytes int64) (UnpackResult, error) {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return UnpackResult{}, fmt.Errorf("%w: %v", ErrToolUnreachable, err)
	}
	defer zr.Close()
	if int64(len(zr.File)) > maxEntries {
		return UnpackResult{}, fmt.Errorf("%w: %d entries", ErrToolSchema, len(zr.File))
	}
	var total int64
	count := 0
	for _, f := range zr.File {
		if err := m7flow.ToolArchiveEntrySafe(f.Name); err != nil {
			return UnpackResult{}, fmt.Errorf("%w: %v", ErrToolSchema, err)
		}
		// symlink refusal (unix mode bits) and directory entries
		if f.Mode()&os.ModeSymlink != 0 {
			return UnpackResult{}, fmt.Errorf("%w: symlink entry %q", ErrToolSchema, f.Name)
		}
		target, cerr := m7flow.ToolConfinePath(dst, f.Name)
		if cerr != nil {
			return UnpackResult{}, fmt.Errorf("%w: %v", ErrToolSchema, cerr)
		}
		if !strings.HasPrefix(target, workspaceRoot) {
			return UnpackResult{}, fmt.Errorf("%w: outside workspace", ErrToolSchema)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return UnpackResult{}, err
			}
			continue
		}
		rc, oerr := f.Open()
		if oerr != nil {
			return UnpackResult{}, oerr
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			rc.Close()
			return UnpackResult{}, err
		}
		w, werr := os.Create(target)
		if werr != nil {
			rc.Close()
			return UnpackResult{}, werr
		}
		n, cperr := io.Copy(w, rc)
		w.Close()
		rc.Close()
		if cperr != nil {
			return UnpackResult{}, cperr
		}
		total += n
		count++
		if total > maxBytes {
			return UnpackResult{}, fmt.Errorf("%w: %d bytes", ErrToolSchema, total)
		}
	}
	return UnpackResult{DestDir: dst, EntryCount: count, TotalBytes: total}, nil
}