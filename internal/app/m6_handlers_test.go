package app

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/extension"
	"github.com/lunitide/lunitide/internal/m6app"
	"github.com/lunitide/lunitide/internal/mcp6"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

// m6Harness wires a real store, an ed25519-signed artifact catalog, the
// extension verifier and the mcp6 registry behind the bridge handlers.
type m6Harness struct {
	e    *Engine
	repo *storage.AgentRuntimeRepository
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

func newM6Harness(t *testing.T) *m6Harness {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "m6.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	verifier := &extension.Verifier{
		TrustedKeys:       map[string]ed25519.PublicKey{"m6-test-key": pub},
		RevokedKeys:       map[string]bool{},
		RevokedPublishers: map[string]bool{},
		RevokedDigests:    map[string]bool{},
		Policy:            extension.Policy{LicenseAllowlist: []string{"MIT"}, RuntimeAllowlist: []string{"wasm"}},
	}
	repo := store.AgentRuntimeRepository()
	invoke := func(ctx context.Context, e *mcp6.Endpoint, tool string, args map[string]any, auth []byte) (map[string]any, error) {
		return map[string]any{"ok": true, "tool": tool}, nil
	}
	e := NewEngine(nil, "test")
	e.SetM6Services(m6app.NewExtensionService(repo, verifier), mcp6.NewRegistry(nil, invoke, m6FakeLease{}), m6app.NewEndpointService(repo))
	return &m6Harness{e: e, repo: repo, pub: pub, priv: priv}
}

type m6FakeLease struct{}

func (m6FakeLease) WithLease(ctx context.Context, authRef string, fn func(auth []byte) error) error {
	return fn([]byte("lease-bytes"))
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// signedManifest builds a complete manifest with a valid ed25519 signature
// over the canonical body + artifact digest.
func (h *m6Harness) signedManifest(t *testing.T, name, version, digest string) extension.Manifest {
	t.Helper()
	m := extension.Manifest{
		SchemaVersion: "1", SkillID: "skill-" + name, Name: name, Version: version,
		Publisher: "acme", Description: "test skill", Entrypoint: "main.wasm",
		InputSchema: map[string]any{"type": "object"}, OutputSchema: map[string]any{"type": "object"},
		Permissions: []string{"fs.read"}, Runtime: "wasm", MinHostVersion: "6.0.0",
		ArtifactDigest: digest, License: "MIT", SBOMRef: "sbom://acme/" + name + "/" + version,
		Triggers:       []string{"onInvoke"},
		Dependencies:   []extension.Dependency{{Name: "dep", Version: "1.0.0", Digest: sha256Hex("dep")}},
		TimeoutMS:      30000,
		ResourceLimits: extension.ResourceLimits{CPUMillis: 500, MemoryMB: 128, DiskMB: 64, Processes: 1, Network: false},
		Signature:      extension.Signature{KeyID: "m6-test-key", Alg: "ed25519"},
	}
	body, err := m.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	content := append(body, []byte(digest)...)
	m.Signature.Value = base64.StdEncoding.EncodeToString(ed25519.Sign(h.priv, content))
	return m
}

// seedArtifact persists a verified artifact row.
func (h *m6Harness) seedArtifact(t *testing.T, m extension.Manifest, digest, risk string) string {
	t.Helper()
	manifestJSON, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	id := ulid.Make().String()
	err = h.repo.TransactM6(context.Background(), func(tx m6app.Tx) error {
		return tx.PutM6Artifact(m6supply.Artifact{
			ID: id, Name: m.Name, Publisher: m.Publisher, Version: m.Version, Digest: digest,
			SignatureState: m6supply.SignatureVerified, SBOMRef: m.SBOMRef,
			ManifestJSON: string(manifestJSON), Risk: risk,
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func m6Request(method bridge.Method, payload, key string) bridge.Request {
	r := validRequest(string(method), payload)
	r.IdempotencyKey = key
	return r
}

func m6Payload(t *testing.T, r bridge.Response, target any) {
	t.Helper()
	if !r.OK {
		t.Fatalf("request failed: code=%s msg=%s", r.Error.Code, r.Error.Message)
	}
	body, _ := json.Marshal(r.Payload)
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatal(err)
	}
}

func (h *m6Harness) search(t *testing.T, query string) []m6app.SearchItem {
	t.Helper()
	var out struct {
		Items []m6app.SearchItem `json:"items"`
	}
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodExtensionSearch, `{"query":"`+query+`","scope":"personal"}`, ""))
	m6Payload(t, resp, &out)
	return out.Items
}

func TestExtensionSearchFiltersRisk(t *testing.T) {
	h := newM6Harness(t)
	digest := sha256Hex("bytes-low")
	id := h.seedArtifact(t, h.signedManifest(t, "pdf-tools", "1.2.0", digest), digest, m6supply.RiskLow)
	highDigest := sha256Hex("bytes-high")
	h.seedArtifact(t, h.signedManifest(t, "crypto-miner", "9.9.9", highDigest), highDigest, m6supply.RiskHigh)

	items := h.search(t, "pdf")
	if len(items) != 1 || items[0].ArtifactID != id || items[0].Risk != "low" {
		t.Fatalf("want only the low-risk artifact, got %+v", items)
	}
	if len(items[0].Permissions) != 1 || items[0].Permissions[0] != "fs.read" {
		t.Fatalf("permissions not surfaced: %+v", items[0].Permissions)
	}
	if got := h.search(t, "crypto"); len(got) != 0 {
		t.Fatalf("high-risk artifact must never surface, got %+v", got)
	}
}

func TestExtensionSearchRejectsBadScope(t *testing.T) {
	h := newM6Harness(t)
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodExtensionSearch, `{"query":"x","scope":"global"}`, ""))
	if resp.OK || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("want schema rejection, got %+v", resp.Error)
	}
}

