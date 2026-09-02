package toolruntime

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/canonpath"
	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/htmlapp"
	"github.com/lunitide/lunitide/internal/jsonutil"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/officetools"
	"github.com/lunitide/lunitide/internal/webfetch"
	_ "modernc.org/sqlite"
)

type Mode string

const (
	Approval   Mode = "approval"
	AutoEdit   Mode = "auto-edit"
	Plan       Mode = "plan"
	FullAccess Mode = "full-access"
)
const maxFile = 1 << 20

var ErrApprovalRequired = errors.New("approval required")

type Runtime struct {
	root string
	db   *sql.DB
	now  func() time.Time
	// fetchWeb is the SSRF-pinned web transport injected by the host
	// (cmd/engine). nil keeps web.* tools unavailable (tests, offline).
	fetchWeb func(ctx context.Context, rawURL string) (networkpolicy.FetchResult, error)
	// fullAccessRoot resolves the user-selected workspace root (workspace-root.json,
	// chosen via the host workspace picker). In full-access mode file tools
	// read/write inside that root; every other mode stays sandboxed to
	// <root>/<session>. nil or a resolver failure falls back to the sandbox.
	fullAccessRoot func() (string, error)
	// sessionStorageRoot overrides the per-session sandbox parent when the
	// user configures a conversations directory in General settings.
	sessionStorageRoot func() (string, error)
	// rulesMu guards commandRules and fullDisk for hot reload
	// (SetCommandPolicyJSON swaps both; Execute copies under RLock).
	rulesMu      sync.RWMutex
	commandRules []commandRule
	// fullDisk is the user opt-in "full-disk full-access" switch persisted in
	// command-policy.json. When true, full-access mode accepts absolute paths
	// on any drive for file tools and runs commands without the allowlist.
	fullDisk      bool
	userRulesPath string
	// hooksMu guards hookRules for hot reload (SetHooksPolicyJSON).
	hooksMu        sync.RWMutex
	hookRules      []hookRule
	hooksRulesPath string
	// auditMu makes the lazy ensureAudit open exactly one SQLite handle
	// even under concurrent callers.
	auditMu sync.Mutex
	// P2-1 FTS workspace-search index: wsFTSReady flips true once the
	// trigram virtual table exists on this handle; wsIdxMu/wsRootMu
	// serialize per-root index refresh against concurrent searches.
	wsFTSReady bool
	wsIdxMu    sync.Mutex
	wsRootMu   map[string]*sync.Mutex
	// ccExec runs the cc.* computer-control tools through the ccapp
	// service (injected by the host; nil keeps them unavailable).
	ccExec func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (ccapp.Outcome, error)
	imSend func(ctx context.Context, kind, to, text string) (desktopApp, output string, err error)
}
type Result struct {
	Output     string    `json:"output"`
	Digest     string    `json:"digest"`
	Artifact   *Artifact `json:"artifact,omitempty"`
	VisionMIME string    `json:"-"`
	VisionData []byte    `json:"-"`
}

type Artifact struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

func New(root string) (*Runtime, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("runtime root must be absolute")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	// Pinned in the operating system's own spelling, because every
	// containment check below compares a resolved child against it.
	real, err := canonpath.Canonical(root)
	if err != nil {
		return nil, err
	}
	r := &Runtime{root: filepath.Clean(real), now: func() time.Time { return time.Now().UTC() }}
	r.commandRules = builtinCommandRules()
	r.userRulesPath = filepath.Join(r.root, "command-policy.json")
	r.hooksRulesPath = filepath.Join(r.root, "hooks-policy.json")
	return r, nil
}

// SetWebFetcher installs the SSRF-pinned fetch transport for web.* tools.
func (r *Runtime) SetWebFetcher(f func(ctx context.Context, rawURL string) (networkpolicy.FetchResult, error)) {
	r.fetchWeb = f
}

// SetCcExecutor installs the computer-control executor backing the cc.*
// agent tools (ccapp.Service.ExecuteTool).
func (r *Runtime) SetCcExecutor(f func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (ccapp.Outcome, error)) {
	r.ccExec = f
}

