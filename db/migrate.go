package db

import (
	"context"
	"embed"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func RunMigrations(ctx context.Context) error {
	if _, err := Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INT PRIMARY KEY,
			label      TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}

	rows, err := Pool.Query(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("failed to query applied migrations: %w", err)
	}
	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan migration version: %w", err)
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error reading applied migrations: %w", err)
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}

		version, label, err := parseMigrationFilename(name)
		if err != nil {
			return fmt.Errorf("invalid migration filename %q: %w", name, err)
		}

		if applied[version] {
			continue
		}

		sql, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", name, err)
		}

		log.Printf("[Migrate] Applying v%04d: %s", version, label)

		tx, err := Pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin transaction for migration v%04d: %w", version, err)
		}

		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migration v%04d (%s) failed: %w", version, label, err)
		}

		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (version, label) VALUES ($1, $2)`,
			version, label,
		); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("failed to record migration v%04d: %w", version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit migration v%04d: %w", version, err)
		}

		log.Printf("[Migrate] v%04d applied", version)
	}

	log.Printf("[Migrate] All migrations up to date (%d total)", len(entries))
	return nil
}

// parseMigrationFilename extracts version and label from "0001_create_users.sql"
func parseMigrationFilename(name string) (int, string, error) {
	name = strings.TrimSuffix(name, ".sql")
	parts := strings.SplitN(name, "_", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("expected format NNNN_label.sql")
	}
	version, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", fmt.Errorf("version prefix %q is not a number", parts[0])
	}
	label := strings.ReplaceAll(parts[1], "_", " ")
	return version, label, nil
}
