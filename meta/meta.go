package meta

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
)

type ShareportMeta struct {
	PublicHost string `json:"public_host"`
}

func MetaPath() string {
	return filepath.Join(".shareport", "meta.json")
}

func LoadMeta() (ShareportMeta, error) {
	path := MetaPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return ShareportMeta{}, err
	}
	var meta ShareportMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return ShareportMeta{}, err
	}
	return meta, nil
}

func SaveMeta(meta ShareportMeta) error {
	if err := os.MkdirAll(filepath.Dir(MetaPath()), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(MetaPath(), data, 0o600)
}

func DetectPublicHost() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if (iface.Flags&net.FlagUp) == 0 || (iface.Flags&net.FlagLoopback) != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue
			}
			return ip.String()
		}
	}
	return ""
}

func EnsurePublicHostCached(fallback string) {
	meta, err := LoadMeta()
	if err == nil && strings.TrimSpace(meta.PublicHost) != "" {
		return
	}
	host := strings.TrimSpace(fallback)
	if host == "" {
		host = DetectPublicHost()
	}
	if host == "" {
		return
	}
	_ = SaveMeta(ShareportMeta{PublicHost: host})
}