func (r *Runtime) SetIMSend(f func(ctx context.Context, kind, to, text string) (desktopApp, output string, err error)) {
	r.imSend = f
}

// SetFullAccessRootResolver installs the user-workspace root resolver used by
// full-access mode. The resolver is consulted per call so a changed root
// selection takes effect immediately; failures fall back to the sandbox.
func (r *Runtime) SetFullAccessRootResolver(f func() (string, error)) {
	r.fullAccessRoot = f
}

func (r *Runtime) SetSessionStorageRoot(f func() (string, error)) { r.sessionStorageRoot = f }

func Open(root string) (*Runtime, error) {
	r, err := New(root)
	if err != nil {
		return nil, err
	}
	if err = r.ensureAudit(); err != nil {
		return nil, err
	}
	if err = r.loadUserCommandPolicy(); err != nil {
		_ = r.Close()
		return nil, err
	}
	if err = r.loadUserHooksPolicy(); err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}
func (r *Runtime) Close() error {
	if r.db == nil {
		return nil
	}
	return r.db.Close()
}

// Execute runs one tool call for the given conversation mode. Subagent and
// delegation paths must stay on this entry point: it never lifts the
// command allowlist or the path confinement, whatever the persisted policy
// says.
func (r *Runtime) Execute(ctx context.Context, mode Mode, session, name string, args json.RawMessage, approved bool) (out Result, err error) {
	return r.execute(ctx, mode, session, name, args, approved, false, nil)
}

// ExecuteStreaming runs one tool with an optional progress sink receiving
// bounded incremental output chunks while the tool runs (P1-2). Only
// command.run emits progress today; other tools simply complete as usual.
// progress may be called from background goroutines but is serialized by
// the runtime, so a non-concurrent-safe sink is fine.
func (r *Runtime) ExecuteStreaming(ctx context.Context, mode Mode, session, name string, args json.RawMessage, approved bool, progress func(chunk string)) (out Result, err error) {
	return r.execute(ctx, mode, session, name, args, approved, false, progress)
}

// ExecuteUnconfined is the user-conversation-only entry point that honors
// the full-disk opt-in: commands skip the allowlist and file tools accept
// absolute paths on any drive. It is reserved for chat tool calls made in
// full-access mode while command-policy.json has "fullAccess": true.
func (r *Runtime) ExecuteUnconfined(ctx context.Context, session, name string, args json.RawMessage, approved bool) (out Result, err error) {
	return r.execute(ctx, FullAccess, session, name, args, approved, true, nil)
}

// ExecuteUnconfinedStreaming is the full-disk variant of ExecuteStreaming.
func (r *Runtime) ExecuteUnconfinedStreaming(ctx context.Context, session, name string, args json.RawMessage, approved bool, progress func(chunk string)) (out Result, err error) {
	return r.execute(ctx, FullAccess, session, name, args, approved, true, progress)
}

