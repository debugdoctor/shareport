package config

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func OpenDB(path string) (*sql.DB, error) {
	if path == "" {
		path = ".shareport/shareport.db"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return sql.Open("sqlite", path)
}

func EnsureSchema(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);`,
		// Runtime state is intentionally separate from "settings" because SaveDB
		// clears settings as part of config rewrite.
		`CREATE TABLE IF NOT EXISTS runtime_state (key TEXT PRIMARY KEY, value TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS pools (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, policy TEXT NOT NULL);`,
		`CREATE TABLE IF NOT EXISTS links (id INTEGER PRIMARY KEY AUTOINCREMENT, pool_id INTEGER NOT NULL, link TEXT NOT NULL, position INTEGER NOT NULL, FOREIGN KEY(pool_id) REFERENCES pools(id));`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func LoadDB(db *sql.DB) (Config, error) {
	if err := EnsureSchema(db); err != nil {
		return Config{}, err
	}

	cfg := Config{}

	rows, err := db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return cfg, err
	}
	defer rows.Close()
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return cfg, err
		}
		switch key {
		case "default_pool":
			cfg.DefaultPool = value
		}
	}

	poolRows, err := db.Query(`SELECT id, name, policy FROM pools ORDER BY id ASC`)
	if err != nil {
		return cfg, err
	}
	defer poolRows.Close()
	for poolRows.Next() {
		var id int
		var name, policy string
		if err := poolRows.Scan(&id, &name, &policy); err != nil {
			return cfg, err
		}
		links, err := loadLinks(db, id)
		if err != nil {
			return cfg, err
		}
		cfg.Pools = append(cfg.Pools, PoolConfig{
			Name:   name,
			Policy: policy,
			Links:  links,
		})
	}

	if len(cfg.Pools) == 0 {
		return cfg, ErrNoPools
	}
	if cfg.DefaultPool == "" {
		cfg.DefaultPool = cfg.Pools[0].Name
	}

	return cfg, nil
}

func SaveDB(db *sql.DB, cfg Config) error {
	if err := EnsureSchema(db); err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec(`DELETE FROM links`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM pools`); err != nil {
		return err
	}
	// Preserve unrelated settings (e.g. user preferences). Only rewrite keys we own.
	if _, err := tx.Exec(`DELETE FROM settings WHERE key = 'default_pool'`); err != nil {
		return err
	}

	if cfg.DefaultPool != "" {
		if _, err := tx.Exec(`INSERT INTO settings(key, value) VALUES('default_pool', ?)`, cfg.DefaultPool); err != nil {
			return err
		}
	}

	for _, pool := range cfg.Pools {
		if pool.Name == "" {
			return fmt.Errorf("pool name is required")
		}
		policy := pool.Policy
		if policy == "" {
			policy = "round_robin"
		}

		res, err := tx.Exec(`INSERT INTO pools(name, policy) VALUES(?, ?)`, pool.Name, policy)
		if err != nil {
			return err
		}
		poolID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		for i, link := range pool.Links {
			if _, err := tx.Exec(`INSERT INTO links(pool_id, link, position) VALUES(?, ?, ?)`, poolID, link, i); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func DBExists(path string) bool {
	if path == "" {
		path = ".shareport/shareport.db"
	}
	_, err := os.Stat(path)
	return err == nil
}

func loadLinks(db *sql.DB, poolID int) ([]string, error) {
	rows, err := db.Query(`SELECT link FROM links WHERE pool_id = ? ORDER BY position ASC`, poolID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var links []string
	for rows.Next() {
		var link string
		if err := rows.Scan(&link); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, nil
}
