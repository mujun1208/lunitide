// M4-D read-only fs tools (tree/stat/read/readMany/glob/grep). Every call is
// authorized against a fenced workspace lease: the lease must be active, the
// presented fencing token must match the stored token exactly, the grant must
// still be usable and its scope must grant the "read" operation for the
// requested path. All file system access is confined to the registered
// canonical root; symlinks are resolved and escapes fail closed. The tools
// mutate nothing, so they carry no idempotency record, run event or audit
// write — the durable trail belongs to the tool_call surface of the runtime
// executor that invokes them.
package agentrunapp

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/canonpath"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
)

var (
	// ErrFsLeaseInvalid is returned when the lease (or its grant or
	// registration) is unknown, expired, revoked or otherwise unusable.
	ErrFsLeaseInvalid = errors.New("workspace lease is not usable")
	// ErrFsFencingStale is returned when the presented fencing token does
	// not match the token stored on the lease (stale handle).
	ErrFsFencingStale = errors.New("fencing token does not match the lease")
	// ErrFsScopeDenied is returned when the path is outside the granted
	// scope or the scope lacks the read operation.
	ErrFsScopeDenied = errors.New("path is outside the granted workspace scope")
	// ErrFsPathInvalid is returned when a path or pattern is malformed,
	// absolute, or escapes the workspace root.
	ErrFsPathInvalid = errors.New("invalid workspace path")
	// ErrFsNotFound is returned when the path does not exist.
	ErrFsNotFound = errors.New("workspace path does not exist")
	// ErrFsNotAFile is returned when a read targets a directory or a
	// non-regular file.
	ErrFsNotAFile = errors.New("workspace path is not a regular file")
	// ErrFsBinary is returned when file content is not valid UTF-8 text.
	ErrFsBinary = errors.New("file content is not UTF-8 text")
	// ErrFsTooLarge is returned when a file exceeds the hard readable cap.
	ErrFsTooLarge = errors.New("file exceeds the readable size limit")
	// ErrFsPathExists is returned when a create targets a path that
	// already exists (including a dangling symlink).
	ErrFsPathExists = errors.New("workspace path already exists")
)

const (
	// fsReadHardCap bounds how many bytes are loaded to answer a read;
	// larger files are rejected rather than silently truncated.
	fsReadHardCap = 64 << 20
	// fsStatDigestCap bounds digest computation for stat results.
	fsStatDigestCap = 32 << 20
	// fsGrepFileCap skips files too large to scan line by line.
	fsGrepFileCap = 16 << 20
	// fsWalkEntryCap bounds total walk work for tree/glob/grep.
	fsWalkEntryCap = 50000

	fsDefaultMaxBytes   = 262144
	fsMaxMaxBytes       = 1048576
	fsDefaultMaxDepth   = 4
	fsDefaultMaxEntries = 512
	fsMaxEntries        = 2048
	fsDefaultMaxResults = 256
	fsMaxGlobResults    = 1024
	fsDefaultGrepMax    = 100
	fsMaxGrepResults    = 500
	fsGrepTextMaxRunes  = 512
	fsReadManyMaxPaths  = 32
)

// fsAccess is the authorized, resolved view of a lease: the canonical root,
// the registration identity (change set approval binding) and the granted
// scope patterns.
type fsAccess struct {
	root           string
	registrationID string
	patterns       []string
}

// fsAuthorize validates the lease, fencing token, grant and registration in
// one read-only transaction and resolves the workspace root and scope. The
// grant scope must contain the required operation ("read" or "write").
func (s *Service) fsAuthorize(ctx context.Context, leaseID string, fencingToken int64, operation string) (fsAccess, error) {
	if err := s.available(); err != nil {
		return fsAccess{}, err
	}
	var access fsAccess
	err := s.uow.TransactAgentRuntime(ctx, func(tx Tx) error {
		var err error
		access, err = authorizeFsLease(tx, leaseID, fencingToken, operation, s.clock.Now().UTC())
		return err
	})
	return access, err
}