func (r *Runtime) execute(ctx context.Context, mode Mode, session, name string, args json.RawMessage, approved, unconfined bool, progress func(chunk string)) (out Result, err error) {
	switch mode {
	case Approval, AutoEdit, Plan, FullAccess:
	default:
		return Result{}, errors.New("invalid execution mode")
	}
	if mode == Plan {
		return Result{}, errors.New("tools disabled in plan mode")
	}
	args = jsonutil.Repair(args)
	// E1: run_terminal_cmd is the Codex-style terminal tool. It shares
	// command.run's entire pipeline (built-in + user allowlist, full-disk
	// lift, hardline floor, approval gate, bounded streaming); only the
	// argument shape differs ({command:"..."} vs {argv:[...]}). Normalize to
	// command.run here — before hooks, the mutating gate and the switch — so
	// gating, audit and execution all stay a single reviewed code path and no
	// new command-execution surface is introduced.
	if name == "run_terminal_cmd" {
		argv, terr := terminalCommandToArgv(args)
		if terr != nil {
			return Result{}, commandFailure(terr.Error())
		}
		name = "command.run"
		args = argv
	}
	// P3-B hooks: evaluate beforeToolCall rules first (block > gate >
	// grant priority, fail-closed). A block refuses before anything else;
	// every matched rule leaves one audit row whatever the outcome.
	hooks := r.evaluateHooks(name)
	defer func() { r.recordHookEvents(ctx, session, name, Digest(name, args), out.Digest, hooks) }()
	if hooks.blockMessage != "" {
		return Result{}, fmt.Errorf("%w: %s", ErrHookBlocked, hooks.blockMessage)
	}
	if hooks.grantApproval && !approved && name != userAskTool {
		approved = true
	}
	mutating := name == "workspace.write" || name == "workspace.edit" || name == "command.run" || name == "desktop.open" || name == "desktop.type" || name == "media.play" || name == "im.send" || officeGenTools[name] || ccToolChangesMachine(name, args)
	if mutating && !approved && (hooks.forceApproval || mode == Approval || (name == "command.run" && mode == AutoEdit)) {
		// Remembered exact approvals (P1-5) satisfy the gate without a new
		// round-trip; unmatched or argument-variant calls still gate.
		if canonical, ce := canonicalArgs(args); ce == nil {
			if d := Digest(name, canonical); d != "" && r.approvalRemembered(ctx, session, name, d) {
				approved = true
			}
		}
		if !approved {
			return Result{}, ErrApprovalRequired
		}
	}
	switch name {
	case "workspace.list":
		var a struct {
			Path string `json:"path"`
		}
		if strict(args, &a) != nil {
			return Result{}, errors.New("invalid arguments")
		}
		if a.Path == "" {
			a.Path = "."
		}
		p, e := r.path(mode, session, a.Path, false, unconfined)
		if e != nil {
			return Result{}, e
		}
		entries, e := os.ReadDir(p)
		if e != nil {
			return Result{}, e
		}
		names := make([]string, 0, len(entries))
		for _, x := range entries {
			n := x.Name()
			if x.IsDir() {
				n += "/"
			}
			names = append(names, n)
		}
		sort.Strings(names)
		return result(strings.Join(names, "\n")), nil
	case "workspace.read":
		var a struct {
			Path string `json:"path"`
		}
		if strict(args, &a) != nil || a.Path == "" {
			return Result{}, errors.New("invalid arguments")
		}
		p, e := r.path(mode, session, a.Path, false, unconfined)
		if e != nil {
			return Result{}, e
		}
		f, e := os.Open(p)
		if e != nil {
			return Result{}, e
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, maxFile+1))
		if e != nil || len(b) > maxFile {
			return Result{}, errors.New("file exceeds limit")
		}
		return result(string(b)), nil
	case "workspace.write":
		var a struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if strict(args, &a) != nil || a.Path == "" || len(a.Content) > maxFile {
			return Result{}, errors.New("invalid arguments")
		}
		p, e := r.path(mode, session, a.Path, true, unconfined)
		if e != nil {
			return Result{}, e
		}
		tmp, e := os.CreateTemp(filepath.Dir(p), ".write-*")
		if e != nil {
			return Result{}, e
		}
		tn := tmp.Name()
		defer os.Remove(tn)
		if e = tmp.Chmod(0600); e == nil {
			_, e = tmp.WriteString(a.Content)
		}
		if e == nil {
			e = tmp.Sync()
		}
		ce := tmp.Close()
		if e == nil {
			e = ce
		}
		if e == nil {
			e = os.Rename(tn, p)
		}
		if e != nil {
			return Result{}, e
		}
		written := result("wrote " + a.Path)
		ext := strings.ToLower(filepath.Ext(a.Path))
		if ext == ".html" || ext == ".htm" {
			written.Artifact = &Artifact{Kind: "html", Path: htmlArtifactPath(a.Path, false), Content: a.Content}
		}
		return written, nil
	case "workspace.search":
		var a struct {
			Query string `json:"query"`
			Path  string `json:"path"`
			Regex bool   `json:"regex"`
			Max   int    `json:"max"`
		}
		if strict(args, &a) != nil || a.Query == "" || len(a.Query) > 512 {
			return Result{}, errors.New("invalid arguments")
		}
		if a.Path == "" {
			a.Path = "."
		}
		max := a.Max
		if max <= 0 {
			max = 50
		}
		if max > 200 {
			max = 200
		}
		hits, e := r.searchWorkspace(mode, session, a.Path, a.Query, a.Regex, max, unconfined)
		if e != nil {
			return Result{}, e
		}
		return result(strings.Join(hits, "\n")), nil
	case "workspace.edit":
		files, e := parseWorkspaceEditArgs(args)
		if e != nil {
			return Result{}, e
		}
		type pendingEdit struct {
			rel     string
			abs     string
			updated string
			count   int
		}
		pending := make([]pendingEdit, 0, len(files))
		total := 0
		for _, f := range files {
			p, pe := r.path(mode, session, f.Path, false, unconfined)
			if pe != nil {
				return Result{}, pe
			}
			b, re := os.ReadFile(p)
			if re != nil || len(b) > maxFile {
				return Result{}, errors.New("file missing or exceeds limit")
			}
			updated, count, ae := applyWorkspaceHunks(string(b), f.Hunks)
			if ae != nil {
				if len(files) > 1 {
					return Result{}, fmt.Errorf("%s: %v", f.Path, ae)
				}
				return Result{}, ae
			}
			if len(updated) > maxFile {
				return Result{}, errors.New("edited file exceeds limit")
			}
			pending = append(pending, pendingEdit{rel: f.Path, abs: p, updated: updated, count: count})
			total += count
		}
		for _, item := range pending {
			if we := writeFileReplace(item.abs, item.updated); we != nil {
				return Result{}, we
			}
		}
		if len(pending) == 1 {
			return result(fmt.Sprintf("edited %s (%d replacement(s))", pending[0].rel, pending[0].count)), nil
		}
		names := make([]string, 0, len(pending))
		for _, item := range pending {
			names = append(names, item.rel)
		}
		return result(fmt.Sprintf("edited %d files (%d replacement(s)): %s", len(pending), total, strings.Join(names, ", "))), nil
	case "todo.write":
		var a struct {
			Todos []struct {
				Content  string `json:"content"`
				Status   string `json:"status"`
				Priority string `json:"priority"`
			} `json:"todos"`
		}
		if strict(args, &a) != nil {
			return Result{}, errors.New("invalid arguments")
		}
		rendered, e := r.writeTodos(session, a.Todos)
		if e != nil {
			return Result{}, e
		}
		return result(rendered), nil
	case userAskTool:
		return executeUserAsk(args, approved)
	case "command.run":
		if mode != FullAccess && !(approved && (mode == Approval || mode == AutoEdit)) {
			return Result{}, errors.New("command denied")
		}
		var a struct {
			Argv []string `json:"argv"`
		}
		if strict(args, &a) != nil || len(a.Argv) == 0 || len(a.Argv) > commandMaxArgv {
			return Result{}, errors.New("command denied")
		}
		// Checked before every mode branch below, because those branches are
		// all reachable without a human in the loop: a companion voice turn
		// upgrades itself to full access, and full-disk lifts the allowlist
		// entirely. This floor has no opt-out.
		if reason := hardlineRefusal(a.Argv); reason != "" {
			return Result{}, commandFailure("refused, this cannot be undone (" + reason + "). Run it yourself in a terminal if you really mean it.")
		}
		// Full-disk opt-in lifts the whitelist for user conversations that
		// came in through ExecuteUnconfined: any argv runs with the max
		// deadline; every other path keeps matching the built-in plus user
		// allowlist.
		var deadline time.Duration
		if unconfined && r.FullDiskEnabled() {
			deadline = commandDeadlineMax
		} else {
			r.rulesMu.RLock()
			rules := r.commandRules
			r.rulesMu.RUnlock()
			rule, ok := matchCommandRule(rules, a.Argv)
			if !ok {
				return Result{}, errors.New("command denied")
			}
			deadline = rule.deadline
		}
		root, e := r.effectiveRoot(mode, session)
		if e != nil {
			return Result{}, e
		}
		if e = os.MkdirAll(root, 0700); e != nil {
			return Result{}, e
		}
		if dir, ok := extractMkdirPath(a.Argv); ok {
			dir = expandWindowsEnv(dir)
			if dir == "" {
				return Result{}, commandFailure("empty directory path")
			}
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(root, dir)
			}
			if e = os.MkdirAll(dir, 0755); e != nil {
				return Result{}, commandFailure(e.Error())
			}
			return result(formatCommandOutput(true, "created directory: "+dir)), nil
		}
		argv, cleanup, wrapErr := prepareCommandArgv(a.Argv)
		if wrapErr != nil {
			return Result{}, commandFailure(wrapErr.Error())
		}
		defer cleanup()
		cctx, cancel := context.WithTimeout(ctx, deadline)
		defer cancel()
		cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_PAGER=cat", "PAGER=cat", "TERM=dumb", "GIT_OPTIONAL_LOCKS=0", "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
		// P1-2: with a progress sink the pipes are read live so long
		// running commands stream bounded stdout/stderr chunks to the
		// caller instead of black-boxing until exit. The final result
		// keeps the legacy combined-output shape (64 KiB cap, error text
		// carried in the failure message); line scanning only normalizes
		// CRLF tails away.
		if progress != nil {
			stdoutPipe, e := cmd.StdoutPipe()
			if e != nil {
				return Result{}, commandFailure(e.Error())
			}
			stderrPipe, e := cmd.StderrPipe()
			if e != nil {
				return Result{}, commandFailure(e.Error())
			}
			if e = cmd.Start(); e != nil {
				return Result{}, commandFailure(e.Error())
			}
			var mu sync.Mutex
			var combined []byte
			emitted := 0
			scan := func(r io.Reader, done chan<- struct{}) {
				sc := bufio.NewScanner(r)
				sc.Buffer(make([]byte, 0, 64*1024), 256*1024)
				for sc.Scan() {
					line := decodeCommandOutput(sc.Bytes())
					mu.Lock()
					if len(combined) < 64<<10 {
						combined = append(combined, line...)
						combined = append(combined, '\n')
					}
					emit := emitted < toolProgressMaxChunks
					if emit {
						emitted++
						// Hold the lock across progress so stdout/stderr
						// scanners cannot interleave send() and race the
						// chat stream sequence cursor.
						progress(truncateRunes(line, 400))
					}
					mu.Unlock()
				}
				done <- struct{}{}
			}
			doneOut, doneErr := make(chan struct{}), make(chan struct{})
			go scan(stdoutPipe, doneOut)
			go scan(stderrPipe, doneErr)
			<-doneOut
			<-doneErr
			waitErr := cmd.Wait()
			out := combined
			if len(out) > 64<<10 {
				out = out[:64<<10]
			}
			text := decodeCommandOutput(out)
			if waitErr != nil {
				return Result{}, commandFailure(text)
			}
			return result(formatCommandOutput(true, text)), nil
		}
		out, e := cmd.CombinedOutput()
		if len(out) > 64<<10 {
			out = out[:64<<10]
		}
		text := decodeCommandOutput(out)
		if e != nil {
			return Result{}, commandFailure(text)
		}
		return result(formatCommandOutput(true, text)), nil
	case "web.fetch":
		var a struct {
			URL string `json:"url"`
		}
		if strict(args, &a) != nil || a.URL == "" || len(a.URL) > 2048 {
			return Result{}, errors.New("invalid arguments")
		}
		if r.fetchWeb == nil {
			return Result{}, errors.New("web tools unavailable")
		}
		page, e := r.fetchWeb(ctx, a.URL)
		if e != nil {
			return Result{}, e
		}
		extracted, ok := webfetch.ExtractText(page.ContentType, page.Body, webfetch.MaxTextBytes)
		if !ok {
			return Result{}, fmt.Errorf("unsupported content type: %s", page.ContentType)
		}
		var b strings.Builder
		if extracted.Title != "" {
			b.WriteString("title: " + extracted.Title + "\n")
		}
		b.WriteString("url: " + page.FinalURL + "\n")
		if extracted.Truncated || page.Truncated {
			b.WriteString("note: content truncated\n")
		}
		b.WriteString("\n" + extracted.Text)
		out := result(truncateRunes(b.String(), 12000))
		preview := extracted.Text
		if len(preview) > 24<<10 {
			preview = preview[:24<<10]
		}
		title := extracted.Title
		// Path must end in .html — the desktop host strips any other
		// artifact (including https:// URLs) before it reaches the
		// renderer, which left the browser tab on an empty placeholder.
		out.Artifact = &Artifact{Kind: "html", Path: "fetch.html", Content: webfetch.RenderExtractHTML(title, page.FinalURL, preview)}
		return out, nil
	case "web.search":
		var a struct {
			Query string `json:"query"`
			Max   int    `json:"max"`
		}
		if strict(args, &a) != nil || strings.TrimSpace(a.Query) == "" || len(a.Query) > 512 {
			return Result{}, errors.New("invalid arguments")
		}
		if r.fetchWeb == nil {
			return Result{}, errors.New("web tools unavailable")
		}
		max := a.Max
		if max <= 0 {
			max = 5
		}
		if max > 10 {
			max = 10
		}
		results, source, pageURL, e := r.searchWeb(ctx, a.Query, max)
		if e != nil {
			return Result{}, e
		}
		if pageURL == "" {
			pageURL = webfetch.BingCNSearchURL(a.Query)
		}
		var b strings.Builder
		b.WriteString("query: " + a.Query + "\n")
		if source != "" && source != "none" {
			b.WriteString("source: " + source + "\n")
		}
		b.WriteString("results_url: " + pageURL + "\n")
		if len(results) == 0 {
			b.WriteString("no results\n")
		}
		for i, hit := range results {
			fmt.Fprintf(&b, "\n%d. %s\n   %s\n", i+1, hit.Title, hit.URL)
			if hit.Snippet != "" {
				b.WriteString("   " + hit.Snippet + "\n")
			}
		}
		out := result(b.String())
		out.Artifact = &Artifact{Kind: "html", Path: "search.html", Content: webfetch.RenderSearchHTML(a.Query, results)}
		return out, nil
	case "excel.gen":
		var a struct {
			Path    string                  `json:"path"`
			Desktop bool                    `json:"desktop"`
			Sheets  []officetools.SheetSpec `json:"sheets"`
		}
		if strict(args, &a) != nil || (a.Path == "" && !a.Desktop) {
			return Result{}, errors.New("invalid arguments")
		}
		if !a.Desktop && strings.ToLower(filepath.Ext(a.Path)) != ".xlsx" {
			return Result{}, errors.New("excel.gen path must end with .xlsx")
		}
		outPath, e := r.desktopWritePath(a.Path, "workbook.xlsx", ".xlsx", a.Desktop, unconfined)
		if e != nil {
			return Result{}, e
		}
		data, e := officetools.GenXLSX(a.Sheets)
		if e != nil {
			return Result{}, e
		}
		return r.finishOfficeGen(mode, session, outPath, data, len(a.Sheets), unconfined, a.Desktop, "workbook.xlsx")
	case "excel.parse":
		var a struct {
			Path string `json:"path"`
		}
		if strict(args, &a) != nil || a.Path == "" {
			return Result{}, errors.New("invalid arguments")
		}
		p, e := r.path(mode, session, a.Path, false, unconfined)
		if e != nil {
			return Result{}, e
		}
		b, e := os.ReadFile(p)
		if e != nil || len(b) > maxGeneratedBytes {
			return Result{}, errors.New("file missing or exceeds limit")
		}
		summary, e := officetools.ParseXLSX(b)
		if e != nil {
			return Result{}, e
		}
		return result(summary), nil
	case "docx.gen":
		var a struct {
			Path     string                  `json:"path"`
			Desktop  bool                    `json:"desktop"`
			Title    string                  `json:"title"`
			Subtitle string                  `json:"subtitle"`
			Author   string                  `json:"author"`
			Kind     string                  `json:"kind"`
			Blocks   []officetools.DocxBlock `json:"blocks"`
		}
		if strict(args, &a) != nil || (a.Path == "" && !a.Desktop) {
			return Result{}, errors.New("invalid arguments")
		}
		if !a.Desktop && strings.ToLower(filepath.Ext(a.Path)) != ".docx" {
			return Result{}, errors.New("docx.gen path must end with .docx")
		}
		outPath, e := r.desktopWritePath(a.Path, "document.docx", ".docx", a.Desktop, unconfined)
		if e != nil {
			return Result{}, e
		}
		data, e := officetools.GenDocxDoc(officetools.DocxDoc{
			Title: a.Title, Subtitle: a.Subtitle, Author: a.Author, Kind: a.Kind, Blocks: a.Blocks,
		})
		if e != nil {
			return Result{}, e
		}
		return r.finishOfficeGen(mode, session, outPath, data, len(a.Blocks), unconfined, a.Desktop, "document.docx")
	case "pptx.gen":
		var a struct {
			Path    string                  `json:"path"`
			Desktop bool                    `json:"desktop"`
			Title   string                  `json:"title"`
			Slides  []officetools.SlideSpec `json:"slides"`
		}
		if strict(args, &a) != nil || (a.Path == "" && !a.Desktop) {
			return Result{}, errors.New("invalid arguments")
		}
		if !a.Desktop && strings.ToLower(filepath.Ext(a.Path)) != ".pptx" {
			return Result{}, errors.New("pptx.gen path must end with .pptx")
		}
		outPath, e := r.desktopWritePath(a.Path, "deck.pptx", ".pptx", a.Desktop, unconfined)
		if e != nil {
			return Result{}, e
		}
		data, e := officetools.GenPptx(a.Title, a.Slides)
		if e != nil {
			return Result{}, e
		}
		return r.finishOfficeGen(mode, session, outPath, data, len(a.Slides), unconfined, a.Desktop, "deck.pptx")
	case "html.gen":
		var a struct {
			Path     string `json:"path"`
			Title    string `json:"title"`
			Template string `json:"template"`
			Desktop  bool   `json:"desktop"`
		}
		if strict(args, &a) != nil {
			return Result{}, errors.New("invalid arguments")
		}
		if strings.TrimSpace(a.Template) == "" {
			a.Template = "penalty-shootout"
		}
		page, e := htmlapp.Render(a.Template, a.Title)
		if e != nil {
			return Result{}, e
		}
		if !a.Desktop && strings.TrimSpace(a.Path) == "" {
			switch a.Template {
			case "timer":
				a.Path = "timer.html"
			case "checklist":
				a.Path = "checklist.html"
			default:
				a.Path = "penalty-shootout.html"
			}
		}
		fallbackHTML := "世界杯点球大战.html"
		switch a.Template {
		case "timer":
			fallbackHTML = "计时器.html"
		case "checklist":
			fallbackHTML = "清单.html"
		}
		outPath, de := r.desktopWritePath(a.Path, fallbackHTML, ".html", a.Desktop, unconfined)
		if de != nil {
			return Result{}, de
		}
		if ext := strings.ToLower(filepath.Ext(outPath)); ext != ".html" && ext != ".htm" {
			return Result{}, errors.New("html.gen path must end with .html")
		}
		written, e := r.writeGenerated(mode, session, outPath, []byte(page), -1, unconfined)
		if e != nil {
			return Result{}, e
		}
		written.Artifact = &Artifact{Kind: "html", Path: htmlArtifactPath(outPath, a.Desktop), Content: page}
		return written, nil
	case "desktop.open":
		var a struct {
			Name string `json:"name"`
		}
		if strict(args, &a) != nil || strings.TrimSpace(a.Name) == "" {
			return Result{}, errors.New("invalid arguments")
		}
		if err := requireDesktopAction(approved); err != nil {
			return Result{}, err
		}
		path, others, e := pickLaunchTarget(a.Name)
		if e != nil {
			if strings.Contains(e.Error(), "无法执行") {
				return Result{}, e
			}
			return Result{}, fmt.Errorf("无法执行：%v", e)
		}
		if path == "" {
			return Result{}, fmt.Errorf("无法执行：桌面上有多份匹配「%s」：%s。请说出完整文件名", strings.TrimSpace(a.Name), strings.Join(others, "、"))
		}
		if e = openWithDefaultApp(path); e != nil {
			return Result{}, fmt.Errorf("无法执行：打不开（%v）", e)
		}
		return result("opened " + path), nil
	case "desktop.type":
		invoke := func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (Result, error) {
			return r.runCcTool(ctx, mode, session, tool, args, approved, unconfined)
		}
		return executeDesktopType(ctx, invoke, session, args, approved, unconfined)
	case "media.play":
		invoke := func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (Result, error) {
			return r.runCcTool(ctx, mode, session, tool, args, approved, unconfined)
		}
		return executeMediaPlayWithCC(ctx, invoke, session, args, unconfined, approved)
	case "im.send":
		return r.executeIMSend(ctx, session, args, approved, unconfined)
	case "pdf.gen":
		var a struct {
			Path    string `json:"path"`
			Desktop bool   `json:"desktop"`
			Title   string `json:"title"`
			Body    string `json:"body"`
		}
		if strict(args, &a) != nil || (a.Path == "" && !a.Desktop) {
			return Result{}, errors.New("invalid arguments")
		}
		if !a.Desktop && strings.ToLower(filepath.Ext(a.Path)) != ".pdf" {
			return Result{}, errors.New("pdf.gen path must end with .pdf")
		}
		outPath, e := r.desktopWritePath(a.Path, "report.pdf", ".pdf", a.Desktop, unconfined)
		if e != nil {
			return Result{}, e
		}
		data, e := officetools.GenPDF(a.Title, a.Body)
		if e != nil {
			return Result{}, e
		}
		return r.finishOfficeGen(mode, session, outPath, data, -1, unconfined, a.Desktop, "report.pdf")
	case "cc.mouse_move", "cc.mouse_click", "cc.keyboard_type",
		"cc.keyboard_shortcut", "cc.screen_capture", "cc.get_active_window":
		return r.runCcTool(ctx, mode, session, name, args, approved, unconfined)
	default:
		if ccapp.IsCcTool(name) {
			return r.runCcTool(ctx, mode, session, name, args, approved, unconfined)
		}
		return Result{}, errors.New("unknown tool")
	}
}

