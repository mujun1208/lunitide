package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/oklog/ulid/v2"
)

// fsFixture builds a workspace tree:
//
//	src/main.go, src/util/helper.go, src/bin.dat (binary)
//	docs/readme.md
//	secret.txt (outside the granted scope)
func fsFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel string, content []byte) {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/main.go", []byte("package main\n\nfunc main() {}\n"))
	write("src/util/helper.go", []byte("package util\n\nfunc Helper() string { return \"hi\" }\n"))
	write("src/bin.dat", []byte{0xff, 0xd8, 0x00, 0x01, 0x02})
	write("docs/readme.md", []byte("# Hello\nreadme body\n"))
	write("secret.txt", []byte("top secret\n"))
	return root
}

type fsLeaseOut struct {
	ID           string `json:"id"`
	FencingToken int64  `json:"fencingToken"`
}

// fsLease registers the root, grants ["src/**","docs/readme.md"] read scope
// and returns an active lease.
func fsLease(t *testing.T, e *Engine, root string, operations []string, paths []string, key string) fsLeaseOut {
	t.Helper()
	reg := decodePayloadInto[workspaceRegistrationOut](t, registerWorkspace(t, e, root, key+"-reg"))
	grant := decodePayloadInto[struct {
		ID string `json:"id"`
	}](t, grantWorkspace(t, e, reg.ID, paths, operations, 3600, key+"-grant"))
	return decodePayloadInto[fsLeaseOut](t, leaseWorkspace(t, e, grant.ID, 900, key+"-lease"))
}

func fsCall(e *Engine, method bridge.Method, payload map[string]any) bridge.Response {
	body, _ := json.Marshal(payload)
	return e.Handle(context.Background(), validRequest(string(method), string(body)))
}

func fsLeasePayload(lease fsLeaseOut) map[string]any {
	return map[string]any{"leaseId": lease.ID, "fencingToken": lease.FencingToken}
}

func TestFsTreeScopeFiltering(t *testing.T) {
	e, _, _ := agentRunEngine(t)
	root := fsFixture(t)
	lease := fsLease(t, e, root, []string{"read"}, []string{"src/**", "docs/readme.md"}, "fs-tree")

	p := fsLeasePayload(lease)
	res := fsCall(e, bridge.MethodFsTree, p)
	out := decodePayloadInto[struct {
		Path      string `json:"path"`
		Entries   []struct {
			Path string `json:"path"`
			Kind string `json:"kind"`
		} `json:"entries"`
		Truncated bool `json:"truncated"`
	}](t, res)
	paths := map[string]string{}
	for _, entry := range out.Entries {
		paths[entry.Path] = entry.Kind
	}
	for _, want := range []string{"src", "src/main.go", "src/util", "src/util/helper.go", "src/bin.dat", "docs", "docs/readme.md"} {
		if _, ok := paths[want]; !ok {
			t.Fatalf("tree missing %q: %+v", want, paths)
		}
	}
	if _, leaked := paths["secret.txt"]; leaked {
		t.Fatalf("tree leaked out-of-scope file: %+v", paths)
	}
	if out.Truncated {
		t.Fatalf("unexpected truncation: %+v", out)
	}
	if paths["src"] != "dir" || paths["src/main.go"] != "file" {
		t.Fatalf("kinds wrong: %+v", paths)
	}

	// A directory outside the scope is denied.
	denied := fsCall(e, bridge.MethodFsTree, map[string]any{
		"leaseId": lease.ID, "fencingToken": lease.FencingToken, "path": "docs",
	})
	// docs is visible as an ancestor of docs/readme.md, so it is allowed;
	// an unrelated path like ".git" must be denied.
	if !denied.OK {
		t.Fatalf("docs should be visible: %#v", denied)
	}
	outside := fsCall(e, bridge.MethodFsTree, map[string]any{
		"leaseId": lease.ID, "fencingToken": lease.FencingToken, "path": "other",
	})
	if outside.OK || outside.Error.Code != "FS_SCOPE_DENIED" {
		t.Fatalf("outside=%#v", outside)
	}
}