func (h *m6Harness) install(t *testing.T, artifactID, version, deltaDigest, key string) (string, string) {
	t.Helper()
	payload := `{"artifactId":"` + artifactID + `","version":"` + version + `","permissionGrant":{"granted":["fs.read"],"confirmedDeltaDigest":"` + deltaDigest + `"}}`
	var out struct {
		InstallID string `json:"installId"`
		State     string `json:"state"`
	}
	m6Payload(t, h.e.Handle(context.Background(), m6Request(bridge.MethodExtensionInstall, payload, key)), &out)
	return out.InstallID, out.State
}

func TestExtensionInstallHappyPathAndIdempotency(t *testing.T) {
	h := newM6Harness(t)
	digest := sha256Hex("happy-bytes")
	id := h.seedArtifact(t, h.signedManifest(t, "pdf-tools", "1.2.0", digest), digest, m6supply.RiskLow)
	delta := extension.DeltaDigest([]string{"fs.read"})

	installID, state := h.install(t, id, "1.2.0", delta, "key-1")
	if state != "installed" || installID == "" {
		t.Fatalf("want installed, got %q %q", state, installID)
	}
	// Replayed key returns the same install.
	replayID, replayState := h.install(t, id, "1.2.0", delta, "key-1")
	if replayID != installID || replayState != "installed" {
		t.Fatalf("idempotent replay diverged: %q %q", replayID, replayState)
	}
	// Same key, different request → conflict.
	payload := `{"artifactId":"` + id + `","version":"1.2.0","permissionGrant":{"granted":["fs.write"],"confirmedDeltaDigest":"` + delta + `"}}`
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodExtensionInstall, payload, "key-1"))
	if resp.OK || resp.Error.Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("want idempotency conflict, got %+v", resp.Error)
	}
}

