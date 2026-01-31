package config

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
)

type LinkTrafficMeta struct {
	LimitBytes int64
	ResetDay   int    // 1-31
	ResetTime  string // HH:MM
}

func linkTrafficHash(link string) string {
	link = strings.TrimSpace(link)
	sum := sha256.Sum256([]byte(link))
	return hex.EncodeToString(sum[:])
}

func GetLinkTrafficMeta(db *sql.DB, link string) (LinkTrafficMeta, bool, error) {
	if err := EnsureSchema(db); err != nil {
		return LinkTrafficMeta{}, false, err
	}
	var (
		limit     int64
		resetDay  int
		resetTime string
	)
	err := db.QueryRow(
		`SELECT limit_bytes, reset_day, reset_time FROM link_attrs WHERE link_hash = ?`,
		linkTrafficHash(link),
	).Scan(&limit, &resetDay, &resetTime)
	if err == sql.ErrNoRows {
		return LinkTrafficMeta{}, false, nil
	}
	if err != nil {
		return LinkTrafficMeta{}, false, err
	}
	if limit <= 0 {
		return LinkTrafficMeta{}, false, nil
	}
	if resetDay < 1 || resetDay > 31 {
		resetDay = 1
	}
	if strings.TrimSpace(resetTime) == "" {
		resetTime = "00:00"
	}
	return LinkTrafficMeta{LimitBytes: limit, ResetDay: resetDay, ResetTime: strings.TrimSpace(resetTime)}, true, nil
}

func SetLinkTrafficMeta(db *sql.DB, link string, meta LinkTrafficMeta) error {
	if err := EnsureSchema(db); err != nil {
		return err
	}
	if meta.LimitBytes <= 0 {
		return ClearLinkTrafficMeta(db, link)
	}
	if meta.ResetDay < 1 || meta.ResetDay > 31 {
		meta.ResetDay = 1
	}
	if strings.TrimSpace(meta.ResetTime) == "" {
		meta.ResetTime = "00:00"
	}
	meta.ResetTime = strings.TrimSpace(meta.ResetTime)

	_, err := db.Exec(
		`INSERT INTO link_attrs(link_hash, limit_bytes, reset_day, reset_time)
		 VALUES(?, ?, ?, ?)
		 ON CONFLICT(link_hash) DO UPDATE SET limit_bytes=excluded.limit_bytes, reset_day=excluded.reset_day, reset_time=excluded.reset_time`,
		linkTrafficHash(link),
		meta.LimitBytes,
		meta.ResetDay,
		meta.ResetTime,
	)
	return err
}

func ClearLinkTrafficMeta(db *sql.DB, link string) error {
	if err := EnsureSchema(db); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM link_attrs WHERE link_hash = ?`, linkTrafficHash(link))
	return err
}
