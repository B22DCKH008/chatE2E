// Package migrations embeds forward-only, transaction-safe SQL migrations.
package migrations

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.up.sql
var files embed.FS

type migration struct{ name, sql, checksum string }

func catalog() ([]migration, error) {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil, err
	}
	var list []migration
	for _, entry := range entries {
		body, err := files.ReadFile(entry.Name())
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(body)
		list = append(list, migration{entry.Name(), string(body), hex.EncodeToString(sum[:])})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].name < list[j].name })
	return list, nil
}

func Up(ctx context.Context, pool *pgxpool.Pool) error {
	list, err := catalog()
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	// Transaction-scoped lock serializes independent migration processes.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(736563757265)`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, checksum TEXT NOT NULL, applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return err
	}
	applied := map[string]string{}
	for rows.Next() {
		var version, checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			rows.Close()
			return err
		}
		applied[version] = checksum
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	known := map[string]bool{}
	for _, m := range list {
		known[m.name] = true
	}
	for version := range applied {
		if !known[version] {
			return fmt.Errorf("database contains unknown migration %s", version)
		}
	}
	for _, m := range list {
		if checksum, exists := applied[m.name]; exists {
			if checksum != m.checksum {
				return fmt.Errorf("migration checksum mismatch: %s", m.name)
			}
			continue
		}
		if _, err := tx.Exec(ctx, m.sql, pgx.QueryExecModeSimpleProtocol); err != nil {
			return fmt.Errorf("migration %s failed: %w", m.name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version, checksum) VALUES ($1, $2)`, m.name, m.checksum); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func Check(ctx context.Context, pool *pgxpool.Pool) error {
	list, err := catalog()
	if err != nil {
		return err
	}
	for _, m := range list {
		var checksum string
		if err := pool.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version = $1`, m.name).Scan(&checksum); err != nil {
			return err
		}
		if checksum != m.checksum {
			return fmt.Errorf("schema version mismatch")
		}
	}
	return nil
}