// M6-EXT-001: a manifest pinned to digest A persisted under digest B is
// quarantined durably (success-shaped response with state=quarantined).
func TestExtensionInstallQuarantinesDigestMismatch(t *testing.T) {
	h := newM6Harness(t)
	pinned := sha256Hex("pinned-bytes")
	stored := sha256Hex("stored-bytes")
	id := h.seedArtifact(t, h.signedManifest(t, "pdf-tools", "1.2.0", pinned), stored, m6supply.RiskLow)
	delta := extension.DeltaDigest([]string{"fs.read"})
	_, state := h.install(t, id, "1.2.0", delta, "key-mm")
	if state != "quarantined" {
		t.Fatalf("want quarantined on digest mismatch (M6-EXT-001), got %q", state)
	}
}
func TestExtensionInstallRejectsWrongDeltaConfirmation(t *testing.T) {
	h := newM6Harness(t)
	digest := sha256Hex("delta-bytes")
	id := h.seedArtifact(t, h.signedManifest(t, "pdf-tools", "1.2.0", digest), digest, m6supply.RiskLow)
	wrongDelta := sha256Hex("not-the-confirmed-delta")
	_, state := h.install(t, id, "1.2.0", wrongDelta, "key-delta")
	if state != "quarantined" {
		t.Fatalf("want quarantined on unconfirmed delta (M6-EXT-004), got %q", state)
	}
}

func TestExtensionInstallUnknownArtifact(t *testing.T) {
	h := newM6Harness(t)
	delta := extension.DeltaDigest([]string{"fs.read"})
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodExtensionInstall,
		`{"artifactId":"`+ulid.Make().String()+`","version":"1.0.0","permissionGrant":{"granted":["fs.read"],"confirmedDeltaDigest":"`+delta+`"}}`, "key-404"))
	if resp.OK || resp.Error.Code != "EXTENSION_ARTIFACT_NOT_FOUND" {
		t.Fatalf("want artifact not found, got %+v", resp.Error)
	}
}

func TestExtensionLifecycleFlow(t *testing.T) {
	h := newM6Harness(t)
	digest := sha256Hex("lifecycle-bytes")
	id := h.seedArtifact(t, h.signedManifest(t, "pdf-tools", "1.2.0", digest), digest, m6supply.RiskLow)
	delta := extension.DeltaDigest([]string{"fs.read"})
	installID, _ := h.install(t, id, "1.2.0", delta, "key-lc")

	lc := func(op, targetVersion, key string) string {
		t.Helper()
		payload := `{"installId":"` + installID + `","op":"` + op + `"`
		if targetVersion != "" {
			payload += `,"targetVersion":"` + targetVersion + `"`
		}
		payload += `}`
		var out struct {
			State string `json:"state"`
		}
		m6Payload(t, h.e.Handle(context.Background(), m6Request(bridge.MethodExtensionLifecycle, payload, key)), &out)
		return out.State
	}

	if s := lc("enable", "", "k1"); s != "enabled" {
		t.Fatalf("enable: %q", s)
	}
	if s := lc("pause", "", "k2"); s != "paused" {
		t.Fatalf("pause: %q", s)
	}
	if s := lc("enable", "", "k3"); s != "enabled" {
		t.Fatalf("re-enable: %q", s)
	}
	if s := lc("uninstall", "", "k4"); s != "uninstalled" {
		t.Fatalf("uninstall: %q", s)
	}
	// Terminal install rejects further ops.
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodExtensionLifecycle,
		`{"installId":"`+installID+`","op":"enable"}`, "k5"))
	if resp.OK || resp.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("want rejection on terminal install, got %+v", resp.Error)
	}
}

