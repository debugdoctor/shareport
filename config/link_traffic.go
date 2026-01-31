package config

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strconv"
	"strings"
)

type LinkTrafficMeta struct {
	LimitBytes int64
	ResetDay   int    // 1-31
	ResetTime  string // HH:MM
}

const (
	linkTrafficLimitKeyPrefix     = "link_traffic_limit_bytes"
	linkTrafficResetDayKeyPrefix  = "link_traffic_reset_day"
	linkTrafficResetTimeKeyPrefix = "link_traffic_reset_time"
)

func linkTrafficKey(prefix, link string) string {
	link = strings.TrimSpace(link)
	sum := sha256.Sum256([]byte(link))
	return prefix + ":" + hex.EncodeToString(sum[:])
}

func GetLinkTrafficMeta(db *sql.DB, link string) (LinkTrafficMeta, bool, error) {
	limitStr, ok, err := GetSetting(db, linkTrafficKey(linkTrafficLimitKeyPrefix, link))
	if err != nil {
		return LinkTrafficMeta{}, false, err
	}
	if !ok || strings.TrimSpace(limitStr) == "" {
		return LinkTrafficMeta{}, false, nil
	}
	limit, err := strconv.ParseInt(strings.TrimSpace(limitStr), 10, 64)
	if err != nil || limit <= 0 {
		return LinkTrafficMeta{}, false, nil
	}

	resetDay := 1
	if dayStr, ok, err := GetSetting(db, linkTrafficKey(linkTrafficResetDayKeyPrefix, link)); err != nil {
		return LinkTrafficMeta{}, true, err
	} else if ok {
		if n, err := strconv.Atoi(strings.TrimSpace(dayStr)); err == nil && n >= 1 && n <= 31 {
			resetDay = n
		}
	}

	resetTime := "00:00"
	if timeStr, ok, err := GetSetting(db, linkTrafficKey(linkTrafficResetTimeKeyPrefix, link)); err != nil {
		return LinkTrafficMeta{}, true, err
	} else if ok && strings.TrimSpace(timeStr) != "" {
		resetTime = strings.TrimSpace(timeStr)
	}

	return LinkTrafficMeta{LimitBytes: limit, ResetDay: resetDay, ResetTime: resetTime}, true, nil
}

func SetLinkTrafficMeta(db *sql.DB, link string, meta LinkTrafficMeta) error {
	if meta.LimitBytes <= 0 {
		return ClearLinkTrafficMeta(db, link)
	}
	if err := SetSetting(db, linkTrafficKey(linkTrafficLimitKeyPrefix, link), strconv.FormatInt(meta.LimitBytes, 10)); err != nil {
		return err
	}
	if meta.ResetDay < 1 || meta.ResetDay > 31 {
		meta.ResetDay = 1
	}
	if strings.TrimSpace(meta.ResetTime) == "" {
		meta.ResetTime = "00:00"
	}
	if err := SetSetting(db, linkTrafficKey(linkTrafficResetDayKeyPrefix, link), strconv.Itoa(meta.ResetDay)); err != nil {
		return err
	}
	return SetSetting(db, linkTrafficKey(linkTrafficResetTimeKeyPrefix, link), strings.TrimSpace(meta.ResetTime))
}

func ClearLinkTrafficMeta(db *sql.DB, link string) error {
	_ = DeleteSetting(db, linkTrafficKey(linkTrafficLimitKeyPrefix, link))
	_ = DeleteSetting(db, linkTrafficKey(linkTrafficResetDayKeyPrefix, link))
	_ = DeleteSetting(db, linkTrafficKey(linkTrafficResetTimeKeyPrefix, link))
	return nil
}
