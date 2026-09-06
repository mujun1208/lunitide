package datasourceapp

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestLocalityUsesEveryEffectivePostgresHost(t *testing.T) {
	for _, dsn := range []string{
		"postgres://u:p@127.0.0.1/db?host=192.0.2.10&sslmode=disable",
		"postgres://u:p@127.0.0.1,192.0.2.10/db?sslmode=disable",
		"host=127.0.0.1,192.0.2.10 user=u dbname=db sslmode=disable",
		"postgres://u:p@0.0.0.0/db?sslmode=disable",
	} {
		t.Run(dsn, func(t *testing.T) {
			if IsLocalDSN("postgres", dsn) {
				t.Fatal("remote/unspecified effective endpoint was authorized as local")
			}
			// These must return without dialing, even with a live context.
			if err := SQLProvisioner(context.Background(), "postgres", dsn); err != nil {
				t.Fatalf("remote auto-provision should be a no-op: %v", err)
			}
			if _, _, _, err := SQLWriteQuerier(context.Background(), "postgres", dsn, "DELETE FROM fixture", nil, 1); !errors.Is(err, ErrStatementDenied) {
				t.Fatalf("write driver failed to enforce locality: %v", err)
			}
		})
	}
	if !IsLocalDSN("postgres", "host=127.0.0.1 user=u dbname=db sslmode=disable") {
		t.Fatal("valid keyword DSN rejected")
	}
	if !IsLocalDSN("mysql", "u:p@tcp([::1]:3306)/db") {
		t.Fatal("IPv6 loopback rejected")
	}
}

func TestPrivilegedDialRejectsRemoteDestination(t *testing.T) {
	for _, addr := range []string{"192.0.2.10:5432", "db.example.com:5432", "[::]:5432", "0.0.0.0:5432"} {
		if _, err := dialLoopback(context.Background(), "tcp", addr); !errors.Is(err, ErrStatementDenied) {
			t.Fatalf("dial %q: %v", addr, err)
		}
	}
}

func TestRedactedQueryErrorDoesNotReformatSecretCause(t *testing.T) {
	for _, raw := range []string{
		"postgres://u:needle-secret@db.internal/ops failed",
		"user=u password='needle-secret' host=db.internal failed",
		"password=needle-secret connection failed",
	} {
		cause := errors.New(raw)
		err := fmtRedacted(cause, cause)
		if strings.Contains(err.Error(), "needle-secret") || !errors.Is(err, cause) {
			t.Fatalf("credential not redacted or cause lost: %v", err)
		}
	}
}