func TestFsStatReadAndReadMany(t *testing.T) {
	e, _, _ := agentRunEngine(t)
	root := fsFixture(t)
	lease := fsLease(t, e, root, []string{"read"}, []string{"src/**", "docs/readme.md"}, "fs-read")

	stat := decodePayloadInto[struct {
		Path      string `json:"path"`
		Kind      string `json:"kind"`
		SizeBytes int64  `json:"sizeBytes"`
		Digest    string `json:"digest"`
	}](t, fsCall(e, bridge.MethodFsStat, map[string]any{
		"leaseId": lease.ID, "fencingToken": lease.FencingToken, "path": "src/main.go",
	}))
	if stat.Kind != "file" || stat.SizeBytes != int64(len("package main\n\nfunc main() {}\n")) || len(stat.Digest) != 64 {
		t.Fatalf("stat=%+v", stat)
	}
	dirStat := decodePayloadInto[struct {
		Kind string `json:"kind"`
	}](t, fsCall(e, bridge.MethodFsStat, map[string]any{
		"leaseId": lease.ID, "fencingToken": lease.FencingToken, "path": "src",
	}))
	if dirStat.Kind != "dir" {
		t.Fatalf("dir stat=%+v", dirStat)
	}

	read := decodePayloadInto[struct {
		Content   string `json:"content"`
		SizeBytes int64  `json:"sizeBytes"`
		Digest    string `json:"digest"`
		Truncated bool   `json:"truncated"`
	}](t, fsCall(e, bridge.MethodFsRead, map[string]any{
		"leaseId": lease.ID, "fencingToken": lease.FencingToken, "path": "src/main.go",
	}))
	if read.Content != "package main\n\nfunc main() {}\n" || read.Truncated || len(read.Digest) != 64 {
		t.Fatalf("read=%+v", read)
	}

	// Truncation keeps UTF-8 validity and flags the partial content.
	truncated := decodePayloadInto[struct {
		Content   string `json:"content"`
		Truncated bool   `json:"truncated"`
	}](t, fsCall(e, bridge.MethodFsRead, map[string]any{
		"leaseId": lease.ID, "fencingToken": lease.FencingToken, "path": "src/main.go", "maxBytes": 8,
	}))
	if !truncated.Truncated || truncated.Content != "package " {
		t.Fatalf("truncated=%+v", truncated)
	}

	// Binary single read fails with FS_BINARY.
	binary := fsCall(e, bridge.MethodFsRead, map[string]any{
		"leaseId": lease.ID, "fencingToken": lease.FencingToken, "path": "src/bin.dat",
	})
	if binary.OK || binary.Error.Code != "FS_BINARY" {
		t.Fatalf("binary=%#v", binary)
	}

	many := decodePayloadInto[struct {
		Items []struct {
			Path   string `json:"path"`
			Status string `json:"status"`
		} `json:"items"`
	}](t, fsCall(e, bridge.MethodFsReadMany, map[string]any{
		"leaseId":      lease.ID,
		"fencingToken": lease.FencingToken,
		"paths":        []string{"src/main.go", "src/missing.go", "src", "src/bin.dat"},
	}))
	want := map[string]string{
		"src/main.go":    "ok",
		"src/missing.go": "not_found",
		"src":            "not_a_file",
		"src/bin.dat":    "binary",
	}
	if len(many.Items) != len(want) {
		t.Fatalf("readMany=%+v", many)
	}
	for _, item := range many.Items {
		if want[item.Path] != item.Status {
			t.Fatalf("item %s status=%s want %s", item.Path, item.Status, want[item.Path])
		}
	}

	// Out-of-scope paths fail the whole call closed.
	scopeDenied := fsCall(e, bridge.MethodFsReadMany, map[string]any{
		"leaseId": lease.ID, "fencingToken": lease.FencingToken, "paths": []string{"secret.txt"},
	})
	if scopeDenied.OK || scopeDenied.Error.Code != "FS_SCOPE_DENIED" {
		t.Fatalf("scopeDenied=%#v", scopeDenied)
	}
	// Path escape is rejected.
	escape := fsCall(e, bridge.MethodFsRead, map[string]any{
		"leaseId": lease.ID, "fencingToken": lease.FencingToken, "path": "../secret.txt",
	})
	if escape.OK || escape.Error.Code != "FS_PATH_INVALID" {
		t.Fatalf("escape=%#v", escape)
	}
}

func TestFsGlobAndGrep(t *testing.T) {
	e, _, _ := agentRunEngine(t)
	root := fsFixture(t)
	lease := fsLease(t, e, root, []string{"read"}, []string{"src/**", "docs/readme.md"}, "fs-search")

	glob := decodePayloadInto[struct {
		Matches   []string `json:"matches"`
		Truncated bool     `json:"truncated"`
	}](t, fsCall(e, bridge.MethodFsGlob, map[string]any{
		"leaseId": lease.ID, "fencingToken": lease.FencingToken, "pattern": "**/*.go",
	}))
	if len(glob.Matches) != 2 || glob.Matches[0] != "src/main.go" || glob.Matches[1] != "src/util/helper.go" {
		t.Fatalf("glob=%+v", glob)
	}
	// A pattern that would match out-of-scope files returns nothing.
	outside := decodePayloadInto[struct {
		Matches []string `json:"matches"`
	}](t, fsCall(e, bridge.MethodFsGlob, map[string]any{
		"leaseId": lease.ID, "fencingToken": lease.FencingToken, "pattern": "**/*.txt",
	}))
	if len(outside.Matches) != 0 {
		t.Fatalf("glob leaked: %+v", outside)
	}
	// Invalid patterns are rejected.
	badPattern := fsCall(e, bridge.MethodFsGlob, map[string]any{
		"leaseId": lease.ID, "fencingToken": lease.FencingToken, "pattern": "../**/*.go",
	})
	if badPattern.OK || badPattern.Error.Code != "FS_PATH_INVALID" {
		t.Fatalf("badPattern=%#v", badPattern)
	}

	grep := decodePayloadInto[struct {
		Matches []struct {
			Path string `json:"path"`
			Line int    `json:"line"`
			Text string `json:"text"`
		} `json:"matches"`
		Truncated bool `json:"truncated"`
	}](t, fsCall(e, bridge.MethodFsGrep, map[string]any{
		"leaseId": lease.ID, "fencingToken": lease.FencingToken, "pattern": "func \\w+",
	}))
	if len(grep.Matches) != 2 || grep.Truncated {
		t.Fatalf("grep=%+v", grep)
	}
	if grep.Matches[0].Path != "src/main.go" || grep.Matches[0].Line != 3 {
		t.Fatalf("grep match=%+v", grep.Matches[0])
	}
	// Restricted to a subdirectory.
	scoped := decodePayloadInto[struct {
		Matches []struct {
			Path string `json:"path"`
		} `json:"matches"`
	}](t, fsCall(e, bridge.MethodFsGrep, map[string]any{
		"leaseId": lease.ID, "fencingToken": lease.FencingToken, "pattern": "func", "path": "src/util",
	}))
	if len(scoped.Matches) != 1 || scoped.Matches[0].Path != "src/util/helper.go" {
		t.Fatalf("scoped grep=%+v", scoped)
	}
	// Invalid regex is rejected.
	badRegex := fsCall(e, bridge.MethodFsGrep, map[string]any{
		"leaseId": lease.ID, "fencingToken": lease.FencingToken, "pattern": "([",
	})
	if badRegex.OK || badRegex.Error.Code != "FS_PATH_INVALID" {
		t.Fatalf("badRegex=%#v", badRegex)
	}
}

