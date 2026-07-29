// Package migrations embeds the append-only PostgreSQL schema migrations.
package migrations

import "embed"

// Files contains the immutable SQL migration files.
//
//go:embed *.sql
var Files embed.FS
