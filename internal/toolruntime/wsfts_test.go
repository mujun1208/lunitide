// P2-1 semantic-parity coverage: the FTS trigram fast path must be
// indistinguishable from the linear scan it replaces — identical hit
// lines, identical DFS ordering, identical max cut-off, identical
// skip rules (binary probe, oversize, 400-rune truncation before
// match) — and must track incremental workspace edits.
package toolruntime

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// wsTree builds one adversarial workspace: nested dirs whose DFS order
// differs from byte order ("b.txt" vs "a/c.txt"), CRLF lines, a long
// line with matches inside and beyond the 400-rune truncation window, a
// binary file, an empty file and an oversized file.
func wsTree(t *testing.T, root string) {
	t.Helper()
	must := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	must("b.txt", "alpha Needle one\r\nplain line\r\nneedle lowercase\n")
	must("a/c.txt", "Needle nested\nnothing here\nneedle again\n")
	must("a/z/deep.txt", "NEEDLE deep\n")
	must("crlf.txt", "Needle with crlf tail\r\n")
	long := strings.Repeat("x", 500) + "Needle beyond four hundred runes"
	must("long.txt", "Needle within window\n"+long+"\n")
	must("empty.txt", "")
	must("bin.dat", "text Needle then\x00binary payload")
	if err := os.WriteFile(filepath.Join(root, "big.txt"), make([]byte, maxFile+1), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertParity(t *testing.T, r *Runtime, root, query string, max int) {
	t.Helper()
	linear, err := searchLinear(root, nil, query, max)
	if err != nil {
		t.Fatalf("linear: %v", err)
	}
	fts, ok, err := r.searchFTS(root, query, max)
	if err != nil {
		t.Fatalf("fts: %v", err)
	}
	if !ok {
		t.Fatalf("fts path declined for query %q", query)
	}
	if strings.Join(linear, "\x00") != strings.Join(fts, "\x00") {
		t.Fatalf("query %q max %d diverged:\nlinear=%q\nfts   =%q", query, max, linear, fts)
	}
}

func TestWorkspaceFTSParityWithLinear(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	root := filepath.Join(r.root, "ws")
	wsTree(t, root)

	for _, q := range []string{"Needle", "needle", "NEEDLE", "NeEdLe", "needle again", "nothing here", "absent-query"} {
		for _, max := range []int{1, 2, 3, 50} {
			assertParity(t, r, root, q, max)
		}
	}
}

func TestWorkspaceFTSTruncationSemantics(t *testing.T) {
	// "Needle beyond four hundred runes" sits past the 400-rune window:
	// BOTH paths must miss it (truncation happens before matching).
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	root := filepath.Join(r.root, "ws")
	wsTree(t, root)
	for _, q := range []string{"beyond four hundred", "within window"} {
		assertParity(t, r, root, q, 50)
	}
}

func TestWorkspaceFTSIncrementalMaintenance(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	root := filepath.Join(r.root, "ws")
	if err = os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "f.txt")
	if err = os.WriteFile(p, []byte("first alpha content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertParity(t, r, root, "alpha", 50)

	// Rewrite: old hit must vanish, new one appear.
	if err = os.WriteFile(p, []byte("beta content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertParity(t, r, root, "alpha", 50)
	assertParity(t, r, root, "beta", 50)

	// Delete: file must drop out of the index.
	if err = os.Remove(p); err != nil {
		t.Fatal(err)
	}
	assertParity(t, r, root, "beta", 50)

	// New nested file after the root was already indexed.
	nested := filepath.Join(root, "d", "g.txt")
	if err = os.MkdirAll(filepath.Dir(nested), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(nested, []byte("gamma arrives\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertParity(t, r, root, "gamma", 50)
}

func TestWorkspaceFTSSubtreeAndFallbacks(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	root := filepath.Join(r.root, "ws")
	wsTree(t, root)
	sub := filepath.Join(root, "a")
	assertParity(t, r, sub, "Needle", 50)

	// Short queries (trigram minimum is 3) and regex keep the linear path.
	if wsFTSEligible("ab", false) {
		t.Fatal("2-rune query must not be FTS-eligible")
	}
	if wsFTSEligible("abc", true) {
		t.Fatal("regex query must not be FTS-eligible")
	}
	re := regexp.MustCompile(`[Nn]eedle`)
	linear, err := searchLinear(root, re, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(linear) != 6 {
		t.Fatalf("regex linear hits = %d (%q), want 6", len(linear), linear)
	}
	// Dispatch: searchWorkspace picks FTS for eligible literals (the
	// session sandbox path mirrors the tree so path resolution succeeds).
	wsTree(t, filepath.Join(r.root, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "ws"))
	hits, err := r.searchWorkspace(Approval, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "ws", "Needle", false, 50, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 4 {
		t.Fatalf("workspace.search hits = %d (%q), want 4 (uppercase NEEDLE and beyond-window match excluded)", len(hits), hits)
	}
}

func TestWorkspaceFTSCaseSensitiveIndexing(t *testing.T) {
	// The trigram table is case_sensitive 1; "GoLang" must not match
	// "golang" through the fast path (linear parity guards the rest).
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	root := filepath.Join(r.root, "ws")
	if err = os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "g.txt"), []byte("golang lower\nGoLang mixed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fts, ok, err := r.searchFTS(root, "GoLang", 50)
	if err != nil || !ok {
		t.Fatalf("fts declined: ok=%v err=%v", ok, err)
	}
	if len(fts) != 1 || !strings.Contains(fts[0], "GoLang mixed") {
		t.Fatalf("case-sensitive hits = %q", fts)
	}
}
