package core

import "strings"

func BuildXrayConfig(sel InboundSelection, outbounds []map[string]any) map[string]any {
	// Xray API is used by this project (e.g. balancer override). We configure it
	// using the "classic" method (api + dokodemo-door inbound + routing rule),
	// which works across Xray versions and matches the official docs.
	apiInbound := map[string]any{
		"listen":   "127.0.0.1",
		"port":     10085,
		"protocol": "dokodemo-door",
		"settings": map[string]any{
			"address": "127.0.0.1",
		},
		"tag": "api",
	}

	inbound := map[string]any{
		"listen":         "0.0.0.0",
		"port":           mustInt(sel.ListenPort, 443),
		"tag":            sel.InboundTag,
		"protocol":       sel.Combo.Protocol,
		"settings":       inboundSettings(sel),
		"streamSettings": streamSettings(sel.Combo, sel.SNI, sel.RealityServerNames, sel.WSPath, sel.HTTPHost, sel.HTTPPath, sel.XHTTPHost, sel.XHTTPPath, sel.XHTTPMode, sel.Dest, sel.RealityKey, sel.ShortIDs, sel.TLSCert, sel.TLSKey),
	}

	var selector []string
	for _, ob := range outbounds {
		if tag, ok := ob["tag"].(string); ok {
			selector = append(selector, tag)
		}
	}

	rules := []any{
		map[string]any{
			"type":        "field",
			"inboundTag":  []string{sel.InboundTag},
			"balancerTag": "balancer-0",
		},
		map[string]any{
			"type":        "field",
			"inboundTag":  []string{"api"},
			"outboundTag": "api",
		},
	}

	return map[string]any{
		"log": map[string]any{
			"loglevel": "warning",
		},
		"api": map[string]any{
			"tag":      "api",
			"services": []string{"RoutingService"},
		},
		"inbounds":  []any{apiInbound, inbound},
		"outbounds": outbounds,
		"routing": map[string]any{
			"balancers": []any{
				map[string]any{
					"tag":      "balancer-0",
					"selector": selector,
					"strategy": map[string]any{"type": "roundRobin"},
				},
			},
			"rules": rules,
		},
	}
}

func inboundSettings(sel InboundSelection) map[string]any {
	c := sel.Combo
	switch c.Protocol {
	case "vless":
		flow := ""
		// Xray warns that VLESS without flow is deprecated; default to Vision for
		// REALITY since it's the common/expected pairing.
		if c.WithReality {
			flow = "xtls-rprx-vision"
		}
		return map[string]any{
			"clients": []any{
				map[string]any{
					"id":   sel.UserID,
					"flow": flow,
				},
			},
			"decryption": "none",
		}
	case "trojan":
		return map[string]any{
			"clients": []any{
				map[string]any{
					"password": sel.Password,
				},
			},
		}
	default:
		return map[string]any{
			"clients": []any{
				map[string]any{
					"id":      sel.UserID,
					"alterId": 0,
				},
			},
		}
	}
}

func streamSettings(c Combo, sni string, realityServerNames []string, wsPath, httpHost, httpPath, xhttpHost, xhttpPath, xhttpMode, realityDest, realityKey string, realityShortIDs []string, tlsCert, tlsKey string) map[string]any {
	settings := map[string]any{
		"network":  c.Network,
		"security": c.Security,
	}

	switch c.Network {
	case "ws":
		wsSettings := map[string]any{
			"path": wsPath,
		}
		sni = strings.TrimSpace(sni)
		if sni != "" {
			wsSettings["headers"] = map[string]any{
				"Host": sni,
			}
		}
		settings["wsSettings"] = wsSettings
	case "xhttp":
		mode := strings.TrimSpace(xhttpMode)
		if mode == "" {
			mode = "auto"
		}
		path := strings.TrimSpace(xhttpPath)
		if path == "" {
			path = "/"
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		xhttp := map[string]any{
			"mode": mode,
			"path": path,
		}
		if host := strings.TrimSpace(xhttpHost); host != "" {
			xhttp["host"] = host
		}
		settings["xhttpSettings"] = xhttp
	case "http":
		host := strings.TrimSpace(httpHost)
		if host == "" {
			host = strings.TrimSpace(sni)
		}
		path := strings.TrimSpace(httpPath)
		if path == "" {
			path = "/"
		}
		httpSettings := map[string]any{
			"path": path,
		}
		if host != "" {
			httpSettings["host"] = []string{host}
		}
		settings["httpSettings"] = httpSettings
	}

	if c.WithTLS {
		cert := strings.TrimSpace(tlsCert)
		key := strings.TrimSpace(tlsKey)
		if cert == "" || key == "" {
			cert = "/path/to/fullchain.pem"
			key = "/path/to/privkey.pem"
		}
		tlsSettings := map[string]any{
			"serverName": sni,
			"certificates": []any{
				map[string]any{
					"certificateFile": cert,
					"keyFile":         key,
				},
			},
		}
		// XHTTP is built on top of HTTP/2; prefer h2 in ALPN to avoid downgrade.
		if c.Network == "xhttp" {
			tlsSettings["alpn"] = []string{"h2"}
		}
		settings["tlsSettings"] = tlsSettings
	}

	if c.WithReality {
		dest := strings.TrimSpace(realityDest)
		if dest == "" {
			dest = sni + ":443"
		}
		privateKey := strings.TrimSpace(realityKey)
		if privateKey != "" {
			if normalized, err := NormalizeRealityPrivateKey(privateKey); err == nil {
				privateKey = normalized
			}
		}
		shortIDs := realityShortIDs
		if len(shortIDs) == 0 {
			shortIDs = []string{"0123456789abcdef"}
		}
		serverNames := realityServerNames
		if len(serverNames) == 0 {
			serverNames = []string{sni}
		}
		settings["realitySettings"] = map[string]any{
			"show":        false,
			"dest":        dest,
			"xver":        0,
			"serverNames": serverNames,
			"privateKey":  privateKey,
			"shortIds":    shortIDs,
		}
	}

	return settings
}

func mustInt(input string, def int) int {
	return MustInt(input, def)
}
