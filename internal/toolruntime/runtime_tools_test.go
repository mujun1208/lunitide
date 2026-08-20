package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/networkpolicy"
)

func TestMatchCommandRuleBuiltinSet(t *testing.T) {
	rules := builtinCommandRules()
	allow := [][]string{
		{"go", "version"},
		{"git", "--no-pager", "status"},
		{"git", "--no-pager", "log", "--oneline", "-n", "20"},
		{"git", "--no-pager", "diff", "--stat"},
		{"git", "--no-pager", "show", "HEAD"},
		{"git", "--no-pager", "branch"},
	}
	for _, argv := range allow {
		if _, ok := matchCommandRule(rules, argv); !ok {
			t.Fatalf("builtin denied %v", argv)
		}
	}
	deny := [][]string{
		{"cmd", "/c", "del", "x"},
		{"git", "status"},                    // pager path not allowed
		{"git", "--no-pager", "push"},        // mutating git
		{"git", "--no-pager", "log", "-n", "1", "-p", "-a", "-b", "-c", "-d", "-e"}, // over maxArgs
		{"go", "build", "./..."},             // not in builtin set
		{},
	}
	for _, argv := range deny {
		if _, ok := matchCommandRule(rules, argv); ok {
			t.Fatalf("builtin allowed %v", argv)
		}
	}
}

func TestUserCommandPolicyLoadAndEnforce(t *testing.T) {
	root := t.TempDir()
	policy := `{"commands":[{"prefix":["go","test"],"maxArgs":8,"timeoutMs":120000},{"prefix":["node","--version"]}]}`
	if err := os.WriteFile(filepath.Join(root, "command-policy.json"), []byte(policy), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, ok := matchCommandRule(r.commandRules, []string{"go", "test", "./..."}); !ok {
		t.Fatal("user rule go test denied")
	}
	if _, ok := matchCommandRule(r.commandRules, []string{"node", "--version"}); !ok {
		t.Fatal("user rule node --version denied")
	}
	// Deadline grading: the go test rule carries its configured 120 s budget.
	for _, rule := range r.commandRules {
		if len(rule.prefix) == 2 && rule.prefix[0] == "go" && rule.prefix[1] == "test" {
			if rule.deadline != 120*1e9 {
				t.Fatalf("go test deadline = %v", rule.deadline)
			}
		}
	}
	// Unknown commands stay denied.
	if _, ok := matchCommandRule(r.commandRules, []string{"npm", "install"}); ok {
		t.Fatal("unlisted command allowed")
	}
}

func TestUserCommandPolicyInvalidFailsClosed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "command-policy.json"), []byte(`{"commands":[{"prefix":[]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err == nil {
		t.Fatal("invalid policy file accepted")
	}
}

func TestUserCommandPolicyRejectsPathQualifiedPrefix(t *testing.T) {
	root := t.TempDir()
	policy := `{"commands":[{"prefix":["../escape"]},{"prefix":["C:\\tools\\x.exe"]}]}`
	if err := os.WriteFile(filepath.Join(root, "command-policy.json"), []byte(policy), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err == nil {
		t.Fatal("path-qualified prefix accepted")
	}
}

const fakePage = `<!doctype html><html><head><title>Example Domain</title></head>
<body><script>evil()</script><h1>Hello Web</h1><p>Body text here.</p></body></html>`

func TestWebFetchToolExtractsText(t *testing.T) {
	r, _ := New(t.TempDir())
	s := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	r.SetWebFetcher(func(_ context.Context, rawURL string) (networkpolicy.FetchResult, error) {
		if rawURL != "https://example.com/page" {
			return networkpolicy.FetchResult{}, errors.New("unexpected url")
		}
		return networkpolicy.FetchResult{FinalURL: "https://example.com/page", Status: 200, ContentType: "text/html; charset=utf-8", Body: []byte(fakePage)}, nil
	})
	out, err := r.Execute(context.Background(), Approval, s, "web.fetch", json.RawMessage(`{"url":"https://example.com/page"}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "title: Example Domain") {
		t.Fatalf("missing title: %q", out.Output)
	}
	if !strings.Contains(out.Output, "Hello Web") || !strings.Contains(out.Output, "Body text here.") {
		t.Fatalf("missing body: %q", out.Output)
	}
	if strings.Contains(out.Output, "evil()") {
		t.Fatalf("script leaked: %q", out.Output)
	}
	// Read-only tool: no approval gate even in approval mode (already proven
	// by the call above executing with approved=false).
}

