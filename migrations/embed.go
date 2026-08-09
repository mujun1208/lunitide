package migrations

import "embed"

// Files contains the immutable, numbered SQLite migrations. Every embedded SQL
// file must also appear in sqlite's ordered manifest with its SHA-256 digest.
//
//go:embed *.sql
var Files embed.FS
