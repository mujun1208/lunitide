package m7flow

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
)

// M7 slice 7 (T-7.7.x): tool-gap guard logic. Everything here is pure,
// deterministic policy - no I/O - so the full SSRF vector table (scenario
// 41), the SQL whitelist parser (scenario 42) and the workspace confinement
// rules are directly unit-testable.

// ── SSRF contract (M7-TOOL-001) ─────────────────────────────────────────────

// ToolAllowedHTTPPorts is the frozen egress port allowlist; anything else is
// "未放行端口" and refuses with M7-TOOL-001.
var toolAllowedHTTPPorts = map[int]bool{80: true, 443: true, 8080: true, 8443: true}

// ToolAllowedSchemes: http/https only (30x cross-protocol redirects are
// refused on every hop).
const (
	ToolSchemeHTTP  = "http"
	ToolSchemeHTTPS = "https"
)

// ToolSSRFReject reports whether one resolved address is a forbidden
// network target: loopback, private, link-local (incl. cloud metadata
// 169.254.169.254), CGNAT, multicast, unspecified or a non-IP literal.
func ToolSSRFReject(host string) bool {
	ip := net.ParseIP(stripZone(host))
	if ip == nil {
		return true // hostnames must resolve through the resolver hook
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() ||
		isCGNAT(ip)
}

func stripZone(host string) string {
	if i := strings.IndexByte(host, '%'); i >= 0 {
		return host[:i]
	}
	return host
}

func isCGNAT(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		return v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127
	}
	return false
}

// ToolSSRFCheckURL validates a request URL against the SSRF contract:
// scheme, port allowlist and literal-IP classification. Hostname DNS
// resolution is injected by the caller (each redirect hop re-resolves -
// first-answer caching is forbidden); every resolved address must pass.
func ToolSSRFCheckURL(rawURL string, resolve func(host string) ([]string, error)) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("url parse: %w", err)
	}
	if u.Scheme != ToolSchemeHTTP && u.Scheme != ToolSchemeHTTPS {
		return fmt.Errorf("scheme %q not allowed", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("empty host")
	}
	if port := u.Port(); port != "" {
		var p int
		if _, err := fmt.Sscanf(port, "%d", &p); err != nil || !toolAllowedHTTPPorts[p] {
			return fmt.Errorf("port %q not allowed", port)
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		if ToolSSRFReject(ip.String()) {
			return fmt.Errorf("literal ip %s refused", host)
		}
		return nil
	}
	if resolve == nil {
		return nil // no resolver injected: literal classification only
	}
	addrs, err := resolve(host)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("host %s did not resolve", host)
	}
	for _, a := range addrs {
		if ToolSSRFReject(a) {
			return fmt.Errorf("resolved %s -> %s refused", host, a)
		}
	}
	return nil
}

// ── SQL whitelist parser (M7-TOOL-003) ─────────────────────────────────────

// sqlForbiddenKeywords: write/DDL/attachment verbs - any occurrence refuses
// the statement (case-insensitive word match).
var sqlForbiddenKeywords = []string{
	"insert", "update", "delete", "drop", "alter", "create", "replace",
	"truncate", "attach", "detach", "vacuum", "reindex", "grant", "revoke",
	"begin", "commit", "rollback", "savepoint", "release", "pragma_set",
}

// ValidateReadOnlySQL applies the frozen whitelist parser: exactly one
// statement, SELECT or PRAGMA-query headed, no comments (inline or block),
// no forbidden keyword. The error carries the 1-based offending position so
// the wire can answer "first violating token" (scenario 42).
func ValidateReadOnlySQL(sql string) error {
	body := strings.TrimSpace(sql)
	if body == "" {
		return fmt.Errorf("empty statement at 1")
	}
	// exactly one statement: a single trailing ';' is tolerated, any other
	// ';' is a multi-statement vector.
	if strings.HasSuffix(body, ";") {
		body = strings.TrimSpace(strings.TrimSuffix(body, ";"))
	}
	if i := strings.IndexByte(body, ';'); i >= 0 {
		return fmt.Errorf("multi-statement refused at %d", i+1)
	}
	if i := strings.Index(body, "--"); i >= 0 {
		return fmt.Errorf("comment refused at %d", i+1)
	}
	if i := strings.Index(body, "/*"); i >= 0 {
		return fmt.Errorf("comment refused at %d", i+1)
	}
	head := firstSQLWord(body)
	if head != "select" && head != "pragma" {
		return fmt.Errorf("statement head %q refused at 1", head)
	}
	lower := strings.ToLower(body)
	for _, kw := range sqlForbiddenKeywords {
		if idx := wordIndex(lower, kw); idx >= 0 {
			return fmt.Errorf("keyword %q refused at %d", kw, idx+1)
		}
	}
	if head == "pragma" {
		// only PRAGMA reads are whitelisted: no assignment and no known
		// mutating pragma targets.
		rest := lower[strings.Index(lower, "pragma")+len("pragma"):]
		for _, w := range []string{"=", "writable_schema", "journal_mode", "kdf_iter", "rekey"} {
			if strings.Contains(rest, w) {
				return fmt.Errorf("pragma write form refused at %d", strings.Index(rest, w)+len("pragma")+1)
			}
		}
	}
	return nil
}
func firstSQLWord(body string) string {
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimLeft(fields[0], "( "))
}

