package config

import "database/sql"

func GetRuntimeState(db *sql.DB, key string) (string, bool, error) {
	if err := EnsureSchema(db); err != nil {
		return "", false, err
	}
	var value string
	err := db.QueryRow(`SELECT value FROM runtime_state WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func SetRuntimeState(db *sql.DB, key, value string) error {
	if err := EnsureSchema(db); err != nil {
		return err
	}
	_, err := db.Exec(`INSERT INTO runtime_state(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func DeleteRuntimeState(db *sql.DB, key string) error {
	if err := EnsureSchema(db); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM runtime_state WHERE key = ?`, key)
	return err
}

