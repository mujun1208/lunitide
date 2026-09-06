package datasourceapp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"time"

	// Pure-Go drivers only: both register with database/sql at init and build
	// cleanly under CGO_ENABLED=0 (the Lunitide production default). Do not swap
	// in a CGO driver without updating the dialect checklist.
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/stdlib"
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
const (
	MaxResultBytes     = 512 << 10
	MaxResultCellBytes = 256 << 10
	MaxResultColumns   = 128
	maxSQLWireBytes    = 8 << 20
)

var ErrResultBudget = errors.New("database result exceeds safety budget; narrow selected columns or rows")

// SQL handles are short lived. The socket budget bounds driver input as well as
// the JSON output; MySQL's fixed 24-bit packet length bounds its one-packet buffer.
type sqlBudgetConn struct {
	net.Conn
	remaining int64
}

func (c *sqlBudgetConn) Read(p []byte) (int, error) {
	if c.remaining <= 0 {
		return 0, ErrResultBudget
	}
	if int64(len(p)) > c.remaining {
		p = p[:c.remaining]
	}
	n, err := c.Conn.Read(p)
	c.remaining -= int64(n)
	return n, err
}
func sqlDial(local bool) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		var conn net.Conn
		var err error
		if local {
			conn, err = dialLoopback(ctx, network, addr)
		} else {
			conn, err = (&net.Dialer{}).DialContext(ctx, network, addr)
		}
		if err != nil {
			return nil, err
		}
		return &sqlBudgetConn{Conn: conn, remaining: maxSQLWireBytes}, nil
	}
}
func openDB(kind, dsn string) (*sql.DB, error) { return openSQLDB(kind, dsn, false) }
func openSQLDB(kind, dsn string, local bool) (*sql.DB, error) {
	if _, err := driverName(kind); err != nil {
		return nil, err
	}
	var db *sql.DB
	if kind == "postgres" {
		cfg, err := pgx.ParseConfig(dsn)
		if err != nil {
			return nil, err
		}
		if local && !localPostgresConfig(cfg) {
			return nil, ErrStatementDenied
		}
		cfg.DialFunc = sqlDial(local)
		cfg.BuildFrontend = func(r io.Reader, w io.Writer) *pgproto3.Frontend {
			frontend := pgproto3.NewFrontend(r, w)
			frontend.SetMaxBodyLen(1 << 20)
			return frontend
		}
		db = stdlib.OpenDB(*cfg)
	} else {
		cfg, err := mysqldriver.ParseDSN(dsn)
		if err != nil {
			return nil, err
		}
		if local && !localMySQLConfig(cfg) {
			return nil, ErrStatementDenied
		}
		cfg.DialFunc = sqlDial(local)
		connector, err := mysqldriver.NewConnector(cfg)
		if err != nil {
			return nil, err
		}
		db = sql.OpenDB(connector)
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
	if len(cols) > MaxResultColumns {
		return nil, nil, false, ErrResultBudget
	}
	maxRows = min(max(maxRows, 1), 1000)
	names, err := json.Marshal(cols)
	if err != nil || len(names) > MaxResultBytes/4 {
		return nil, nil, false, ErrResultBudget
	}
	used := len(names) + 32
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
		rawBytes := 0
		over := false
		for _, value := range scan {
			size := 32
			switch v := value.(type) {
			case []byte:
				size = len(v)
			case string:
				size = len(v)
			}
			rawBytes += size
			if size > MaxResultCellBytes || rawBytes > MaxResultBytes {
				over = true
				break
			}
		}
		if over {
			truncated = true
			break
		}
		normalized := normalizeRow(scan)
		encoded, err := json.Marshal(normalized)
		if err != nil {
			return nil, nil, false, err
		}
		if used+len(encoded)+1 > MaxResultBytes {
			truncated = true
			break
		}
		used += len(encoded) + 1
		out = append(out, normalized)
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
	case "localhost", "127.0.0.1", "::1":
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
	if !localMySQLConfig(cfg) {
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
	cfg.DialFunc = dialLoopback
	connector, err := mysqldriver.NewConnector(cfg)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProvisionFailed, err)
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	if _, err := db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS `"+target+"`"); err != nil {
		return fmt.Errorf("%w: %v", ErrProvisionFailed, err)
	}
	return nil
}

func provisionPostgres(ctx context.Context, dsn string) error {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("%w: invalid PostgreSQL configuration", ErrProvisionFailed)
	}
	if !localPostgresConfig(cfg) {
		return nil
	}
	target := cfg.Database
	if target == "" || strings.EqualFold(target, "postgres") {
		return nil
	}
	if !reSafeIdent.MatchString(target) {
		return fmt.Errorf("%w: unsafe database name", ErrProvisionFailed)
	}
	// Execute the exact configuration that passed validation, including URI
	// query overrides, keyword DSNs, environment settings, and fallback hosts.
	cfg.Database = "postgres"
	cfg.DialFunc = dialLoopback
	db := stdlib.OpenDB(*cfg)
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
		return localMySQLConfig(cfg)
	case "postgres":
		cfg, err := pgx.ParseConfig(dsn)
		if err != nil {
			return false
		}
		return localPostgresConfig(cfg)
	default:
		return false
	}
}

func localMySQLConfig(cfg *mysqldriver.Config) bool {
	host, _, err := net.SplitHostPort(cfg.Addr)
	return err == nil && cfg.Net == "tcp" && isLocalHost(host)
}

func localPostgresConfig(cfg *pgx.ConnConfig) bool {
	if !isLocalHost(cfg.Host) {
		return false
	}
	for _, fallback := range cfg.Fallbacks {
		if !isLocalHost(fallback.Host) {
			return false
		}
	}
	return true
}

// Recheck the actual socket destination: a local-looking hostname or driver
// fallback must never turn into a remote privileged connection after parsing.
func dialLoopback(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || network != "tcp" {
		return nil, ErrStatementDenied
	}
	if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, ErrStatementDenied
	}
	return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(host, port))
}

func openLocalWriteDB(kind, dsn string) (*sql.DB, error) { return openSQLDB(kind, dsn, true) }

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
	db, err := openLocalWriteDB(kind, dsn)
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
