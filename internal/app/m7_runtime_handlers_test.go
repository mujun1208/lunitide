package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

// newM7RuntimeEngineHarness wires the slice 6-8 services onto one engine so
// the subagent/toolgap/mcp handlers can be exercised end-to-end through the
// bridge with deterministic execution ports.
func newM7RuntimeEngineHarness(t *testing.T) (*Engine, *m7app.ToolgapService, *m7app.McpRuntimeService) {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "m7rt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	repo := store.AgentRuntimeRepository()
	subagentSvc := m7app.NewSubagentService(repo)
	toolgapSvc := m7app.NewToolgapService(repo)
	mcpSvc := m7app.NewMcpRuntimeService(repo)
	e := NewEngine(nil, "test")
	e.SetM7RuntimeServices(subagentSvc, toolgapSvc, mcpSvc)
	return e, toolgapSvc, mcpSvc
}

func m7ErrCode(t *testing.T, r bridge.Response) string {
	t.Helper()
	if r.OK {
		t.Fatalf("expected failure, got success: %+v", r.Payload)
	}
	return r.Error.Code
}

// ── slice 6: subagent.spawn / join / tree ───────────────────────────────────

func TestSubagentSpawnJoinTreeHappyPath(t *testing.T) {
	e, _, _ := newM7RuntimeEngineHarness(t)
	ctx := context.Background()

	spawned := e.Handle(ctx, m7Request(bridge.MethodSubagentSpawn,
		`{"rootRunId":"root-run-1","purpose":"收集 CR-42 证据","readCaps":["evidence.list","fs.grep"],`+
			`"budgetTokens":8000,"deadlineMs":300000,"requestId":"req-h1","actor":"op-1"}`, "idem-sag-h1"))
	var sp struct {
		SubagentID       string `json:"subagentId"`
		Status           string `json:"status"`
		CapabilityDigest string `json:"capabilityDigest"`
	}
	m7Decode(t, spawned, &sp)
	if len(sp.SubagentID) != 26 || sp.Status != m7flow.SagRunning || len(sp.CapabilityDigest) != 64 {
		t.Fatalf("unexpected spawn: %+v", sp)
	}

	// idempotent replay answers the original run
	replay := e.Handle(ctx, m7Request(bridge.MethodSubagentSpawn,
		`{"rootRunId":"root-run-1","purpose":"收集 CR-42 证据","readCaps":["evidence.list","fs.grep"],`+
			`"budgetTokens":8000,"deadlineMs":300000,"requestId":"req-h1","actor":"op-1"}`, "idem-sag-h1b"))
	var rp struct {
		SubagentID string `json:"subagentId"`
	}
	m7Decode(t, replay, &rp)
	if rp.SubagentID != sp.SubagentID {
		t.Fatalf("replay returned different id: %s vs %s", rp.SubagentID, sp.SubagentID)
	}

	// join before completion is refused (M7-SAG-004)
	stale := e.Handle(ctx, m7Request(bridge.MethodSubagentJoin,
		`{"subagentId":"`+sp.SubagentID+`","waitMs":1000}`, ""))
	if code := m7ErrCode(t, stale); code != "M7-SAG-004" {
		t.Fatalf("join(running) expected M7-SAG-004, got %s", code)
	}

	// complete through the service, then join answers summaries only
	if _, err := e.m7subagent.Complete(ctx, sp.SubagentID, 4200, []m7app.ObservationInput{
		{EvidenceID: "EV-1", Summary: "测试证据 A：全部通过"},
		{EvidenceID: "EV-2", Summary: "扫描证据 B：无高危"},
	}); err != nil {
		t.Fatal(err)
	}
	joined := e.Handle(ctx, m7Request(bridge.MethodSubagentJoin,
		`{"subagentId":"`+sp.SubagentID+`","waitMs":1000}`, ""))
	var jr struct {
		Status      string   `json:"status"`
		Summary     string   `json:"summary"`
		Digests     []string `json:"digests"`
		Truncated   bool     `json:"truncated"`
		SpentTokens int64    `json:"spentTokens"`
	}
	m7Decode(t, joined, &jr)
	if jr.Status != m7flow.SagCompleted || jr.SpentTokens != 4200 || len(jr.Digests) != 2 || jr.Truncated {
		t.Fatalf("unexpected join: %+v", jr)
	}
	if !strings.Contains(jr.Summary, "测试证据 A") {
		t.Fatalf("join summary missing observation: %q", jr.Summary)
	}

	// tree lists the run for the root
	tree := e.Handle(ctx, m7Request(bridge.MethodSubagentTree,
		`{"rootRunId":"root-run-1","limit":10}`, ""))
	var tr struct {
		Subagents []struct {
			ID          string `json:"id"`
			Status      string `json:"status"`
			SpentTokens int64  `json:"spentTokens"`
		} `json:"subagents"`
	}
	m7Decode(t, tree, &tr)
	if len(tr.Subagents) != 1 || tr.Subagents[0].ID != sp.SubagentID ||
		tr.Subagents[0].Status != m7flow.SagCompleted {
		t.Fatalf("unexpected tree: %+v", tr)
	}
}

