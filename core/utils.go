package core

import (
	"crypto/ecdh"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	mrand "math/rand"
	"strings"
	"time"
)

func decodeBase64Any(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	decoders := []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	}
	var lastErr error
	for _, enc := range decoders {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		} else {
			lastErr = err
		}
	}
	return nil, lastErr
}

func MustInt(input string, def int) int {
	var n int
	if _, err := fmt.Sscanf(input, "%d", &n); err == nil && n > 0 {
		return n
	}
	return def
}

func splitHostPort(hostport string) (string, string, error) {
	if strings.Contains(hostport, ":") {
		parts := strings.Split(hostport, ":")
		if len(parts) < 2 {
			return hostport, "", fmt.Errorf("invalid host:port")
		}
		return parts[0], parts[1], nil
	}
	return hostport, "", fmt.Errorf("missing port")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func NewUUID() string {
	var b [16]byte
	if _, err := crand.Read(b[:]); err == nil {
		b[6] = (b[6] & 0x0f) | 0x40
		b[8] = (b[8] & 0x3f) | 0x80
		return fmt.Sprintf("%s-%s-%s-%s-%s",
			hex.EncodeToString(b[0:4]),
			hex.EncodeToString(b[4:6]),
			hex.EncodeToString(b[6:8]),
			hex.EncodeToString(b[8:10]),
			hex.EncodeToString(b[10:16]),
		)
	}
	mrand.Seed(time.Now().UnixNano())
	return fmt.Sprintf("11111111-1111-4%03x-8%03x-111111111111", mrand.Intn(4096), mrand.Intn(4096))
}

func NewPassword() string {
	// 18 bytes => 24 chars in raw base64url (no padding).
	var b [18]byte
	if _, err := crand.Read(b[:]); err == nil {
		return base64.RawURLEncoding.EncodeToString(b[:])
	}
	mrand.Seed(time.Now().UnixNano())
	return fmt.Sprintf("pw-%08x%08x", mrand.Uint32(), mrand.Uint32())
}

func GenerateRealityKeyPair() (string, string, error) {
	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(crand.Reader)
	if err != nil {
		return "", "", err
	}
	// Xray expects URL-safe base64 (commonly without padding).
	privateKey := base64.RawURLEncoding.EncodeToString(priv.Bytes())
	publicKey := base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
	return privateKey, publicKey, nil
}

func NormalizeRealityPrivateKey(privateKeyB64 string) (string, error) {
	raw, err := decodeBase64Any(privateKeyB64)
	if err != nil {
		return "", err
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("invalid reality private key length: %d", len(raw))
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func DeriveRealityPublicKey(privateKeyB64 string) (string, error) {
	raw, err := decodeBase64Any(privateKeyB64)
	if err != nil {
		return "", err
	}
	curve := ecdh.X25519()
	priv, err := curve.NewPrivateKey(raw)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()), nil
}

func GenerateShortID() string {
	var b [8]byte
	if _, err := crand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return "0123456789abcdef"
}

func SplitShortIDs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' '
	})
	var ids []string
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p != "" {
			ids = append(ids, p)
		}
	}
	return ids
}
