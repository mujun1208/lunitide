package datasourceapp

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestDriverNameMapsKnownKinds(t *testing.T) {
	cases := map[string]string{"postgres": "pgx", "mysql": "mysql"}
	for kind, want := range cases {
		got, err := driverName(kind)
		if err != nil || got != want {
			t.Fatalf("driverName(%q) = %q,%v want %q", kind, got, err, want)
		}
	}
	if _, err := driverName("oracle"); !errors.Is(err, ErrProbeUnavailable) {
		t.Fatalf("unknown kind err = %v want ErrProbeUnavailable", err)
	}
}

func TestOpenDBRegistersPureGoDrivers(t *testing.T) {
	// Open is lazy: a well-formed DSN must not error even though no database is
	// reachable. A failure here means the pure-Go driver did not register.
	for kind, dsn := range map[string]string{
		"postgres": "postgres://user:pass@127.0.0.1:5432/db?sslmode=disable",
		"mysql":    "user:pass@tcp(127.0.0.1:3306)/db",
	} {
		db, err := openDB(kind, dsn)
		if err != nil {
			t.Fatalf("openDB(%q) driver not registered: %v", kind, err)
		}
		_ = db.Close()
	}
}

func TestSQLPingerRejectsUnknownKind(t *testing.T) {
	if err := SQLPinger(context.Background(), "sqlserver", "whatever"); !errors.Is(err, ErrProbeUnavailable) {
		t.Fatalf("pinger err = %v want ErrProbeUnavailable", err)
	}
}

func TestSQLQuerierRejectsUnknownKind(t *testing.T) {
	_, _, _, err := SQLQuerier(context.Background(), "sqlserver", "dsn", "SELECT 1", nil, 10)
	if !errors.Is(err, ErrProbeUnavailable) {
		t.Fatalf("querier err = %v want ErrProbeUnavailable", err)
	}
}

func TestSQLPingerFailsFastOnUnreachableHost(t *testing.T) {
	// A read-only probe against a dead port must surface an error rather than
	// hang; the service wraps this in QueryTimeout but the driver must still
	// return promptly when the context is already short.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := SQLPinger(ctx, "postgres", "postgres://user:pass@127.0.0.1:1/db?sslmode=disable&connect_timeout=1"); err == nil {
		t.Fatal("expected an error pinging a dead port")
	}
}

func TestNormalizeRowStringifiesBytes(t *testing.T) {
	got := normalizeRow([]any{[]byte("PN-1234"), int64(5), nil, "A1"})
	want := []any{"PN-1234", int64(5), nil, "A1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeRow = %#v want %#v", got, want)
	}
}

func TestSQLProvisionerRejectsUnknownKind(t *testing.T) {
	if err := SQLProvisioner(context.Background(), "sqlserver", "dsn"); !errors.Is(err, ErrProbeUnavailable) {
		t.Fatalf("provisioner err = %v want ErrProbeUnavailable", err)
	}
}

func TestSQLProvisionerSkipsRemoteHosts(t *testing.T) {
	// Auto-create must never touch a remote server: the provisioner returns nil
	// without opening a connection when the host is not local.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := SQLProvisioner(ctx, "mysql", "root:pw@tcp(10.0.0.5:3306)/lunitide"); err != nil {
		t.Fatalf("remote mysql must be a no-op, got %v", err)
	}
	if err := SQLProvisioner(ctx, "postgres", "postgres://u:p@db.example.com:5432/lunitide?sslmode=disable"); err != nil {
		t.Fatalf("remote postgres must be a no-op, got %v", err)
	}
}

func TestSQLProvisionerRejectsUnsafeDatabaseName(t *testing.T) {
	if err := SQLProvisioner(context.Background(), "mysql", "root:pw@tcp(127.0.0.1:3306)/bad;name"); !errors.Is(err, ErrProvisionFailed) {
		t.Fatalf("mysql unsafe name err = %v want ErrProvisionFailed", err)
	}
	if err := SQLProvisioner(context.Background(), "postgres", "postgres://u:p@127.0.0.1:5432/bad;name?sslmode=disable"); !errors.Is(err, ErrProvisionFailed) {
		t.Fatalf("postgres unsafe name err = %v want ErrProvisionFailed", err)
	}
}

func TestSQLProvisionerSkipsPostgresMaintenanceTarget(t *testing.T) {
	// When the target is already the maintenance catalog there is nothing to
	// create, so no connection is opened.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := SQLProvisioner(ctx, "postgres", "postgres://u:p@127.0.0.1:5432/postgres?sslmode=disable"); err != nil {
		t.Fatalf("postgres maintenance target must be a no-op, got %v", err)
	}
}

func TestIsLocalDSNDetectsLocalAndRemote(t *testing.T) {
	for kind, dsn := range map[string]string{
		"mysql":    "root:pw@tcp(127.0.0.1:3306)/lunitide",
		"postgres": "postgres://u:p@localhost:5432/lunitide?sslmode=prefer",
	} {
		if !IsLocalDSN(kind, dsn) {
			t.Fatalf("%s dsn should be local: %s", kind, dsn)
		}
	}
	for kind, dsn := range map[string]string{
		"mysql":    "root:pw@tcp(10.0.0.5:3306)/lunitide",
		"postgres": "postgres://u:p@db.example.com:5432/ops",
		"oracle":   "whatever",
	} {
		if IsLocalDSN(kind, dsn) {
			t.Fatalf("%s dsn should be remote: %s", kind, dsn)
		}
	}
}

func TestIsRowReturningClassifiesStatements(t *testing.T) {
	for _, s := range []string{"SELECT 1", " select * from t", "WITH x AS (SELECT 1) SELECT * FROM x", "SHOW TABLES", "(SELECT 1)", "EXPLAIN SELECT 1"} {
		if !isRowReturning(s) {
			t.Fatalf("expected row-returning: %q", s)
		}
	}
	for _, s := range []string{"INSERT INTO t VALUES (1)", "UPDATE t SET a=1", "DELETE FROM t", "CREATE TABLE t(id int)", "DROP TABLE t", "   "} {
		if isRowReturning(s) {
			t.Fatalf("expected non-row-returning: %q", s)
		}
	}
}

func TestSQLWriteQuerierRejectsUnknownKind(t *testing.T) {
	_, _, _, err := SQLWriteQuerier(context.Background(), "sqlserver", "dsn", "SELECT 1", nil, 10)
	if !errors.Is(err, ErrProbeUnavailable) {
		t.Fatalf("write querier err = %v want ErrProbeUnavailable", err)
	}
}