func TestSubagentSpawnGuardFamily(t *testing.T) {
	e, _, _ := newM7RuntimeEngineHarness(t)
	ctx := context.Background()

	cases := []struct {
		name    string
		payload string
		code    string
	}{
		{"writecap", `{"rootRunId":"r1","purpose":"p","readCaps":["changeset.write"],` +
			`"budgetTokens":1000,"deadlineMs":120000,"requestId":"q1"}`, "M7-SAG-002"},
		{"budgetceiling", `{"rootRunId":"r1","purpose":"p","readCaps":["fs.read"],` +
			`"budgetTokens":50001,"deadlineMs":120000,"requestId":"q2"}`, "M7-SAG-003"},
		{"budgetfloor", `{"rootRunId":"r1","purpose":"p","readCaps":["fs.read"],` +
			`"budgetTokens":0,"deadlineMs":120000,"requestId":"q3"}`, "M7-SAG-003"},
		{"deadlinewindow", `{"rootRunId":"r1","purpose":"p","readCaps":["fs.read"],` +
			`"budgetTokens":1000,"deadlineMs":59000,"requestId":"q4"}`, "M7-SAG-003"},
		{"emptypurpose", `{"rootRunId":"r1","purpose":"","readCaps":["fs.read"],` +
			`"budgetTokens":1000,"deadlineMs":120000,"requestId":"q5"}`, "BRIDGE_SCHEMA_INVALID"},
	}
	for _, tc := range cases {
		resp := e.Handle(ctx, m7Request(bridge.MethodSubagentSpawn, tc.payload, "idem-"+tc.name))
		if code := m7ErrCode(t, resp); code != tc.code {
			t.Fatalf("%s: want %s, got %s", tc.name, tc.code, code)
		}
	}
}

func TestSubagentJoinDriftAndUnknown(t *testing.T) {
	e, _, _ := newM7RuntimeEngineHarness(t)
	ctx := context.Background()

	// unknown target refuses (M7-SAG-004)
	unknown := e.Handle(ctx, m7Request(bridge.MethodSubagentJoin,
		`{"subagentId":"01ARZ3NDEKTSV4RRFFQ69G5FA9","waitMs":1000}`, ""))
	if code := m7ErrCode(t, unknown); code != "M7-SAG-004" {
		t.Fatalf("join(unknown) expected M7-SAG-004, got %s", code)
	}

	spawned := e.Handle(ctx, m7Request(bridge.MethodSubagentSpawn,
		`{"rootRunId":"root-drift","purpose":"drift probe","readCaps":["fs.stat"],`+
			`"budgetTokens":1000,"deadlineMs":120000,"requestId":"rd1"}`, "idem-sag-rd"))
	var sp struct {
		SubagentID string `json:"subagentId"`
	}
	m7Decode(t, spawned, &sp)
	if _, err := e.m7subagent.Complete(ctx, sp.SubagentID, 10, []m7app.ObservationInput{
		{EvidenceID: "EV", Summary: "ok"},
	}); err != nil {
		t.Fatal(err)
	}

	// capability digest drift fails closed (TOCTOU, M7-SAG-004)
	bogus := strings.Repeat("cc", 32)
	drift := e.Handle(ctx, m7Request(bridge.MethodSubagentJoin,
		`{"subagentId":"`+sp.SubagentID+`","waitMs":1000,"expectedCapabilityDigest":"`+bogus+`"}`, ""))
	if code := m7ErrCode(t, drift); code != "M7-SAG-004" {
		t.Fatalf("join(drift) expected M7-SAG-004, got %s", code)
	}
}

