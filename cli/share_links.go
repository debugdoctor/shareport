package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"aimerick.com/shareport/config"
	"aimerick.com/shareport/core"
	"aimerick.com/shareport/i18n"
	"aimerick.com/shareport/meta"
	"aimerick.com/shareport/ui"
)

func printConfig(term *ui.TUI, msgs i18n.Messages, cfg config.Config, xrayConfigPath string) {
	term.Batch(func() {
		term.Println("====")
		term.Println("default_pool: " + cfg.DefaultPool)
		term.Println("")
		printSubscriptionLinks(term, msgs, cfg)
		term.Println("")
		printShareLinkFromXray(term, msgs, xrayConfigPath)
		term.Println("====")
		term.WaitEnter(msgs.Get("press_enter_return"))
	})
}

func printSubscriptionLinks(term *ui.TUI, msgs i18n.Messages, cfg config.Config) {
	poolName := cfg.DefaultPool
	if poolName == "" && len(cfg.Pools) > 0 {
		poolName = cfg.Pools[0].Name
	}
	term.Batch(func() {
		term.Println(msgs.Get("sub_links_title") + " " + poolName)
		term.Println("----")
	})
	for _, p := range cfg.Pools {
		if p.Name != poolName {
			continue
		}
		term.Batch(func() {
			for i, link := range p.Links {
				// Add numbering/indentation so long links remain scannable.
				term.Println(fmt.Sprintf("  %d) %s", i+1, link))
			}
		})
		return
	}
	term.Println(msgs.Get("sub_links_empty"))
}

func printShareLinkFromXray(term *ui.TUI, msgs i18n.Messages, xrayConfigPath string) {
	link, err := buildShareLinkFromXray(term, msgs, xrayConfigPath)
	if err != nil {
		term.Println(msgs.Get("share_link_failed") + ": " + err.Error())
		return
	}
	term.Batch(func() {
		term.Println(msgs.Get("share_link_title"))
		term.Println(link)
	})
}

func buildShareLinkFromXray(term *ui.TUI, msgs i18n.Messages, xrayConfigPath string) (string, error) {
	data, err := os.ReadFile(xrayConfigPath)
	if err != nil {
		return "", err
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return "", err
	}
	inbounds := getSlice(root, "inbounds")
	if len(inbounds) == 0 {
		return "", fmt.Errorf("no inbounds")
	}
	// The generated config may include a management/API inbound (dokodemo-door).
	// For share links we need the first real proxy inbound.
	var inbound map[string]any
	for _, it := range inbounds {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		switch getString(m, "protocol") {
		case "vless", "trojan":
			inbound = m
		}
		if inbound != nil {
			break
		}
	}
	if inbound == nil {
		return "", fmt.Errorf("no supported inbound (expected vless/trojan)")
	}

	protocol := getString(inbound, "protocol")
	port := getInt(inbound, "port")
	tag := getString(inbound, "tag")
	settings := getMap(inbound, "settings")
	stream := getMap(inbound, "streamSettings")

	network := getString(stream, "network")
	security := getString(stream, "security")

	sni := ""
	if tls := getMap(stream, "tlsSettings"); tls != nil {
		sni = getString(tls, "serverName")
	}
	if sni == "" {
		if reality := getMap(stream, "realitySettings"); reality != nil {
			if names := getSlice(reality, "serverNames"); len(names) > 0 {
				if s, ok := names[0].(string); ok {
					sni = s
				}
			}
		}
	}

	defaultHost := sni
	if cached, err := meta.LoadMeta(); err == nil && strings.TrimSpace(cached.PublicHost) != "" {
		// Prefer cached value; share link generation should not re-prompt on every view.
		defaultHost = strings.TrimSpace(cached.PublicHost)
	} else {
		host, err := promptAndSavePublicHost(term, msgs, defaultHost)
		if err != nil {
			return "", err
		}
		defaultHost = host
	}

	switch protocol {
	case "vless":
		return buildVlessShareLink(term, msgs, defaultHost, port, tag, settings, stream, security, sni, network)
	case "trojan":
		return buildTrojanShareLink(defaultHost, port, tag, settings, stream, security, sni, network)
	default:
		return "", fmt.Errorf("unsupported protocol: %s", protocol)
	}
}