// Lifecycle upgrade repoints to another verified version of the same skill.
func TestExtensionLifecycleUpgrade(t *testing.T) {
	h := newM6Harness(t)
	d1, d2 := sha256Hex("v1-bytes"), sha256Hex("v2-bytes")
	id1 := h.seedArtifact(t, h.signedManifest(t, "pdf-tools", "1.2.0", d1), d1, m6supply.RiskLow)
	h.seedArtifact(t, h.signedManifest(t, "pdf-tools", "2.0.0", d2), d2, m6supply.RiskLow)
	delta := extension.DeltaDigest([]string{"fs.read"})
	installID, _ := h.install(t, id1, "1.2.0", delta, "key-up")

	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodExtensionLifecycle,
		`{"installId":"`+installID+`","op":"upgrade","targetVersion":"2.0.0"}`, "key-up2"))
	var out struct {
		State string `json:"state"`
	}
	m6Payload(t, resp, &out)
	if out.State != "installed" {
		t.Fatalf("upgrade: %q", out.State)
	}
	// Unknown target version is rejected.
	h2 := h.e.Handle(context.Background(), m6Request(bridge.MethodExtensionLifecycle,
		`{"installId":"`+installID+`","op":"upgrade","targetVersion":"3.0.0"}`, "key-up3"))
	if h2.OK || h2.Error.Code != "EXTENSION_ARTIFACT_NOT_FOUND" {
		t.Fatalf("want artifact not found for unknown target, got %+v", h2.Error)
	}
}

func TestMcp6RegisterInvokeRevokeRoundTrip(t *testing.T) {
	h := newM6Harness(t)
	toolDigest := sha256Hex("tool-schema")
	reg := h.e.Handle(context.Background(), m6Request(bridge.MethodMcp6Register,
		`{"endpoint":{"transport":"https","url":"https://mcp.example.com/sse","authRef":"secretref:pool/a"},"capabilityPin":{"serverIdentityDigest":"`+sha256Hex("srv")+`","toolSchemaDigests":{"searchDocs":"`+toolDigest+`"}}}`, ""))
	var regOut struct {
		EndpointID string `json:"endpointId"`
		State      string `json:"state"`
	}
	m6Payload(t, reg, &regOut)
	if regOut.State != "ready" {
		t.Fatalf("want ready, got %q", regOut.State)
	}

	var inv struct {
		Result map[string]any `json:"result"`
		Trace  string         `json:"traceId"`
		Bytes  int            `json:"bytes"`
	}
	m6Payload(t, h.e.Handle(context.Background(), m6Request(bridge.MethodMcp6Invoke,
		`{"endpointId":"`+regOut.EndpointID+`","tool":"searchDocs","args":{"q":"durable"},"idempotencyKey":"c1","deadlineMs":5000}`, "")), &inv)
	if inv.Result["ok"] != true || inv.Trace == "" || inv.Bytes <= 0 {
		t.Fatalf("invoke result bad: %+v", inv)
	}

	var rev struct {
		State        string `json:"state"`
		PoolsCleared bool   `json:"poolsCleared"`
	}
	m6Payload(t, h.e.Handle(context.Background(), m6Request(bridge.MethodMcp6Revoke,
		`{"endpointId":"`+regOut.EndpointID+`","reason":"drift"}`, "")), &rev)
	if rev.State != "revoked" || !rev.PoolsCleared {
		t.Fatalf("revoke: %+v", rev)
	}
	// Post-revoke invoke answers the revoked code.
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodMcp6Invoke,
		`{"endpointId":"`+regOut.EndpointID+`","tool":"searchDocs","args":{},"idempotencyKey":"c2","deadlineMs":5000}`, ""))
	if resp.OK || resp.Error.Code != mcp6.CodeCredentialRevoked && resp.Error.Code != "MCP6_ENDPOINT_REVOKED" {
		t.Fatalf("want revoked rejection, got %+v", resp.Error)
	}
}