// ── slice 7: the seven gap tools ────────────────────────────────────────────

// fakeHTTPDoer answers fixed bodies without touching the network.
type fakeHTTPDoer struct {
	status int
	body   []byte
}

func (f fakeHTTPDoer) Do(_ context.Context, _ string, _ string, _ map[string]string, _ string,
	_ time.Duration, _ int64) (int, []byte, bool, error) {
	return f.status, f.body, false, nil
}

func publicResolver(string) ([]string, error) { return []string{"93.184.216.34"}, nil }

func TestHttpRequestHappyAndSSRF(t *testing.T) {
	e, svc, _ := newM7RuntimeEngineHarness(t)
	ctx := context.Background()
	svc.SetHTTPDoer(fakeHTTPDoer{status: 200, body: []byte("hello m7")})
	svc.SetResolver(publicResolver)

	ok := e.Handle(ctx, m7Request(bridge.MethodHttpRequest,
		`{"runId":"run-http","method":"GET","url":"https://example.com/data","timeoutMs":5000,`+
			`"maxResponseBytes":4096,"requestId":"hr1"}`, "idem-hr1"))
	var out struct {
		Status     int    `json:"status"`
		Body       string `json:"body"`
		BodyDigest string `json:"bodyDigest"`
		Bytes      int64  `json:"bytes"`
		Truncated  bool   `json:"truncated"`
	}
	m7Decode(t, ok, &out)
	if out.Status != 200 || out.Body != "hello m7" || out.Bytes != 8 || len(out.BodyDigest) != 64 || out.Truncated {
		t.Fatalf("unexpected http.request: %+v", out)
	}

	// loopback target is refused by the SSRF contract (M7-TOOL-001)
	ssrf := e.Handle(ctx, m7Request(bridge.MethodHttpRequest,
		`{"runId":"run-http","method":"GET","url":"http://127.0.0.1:8080/admin","timeoutMs":5000,`+
			`"maxResponseBytes":4096,"requestId":"hr2"}`, "idem-hr2"))
	if code := m7ErrCode(t, ssrf); code != "M7-TOOL-001" {
		t.Fatalf("ssrf want M7-TOOL-001, got %s", code)
	}

	// write-method without review is policy-refused
	post := e.Handle(ctx, m7Request(bridge.MethodHttpRequest,
		`{"runId":"run-http","method":"POST","url":"https://example.com/api","timeoutMs":5000,`+
			`"maxResponseBytes":4096,"requestId":"hr3"}`, "idem-hr3"))
	if code := m7ErrCode(t, post); code != "FORBIDDEN_BY_POLICY" {
		t.Fatalf("unreviewed POST want FORBIDDEN_BY_POLICY, got %s", code)
	}
}

// fakeSQLQuerier echoes one fixed row without a database.
type fakeSQLQuerier struct{}

func (fakeSQLQuerier) Query(_ context.Context, _ string, _ string, _ []any, _ int) ([]string, [][]any, bool, error) {
	return []string{"id", "name"}, [][]any{{int64(1), "alpha"}}, false, nil
}