// authorizeFsLease is fsAuthorize running inside an existing transaction so
// mutating use cases (change sets) can authorize and write atomically.
func authorizeFsLease(tx Tx, leaseID string, fencingToken int64, operation string, now time.Time) (fsAccess, error) {
	if fencingToken < 1 {
		return fsAccess{}, ErrFsFencingStale
	}
	lease, err := tx.GetLease(leaseID)
	if err != nil {
		if errors.Is(err, agentrun.ErrNotFound) {
			return fsAccess{}, ErrFsLeaseInvalid
		}
		return fsAccess{}, err
	}
	if !lease.UsableAt(now) {
		return fsAccess{}, ErrFsLeaseInvalid
	}
	if lease.FencingToken != fencingToken {
		return fsAccess{}, ErrFsFencingStale
	}
	grant, err := tx.GetGrant(lease.GrantID)
	if err != nil {
		if errors.Is(err, agentrun.ErrNotFound) {
			return fsAccess{}, ErrFsLeaseInvalid
		}
		return fsAccess{}, err
	}
	if !grant.UsableAt(now) {
		return fsAccess{}, ErrFsLeaseInvalid
	}
	registration, err := tx.GetRegistration(grant.RegistrationID)
	if err != nil {
		if errors.Is(err, agentrun.ErrNotFound) {
			return fsAccess{}, ErrFsLeaseInvalid
		}
		return fsAccess{}, err
	}
	if registration.Status != agentrun.RegistrationActive {
		return fsAccess{}, ErrFsLeaseInvalid
	}
	var scope struct {
		Operations []string `json:"operations"`
		Paths      []string `json:"paths"`
	}
	if err := json.Unmarshal(grant.Scope, &scope); err != nil || len(scope.Paths) == 0 {
		return fsAccess{}, ErrFsScopeDenied
	}
	granted := false
	for _, op := range scope.Operations {
		if op == operation {
			granted = true
			break
		}
	}
	if !granted {
		return fsAccess{}, ErrFsScopeDenied
	}
	return fsAccess{root: registration.CanonicalRoot, registrationID: registration.ID, patterns: scope.Paths}, nil
}

// validFsRelPath reports whether p is a well-formed workspace-relative path:
// forward slashes, no absolute prefix, no drive/UNC, no "." or ".." segments.
func validFsRelPath(p string) bool {
	if p == "" || len(p) > 512 || strings.ContainsRune(p, '\\') || strings.ContainsRune(p, ':') {
		return false
	}
	if strings.HasPrefix(p, "/") {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// resolve maps a validated relative path to an absolute path under the root,
// resolving symlinks and failing closed on any escape. The path must exist.
func (a fsAccess) resolve(rel string) (string, error) {
	if rel != "" && !validFsRelPath(rel) {
		return "", ErrFsPathInvalid
	}
	// Both sides of the containment test have to be spelled the same way.
	// Resolving only the child and comparing it against the root as it was
	// spelled at registration reports an escape for every path in a
	// workspace that sits under a short-name or redirected directory.
	// Canonicalizing here rather than trusting the caller keeps that from
	// depending on which entry point registered the workspace.
	root, err := canonpath.Canonical(a.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrFsNotFound
		}
		return "", err
	}
	joined := filepath.Join(root, filepath.FromSlash(rel))
	resolved, err := canonpath.Canonical(joined)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrFsNotFound
		}
		return "", err
	}
	rest, err := filepath.Rel(root, resolved)
	if err != nil || rest == ".." || strings.HasPrefix(rest, ".."+string(filepath.Separator)) || filepath.IsAbs(rest) {
		return "", ErrFsPathInvalid
	}
	return resolved, nil
}

// scopeAllows reports whether rel (slash path, "" = root) is granted by at
// least one scope pattern. Patterns are exact paths or "<prefix>/**" / "**".
func scopeAllows(patterns []string, rel string) bool {
	if rel == "" {
		return len(patterns) > 0
	}
	for _, pattern := range patterns {
		switch {
		case pattern == "**":
			return true
		case strings.HasSuffix(pattern, "/**"):
			base := strings.TrimSuffix(pattern, "/**")
			if rel == base || strings.HasPrefix(rel, base+"/") {
				return true
			}
		case pattern == rel:
			return true
		}
	}
	return false
}

// scopeVisible reports whether rel may appear in listings: it is granted, or
// it is a strict ancestor of a granted path so the caller can navigate to it.
func scopeVisible(patterns []string, rel string) bool {
	if scopeAllows(patterns, rel) {
		return true
	}
	if rel == "" {
		return len(patterns) > 0
	}
	for _, pattern := range patterns {
		base := strings.TrimSuffix(pattern, "/**")
		if strings.HasPrefix(base, rel+"/") {
			return true
		}
	}
	return false
}

