package core

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

func BuildOutboundsFromLinks(links []string) ([]map[string]any, error) {
	outbounds := make([]map[string]any, 0, len(links))
	for i, link := range links {
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}
		tag := fmt.Sprintf("node-%d", i+1)
		var ob map[string]any
		var err error
		if strings.HasPrefix(link, "vless://") {
			ob, err = vlessOutbound(link, tag)
		} else if strings.HasPrefix(link, "vmess://") {
			ob, err = vmessOutbound(link, tag)
		} else if strings.HasPrefix(link, "trojan://") {
			ob, err = trojanOutbound(link, tag)
		} else {
			return nil, fmt.Errorf("unsupported link: %s", link)
		}
		if err != nil {
			return nil, err
		}
		outbounds = append(outbounds, ob)
	}
	if len(outbounds) == 0 {
		return nil, fmt.Errorf("no valid outbounds")
	}
	return outbounds, nil
}

func vlessOutbound(raw, tag string) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "vless" {
		return nil, fmt.Errorf("invalid vless link")
	}
	if u.User == nil || u.User.Username() == "" {
		return nil, fmt.Errorf("missing vless uuid")
	}
	if u.Host == "" {
		return nil, fmt.Errorf("missing vless host")
	}
	host, portStr, err := splitHostPort(u.Host)
	if err != nil {
		return nil, err
	}
	port := MustInt(portStr, 443)
	query := u.Query()
	network := firstNonEmpty(query.Get("type"), "tcp")
	security := firstNonEmpty(query.Get("security"), "none")
	sni := firstNonEmpty(query.Get("sni"), query.Get("serverName"), query.Get("host"), host)
	wsPath := firstNonEmpty(query.Get("path"), "/")
	grpcService := firstNonEmpty(query.Get("serviceName"), query.Get("service"), strings.TrimPrefix(query.Get("path"), "/"))

	stream := map[string]any{
		"network":  network,
		"security": security,
	}
	switch network {
	case "ws":
		stream["wsSettings"] = map[string]any{
			"path": wsPath,
			"headers": map[string]any{
				"Host": sni,
			},
		}
	case "grpc":
		stream["grpcSettings"] = map[string]any{
			"serviceName": grpcService,
		}
	case "http":
		httpSettings := map[string]any{
			"path": wsPath,
		}
		if strings.TrimSpace(sni) != "" {
			httpSettings["host"] = []string{sni}
		}
		stream["httpSettings"] = httpSettings
	}
	if security == "tls" {
		stream["tlsSettings"] = map[string]any{
			"serverName": sni,
			"certificates": []any{
				map[string]any{
					"certificateFile": "",
					"keyFile":         "",
				},
			},
		}
	} else if security == "reality" {
		// Client-side REALITY uses singular `serverName` in recent Xray versions.
		// Also map common share-link query params into the config.
		reality := map[string]any{
			"serverName": sni,
		}
		if pbk := strings.TrimSpace(query.Get("pbk")); pbk != "" {
			reality["publicKey"] = pbk
		}
		if fp := strings.TrimSpace(query.Get("fp")); fp != "" {
			reality["fingerprint"] = fp
		}
		if sid := strings.TrimSpace(firstNonEmpty(query.Get("sid"), query.Get("shortId"))); sid != "" {
			reality["shortId"] = sid
		}
		stream["realitySettings"] = reality
	}

	return map[string]any{
		"tag":      tag,
		"protocol": "vless",
		"settings": map[string]any{
			"vnext": []any{
				map[string]any{
					"address": host,
					"port":    port,
					"users": []any{
						func() map[string]any {
							user := map[string]any{
								"id":         u.User.Username(),
								"encryption": firstNonEmpty(query.Get("encryption"), "none"),
							}
							// Preserve flow from share links (e.g. xtls-rprx-vision).
							if flow := strings.TrimSpace(query.Get("flow")); flow != "" {
								user["flow"] = flow
							}
							return user
						}(),
					},
				},
			},
		},
		"streamSettings": stream,
	}, nil
}