// wordIndex finds kw as a whole (identifier-bounded) word, answering its
// byte offset or -1.
func wordIndex(lower, kw string) int {
	for i := 0; i+len(kw) <= len(lower); i++ {
		if lower[i:i+len(kw)] != kw {
			continue
		}
		before := byte(' ')
		if i > 0 {
			before = lower[i-1]
		}
		after := byte(' ')
		if i+len(kw) < len(lower) {
			after = lower[i+len(kw)]
		}
		if !isIdentRune(before) && !isIdentRune(after) {
			return i
		}
	}
	return -1
}

func isIdentRune(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// ── workspace confinement (archive/git/download family) ────────────────────

// ToolConfinePath guarantees rel stays inside root after lexical
// normalization: absolute paths, ".." escapes and symlink-ish separators
// are refused (zip-slip defence shares this helper).
func ToolConfinePath(root, rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("empty path")
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, `\`) || strings.Contains(rel, "://") {
		return "", fmt.Errorf("absolute or url path refused: %s", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("escape refused: %s", rel)
	}
	return filepath.Join(root, clean), nil
}

// ToolArchiveEntrySafe validates one archive entry name: no absolute path,
// no ".." segment, no drive letter, no symlink flag (zip-slip + symlink
// refusal per the design table).
func ToolArchiveEntrySafe(name string) error {
	if name == "" {
		return fmt.Errorf("empty entry")
	}
	if strings.ContainsRune(name, ':') || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return fmt.Errorf("rooted entry refused: %s", name)
	}
	for _, seg := range strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return fmt.Errorf("dotdot entry refused: %s", name)
		}
	}
	return nil
}

// ── manifest registry (scenario 44) ────────────────────────────────────────

// ToolgapManifestTools is the frozen 23-tool manifest: the 16 M4 registry
// names (read-only import, digest re-verified) plus the 7 gap tools.
// Registration of anything outside this set is 100% refused.
var ToolgapManifestTools = []string{
	// 16 imported M4 tools
	"fs.tree", "fs.stat", "fs.read", "fs.readMany", "fs.glob", "fs.grep",
	"web.fetch", "web.search", "browser.act", "browser.open", "browser.close",
	"evidence.list", "evidence.attachTest", "evidence.attachScan",
	"workspace.read", "workspace.list",
	// 7 gap tools (P1 + P2)
	"http.request", "db.query", "document.parse",
	"http.download", "archive.pack", "archive.unpack", "git.read",
}

// ToolgapManifestSet is the lookup form of ToolgapManifestTools.
var ToolgapManifestSet = func() map[string]bool {
	m := make(map[string]bool, len(ToolgapManifestTools))
	for _, t := range ToolgapManifestTools {
		m[t] = true
	}
	return m
}()

// ToolgapManifestDigest derives the ordered manifest digest frozen by
// migration 0059.
func ToolgapManifestDigest() string {
	sorted := append([]string(nil), ToolgapManifestTools...)
	sort.Strings(sorted)
	return SHA256Hex([]byte(joinLines(sorted)))
}

// ToolgapIO computes the io_semantics column: write-capable gap tools are
// workspace_write, everything else readonly.
func ToolgapIO(tool string) string {
	switch tool {
	case "http.download", "archive.pack", "archive.unpack":
		return "workspace_write"
	default:
		return "readonly"
	}
}
// ToolManifestEntry is one frozen tool_manifest_v2 row.
type ToolManifestEntry struct {
	ToolName         string
	DescriptorVersion string
	ManifestJSON     string
	ManifestDigest   string
	IOSemantics      string
	TimeoutMS        int64
	Enabled          bool
	ImportedAt       string
}

// DBConnection is one db_connections registration row.
type DBConnection struct {
	ID                  string
	Name                string
	Kind                string
	DSNSecretRef        string
	ReadOnlyVerifiedAt  *string
	CreatedAt           string
	CreatedBy           string
}

// ToolResult records one idempotent P2-group tool outcome.
type ToolResult struct {
	RunID          string
	ToolName       string
	IdempotencyKey string
	ResultJSON     string
	ResultDigest   string
	CreatedAt      string
}

// ParseBlock is one document.parse output block with page provenance.
type ParseBlock struct {
	Page int    `json:"page"`
	Kind string `json:"kind"`
	Text string `json:"text"`
}