// FsTreeEntry is one directory listing row, path relative to the root.
type FsTreeEntry struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	SizeBytes int64  `json:"sizeBytes,omitempty"`
}

// FsTreeResult is the fs.tree payload.
type FsTreeResult struct {
	Path      string        `json:"path"`
	Entries   []FsTreeEntry `json:"entries"`
	Truncated bool          `json:"truncated"`
}

// FsTree lists the directory at rel ("" = root) up to maxDepth levels deep,
// filtered to the granted scope. Entries are sorted by path.
func (s *Service) FsTree(ctx context.Context, leaseID string, fencingToken int64, rel string, maxDepth, maxEntries int) (FsTreeResult, error) {
	access, err := s.fsAuthorize(ctx, leaseID, fencingToken, "read")
	if err != nil {
		return FsTreeResult{}, err
	}
	if rel != "" && !validFsRelPath(rel) {
		return FsTreeResult{}, ErrFsPathInvalid
	}
	if !scopeVisible(access.patterns, rel) {
		return FsTreeResult{}, ErrFsScopeDenied
	}
	if maxDepth < 1 {
		maxDepth = fsDefaultMaxDepth
	}
	if maxDepth > 8 {
		maxDepth = 8
	}
	if maxEntries < 1 {
		maxEntries = fsDefaultMaxEntries
	}
	if maxEntries > fsMaxEntries {
		maxEntries = fsMaxEntries
	}
	abs, err := access.resolve(rel)
	if err != nil {
		return FsTreeResult{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return FsTreeResult{}, err
	}
	if !info.IsDir() {
		return FsTreeResult{}, ErrFsNotFound
	}
	result := FsTreeResult{Path: rel, Entries: []FsTreeEntry{}}
	visited := 0
	var walk func(dirAbs, dirRel string, depth int) error
	walk = func(dirAbs, dirRel string, depth int) error {
		if depth > maxDepth || result.Truncated {
			return nil
		}
		children, err := os.ReadDir(dirAbs)
		if err != nil {
			return err
		}
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		for _, child := range children {
			if result.Truncated {
				return nil
			}
			visited++
			if visited > fsWalkEntryCap {
				result.Truncated = true
				return nil
			}
			childRel := child.Name()
			if dirRel != "" {
				childRel = dirRel + "/" + child.Name()
			}
			if !scopeVisible(access.patterns, childRel) {
				continue
			}
			childInfo, err := child.Info()
			if err != nil {
				continue
			}
			entry := FsTreeEntry{Path: childRel, Kind: "file", SizeBytes: childInfo.Size()}
			if childInfo.IsDir() {
				entry = FsTreeEntry{Path: childRel, Kind: "dir"}
			} else if !childInfo.Mode().IsRegular() {
				continue
			}
			if len(result.Entries) >= maxEntries {
				result.Truncated = true
				return nil
			}
			result.Entries = append(result.Entries, entry)
			if entry.Kind == "dir" {
				if err := walk(filepath.Join(dirAbs, child.Name()), childRel, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(abs, rel, 1); err != nil {
		return FsTreeResult{}, err
	}
	return result, nil
}

// FsStatResult is the fs.stat payload. Digest is present for regular files
// up to fsStatDigestCap bytes.
type FsStatResult struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	SizeBytes  int64  `json:"sizeBytes"`
	ModifiedAt string `json:"modifiedAt"`
	Digest     string `json:"digest,omitempty"`
}

// FsStat returns metadata for one in-scope path.
func (s *Service) FsStat(ctx context.Context, leaseID string, fencingToken int64, rel string) (FsStatResult, error) {
	access, err := s.fsAuthorize(ctx, leaseID, fencingToken, "read")
	if err != nil {
		return FsStatResult{}, err
	}
	if !validFsRelPath(rel) {
		return FsStatResult{}, ErrFsPathInvalid
	}
	if !scopeAllows(access.patterns, rel) && !scopeVisible(access.patterns, rel) {
		return FsStatResult{}, ErrFsScopeDenied
	}
	abs, err := access.resolve(rel)
	if err != nil {
		return FsStatResult{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return FsStatResult{}, ErrFsNotFound
		}
		return FsStatResult{}, err
	}
	result := FsStatResult{
		Path:       rel,
		Kind:       "file",
		SizeBytes:  info.Size(),
		ModifiedAt: info.ModTime().UTC().Format("2006-01-02T15:04:05.000Z"),
	}
	if info.IsDir() {
		result.Kind = "dir"
		result.SizeBytes = 0
		return result, nil
	}
	if !info.Mode().IsRegular() {
		return FsStatResult{}, ErrFsNotAFile
	}
	if info.Size() <= fsStatDigestCap {
		content, err := os.ReadFile(abs)
		if err != nil {
			return FsStatResult{}, err
		}
		sum := sha256.Sum256(content)
		result.Digest = hex.EncodeToString(sum[:])
	}
	return result, nil
}

// FsReadResult is the fs.read payload. Digest covers exactly the returned
// (possibly truncated) content bytes.
type FsReadResult struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	SizeBytes int64  `json:"sizeBytes"`
	Digest    string `json:"digest"`
	Truncated bool   `json:"truncated"`
}

// readTextFile loads a regular file under the hard cap and validates UTF-8.
func readTextFile(abs string, size int64) ([]byte, error) {
	if size > fsReadHardCap {
		return nil, ErrFsTooLarge
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(content) {
		return nil, ErrFsBinary
	}
	return content, nil
}

// FsRead returns the text content of one in-scope file, truncated to
// maxBytes when the file is larger.
func (s *Service) FsRead(ctx context.Context, leaseID string, fencingToken int64, rel string, maxBytes int) (FsReadResult, error) {
	access, err := s.fsAuthorize(ctx, leaseID, fencingToken, "read")
	if err != nil {
		return FsReadResult{}, err
	}
	if !validFsRelPath(rel) {
		return FsReadResult{}, ErrFsPathInvalid
	}
	if !scopeAllows(access.patterns, rel) {
		return FsReadResult{}, ErrFsScopeDenied
	}
	if maxBytes < 1 {
		maxBytes = fsDefaultMaxBytes
	}
	if maxBytes > fsMaxMaxBytes {
		maxBytes = fsMaxMaxBytes
	}
	abs, err := access.resolve(rel)
	if err != nil {
		return FsReadResult{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return FsReadResult{}, ErrFsNotFound
		}
		return FsReadResult{}, err
	}
	if !info.Mode().IsRegular() {
		return FsReadResult{}, ErrFsNotAFile
	}
	content, err := readTextFile(abs, info.Size())
	if err != nil {
		return FsReadResult{}, err
	}
	truncated := false
	if len(content) > maxBytes {
		content = content[:maxBytes]
		for len(content) > 0 && !utf8.Valid(content) {
			content = content[:len(content)-1]
		}
		truncated = true
	}
	sum := sha256.Sum256(content)
	return FsReadResult{
		Path:      rel,
		Content:   string(content),
		SizeBytes: info.Size(),
		Digest:    hex.EncodeToString(sum[:]),
		Truncated: truncated,
	}, nil
}

// FsReadManyItem is one per-path read outcome.
type FsReadManyItem struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Content   string `json:"content,omitempty"`
	SizeBytes int64  `json:"sizeBytes,omitempty"`
	Digest    string `json:"digest,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// FsReadManyResult is the fs.readMany payload.
type FsReadManyResult struct {
	Items []FsReadManyItem `json:"items"`
}

// FsReadMany reads up to 32 in-scope files. Per-path failures are reported
// in the item status instead of failing the whole batch.
func (s *Service) FsReadMany(ctx context.Context, leaseID string, fencingToken int64, rels []string, maxBytes int) (FsReadManyResult, error) {
	access, err := s.fsAuthorize(ctx, leaseID, fencingToken, "read")
	if err != nil {
		return FsReadManyResult{}, err
	}
	if len(rels) < 1 || len(rels) > fsReadManyMaxPaths {
		return FsReadManyResult{}, ErrFsPathInvalid
	}
	if maxBytes < 1 {
		maxBytes = fsDefaultMaxBytes
	}
	if maxBytes > fsMaxMaxBytes {
		maxBytes = fsMaxMaxBytes
	}
	result := FsReadManyResult{Items: make([]FsReadManyItem, 0, len(rels))}
	for _, rel := range rels {
		if !validFsRelPath(rel) {
			return FsReadManyResult{}, ErrFsPathInvalid
		}
		if !scopeAllows(access.patterns, rel) {
			return FsReadManyResult{}, ErrFsScopeDenied
		}
		item := FsReadManyItem{Path: rel}
		abs, err := access.resolve(rel)
		if err != nil {
			if errors.Is(err, ErrFsNotFound) {
				item.Status = "not_found"
				result.Items = append(result.Items, item)
				continue
			}
			return FsReadManyResult{}, err
		}
		info, err := os.Stat(abs)
		if err != nil || !info.Mode().IsRegular() {
			item.Status = "not_a_file"
			result.Items = append(result.Items, item)
			continue
		}
		content, err := readTextFile(abs, info.Size())
		if err != nil {
			if errors.Is(err, ErrFsBinary) {
				item.Status = "binary"
				result.Items = append(result.Items, item)
				continue
			}
			if errors.Is(err, ErrFsTooLarge) {
				item.Status = "too_large"
				result.Items = append(result.Items, item)
				continue
			}
			return FsReadManyResult{}, err
		}
		if len(content) > maxBytes {
			content = content[:maxBytes]
			for len(content) > 0 && !utf8.Valid(content) {
				content = content[:len(content)-1]
			}
			item.Truncated = true
		}
		sum := sha256.Sum256(content)
		item.Status = "ok"
		item.Content = string(content)
		item.SizeBytes = info.Size()
		item.Digest = hex.EncodeToString(sum[:])
		result.Items = append(result.Items, item)
	}
	return result, nil
}

// validGlobPattern reports whether pattern is a well-formed workspace glob:
// forward slashes, no absolute prefix, no drive, no "." or ".." segments.
func validGlobPattern(pattern string) bool {
	if pattern == "" || len(pattern) > 512 || strings.ContainsRune(pattern, '\\') || strings.ContainsRune(pattern, ':') {
		return false
	}
	if strings.HasPrefix(pattern, "/") {
		return false
	}
	for _, segment := range strings.Split(pattern, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		if segment == "**" {
			continue
		}
		if strings.Contains(segment, "**") {
			return false
		}
	}
	return true
}

// globMatch matches a slash path against a glob pattern supporting **, *, ?
// and [class] within a segment. ** matches zero or more whole segments.
func globMatch(pattern, rel string) bool {
	patSegs := strings.Split(pattern, "/")
	pathSegs := strings.Split(rel, "/")
	var match func(pi, si int) bool
	match = func(pi, si int) bool {
		for pi < len(patSegs) {
			if patSegs[pi] == "**" {
				for skip := 0; si+skip <= len(pathSegs); skip++ {
					if match(pi+1, si+skip) {
						return true
					}
				}
				return false
			}
			if si >= len(pathSegs) {
				return false
			}
			ok, err := path.Match(patSegs[pi], pathSegs[si])
			if err != nil || !ok {
				return false
			}
			pi++
			si++
		}
		return si == len(pathSegs)
	}
	return match(0, 0)
}

// walkFiles visits regular files (and optionally directories) under abs,
// calling fn with the slash-relative path. It stops early after
// fsWalkEntryCap visited entries and reports the truncation.
func walkFiles(abs, rel string, includeDirs bool, fn func(childRel string, isDir bool, size int64)) (truncated bool, err error) {
	visited := 0
	var walk func(dirAbs, dirRel string) error
	walk = func(dirAbs, dirRel string) error {
		if truncated {
			return nil
		}
		children, err := os.ReadDir(dirAbs)
		if err != nil {
			return err
		}
		for _, child := range children {
			if truncated {
				return nil
			}
			visited++
			if visited > fsWalkEntryCap {
				truncated = true
				return nil
			}
			childRel := child.Name()
			if dirRel != "" {
				childRel = dirRel + "/" + child.Name()
			}
			info, err := child.Info()
			if err != nil {
				continue
			}
			if info.IsDir() {
				if includeDirs {
					fn(childRel, true, 0)
				}
				if err := walk(filepath.Join(dirAbs, child.Name()), childRel); err != nil {
					return err
				}
				continue
			}
			if !info.Mode().IsRegular() {
				continue
			}
			fn(childRel, false, info.Size())
		}
		return nil
	}
	err = walk(abs, rel)
	return truncated, err
}

// FsGlobResult is the fs.glob payload.
type FsGlobResult struct {
	Matches   []string `json:"matches"`
	Truncated bool     `json:"truncated"`
}

// FsGlob returns in-scope file paths matching the glob pattern, sorted.
func (s *Service) FsGlob(ctx context.Context, leaseID string, fencingToken int64, pattern string, maxResults int) (FsGlobResult, error) {
	access, err := s.fsAuthorize(ctx, leaseID, fencingToken, "read")
	if err != nil {
		return FsGlobResult{}, err
	}
	if !validGlobPattern(pattern) {
		return FsGlobResult{}, ErrFsPathInvalid
	}
	if maxResults < 1 {
		maxResults = fsDefaultMaxResults
	}
	if maxResults > fsMaxGlobResults {
		maxResults = fsMaxGlobResults
	}
	result := FsGlobResult{Matches: []string{}}
	truncated, err := walkFiles(access.root, "", false, func(rel string, _ bool, _ int64) {
		if result.Truncated || !globMatch(pattern, rel) || !scopeAllows(access.patterns, rel) {
			return
		}
		if len(result.Matches) >= maxResults {
			result.Truncated = true
			return
		}
		result.Matches = append(result.Matches, rel)
	})
	if err != nil {
		return FsGlobResult{}, err
	}
	result.Truncated = result.Truncated || truncated
	sort.Strings(result.Matches)
	return result, nil
}

// FsGrepMatch is one matching line.
type FsGrepMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// FsGrepResult is the fs.grep payload.
type FsGrepResult struct {
	Matches   []FsGrepMatch `json:"matches"`
	Truncated bool          `json:"truncated"`
}

// FsGrep searches in-scope text files for lines matching the RE2 pattern.
// Binary and oversized files are skipped. The optional rel restricts the
// search to one in-scope directory.
func (s *Service) FsGrep(ctx context.Context, leaseID string, fencingToken int64, expr, rel string, maxResults int) (FsGrepResult, error) {
	access, err := s.fsAuthorize(ctx, leaseID, fencingToken, "read")
	if err != nil {
		return FsGrepResult{}, err
	}
	if expr == "" || len(expr) > 256 {
		return FsGrepResult{}, ErrFsPathInvalid
	}
	compiled, err := regexp.Compile(expr)
	if err != nil {
		return FsGrepResult{}, ErrFsPathInvalid
	}
	if rel != "" {
		if !validFsRelPath(rel) {
			return FsGrepResult{}, ErrFsPathInvalid
		}
		if !scopeVisible(access.patterns, rel) {
			return FsGrepResult{}, ErrFsScopeDenied
		}
	}
	if maxResults < 1 {
		maxResults = fsDefaultGrepMax
	}
	if maxResults > fsMaxGrepResults {
		maxResults = fsMaxGrepResults
	}
	abs := access.root
	if rel != "" {
		abs, err = access.resolve(rel)
		if err != nil {
			return FsGrepResult{}, err
		}
	}
	result := FsGrepResult{Matches: []FsGrepMatch{}}
	truncated, err := walkFiles(abs, rel, false, func(fileRel string, _ bool, size int64) {
		if result.Truncated || size > fsGrepFileCap || !scopeAllows(access.patterns, fileRel) {
			return
		}
		content, err := readTextFile(filepath.Join(access.root, filepath.FromSlash(fileRel)), size)
		if err != nil {
			return
		}
		scanner := bufio.NewScanner(strings.NewReader(string(content)))
		scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
		line := 0
		for scanner.Scan() {
			line++
			text := scanner.Text()
			if !compiled.MatchString(text) {
				continue
			}
			if utf8.RuneCountInString(text) > fsGrepTextMaxRunes {
				runes := []rune(text)
				text = string(runes[:fsGrepTextMaxRunes])
			}
			result.Matches = append(result.Matches, FsGrepMatch{Path: fileRel, Line: line, Text: text})
			if len(result.Matches) >= maxResults {
				result.Truncated = true
				return
			}
		}
	})
	if err != nil {
		return FsGrepResult{}, err
	}
	result.Truncated = result.Truncated || truncated
	return result, nil
}