func trojanOutbound(raw, tag string) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "trojan" {
		return nil, fmt.Errorf("invalid trojan link")
	}
	if u.User == nil || strings.TrimSpace(u.User.Username()) == "" {
		return nil, fmt.Errorf("missing trojan password")
	}
	if u.Host == "" {
		return nil, fmt.Errorf("missing trojan host")
	}
	host, portStr, err := splitHostPort(u.Host)
	if err != nil {
		return nil, err
	}
	port := MustInt(portStr, 443)
	query := u.Query()

	network := firstNonEmpty(query.Get("type"), "tcp")
	security := firstNonEmpty(query.Get("security"), "tls")
	if security == "" || security == "none" {
		security = "tls"
	}
	if security != "tls" {
		return nil, fmt.Errorf("unsupported trojan security: %s", security)
	}

	sni := firstNonEmpty(query.Get("sni"), query.Get("peer"), query.Get("serverName"), query.Get("host"), host)
	wsPath := firstNonEmpty(query.Get("path"), "/")
	grpcService := firstNonEmpty(query.Get("serviceName"), query.Get("service"), strings.TrimPrefix(query.Get("path"), "/"))

	stream := map[string]any{
		"network":  network,
		"security": security,
	}
	switch network {
	case "ws":
		stream["wsSettings"] = map[string]any{
			"path": wsPath,
			"headers": map[string]any{
				"Host": sni,
			},
		}
	case "grpc":
		stream["grpcSettings"] = map[string]any{
			"serviceName": grpcService,
		}
	case "http":
		httpSettings := map[string]any{
			"path": wsPath,
		}
		if strings.TrimSpace(sni) != "" {
			httpSettings["host"] = []string{sni}
		}
		stream["httpSettings"] = httpSettings
	}
	stream["tlsSettings"] = map[string]any{
		"serverName": sni,
		"certificates": []any{
			map[string]any{
				"certificateFile": "",
				"keyFile":         "",
			},
		},
	}

	return map[string]any{
		"tag":      tag,
		"protocol": "trojan",
		"settings": map[string]any{
			"servers": []any{
				map[string]any{
					"address":  host,
					"port":     port,
					"password": u.User.Username(),
				},
			},
		},
		"streamSettings": stream,
	}, nil
}

func vmessOutbound(raw, tag string) (map[string]any, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "vmess" {
		return nil, fmt.Errorf("invalid vmess link")
	}

	payload := strings.TrimSpace(strings.TrimPrefix(raw, "vmess://"))
	if payload == "" {
		return nil, fmt.Errorf("missing vmess payload")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return nil, err
		}
	}

	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}

	host := strings.TrimSpace(m["add"])
	if host == "" {
		return nil, fmt.Errorf("missing host")
	}
	port := MustInt(m["port"], 443)
	userID := strings.TrimSpace(m["id"])
	if userID == "" {
		return nil, fmt.Errorf("missing id")
	}

	network := firstNonEmpty(strings.TrimSpace(m["net"]), "tcp")
	security := "none"
	if strings.TrimSpace(m["tls"]) == "tls" {
		security = "tls"
	}
	sni := firstNonEmpty(strings.TrimSpace(m["sni"]), strings.TrimSpace(m["host"]), host)
	wsPath := firstNonEmpty(strings.TrimSpace(m["path"]), "/")
	grpcService := firstNonEmpty(strings.TrimSpace(m["serviceName"]), strings.TrimPrefix(strings.TrimSpace(m["path"]), "/"))

	stream := map[string]any{
		"network":  network,
		"security": security,
	}
	switch network {
	case "ws":
		stream["wsSettings"] = map[string]any{
			"path": wsPath,
			"headers": map[string]any{
				"Host": sni,
			},
		}
	case "grpc":
		stream["grpcSettings"] = map[string]any{
			"serviceName": grpcService,
		}
	case "http":
		httpSettings := map[string]any{
			"path": wsPath,
		}
		if strings.TrimSpace(sni) != "" {
			httpSettings["host"] = []string{sni}
		}
		stream["httpSettings"] = httpSettings
	}
	if security == "tls" {
		stream["tlsSettings"] = map[string]any{
			"serverName": sni,
			"certificates": []any{
				map[string]any{
					"certificateFile": "",
					"keyFile":         "",
				},
			},
		}
	}

	return map[string]any{
		"tag":      tag,
		"protocol": "vmess",
		"settings": map[string]any{
			"vnext": []any{
				map[string]any{
					"address": host,
					"port":    port,
					"users": []any{
						map[string]any{
							"id":      userID,
							"alterId": MustInt(m["aid"], 0),
						},
					},
				},
			},
		},
		"streamSettings": stream,
	}, nil
}
