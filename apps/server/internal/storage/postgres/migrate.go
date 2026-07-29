package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/Thespectier/AgentsharkX/apps/server/migrations"
	"github.com/jackc/pgx/v5"
)

const migrationLockID int64 = 0x41534841524b5801

func (store *Store) Migrate(ctx context.Context) error {
	connection, err := store.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer connection.Release()
	if _, err := connection.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return err
	}
	defer func() { _, _ = connection.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockID) }()
	if _, err := connection.Exec(ctx, `
CREATE TABLE IF NOT EXISTS agentshark_schema_migrations (
    version text PRIMARY KEY,
    checksum text NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now()
)
`); err != nil {
		return err
	}
	files, err := migrationFiles()
	if err != nil {
		return err
	}
	for _, name := range files {
		document, err := fs.ReadFile(migrations.Files, name)
		if err != nil {
			return err
		}
		checksum := migrationChecksum(document)
		var stored string
		err = connection.QueryRow(ctx, `SELECT checksum FROM agentshark_schema_migrations WHERE version = $1`, name).Scan(&stored)
		if err == nil {
			if stored != checksum {
				return fmt.Errorf("migration %s checksum changed", name)
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		transaction, err := connection.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = transaction.Exec(ctx, string(document)); err == nil {
			_, err = transaction.Exec(ctx, `
INSERT INTO agentshark_schema_migrations (version, checksum) VALUES ($1, $2)
`, name, checksum)
		}
		if err != nil {
			_ = transaction.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}

func (store *Store) Ready(ctx context.Context) error {
	if err := store.pool.Ping(ctx); err != nil {
		return err
	}
	files, err := migrationFiles()
	if err != nil {
		return err
	}
	for _, name := range files {
		document, err := fs.ReadFile(migrations.Files, name)
		if err != nil {
			return err
		}
		var checksum string
		if err := store.pool.QueryRow(ctx, `
SELECT checksum FROM agentshark_schema_migrations WHERE version = $1
`, name).Scan(&checksum); err != nil {
			return fmt.Errorf("migration %s is not applied: %w", name, err)
		}
		if checksum != migrationChecksum(document) {
			return fmt.Errorf("migration %s checksum does not match", name)
		}
	}
	return nil
}

func migrationFiles() ([]string, error) {
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

func migrationChecksum(document []byte) string {
	digest := sha256.Sum256(document)
	return hex.EncodeToString(digest[:])
}
