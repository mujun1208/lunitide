package datasourceapp

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	// Pure-Go drivers only: both register with database/sql at init and build
	// cleanly under CGO_ENABLED=0 (the Lunitide production default). Do not swap
	// in a CGO driver without updating the dialect checklist.
	mysqldriver "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// driverName maps a connection kind to its registered database/sql driver.
func driverName(kind string) (string, error) {
	switch kind {
	case "postgres":
		return "pgx", nil
	case "mysql":
		return "mysql", nil
	default:
		return "", fmt.Errorf("%w: kind %q", ErrProbeUnavailable, kind)
	}
}

// openDB opens a short-lived pooled handle. Open is lazy (no socket yet); the
// caller drives the connection under the service's QueryTimeout.
func openDB(kind, dsn string) (*sql.DB, error) {
	name, err := driverName(kind)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(name, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Minute)
	return db, nil
}

// SQLPinger is the production Pinger: it verifies a connection is reachable and
// answers a trivial SELECT 1 inside a read-only transaction. The read-only tx
// is defence in depth on top of the service-layer SQL allowlists — a source
// bound here can never mutate the customer database.
func SQLPinger(ctx context.Context, kind, dsn string) error {
	db, err := openDB(kind, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var one int
	if err := tx.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return err
	}
	return nil
}

// SQLQuerier is the production Querier: it runs one already-validated read-only
// statement in a read-only transaction and returns up to maxRows rows. []byte
// values are surfaced as strings so the bridge envelope stays JSON-clean.
func SQLQuerier(ctx context.Context, kind, dsn, statement string, args []any, maxRows int) ([]string, [][]any, bool, error) {
	db, err := openDB(kind, dsn)
	if err != nil {
		return nil, nil, false, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, nil, false, err
	}
	defer rows.Close()
	return scanRows(rows, maxRows)
}

// scanRows collects up to maxRows rows, flags truncation, and stringifies []byte
// columns. Shared by the read-only and read-write query paths.
func scanRows(rows *sql.Rows, maxRows int) ([]string, [][]any, bool, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, false, err
	}
	out := make([][]any, 0, maxRows)
	truncated := false
	for rows.Next() {
		if len(out) >= maxRows {
			truncated = true
			break
		}
		scan := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range scan {
			ptrs[i] = &scan[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, false, err
		}
		out = append(out, normalizeRow(scan))
	}
	if err := rows.Err(); err != nil {
		return nil, nil, false, err
	}
	return cols, out, truncated, nil
}

// normalizeRow makes driver-native values JSON friendly: raw []byte (BLOB /
// TEXT for the mysql driver) becomes a string; everything else passes through.
func normalizeRow(row []any) []any {
	out := make([]any, len(row))
	for i, v := range row {
		if b, ok := v.([]byte); ok {
			out[i] = string(b)
			continue
		}
		out[i] = v
	}
	return out
}

// reSafeIdent guards the database name before it is interpolated into a CREATE
// DATABASE statement — identifiers can never be bound as parameters, so the name
// must be validated as a plain SQL identifier first.
var reSafeIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

// isLocalHost reports whether a host points at the machine running Lunitide.
// Auto-create is deliberately restricted to a local server so the tool never
// creates databases on a remote customer host.
func isLocalHost(host string) bool {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]")) {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	}
	return false
}

// SQLProvisioner is the production auto-create step wired into Probe. For a LOCAL
// connection it ensures the target database named in the DSN exists, creating it
// if missing so a first-time user only needs an account + password. Remote
// connections are a no-op — the tool must not create databases on someone else's
// server. Creating a database is DDL and needs an account with the privilege;
// the read-only query path is unchanged.
func SQLProvisioner(ctx context.Context, kind, dsn string) error {
	switch kind {
	case "mysql":
		return provisionMySQL(ctx, dsn)
	case "postgres":
		return provisionPostgres(ctx, dsn)
	default:
		return fmt.Errorf("%w: kind %q", ErrProbeUnavailable, kind)
	}
}