func TestFsLeaseFencingAndExpiry(t *testing.T) {
	e, _, store := agentRunEngine(t)
	root := fsFixture(t)
	reg := decodePayloadInto[workspaceRegistrationOut](t, registerWorkspace(t, e, root, "fs-fence-reg"))
	grant := decodePayloadInto[struct {
		ID string `json:"id"`
	}](t, grantWorkspace(t, e, reg.ID, []string{"src/**"}, []string{"read"}, 3600, "fs-fence-grant"))
	first := decodePayloadInto[fsLeaseOut](t, leaseWorkspace(t, e, grant.ID, 900, "fs-fence-lease-1"))

	read := func(lease fsLeaseOut) bridge.Response {
		return fsCall(e, bridge.MethodFsRead, map[string]any{
			"leaseId": lease.ID, "fencingToken": lease.FencingToken, "path": "src/main.go",
		})
	}
	if res := read(first); !res.OK {
		t.Fatalf("first lease read: %#v", res)
	}

	// A stale fencing token fails closed.
	stale := fsCall(e, bridge.MethodFsRead, map[string]any{
		"leaseId": first.ID, "fencingToken": first.FencingToken + 1, "path": "src/main.go",
	})
	if stale.OK || stale.Error.Code != "FS_FENCING_STALE" {
		t.Fatalf("stale=%#v", stale)
	}

	// A newer lease on the same grant supersedes the old token.
	second := decodePayloadInto[fsLeaseOut](t, leaseWorkspace(t, e, grant.ID, 900, "fs-fence-lease-2"))
	if second.FencingToken != first.FencingToken+1 {
		t.Fatalf("fencing not monotonic: %+v vs %+v", second, first)
	}

	// Unknown lease.
	unknown := fsCall(e, bridge.MethodFsRead, map[string]any{
		"leaseId": ulid.Make().String(), "fencingToken": 1, "path": "src/main.go",
	})
	if unknown.OK || unknown.Error.Code != "FS_LEASE_INVALID" {
		t.Fatalf("unknown=%#v", unknown)
	}

	// Expired lease rejects every call.
	if err := store.AgentRuntimeRepository().Transact(context.Background(), func(tx agentrun.Tx) error {
		lease, err := tx.GetLease(first.ID)
		if err != nil {
			return err
		}
		lease.ExpiresAt = time.Now().UTC().Add(-time.Minute)
		return tx.PutLease(lease)
	}); err != nil {
		t.Fatal(err)
	}
	expired := read(first)
	if expired.OK || expired.Error.Code != "FS_LEASE_INVALID" {
		t.Fatalf("expired=%#v", expired)
	}
}

func TestFsReadScopeRequiresReadOperation(t *testing.T) {
	e, _, _ := agentRunEngine(t)
	root := fsFixture(t)
	// Grant without the read operation cannot read.
	lease := fsLease(t, e, root, []string{"write"}, []string{"src/**"}, "fs-write-only")
	res := fsCall(e, bridge.MethodFsRead, map[string]any{
		"leaseId": lease.ID, "fencingToken": lease.FencingToken, "path": "src/main.go",
	})
	if res.OK || res.Error.Code != "FS_SCOPE_DENIED" {
		t.Fatalf("write-only=%#v", res)
	}

	// Schema-level validation still applies.
	bad := fsCall(e, bridge.MethodFsRead, map[string]any{
		"leaseId": "not-a-ulid", "fencingToken": 1, "path": "src/main.go",
	})
	if bad.OK || bad.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("bad=%#v", bad)
	}
}