func TestWebSearchToolParsesResults(t *testing.T) {
	r, _ := New(t.TempDir())
	s := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	r.SetWebFetcher(func(_ context.Context, _ string) (networkpolicy.FetchResult, error) {
		return networkpolicy.FetchResult{Status: 200, ContentType: "text/html", Body: []byte(ddgLiteBody)}, nil
	})
	out, err := r.Execute(context.Background(), Approval, s, "web.search", json.RawMessage(`{"query":"golang","max":3}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "query: golang") {
		t.Fatalf("missing query echo: %q", out.Output)
	}
	if !strings.Contains(out.Output, "https://go.dev/") {
		t.Fatalf("missing result url: %q", out.Output)
	}
	if out.Artifact == nil || out.Artifact.Kind != "html" || !strings.Contains(out.Artifact.Content, "Go Programming Language") {
		t.Fatalf("missing html artifact: %+v", out.Artifact)
	}
}

func TestWebSearchFallsBackToBing(t *testing.T) {
	r, _ := New(t.TempDir())
	s := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	r.SetWebFetcher(func(_ context.Context, rawURL string) (networkpolicy.FetchResult, error) {
		if strings.Contains(rawURL, "duckduckgo") {
			return networkpolicy.FetchResult{}, errors.New("timeout")
		}
		if strings.Contains(rawURL, "bing.com") {
			body := `<li class="b_algo"><h2><a href="https://go.dev/">Go</a></h2><p>The Go language.</p></li>`
			return networkpolicy.FetchResult{Status: 200, ContentType: "text/html", Body: []byte(body)}, nil
		}
		return networkpolicy.FetchResult{}, errors.New("unexpected url " + rawURL)
	})
	out, err := r.Execute(context.Background(), Approval, s, "web.search", json.RawMessage(`{"query":"golang","max":3}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "https://go.dev/") || !strings.Contains(out.Output, "source: bing") {
		t.Fatalf("bing fallback missing: %q", out.Output)
	}
	if out.Artifact == nil || !strings.Contains(out.Artifact.Content, "搜索结果") {
		t.Fatalf("missing search html: %+v", out.Artifact)
	}
}

const ddgLiteBody = `<html><body><table>
<tr><td><a class="result-link" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2F">Go Programming Language</a></td></tr>
<tr><td class="result-snippet">An open-source programming language.</td></tr>
</table></body></html>`

func TestWebToolsUnavailableWithoutFetcher(t *testing.T) {
	r, _ := New(t.TempDir())
	s := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if _, err := r.Execute(context.Background(), FullAccess, s, "web.fetch", json.RawMessage(`{"url":"https://example.com"}`), true); err == nil {
		t.Fatal("web.fetch ran without transport")
	}
	if _, err := r.Execute(context.Background(), FullAccess, s, "web.search", json.RawMessage(`{"query":"x"}`), true); err == nil {
		t.Fatal("web.search ran without transport")
	}
}

// currentRules snapshots the live rule slice under the reload lock so tests
// observe the same view Execute does after a hot policy swap.
func currentRules(r *Runtime) []commandRule {
	r.rulesMu.RLock()
	defer r.rulesMu.RUnlock()
	return r.commandRules
}

func TestCommandPolicyJSONRoundTripAndHotApply(t *testing.T) {
	root := t.TempDir()
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	// Absent file answers the empty document instead of an error.
	raw, err := r.CommandPolicyJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != `{"commands":[]}` {
		t.Fatalf("absent policy = %q", raw)
	}

	// Hot-apply a valid document: the rule enforces without a reopen and
	// the built-in set stays intact.
	doc := `{"commands":[{"prefix":["node","--version"],"maxArgs":2,"timeoutMs":15000}]}`
	if err := r.SetCommandPolicyJSON([]byte(doc)); err != nil {
		t.Fatal(err)
	}
	if _, ok := matchCommandRule(currentRules(r), []string{"node", "--version"}); !ok {
		t.Fatal("hot-applied rule denied")
	}
	if _, ok := matchCommandRule(currentRules(r), []string{"git", "--no-pager", "status"}); !ok {
		t.Fatal("builtin rule lost after hot apply")
	}
	raw, err = r.CommandPolicyJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != doc {
		t.Fatalf("persisted policy = %q", raw)
	}

	// Invalid documents are refused whole: live rules and the stored file
	// keep the last accepted state.
	for _, bad := range []string{`not json`, `{"commands":[{"prefix":[]}]}`, `{"commands":[{"prefix":["../escape"]}]}`} {
		if err := r.SetCommandPolicyJSON([]byte(bad)); err == nil {
			t.Fatalf("invalid policy accepted: %q", bad)
		}
	}
	if _, ok := matchCommandRule(currentRules(r), []string{"node", "--version"}); !ok {
		t.Fatal("live rules changed after refused update")
	}
	raw, err = r.CommandPolicyJSON()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != doc {
		t.Fatal("stored file changed after refused update")
	}

	// A fresh open reloads the persisted document (restart persistence).
	r2, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	if _, ok := matchCommandRule(currentRules(r2), []string{"node", "--version"}); !ok {
		t.Fatal("persisted rule lost on reopen")
	}
}