// M6-MCP-004 (gate opened 2026-08-16): stdio registers through the
// whitelist — non-whitelisted commands still refuse; the admitted shape
// registers and answers ready.
func TestMcp6RegisterStdio(t *testing.T) {
	h := newM6Harness(t)
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodMcp6Register,
		`{"endpoint":{"transport":"stdio","command":"bash","args":["-c","ls"]},"capabilityPin":{"serverIdentityDigest":"`+sha256Hex("srv")+`","toolSchemaDigests":{"t":"`+sha256Hex("t")+`"}}}`, ""))
	if resp.OK || resp.Error.Code != mcp6.CodeStdioDisabled {
		t.Fatalf("want M6-MCP-004 for non-whitelisted command, got %+v", resp.Error)
	}
	badShape := h.e.Handle(context.Background(), m6Request(bridge.MethodMcp6Register,
		`{"endpoint":{"transport":"stdio","url":"https://mcp.example.com/sse"},"capabilityPin":{"serverIdentityDigest":"`+sha256Hex("srv")+`","toolSchemaDigests":{"t":"`+sha256Hex("t")+`"}}}`, ""))
	if badShape.OK || badShape.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("want schema rejection for stdio without command/args, got %+v", badShape.Error)
	}
	ok := h.e.Handle(context.Background(), m6Request(bridge.MethodMcp6Register,
		`{"endpoint":{"transport":"stdio","command":"npx","args":["-y","@modelcontextprotocol/server-everything"]},"capabilityPin":{"serverIdentityDigest":"`+sha256Hex("srv")+`","toolSchemaDigests":{"t":"`+sha256Hex("t")+`"}}}`, ""))
	var regOut struct {
		EndpointID string `json:"endpointId"`
		State      string `json:"state"`
	}
	m6Payload(t, ok, &regOut)
	if regOut.State != "ready" {
		t.Fatalf("want ready stdio endpoint, got %q", regOut.State)
	}
}

// M6-MCP-002: invoking an unpinned tool degrades the endpoint.
func TestMcp6InvokeUnpinnedTool(t *testing.T) {
	h := newM6Harness(t)
	var regOut struct {
		EndpointID string `json:"endpointId"`
	}
	m6Payload(t, h.e.Handle(context.Background(), m6Request(bridge.MethodMcp6Register,
		`{"endpoint":{"transport":"https","url":"https://mcp.example.com/sse","authRef":"secretref:pool/a"},"capabilityPin":{"serverIdentityDigest":"`+sha256Hex("srv")+`","toolSchemaDigests":{"searchDocs":"`+sha256Hex("t")+`"}}}`, "")), &regOut)
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodMcp6Invoke,
		`{"endpointId":"`+regOut.EndpointID+`","tool":"dangerTool","args":{},"idempotencyKey":"c1","deadlineMs":5000}`, ""))
	if resp.OK || resp.Error.Code != mcp6.CodeCapabilityDrift {
		t.Fatalf("want M6-MCP-002, got %+v", resp.Error)
	}
	// Subsequent invoke on the degraded endpoint answers M6-MCP-001.
	resp2 := h.e.Handle(context.Background(), m6Request(bridge.MethodMcp6Invoke,
		`{"endpointId":"`+regOut.EndpointID+`","tool":"searchDocs","args":{},"idempotencyKey":"c2","deadlineMs":5000}`, ""))
	if resp2.OK || resp2.Error.Code != mcp6.CodeHealthCheckFailed {
		t.Fatalf("want M6-MCP-001 not-ready, got %+v", resp2.Error)
	}
}

func TestMcp6ServicesUnwired(t *testing.T) {
	e := NewEngine(nil, "test")
	for _, tc := range []struct {
		method  bridge.Method
		payload string
	}{
		{bridge.MethodExtensionSearch, `{"query":"x","scope":"personal"}`},
		{bridge.MethodExtensionInstall, `{"artifactId":"` + ulid.Make().String() + `","version":"1","permissionGrant":{"granted":["fs.read"],"confirmedDeltaDigest":"` + sha256Hex("d") + `"}}`},
		{bridge.MethodMcp6Register, `{"endpoint":{"transport":"https","url":"https://x.example.com/","authRef":"secretref:p/a"},"capabilityPin":{"serverIdentityDigest":"` + sha256Hex("s") + `","toolSchemaDigests":{"t":"` + sha256Hex("t") + `"}}}`},
	} {
		resp := e.Handle(context.Background(), m6Request(tc.method, tc.payload, ""))
		if resp.OK || resp.Error.Code != "STORAGE_UNAVAILABLE" && resp.Error.Code != "FEATURE_DISABLED" {
			t.Fatalf("%s unwired: %+v", tc.method, resp.Error)
		}
	}
}

