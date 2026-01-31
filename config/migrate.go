package config

import (
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"time"
)

//go:embed migration/*.sql
var migrationFS embed.FS

func ApplyMigrations(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}

	if _, err := db.Exec(
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			id TEXT PRIMARY KEY,
			checksum TEXT NOT NULL,
			applied_at TEXT NOT NULL
		);`,
	); err != nil {
		return err
	}

	paths, err := fs.Glob(migrationFS, "migration/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(paths)

	for _, p := range paths {
		id := path.Base(p)
		b, err := migrationFS.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		checksum := hex.EncodeToString(sum[:])

		var existing string
		err = db.QueryRow(`SELECT checksum FROM schema_migrations WHERE id = ?`, id).Scan(&existing)
		switch {
		case err == sql.ErrNoRows:
			if err := applyMigration(db, id, checksum, string(b)); err != nil {
				return err
			}
		case err != nil:
			return err
		case existing != checksum:
			return fmt.Errorf("migration %s checksum mismatch (db=%s file=%s)", id, existing, checksum)
		}
	}

	return nil
}

func applyMigration(db *sql.DB, id, checksum, body string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(body); err != nil {
		return fmt.Errorf("apply migration %s: %w", id, err)
	}

	appliedAt := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(
		`INSERT INTO schema_migrations(id, checksum, applied_at) VALUES(?, ?, ?)`,
		id, checksum, appliedAt,
	); err != nil {
		return fmt.Errorf("record migration %s: %w", id, err)
	}

	return tx.Commit()
}