func provisionMySQL(ctx context.Context, dsn string) error {
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProvisionFailed, err)
	}
	host := cfg.Addr
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	if cfg.Net != "tcp" || !isLocalHost(host) {
		return nil
	}
	target := strings.TrimSpace(cfg.DBName)
	if target == "" {
		return nil
	}
	if !reSafeIdent.MatchString(target) {
		return fmt.Errorf("%w: unsafe database name", ErrProvisionFailed)
	}
	cfg.DBName = ""
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProvisionFailed, err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS `"+target+"`"); err != nil {
		return fmt.Errorf("%w: %v", ErrProvisionFailed, err)
	}
	return nil
}

func provisionPostgres(ctx context.Context, dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" {
		return nil
	}
	if !isLocalHost(u.Hostname()) {
		return nil
	}
	target := strings.TrimPrefix(u.Path, "/")
	if target == "" || strings.EqualFold(target, "postgres") {
		return nil
	}
	if !reSafeIdent.MatchString(target) {
		return fmt.Errorf("%w: unsafe database name", ErrProvisionFailed)
	}
	maint := *u
	maint.Path = "/postgres"
	db, err := sql.Open("pgx", maint.String())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProvisionFailed, err)
	}
	defer db.Close()
	var exists bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=$1)", target).Scan(&exists); err != nil {
		return fmt.Errorf("%w: %v", ErrProvisionFailed, err)
	}
	if exists {
		return nil
	}
	// CREATE DATABASE cannot run inside a transaction and has no IF NOT EXISTS,
	// which is why existence is checked first. The name is validated above.
	if _, err := db.ExecContext(ctx, `CREATE DATABASE "`+target+`"`); err != nil {
		return fmt.Errorf("%w: %v", ErrProvisionFailed, err)
	}
	return nil
}

// IsLocalDSN reports whether a DSN targets the machine running Lunitide. Both
// auto-create and the read-write query path are gated on this so a remote /
// customer database keeps the strict read-only guarantee.
func IsLocalDSN(kind, dsn string) bool {
	switch kind {
	case "mysql":
		cfg, err := mysqldriver.ParseDSN(dsn)
		if err != nil {
			return false
		}
		host := cfg.Addr
		if i := strings.LastIndex(host, ":"); i >= 0 {
			host = host[:i]
		}
		return cfg.Net == "tcp" && isLocalHost(host)
	case "postgres":
		u, err := url.Parse(dsn)
		if err != nil {
			return false
		}
		return isLocalHost(u.Hostname())
	default:
		return false
	}
}

// isRowReturning reports whether a statement yields a result set and so must run
// via Query rather than Exec on the read-write path. Conservative: anything not
// clearly row-returning (INSERT/UPDATE/DELETE/CREATE/…) goes through Exec.
func isRowReturning(statement string) bool {
	s := strings.TrimLeft(strings.TrimSpace(statement), "(")
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return false
	}
	switch strings.ToUpper(fields[0]) {
	case "SELECT", "WITH", "SHOW", "PRAGMA", "EXPLAIN", "VALUES", "TABLE", "DESCRIBE", "DESC":
		return true
	}
	return false
}

// SQLWriteQuerier is the read-WRITE execution path used ONLY for local
// connections (see IsLocalDSN); remote connections never reach it. Row-returning
// statements stream up to maxRows; any other statement runs via Exec and reports
// rows_affected. Unlike the read-only SQLQuerier it commits the transaction.
func SQLWriteQuerier(ctx context.Context, kind, dsn, statement string, args []any, maxRows int) ([]string, [][]any, bool, error) {
	db, err := openDB(kind, dsn)
	if err != nil {
		return nil, nil, false, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, false, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if !isRowReturning(statement) {
		res, err := tx.ExecContext(ctx, statement, args...)
		if err != nil {
			return nil, nil, false, err
		}
		if err := tx.Commit(); err != nil {
			return nil, nil, false, err
		}
		committed = true
		affected, _ := res.RowsAffected()
		return []string{"rows_affected"}, [][]any{{affected}}, false, nil
	}
	rows, err := tx.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, nil, false, err
	}
	cols, out, truncated, err := scanRows(rows, maxRows)
	_ = rows.Close()
	if err != nil {
		return nil, nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, false, err
	}
	committed = true
	return cols, out, truncated, nil
}