// c3-mcp: mcp6.presets.list answers the curated catalog, every returned
// row clears the unchanged stdio admission gate via mcp6.register, and the
// unknown-field payload is rejected like any strict-schema method.
func TestMcp6PresetsList(t *testing.T) {
	h := newM6Harness(t)
	resp := h.e.Handle(context.Background(), m6Request(bridge.MethodMcp6PresetsList, `{}`, ""))
	var out struct {
		Items []mcp6.Preset `json:"items"`
	}
	m6Payload(t, resp, &out)
	if len(out.Items) < 30 || len(out.Items) > 48 {
		t.Fatalf("want curated ~40 live presets, got %d", len(out.Items))
	}
	byID := make(map[string]mcp6.Preset, len(out.Items))
	for _, it := range out.Items {
		byID[it.ID] = it
	}
	for _, id := range []string{"everything", "filesystem", "fetch", "memory", "sequentialthinking", "playwright", "time", "context7"} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("preset %s missing from bridge answer", id)
		}
	}
	for _, archived := range []string{"git", "github", "puppeteer", "sqlite"} {
		if _, ok := byID[archived]; ok {
			t.Fatalf("archived preset %s must not appear in bridge catalog", archived)
		}
	}
	if fs := byID["filesystem"]; !fs.NeedsArgs || fs.ArgPlaceholder != "{{dir}}" || fs.ArgHint == "" || fs.ArgDefault == "" || strings.Contains(fs.ArgDefault, `\`) {
		t.Fatalf("filesystem preset placeholder contract broken: %+v", fs)
	}
	if pg := byID["postgres"]; !pg.NeedsArgs || pg.ArgDefault != "" {
		t.Fatalf("secret/url presets must not receive a sandbox path default: %+v", pg)
	}
	if maps := byID["google-maps"]; !maps.NeedsArgs || maps.ArgPlaceholder != "{{key}}" || maps.ArgDefault != "" {
		t.Fatalf("API-key presets must not receive a sandbox path default: %+v", maps)
	}
	if drive := byID["gdrive"]; !drive.NeedsArgs || drive.ArgPlaceholder != "{{dir}}" || drive.ArgDefault == "" {
		t.Fatalf("gdrive credential dir should use the local sandbox default: %+v", drive)
	}

	// End-to-end: each catalog row (placeholder resolved to a benign path)
	// registers through the real mcp6.register admission path.
	for _, it := range out.Items {
		argsJSON, err := json.Marshal(it.ResolveArgs("C:/Users/demo/projects/sample"))
		if err != nil {
			t.Fatal(err)
		}
		reg := h.e.Handle(context.Background(), m6Request(bridge.MethodMcp6Register,
			`{"endpoint":{"transport":"stdio","command":"`+it.Command+`","args":`+string(argsJSON)+`},"capabilityPin":{"serverIdentityDigest":"`+sha256Hex("srv")+`","toolSchemaDigests":{"t":"`+sha256Hex("t")+`"}}}`, ""))
		var regOut struct {
			State string `json:"state"`
		}
		m6Payload(t, reg, &regOut)
		if regOut.State != "ready" {
			t.Fatalf("preset %s did not register ready: %q", it.ID, regOut.State)
		}
	}

	bad := h.e.Handle(context.Background(), m6Request(bridge.MethodMcp6PresetsList, `{"unexpected":true}`, ""))
	if bad.OK || bad.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("want schema rejection for unknown fields, got %+v", bad.Error)
	}
}