func buildVlessShareLink(term *ui.TUI, msgs i18n.Messages, host string, port int, tag string, settings, stream map[string]any, security, sni, network string) (string, error) {
	clients := getSlice(settings, "clients")
	if len(clients) == 0 {
		return "", fmt.Errorf("missing clients")
	}
	client, _ := clients[0].(map[string]any)
	userID := getString(client, "id")
	if userID == "" {
		return "", fmt.Errorf("missing id")
	}

	q := url.Values{}
	q.Set("encryption", "none")
	if flow := getString(client, "flow"); flow != "" {
		q.Set("flow", flow)
	}
	if security != "" {
		q.Set("security", security)
	}
	if network != "" {
		q.Set("type", network)
	}
	if sni != "" {
		q.Set("sni", sni)
	}

	switch network {
	case "ws":
		if ws := getMap(stream, "wsSettings"); ws != nil {
			if path := getString(ws, "path"); path != "" {
				q.Set("path", path)
			}
			if headers := getMap(ws, "headers"); headers != nil {
				if hostHeader := getString(headers, "Host"); hostHeader != "" {
					q.Set("host", hostHeader)
				}
			}
		}
	case "xhttp":
		if xhs := getMap(stream, "xhttpSettings"); xhs != nil {
			if mode := strings.TrimSpace(getString(xhs, "mode")); mode != "" {
				q.Set("mode", mode)
			} else {
				q.Set("mode", "auto")
			}
			if path := strings.TrimSpace(getString(xhs, "path")); path != "" {
				q.Set("path", path)
			}
			if hostHeader := strings.TrimSpace(getString(xhs, "host")); hostHeader != "" {
				q.Set("host", hostHeader)
			}
		} else {
			q.Set("mode", "auto")
		}
	case "http":
		if hs := getMap(stream, "httpSettings"); hs != nil {
			if path := getString(hs, "path"); path != "" {
				q.Set("path", path)
			}
			if hosts := getSlice(hs, "host"); len(hosts) > 0 {
				if h, ok := hosts[0].(string); ok && strings.TrimSpace(h) != "" {
					q.Set("host", strings.TrimSpace(h))
				}
			}
		}
	}

	if security == "reality" {
		// For server-side REALITY inbound we have privateKey in the generated
		// config; derive public key to avoid interactive prompts during "view".
		pub := ""
		if reality := getMap(stream, "realitySettings"); reality != nil {
			if pk := getString(reality, "privateKey"); pk != "" {
				if derived, err := core.DeriveRealityPublicKey(pk); err == nil {
					pub = derived
				}
			}
		}
		if pub == "" {
			pub = strings.TrimSpace(term.Prompt(msgs.Get("prompt_reality_public_key"), ""))
		}
		if pub == "" {
			return "", fmt.Errorf("missing reality public key")
		}
		q.Set("pbk", pub)
		if reality := getMap(stream, "realitySettings"); reality != nil {
			if ids := getSlice(reality, "shortIds"); len(ids) > 0 {
				if sid, ok := ids[0].(string); ok && sid != "" {
					q.Set("sid", sid)
				}
			}
		}
		// Default fingerprint; callers can still edit the link if desired.
		q.Set("fp", "chrome")
	}

	return fmt.Sprintf("vless://%s@%s:%d?%s#%s", userID, host, port, q.Encode(), url.QueryEscape(tag)), nil
}

func buildTrojanShareLink(host string, port int, tag string, settings, stream map[string]any, security, sni, network string) (string, error) {
	clients := getSlice(settings, "clients")
	if len(clients) == 0 {
		return "", fmt.Errorf("missing clients")
	}
	client, _ := clients[0].(map[string]any)
	password := getString(client, "password")
	if password == "" {
		return "", fmt.Errorf("missing password")
	}

	q := url.Values{}
	if security != "" {
		q.Set("security", security)
	}
	if network != "" {
		q.Set("type", network)
	}
	if sni != "" {
		q.Set("sni", sni)
	}

	switch network {
	case "ws":
		if ws := getMap(stream, "wsSettings"); ws != nil {
			if path := getString(ws, "path"); path != "" {
				q.Set("path", path)
			}
			if headers := getMap(ws, "headers"); headers != nil {
				if hostHeader := getString(headers, "Host"); hostHeader != "" {
					q.Set("host", hostHeader)
				}
			}
		}
	case "xhttp":
		if xhs := getMap(stream, "xhttpSettings"); xhs != nil {
			if mode := strings.TrimSpace(getString(xhs, "mode")); mode != "" {
				q.Set("mode", mode)
			} else {
				q.Set("mode", "auto")
			}
			if path := strings.TrimSpace(getString(xhs, "path")); path != "" {
				q.Set("path", path)
			}
			if hostHeader := strings.TrimSpace(getString(xhs, "host")); hostHeader != "" {
				q.Set("host", hostHeader)
			}
		} else {
			q.Set("mode", "auto")
		}
	case "http":
		if hs := getMap(stream, "httpSettings"); hs != nil {
			if path := getString(hs, "path"); path != "" {
				q.Set("path", path)
			}
			if hosts := getSlice(hs, "host"); len(hosts) > 0 {
				if h, ok := hosts[0].(string); ok && strings.TrimSpace(h) != "" {
					q.Set("host", strings.TrimSpace(h))
				}
			}
		}
	}

	u := url.URL{
		Scheme:   "trojan",
		User:     url.User(password),
		Host:     fmt.Sprintf("%s:%d", host, port),
		RawQuery: q.Encode(),
		Fragment: tag,
	}
	return u.String(), nil
}

func getMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

func getSlice(m map[string]any, key string) []any {
	if m == nil {
		return nil
	}
	if v, ok := m[key].([]any); ok {
		return v
	}
	return nil
}

func getString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n := 0
		_, _ = fmt.Sscanf(v, "%d", &n)
		return n
	default:
		return 0
	}
}
