package components

import (
	"fmt"
	"net/url"
	"strings"
)

type Link struct {
	Scheme string
	Host   string
	Port   string
	Raw    string
}

func Parse(raw string) (Link, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Link{}, fmt.Errorf("empty link")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return Link{}, err
	}
	if u.Scheme != "vless" && u.Scheme != "vmess" && u.Scheme != "trojan" {
		return Link{}, fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	if u.Scheme == "vmess" {
		opaque := strings.TrimSpace(u.Opaque)
		path := strings.Trim(strings.TrimSpace(u.Path), "/")
		if u.Host == "" && opaque == "" && path == "" {
			return Link{}, fmt.Errorf("missing vmess payload")
		}
		return Link{
			Scheme: u.Scheme,
			Host:   "",
			Port:   "",
			Raw:    raw,
		}, nil
	}
	if u.Host == "" {
		return Link{}, fmt.Errorf("missing host")
	}
	if u.Scheme == "trojan" {
		if u.User == nil || strings.TrimSpace(u.User.Username()) == "" {
			return Link{}, fmt.Errorf("missing trojan password")
		}
	}

	host := u.Host
	port := ""
	if strings.Contains(u.Host, ":") {
		parts := strings.Split(u.Host, ":")
		host = parts[0]
		if len(parts) > 1 {
			port = parts[1]
		}
	}

	return Link{
		Scheme: u.Scheme,
		Host:   host,
		Port:   port,
		Raw:    raw,
	}, nil
}
