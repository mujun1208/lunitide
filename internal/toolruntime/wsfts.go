// P2-1 FTS5 workspace search: literal queries of 3+ runes route through a
// persistent SQLite trigram index instead of re-reading every file per
// search. The index is namespaced by resolved search root, built lazily on
// first search and maintained incrementally from (mtime,size) fingerprints,
// so unchanged files cost one stat during refresh and zero parsing.
//
// Semantic parity with the linear scan (searchWorkspace) is contractual:
// identical "path:line: text" output, identical 400-rune line truncation
// applied BEFORE matching, identical binary/oversize skipping, identical
// max-hit cut-off and identical hit ordering (DFS walk order, reproduced
// by the segment comparator below). Regex and short-literal queries keep
// the linear path. Any index error degrades to the linear scan.
package toolruntime

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// wsFTSMaxRows bounds one MATCH fetch (defense in depth; session
// workspaces are far below this).
const wsFTSMaxRows = 20000

// wsFingerprint is one file's disk identity used for incremental refresh.
type wsFingerprint struct {
	mtime int64
	size  int64
}

// wsFTSEligible answers whether a query can use the trigram index:
// literal-only, valid UTF-8, at least 3 runes (trigram minimum).
func wsFTSEligible(query string, regex bool) bool {
	if regex || !utf8.ValidString(query) {
		return false
	}
	return len([]rune(query)) >= 3
}

// wsFTSPhrase quotes a literal into an FTS5 phrase string; embedded
// double quotes double up per FTS5 string syntax.
func wsFTSPhrase(query string) string {
	return `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
}

// dfsLess reproduces filepath.WalkDir's lexical DFS ordering for
// slash-separated relative paths: compare component by component; the
// first differing segment decides. (WalkDir visits each directory's
// entries sorted, recursing into subdirectories in place.)
func dfsLess(a, b string) bool {
	as, bs := strings.Split(a, "/"), strings.Split(b, "/")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] != bs[i] {
			return as[i] < bs[i]
		}
	}
	return len(as) < len(bs)
}

// ensureWSFTS creates the index tables once per handle. A driver without
// FTS5 answers false and searches keep the linear path.
func (r *Runtime) ensureWSFTS() bool {
	if r.wsFTSReady {
		return true
	}
	if r.db == nil {
		return false
	}
	if _, err := r.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS ws_fts USING fts5(
		root UNINDEXED, relpath UNINDEXED, line_no UNINDEXED, text,
		tokenize='trigram case_sensitive 1');
	CREATE TABLE IF NOT EXISTS ws_files(
		root TEXT NOT NULL, relpath TEXT NOT NULL,
		mtime INTEGER NOT NULL, size INTEGER NOT NULL, indexed INTEGER NOT NULL,
		PRIMARY KEY(root, relpath));`); err != nil {
		return false
	}
	r.wsFTSReady = true
	return true
}

// wsRootLock serializes index refresh per root (fingerprint diff plus
// reindex must be one critical section or two concurrent searches could
// double-insert a changed file's lines).
func (r *Runtime) wsRootLock(root string) *sync.Mutex {
	r.wsIdxMu.Lock()
	defer r.wsIdxMu.Unlock()
	if r.wsRootMu == nil {
		r.wsRootMu = make(map[string]*sync.Mutex)
	}
	mu, ok := r.wsRootMu[root]
	if !ok {
		mu = &sync.Mutex{}
		r.wsRootMu[root] = mu
	}
	return mu
}

// searchFTS answers the same shape as searchWorkspace's linear scan, or
// (nil, false, nil) when the index path is unavailable/fails and the
// caller must fall back to linear.
func (r *Runtime) searchFTS(root, query string, max int) ([]string, bool, error) {
	if !r.ensureWSFTS() || !wsFTSEligible(query, false) {
		return nil, false, nil
	}
	mu := r.wsRootLock(root)
	mu.Lock()
	if err := r.wsRefreshIndex(root); err != nil {
		mu.Unlock()
		return nil, false, nil
	}
	rows, err := r.db.Query(`SELECT relpath, line_no, text FROM ws_fts WHERE ws_fts MATCH ? AND root = ?`, wsFTSPhrase(query), root)
	mu.Unlock()
	if err != nil {
		return nil, false, nil
	}
	defer rows.Close()
	type hit struct {
		rel  string
		line int
		text string
	}
	var hits []hit
	for rows.Next() {
		var h hit
		if err = rows.Scan(&h.rel, &h.line, &h.text); err != nil {
			return nil, false, nil
		}
		hits = append(hits, h)
		if len(hits) >= wsFTSMaxRows {
			break
		}
	}
	if err = rows.Err(); err != nil {
		return nil, false, nil
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].rel != hits[j].rel {
			return dfsLess(hits[i].rel, hits[j].rel)
		}
		return hits[i].line < hits[j].line
	})
	if len(hits) > max {
		hits = hits[:max]
	}
	if len(hits) == 0 {
		return []string{"no matches"}, true, nil
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = fmt.Sprintf("%s:%d: %s", h.rel, h.line, strings.TrimRight(h.text, "\r"))
	}
	return out, true, nil
}

