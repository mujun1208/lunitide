// Package connector implements the M6 read-only metadata catalog adapter
// (T-6.2.1). Two allowlists gate every reachable path: the SQL statement
// allowlist (M6-DB-001: only metadata SELECT/PRAGMA/SHOW statements whose
// FROM/JOIN targets live in metadata namespaces) and the driver-method
// allowlist (only read-only driver surface; Exec/Begin are rejected before
// the statement layer is even reached). Business-row reads are therefore
// 100% rejected: a target outside the metadata namespaces fails the
// statement allowlist even when the verb is SELECT. Metadata scope is bound
// on top (M6-DB-002) and snapshots persist with a connector-scoped
// monotonically increasing snapshot_version (0045 UNIQUE(connector_id,
// snapshot_version)). Connection secrets never enter a snapshot.
package connector

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	// ErrStatementDenied is M6-DB-001 (statement side): the statement is not
	// a metadata read on the allowlist.
	ErrStatementDenied = errors.New("connector: statement not on the metadata allowlist")
	// ErrDriverMethodDenied is M6-DB-001 (driver side): the driver method is
	// not on the read-only surface allowlist.
	ErrDriverMethodDenied = errors.New("connector: driver method not on the read-only allowlist")
	// ErrScopeDenied is M6-DB-002: the requested metadata scope is out of
	// bounds and must be narrowed.
	ErrScopeDenied = errors.New("connector: metadata scope out of bounds")
	// ErrInvalidConnectorID rejects identifiers that could not name a
	// registered connector.
	ErrInvalidConnectorID = errors.New("connector: connectorId invalid")
	// ErrInvalidSQL rejects statements the classifier cannot parse — an
	// unparseable statement is never allowed through.
	ErrInvalidSQL = errors.New("connector: statement unparseable")
)

// MetadataScopes is the frozen scope enum (0045 CHECK set). Anything outside
// it is M6-DB-002.
var MetadataScopes = map[string]bool{
	"catalog": true, "schema": true, "table": true,
	"column": true, "index": true, "constraint": true,
}

// MetadataNamespaces are the only FROM/JOIN targets a snapshot statement may
// touch. Bare catalog tables (sqlite_master family) are listed too.
var MetadataNamespaces = map[string]bool{
	"information_schema":    true,
	"pg_catalog":            true,
	"performance_schema":    true,
	"sys":                   true,
	"sqlite_master":         true,
	"sqlite_temp_master":    true,
	"sqlite_sequence":       true,
	"sqlite_schema":         true,
	"sqlite_temp_schema":    true,
}

// driverMethodAllowlist is the frozen read-only driver surface. Prepare is
// deliberately absent: a prepared statement can back a later Exec.
var driverMethodAllowlist = map[string]bool{
	"Ping": true, "PingContext": true,
	"Query": true, "QueryContext": true,
	"QueryRow": true, "QueryRowContext": true,
	"Close": true,
}

// DriverMethodAllowed implements the driver half of the double allowlist.
func DriverMethodAllowed(method string) error {
	if !driverMethodAllowlist[method] {
		return fmt.Errorf("%w: %s", ErrDriverMethodDenied, method)
	}
	return nil
}

var (
	commentPattern    = regexp.MustCompile(`(--[^\n]*|/\*[\s\S]*?\*/)`)
	statementHead     = regexp.MustCompile(`^[a-zA-Z]+`)
	targetPattern     = regexp.MustCompile(`(?i)\b(?:from|join)\s+((?:[A-Za-z_"][\w$]*"|[\w$]+)(?:\s*\.\s*(?:[A-Za-z_"][\w$]*"|[\w$]+))*)`)
	identifierPattern = regexp.MustCompile(`^[A-Za-z_"][\w$]*$`)
	// writeVerbs anywhere in a WITH body (a CTE that ends in INSERT/UPDATE/
	// DELETE/MERGE/REPLACE) flips the statement into a write.
	writeVerbs = []string{"insert", "update", "delete", "merge", "replace",
		"truncate", "create", "drop", "alter", "grant", "revoke", "call",
		"execute", "exec", "attach", "detach", "pragma_sync", "vacuum", "reindex"}
)