// runCcTool executes one computer-control tool through the injected ccapp
// service. Plan mode never reaches here (tools are globally disabled); the
// ccapp confirmation gate maps onto the standard approval flow so
// high/critical operations pause for a manual decision.
func (r *Runtime) runCcTool(ctx context.Context, mode Mode, session, name string, args json.RawMessage, approved, unconfined bool) (Result, error) {
	if r.ccExec == nil {
		return Result{}, errors.New("computer control unavailable (M10-CC-010)")
	}
	outcome, err := r.ccExec(ctx, session, name, args, approved)
	if err != nil {
		if errors.Is(err, ccapp.ErrCcConfirmRequired) {
			return Result{}, ErrApprovalRequired
		}
		return Result{}, fmt.Errorf("%s: %v", ccapp.Code(err), err)
	}
	res := result(outcome.Summary)
	if len(outcome.CapturePNG) > 0 {
		rel := fmt.Sprintf("screen-capture-%s.png", r.now().UTC().Format("20060102T150405.000000000"))
		p, e := r.path(mode, session, rel, true, unconfined)
		if e != nil {
			return Result{}, e
		}
		if e = os.WriteFile(p, outcome.CapturePNG, 0600); e != nil {
			return Result{}, e
		}
		res.Artifact = &Artifact{Kind: "image", Path: rel}
		res.Output = fmt.Sprintf("%s (saved %s)", outcome.Summary, rel)
		if data, mime, ve := ccapp.PrepareVisionImage(outcome.CapturePNG); ve == nil && len(data) > 0 {
			res.VisionMIME = mime
			res.VisionData = data
		}
	}
	return res, nil
}

func requireDesktopAction(approved bool) error {
	if approved {
		return nil
	}
	return errors.New("desktop action requires full-access or user approval")
}

func strict(b []byte, v any) error {
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing json")
	}
	return nil
}
func result(s string) Result {
	h := sha256.Sum256([]byte(s))
	return Result{Output: s, Digest: hex.EncodeToString(h[:])}
}