// wsRefreshIndex diffs disk state against stored fingerprints: new and
// changed files are reindexed, vanished files purged, untouched files
// skipped with zero reads.
func (r *Runtime) wsRefreshIndex(root string) error {
	disk := make(map[string]wsFingerprint)
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		info, ie := d.Info()
		if ie != nil || info.Size() > maxFile {
			return nil
		}
		rel, re := filepath.Rel(root, p)
		if re != nil {
			return nil
		}
		disk[filepath.ToSlash(rel)] = wsFingerprint{mtime: info.ModTime().UnixNano(), size: info.Size()}
		return nil
	})
	if err != nil {
		return err
	}
	rows, err := r.db.Query(`SELECT relpath, mtime, size FROM ws_files WHERE root = ?`, root)
	if err != nil {
		return err
	}
	stored := make(map[string]wsFingerprint)
	for rows.Next() {
		var rel string
		var f wsFingerprint
		if err = rows.Scan(&rel, &f.mtime, &f.size); err != nil {
			rows.Close()
			return err
		}
		stored[rel] = f
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	// Vanished files: purge index rows and fingerprints.
	for rel := range stored {
		if _, ok := disk[rel]; !ok {
			if err = r.wsPurgeFile(root, rel); err != nil {
				return err
			}
		}
	}
	for rel, f := range disk {
		s, ok := stored[rel]
		if ok && s.mtime == f.mtime && s.size == f.size {
			continue
		}
		if err = r.wsIndexFile(root, filepath.Join(root, filepath.FromSlash(rel)), rel, f); err != nil {
			return err
		}
	}
	return nil
}

// wsIndexFile replaces the index rows for one file. Binary files (NUL in
// the first 8 KiB) and read failures store indexed=0 so refreshes skip
// them until (mtime,size) changes again.
func (r *Runtime) wsIndexFile(root, abs, rel string, f wsFingerprint) error {
	type indexedLine struct {
		no   int
		text string
	}
	var lines []indexedLine
	indexed := 0
	if b, err := os.ReadFile(abs); err == nil {
		probe := b
		if len(probe) > 8192 {
			probe = probe[:8192]
		}
		if bytes.IndexByte(probe, 0) < 0 {
			indexed = 1
			for i, line := range strings.Split(string(b), "\n") {
				if line == "" {
					continue
				}
				// Same truncation-before-match contract as the linear
				// scan: only the first 400 runes can ever match.
				if len(line) > 400 {
					line = truncateRunes(line, 400)
				}
				if line != "" {
					lines = append(lines, indexedLine{no: i + 1, text: line})
				}
			}
		}
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.Exec(`DELETE FROM ws_fts WHERE root = ? AND relpath = ?`, root, rel); err != nil {
		return err
	}
	for _, l := range lines {
		if _, err = tx.Exec(`INSERT INTO ws_fts(root, relpath, line_no, text) VALUES(?,?,?,?)`, root, rel, l.no, l.text); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`INSERT INTO ws_files(root, relpath, mtime, size, indexed) VALUES(?,?,?,?,?)
		ON CONFLICT(root, relpath) DO UPDATE SET mtime=excluded.mtime, size=excluded.size, indexed=excluded.indexed`,
		root, rel, f.mtime, f.size, indexed); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Runtime) wsPurgeFile(root, rel string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.Exec(`DELETE FROM ws_fts WHERE root = ? AND relpath = ?`, root, rel); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM ws_files WHERE root = ? AND relpath = ?`, root, rel); err != nil {
		return err
	}
	return tx.Commit()
}