func TestDbQueryWhitelistAndConnectionGuards(t *testing.T) {
	e, svc, _ := newM7RuntimeEngineHarness(t)
	ctx := context.Background()
	svc.SetSQLQuerier(fakeSQLQuerier{})

	ok := e.Handle(ctx, m7Request(bridge.MethodDbQuery,
		`{"runId":"run-db","target":"sqlite","sql":"SELECT id, name FROM t WHERE id = ?","params":[1],`+
			`"maxRows":100,"timeoutMs":5000}`, ""))
	var out struct {
		Columns      []string `json:"columns"`
		RowCount     int      `json:"rowCount"`
		ResultDigest string   `json:"resultDigest"`
		Truncated    bool     `json:"truncated"`
	}
	m7Decode(t, ok, &out)
	if len(out.Columns) != 2 || out.RowCount != 1 || len(out.ResultDigest) != 64 || out.Truncated {
		t.Fatalf("unexpected db.query: %+v", out)
	}

	// mutating statement refused by the whitelist parser (M7-TOOL-003)
	del := e.Handle(ctx, m7Request(bridge.MethodDbQuery,
		`{"runId":"run-db","target":"sqlite","sql":"DELETE FROM t WHERE id = 1",`+
			`"maxRows":100,"timeoutMs":5000}`, ""))
	if code := m7ErrCode(t, del); code != "M7-TOOL-003" {
		t.Fatalf("delete want M7-TOOL-003, got %s", code)
	}

	// unregistered external connection refused (M7-TOOL-004)
	ext := e.Handle(ctx, m7Request(bridge.MethodDbQuery,
		`{"runId":"run-db","target":"external:`+ulid.Make().String()+`","sql":"SELECT 1",`+
			`"maxRows":100,"timeoutMs":5000}`, ""))
	if code := m7ErrCode(t, ext); code != "M7-TOOL-004" {
		t.Fatalf("external want M7-TOOL-004, got %s", code)
	}
}