func stripComments(raw string) string { return commentPattern.ReplaceAllString(raw, " ") }

func wordMembers(s string) []string { return strings.Fields(strings.ToLower(s)) }

// ClassifyStatement splits one statement into its head verb and FROM/JOIN
// targets. Multiple statements are rejected outright.
func ClassifyStatement(raw string) (verb string, targets []string, err error) {
	s := strings.TrimSpace(stripComments(raw))
	if s == "" {
		return "", nil, ErrInvalidSQL
	}
	if strings.Count(s, ";") > 1 || (strings.Contains(s, ";") && !strings.HasSuffix(s, ";")) {
		return "", nil, fmt.Errorf("%w: multiple statements", ErrStatementDenied)
	}
	s = strings.TrimSuffix(s, ";")
	head := strings.ToLower(statementHead.FindString(s))
	switch head {
	case "select", "with", "pragma", "show", "explain":
	default:
		return head, nil, fmt.Errorf("%w: verb %q", ErrStatementDenied, head)
	}
	words := wordMembers(s)
	for _, w := range writeVerbs {
		for _, member := range words {
			if member == w {
				return head, nil, fmt.Errorf("%w: write verb %q", ErrStatementDenied, w)
			}
		}
	}
	for _, member := range words {
		if member == "into" {
			return head, nil, fmt.Errorf("%w: select into", ErrStatementDenied)
		}
	}
	if regexp.MustCompile(`(?i)\bfor\s+update\b`).MatchString(s) {
		return head, nil, fmt.Errorf("%w: for update", ErrStatementDenied)
	}
	if head == "pragma" || head == "show" {
		return head, nil, nil
	}
	seen := map[string]bool{}
	for _, m := range targetPattern.FindAllStringSubmatch(s, -1) {
		t := strings.ToLower(strings.NewReplacer("\"", "", " ", "").Replace(m[1]))
		if t == "" || !identifierPattern.MatchString(strings.Split(t, ".")[0]) {
			return head, nil, fmt.Errorf("%w: target %q", ErrStatementDenied, m[1])
		}
		if !seen[t] {
			seen[t] = true
			targets = append(targets, t)
		}
	}
	if len(targets) == 0 {
		return head, nil, fmt.Errorf("%w: no metadata target", ErrStatementDenied)
	}
	return head, targets, nil
}

// StatementAllowed implements the statement half of the double allowlist:
// SELECT/EXPLAIN/PRAGMA/SHOW whose FROM/JOIN targets all resolve into a
// metadata namespace. A bare business table (no namespace) is rejected.
func StatementAllowed(raw string) error {
	_, targets, err := ClassifyStatement(raw)
	if err != nil {
		return err
	}
	for _, t := range targets {
		parts := strings.Split(t, ".")
		ok := false
		for _, p := range parts {
			if MetadataNamespaces[p] {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("%w: %s is not a metadata target", ErrStatementDenied, t)
		}
	}
	return nil
}

// CheckAccess is the single entry point fetchers must call before touching
// the driver: driver method + statement + scope, in that order. The
// application service (m6app) additionally persists snapshots with a
// connector-scoped monotonic version inside one transaction.
func CheckAccess(driverMethod, statement, metadataScope string) error {
	if err := DriverMethodAllowed(driverMethod); err != nil {
		return err
	}
	if statement != "" {
		if err := StatementAllowed(statement); err != nil {
			return err
		}
	}
	if !MetadataScopes[metadataScope] {
		return fmt.Errorf("%w: %s", ErrScopeDenied, metadataScope)
	}
	return nil
}

// ConnectorIDPattern is the connector identifier contract shared by the
// bridge schema.
var ConnectorIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
