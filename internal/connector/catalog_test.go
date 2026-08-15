package connector

import (
	"errors"
	"testing"
)

// M6-DB-001 statement side: every DML/DDL verb is rejected, business-row
// SELECT targets are rejected, and only metadata targets pass.
func TestReadOnlyStatementAllowlist(t *testing.T) {
	allowed := []string{
		"SELECT table_name FROM information_schema.tables",
		"select * from pg_catalog.pg_tables",
		"SELECT name FROM sqlite_master",
		"SELECT c.column_name FROM information_schema.columns AS c",
		"PRAGMA table_info(orders)",
		"SHOW TABLES",
		"EXPLAIN SELECT * FROM information_schema.tables",
	}
	for _, q := range allowed {
		if err := StatementAllowed(q); err != nil {
			t.Errorf("want allowed %q, got %v", q, err)
		}
	}
	denied := map[string]string{
		"INSERT INTO app_users VALUES (1)":                   "insert",
		"UPDATE app_users SET n=1":                           "update",
		"DELETE FROM app_users":                              "delete",
		"CREATE TABLE t (id int)":                            "create",
		"DROP TABLE t":                                       "drop",
		"ALTER TABLE t ADD c int":                            "alter",
		"TRUNCATE TABLE t":                                   "truncate",
		"GRANT ALL ON t TO u":                                "grant",
		"SELECT * FROM app_users":                            "business target",
		"SELECT u.name FROM app.public.users u":              "schema-qualified business target",
		"SELECT * FROM information_schema.tables; SELECT 1":  "multi statement",
		"SELECT * INTO new_t FROM information_schema.tables": "select into",
		"SELECT * FROM information_schema.tables FOR UPDATE": "for update",
		"WITH x AS (SELECT 1) INSERT INTO t SELECT * FROM x": "cte write",
		"SELECT * FROM sqlite_master; DELETE FROM app_users": "trailing second statement",
		"": "empty",
	}
	for q, why := range denied {
		if err := StatementAllowed(q); err == nil {
			t.Errorf("want denied %q (%s), got allowed", q, why)
		} else if !errors.Is(err, ErrStatementDenied) && !errors.Is(err, ErrInvalidSQL) {
			t.Errorf("deny %q: unexpected error %v", q, err)
		}
	}
}

// M6-DB-001 driver side: only the read-only driver surface is reachable.
func TestReadOnlyDriverMethodAllowlist(t *testing.T) {
	for _, m := range []string{"Query", "QueryContext", "QueryRow", "QueryRowContext", "Ping", "PingContext", "Close"} {
		if err := DriverMethodAllowed(m); err != nil {
			t.Errorf("want allowed %s, got %v", m, err)
		}
	}
	for _, m := range []string{"Exec", "ExecContext", "Begin", "BeginTx", "Prepare", "PrepareContext"} {
		if err := DriverMethodAllowed(m); err == nil {
			t.Errorf("want denied %s, got allowed", m)
		} else if !errors.Is(err, ErrDriverMethodDenied) {
			t.Errorf("deny %s: unexpected error %v", m, err)
		}
	}
}

// CheckAccess composes the double allowlist: driver first, statement second,
// scope third.
func TestReadOnlyCheckAccessOrder(t *testing.T) {
	if err := CheckAccess("Exec", "SELECT * FROM information_schema.tables", "table"); !errors.Is(err, ErrDriverMethodDenied) {
		t.Fatalf("want driver denial first, got %v", err)
	}
	if err := CheckAccess("Query", "DELETE FROM t", "table"); !errors.Is(err, ErrStatementDenied) {
		t.Fatalf("want statement denial second, got %v", err)
	}
	if err := CheckAccess("Query", "SELECT * FROM information_schema.tables", "rows"); !errors.Is(err, ErrScopeDenied) {
		t.Fatalf("want scope denial third, got %v", err)
	}
	if err := CheckAccess("Query", "SELECT * FROM information_schema.tables", "table"); err != nil {
		t.Fatalf("want allowed, got %v", err)
	}
}

// Scope bound (DB-002) and identifier contract are pure checks reused by
// the application service.
func TestReadOnlyScopeAndIdentifierBounds(t *testing.T) {
	for _, s := range []string{"catalog", "schema", "table", "column", "index", "constraint"} {
		if !MetadataScopes[s] {
			t.Errorf("scope %s must be in the frozen enum", s)
		}
	}
	for _, s := range []string{"rows", "data", "", "TABLE"} {
		if MetadataScopes[s] {
			t.Errorf("scope %q must be out of bounds", s)
		}
	}
	for _, id := range []string{"pg-main", "a", "x-1"} {
		if !ConnectorIDPattern.MatchString(id) {
			t.Errorf("connectorId %q must match", id)
		}
	}
	for _, id := range []string{"Bad_ID", "1abc", "-x", "", "a_b"} {
		if ConnectorIDPattern.MatchString(id) {
			t.Errorf("connectorId %q must be rejected", id)
		}
	}
}