func TestArchivePackUnpackRoundTrip(t *testing.T) {
	e, _, _ := newM7RuntimeEngineHarness(t)
	ctx := context.Background()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data", "a.txt"), []byte("pack-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootJSON := strings.ReplaceAll(root, `\`, `\\`)

	packed := e.Handle(ctx, m7Request(bridge.MethodArchivePack,
		`{"runId":"run-ar","sources":["data"],"destPath":"out/bundle.zip","workspaceRoot":"`+rootJSON+`",`+
			`"format":"zip","maxEntries":100,"maxBytes":1048576,"requestId":"ap1"}`, "idem-ap1"))
	var pr struct {
		ArchivePath string `json:"archivePath"`
		EntryCount  int    `json:"entryCount"`
		SHA256      string `json:"sha256"`
		Bytes       int64  `json:"bytes"`
	}
	m7Decode(t, packed, &pr)
	if pr.EntryCount != 1 || pr.Bytes <= 0 || len(pr.SHA256) != 64 {
		t.Fatalf("unexpected pack: %+v", pr)
	}

	// escaping dest is refused at the confinement fence
	escape := e.Handle(ctx, m7Request(bridge.MethodArchivePack,
		`{"runId":"run-ar","sources":["data"],"destPath":"../escape.zip","workspaceRoot":"`+rootJSON+`",`+
			`"format":"zip","maxEntries":100,"maxBytes":1048576,"requestId":"ap2"}`, "idem-ap2"))
	if code := m7ErrCode(t, escape); code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("escape want BRIDGE_SCHEMA_INVALID, got %s", code)
	}

	unpacked := e.Handle(ctx, m7Request(bridge.MethodArchiveUnpack,
		`{"runId":"run-ar","archivePath":"out/bundle.zip","destDir":"restored","workspaceRoot":"`+rootJSON+`",`+
			`"maxEntries":100,"maxBytes":1048576,"requestId":"au1"}`, "idem-au1"))
	var ur struct {
		EntryCount int   `json:"entryCount"`
		TotalBytes int64 `json:"totalBytes"`
	}
	m7Decode(t, unpacked, &ur)
	if ur.EntryCount != 1 || ur.TotalBytes != 7 {
		t.Fatalf("unexpected unpack: %+v", ur)
	}
	data, err := os.ReadFile(filepath.Join(root, "restored", "data", "a.txt"))
	if err != nil || string(data) != "pack-me" {
		t.Fatalf("restored content mismatch: %q %v", data, err)
	}
}

func TestDocumentParseFormatGuard(t *testing.T) {
	e, _, _ := newM7RuntimeEngineHarness(t)
	ctx := context.Background()
	root := t.TempDir()
	doc := filepath.Join(root, "doc.pdf")
	if err := os.WriteFile(doc, []byte("%PDF-1.4 stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	rootJSON := strings.ReplaceAll(root, `\`, `\\`)

	bad := e.Handle(ctx, m7Request(bridge.MethodDocumentParse,
		`{"runId":"run-doc","fileRef":"doc.pdf","workspaceRoot":"`+rootJSON+`","format":"txt",`+
			`"maxOutputBytes":4096}`, ""))
	if code := m7ErrCode(t, bad); code != "M7-TOOL-005" {
		t.Fatalf("format guard want M7-TOOL-005, got %s", code)
	}
}

// fakeGitRunner answers a fixed status payload.
type fakeGitRunner struct{}

func (fakeGitRunner) Read(_ context.Context, _ string, _ string, _ string, _ int64) ([]byte, error) {
	return []byte("## main...origin/main\n"), nil
}

func TestGitReadGuardAndHappyPath(t *testing.T) {
	e, svc, _ := newM7RuntimeEngineHarness(t)
	ctx := context.Background()
	svc.SetGitRunner(fakeGitRunner{})
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "repo", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	rootJSON := strings.ReplaceAll(root, `\`, `\\`)

	bad := e.Handle(ctx, m7Request(bridge.MethodGitRead,
		`{"runId":"run-git","repoPath":"repo","workspaceRoot":"`+rootJSON+`","op":"push",`+
			`"maxOutputBytes":4096,"requestId":"gr1"}`, "idem-gr1"))
	if code := m7ErrCode(t, bad); code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("op guard want BRIDGE_SCHEMA_INVALID, got %s", code)
	}

	ok := e.Handle(ctx, m7Request(bridge.MethodGitRead,
		`{"runId":"run-git","repoPath":"repo","workspaceRoot":"`+rootJSON+`","op":"status",`+
			`"maxOutputBytes":4096,"requestId":"gr2"}`, "idem-gr2"))
	var out struct {
		Output       string `json:"output"`
		OutputDigest string `json:"outputDigest"`
		Bytes        int64  `json:"bytes"`
	}
	m7Decode(t, ok, &out)
	if !strings.Contains(out.Output, "main") || len(out.OutputDigest) != 64 || out.Bytes == 0 {
		t.Fatalf("unexpected git.read: %+v", out)
	}
}

// ── slice 8: mcp.add / list / toggle / health / market.search ───────────────

// scriptedProber answers a queue of digests, one per probe.
type scriptedProber struct {
	digests []string
	calls   int
}

func (p *scriptedProber) Probe(_ context.Context, _ m7flow.McpEndpointConfig) (string, error) {
	if p.calls >= len(p.digests) {
		return "", errors.New("probe exhausted")
	}
	d := p.digests[p.calls]
	p.calls++
	if d == "" {
		return "", errors.New("probe failed")
	}
	return d, nil
}

func TestMcpAddListToggleHealthFlow(t *testing.T) {
	e, _, mcpSvc := newM7RuntimeEngineHarness(t)
	ctx := context.Background()
	probe := &scriptedProber{digests: []string{strings.Repeat("aa", 32), strings.Repeat("aa", 32), strings.Repeat("aa", 32), strings.Repeat("aa", 32)}}
	mcpSvc.SetProber(probe)

	added := e.Handle(ctx, m7Request(bridge.MethodMcpAdd,
		`{"origin":"manual","url":"https://mcp.example.com/sse","riskConfirmed":true,`+
			`"requestId":"ma1","actor":"op-1"}`, "idem-ma1"))
	var ar struct {
		EndpointID       string `json:"endpointId"`
		State            string `json:"state"`
		CapabilityDigest string `json:"capabilityDigest"`
	}
	m7Decode(t, added, &ar)
	if !strings.HasPrefix(ar.EndpointID, "mcp-") || ar.State != m7flow.McpStateReady ||
		ar.CapabilityDigest != strings.Repeat("aa", 32) {
		t.Fatalf("unexpected add: %+v", ar)
	}

	// fingerprint replay answers the original endpoint id
	replay := e.Handle(ctx, m7Request(bridge.MethodMcpAdd,
		`{"origin":"manual","url":"https://mcp.example.com/sse","riskConfirmed":true,`+
			`"requestId":"ma1b","actor":"op-1"}`, "idem-ma1b"))
	var rr struct {
		EndpointID string `json:"endpointId"`
	}
	m7Decode(t, replay, &rr)
	if rr.EndpointID != ar.EndpointID {
		t.Fatalf("replay endpoint drift: %s vs %s", rr.EndpointID, ar.EndpointID)
	}

	list := e.Handle(ctx, m7Request(bridge.MethodMcpList, `{"transport":"https"}`, ""))
	var lr struct {
		Endpoints []struct {
			EndpointID string `json:"endpointId"`
			State      string `json:"state"`
			Enabled    bool   `json:"enabled"`
		} `json:"endpoints"`
	}
	m7Decode(t, list, &lr)
	if len(lr.Endpoints) != 1 || lr.Endpoints[0].EndpointID != ar.EndpointID || lr.Endpoints[0].Enabled {
		t.Fatalf("unexpected list: %+v", lr)
	}

	toggled := e.Handle(ctx, m7Request(bridge.MethodMcpToggle,
		`{"endpointId":"`+ar.EndpointID+`","enabled":true,"actor":"op-1"}`, ""))
	var tr struct {
		Enabled bool `json:"enabled"`
	}
	m7Decode(t, toggled, &tr)
	if !tr.Enabled {
		t.Fatal("toggle failed")
	}

	health := e.Handle(ctx, m7Request(bridge.MethodMcpHealth,
		`{"endpointId":"`+ar.EndpointID+`"}`, ""))
	var hr struct {
		State         string `json:"state"`
		DriftDetected bool   `json:"driftDetected"`
	}
	m7Decode(t, health, &hr)
	if hr.State != m7flow.McpStateReady || hr.DriftDetected {
		t.Fatalf("unexpected health: %+v", hr)
	}
}

func TestMcpAddTwoStdioNpxServersStayDistinct(t *testing.T) {
	e, _, _ := newM7RuntimeEngineHarness(t)
	ctx := context.Background()
	first := e.Handle(ctx, m7Request(bridge.MethodMcpAdd,
		`{"origin":"manual","transport":"stdio","command":"npx","args":["-y","@modelcontextprotocol/server-memory"],`+
			`"riskConfirmed":true,"requestId":"npx-1"}`, "idem-npx-1"))
	second := e.Handle(ctx, m7Request(bridge.MethodMcpAdd,
		`{"origin":"manual","transport":"stdio","command":"npx","args":["-y","@modelcontextprotocol/server-sequential-thinking"],`+
			`"riskConfirmed":true,"requestId":"npx-2"}`, "idem-npx-2"))
	var a, b struct {
		EndpointID string `json:"endpointId"`
	}
	m7Decode(t, first, &a)
	m7Decode(t, second, &b)
	if a.EndpointID == "" || a.EndpointID == b.EndpointID {
		t.Fatalf("stdio npx fingerprints collided: %q %q", a.EndpointID, b.EndpointID)
	}
}

func TestMcpAddRemembersCuratedPresetID(t *testing.T) {
	dir := t.TempDir()
	e, _, _ := newM7RuntimeEngineHarness(t)
	e.SetPersistDir(dir)
	added := e.Handle(context.Background(), m7Request(bridge.MethodMcpAdd,
		`{"origin":"manual","transport":"stdio","command":"npx","args":["-y","@playwright/mcp"],`+
			`"riskConfirmed":true,"requestId":"preset-pw"}`, "idem-preset-pw"))
	var ar struct {
		EndpointID string `json:"endpointId"`
	}
	m7Decode(t, added, &ar)
	if ar.EndpointID == "" {
		t.Fatal("add returned empty endpoint")
	}
	if got := e.endpointPresetID(ar.EndpointID); got != "playwright" {
		t.Fatalf("preset after add = %q", got)
	}
	second := NewEngine(nil, "test")
	second.SetPersistDir(dir)
	if got := second.endpointPresetID(ar.EndpointID); got != "playwright" {
		t.Fatalf("preset after reload = %q", got)
	}
}

func TestMcpAddGuardFamily(t *testing.T) {
	e, _, _ := newM7RuntimeEngineHarness(t)
	ctx := context.Background()

	cases := []struct {
		name    string
		payload string
		code    string
	}{
		{"noconfirm", `{"origin":"manual","url":"https://mcp.example.com/sse",` +
			`"requestId":"g1"}`, "M7-MCP-002"},
		{"marketmissing", `{"origin":"market","marketItemId":"` + ulid.Make().String() + `",` +
			`"transport":"stdio","command":"npx","args":["-y","mcp-demo"],"requestId":"g2"}`, "M7-MCP-002"},
		{"cmdnotwhitelist", `{"origin":"manual","transport":"stdio","command":"bash",` +
			`"args":["-c","echo"],"riskConfirmed":true,"requestId":"g3"}`, "M7-MCP-001"},
		{"argmetachar", `{"origin":"manual","transport":"stdio","command":"npx",` +
			`"args":["-y","a;rm -rf /"],"riskConfirmed":true,"requestId":"g4"}`, "M7-MCP-001"},
		{"plainhttp", `{"origin":"manual","url":"http://mcp.example.com/sse",` +
			`"riskConfirmed":true,"requestId":"g5"}`, "M7-MCP-001"},
	}
	for _, tc := range cases {
		resp := e.Handle(ctx, m7Request(bridge.MethodMcpAdd, tc.payload, "idem-"+tc.name))
		if code := m7ErrCode(t, resp); code != tc.code {
			t.Fatalf("%s: want %s, got %s", tc.name, tc.code, code)
		}
	}
}

func TestMcpHealthDriftQuarantines(t *testing.T) {
	e, _, mcpSvc := newM7RuntimeEngineHarness(t)
	ctx := context.Background()
	probe := &scriptedProber{digests: []string{strings.Repeat("aa", 32), strings.Repeat("bb", 32)}}
	mcpSvc.SetProber(probe)

	added := e.Handle(ctx, m7Request(bridge.MethodMcpAdd,
		`{"origin":"manual","url":"https://mcp.example.com/sse","riskConfirmed":true,`+
			`"requestId":"dq1"}`, "idem-dq1"))
	var ar struct {
		EndpointID string `json:"endpointId"`
	}
	m7Decode(t, added, &ar)

	// second probe observes a different capability digest -> quarantine
	drift := e.Handle(ctx, m7Request(bridge.MethodMcpHealth,
		`{"endpointId":"`+ar.EndpointID+`"}`, ""))
	var hr struct {
		State         string `json:"state"`
		DriftDetected bool   `json:"driftDetected"`
	}
	m7Decode(t, drift, &hr)
	if hr.State != m7flow.McpStateQuarantined || !hr.DriftDetected {
		t.Fatalf("drift must quarantine: %+v", hr)
	}
}

// signedMarketItem builds one catalog row whose signature satisfies the
// default verifier (sha256 over id|catalogDigest).
func signedMarketItem(name string) m7flow.McpMarketItem {
	it := m7flow.McpMarketItem{
		ID:                ulid.Make().String(),
		Name:              name,
		Publisher:         "lunitide-test",
		Description:       "test catalog item " + name,
		TransportHint:     m7flow.McpTransportStdio,
		InstallConfigJSON: `{"command":"npx","args":["-y","` + name + `"]}`,
		CatalogDigest:     strings.Repeat("11", 32),
		FetchedAt:         time.Now().UTC().Format(time.RFC3339),
	}
	sum := sha256.Sum256([]byte(it.ID + "|" + it.CatalogDigest))
	it.Signature = hex.EncodeToString(sum[:])
	return it
}

func TestMcpMarketSearchFreshAndDegraded(t *testing.T) {
	e, _, mcpSvc := newM7RuntimeEngineHarness(t)
	ctx := context.Background()
	item := signedMarketItem("mcp-demo")
	mcpSvc.SetRegistry(func(context.Context) ([]m7flow.McpMarketItem, error) {
		return []m7flow.McpMarketItem{item}, nil
	})

	fresh := e.Handle(ctx, m7Request(bridge.MethodMcpMarketSearch,
		`{"query":"demo","limit":10}`, ""))
	var fr struct {
		Items []struct {
			ItemID string `json:"itemId"`
			Name   string `json:"name"`
		} `json:"items"`
		Fresh bool `json:"fresh"`
	}
	m7Decode(t, fresh, &fr)
	if !fr.Fresh || len(fr.Items) != 1 || fr.Items[0].ItemID != item.ID {
		t.Fatalf("unexpected fresh search: %+v", fr)
	}

	// registry outage degrades to the read-only cache (M7-MCP-005 semantics)
	mcpSvc.SetRegistry(func(context.Context) ([]m7flow.McpMarketItem, error) {
		return nil, fmt.Errorf("registry down")
	})
	cached := e.Handle(ctx, m7Request(bridge.MethodMcpMarketSearch,
		`{"query":"demo","limit":10}`, ""))
	var cr struct {
		Items []struct {
			ItemID string `json:"itemId"`
		} `json:"items"`
		Fresh bool `json:"fresh"`
	}
	m7Decode(t, cached, &cr)
	if cr.Fresh || len(cr.Items) != 1 || cr.Items[0].ItemID != item.ID {
		t.Fatalf("degraded search must serve cache: %+v", cr)
	}
}

// TestMcpMarketSignedInstallWalksSignedPath exercises the market-origin add
// path: a verified catalog item installs into probe state with signed trust.
func TestMcpMarketSignedInstallWalksSignedPath(t *testing.T) {
	e, _, mcpSvc := newM7RuntimeEngineHarness(t)
	ctx := context.Background()
	item := signedMarketItem("mcp-signed")
	mcpSvc.SetRegistry(func(context.Context) ([]m7flow.McpMarketItem, error) {
		return []m7flow.McpMarketItem{item}, nil
	})
	mcpSvc.SetProber(&scriptedProber{digests: []string{strings.Repeat("aa", 32), strings.Repeat("aa", 32)}})
	// warm the cache
	if _, _, err := mcpSvc.MarketSearch(ctx, "signed", "", 10); err != nil {
		t.Fatal(err)
	}

	added := e.Handle(ctx, m7Request(bridge.MethodMcpAdd,
		`{"origin":"market","marketItemId":"`+item.ID+`","transport":"stdio","command":"npx",`+
			`"args":["-y","mcp-signed"],"requestId":"ms1"}`, "idem-ms1"))
	var ar struct {
		EndpointID string `json:"endpointId"`
		State      string `json:"state"`
	}
	m7Decode(t, added, &ar)
	if !strings.HasPrefix(ar.EndpointID, "mcp-") || ar.State != m7flow.McpStateReady {
		t.Fatalf("signed market install must reach ready: %+v", ar)
	}
}
