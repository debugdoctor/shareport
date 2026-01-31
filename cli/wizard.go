package cli

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"aimerick.com/shareport/acme"
	"aimerick.com/shareport/config"
	"aimerick.com/shareport/core"
	"aimerick.com/shareport/i18n"
	"aimerick.com/shareport/runtime"
	"aimerick.com/shareport/ui"
)

const settingRealityServerNames = "reality_server_names"

func promptInbound(term *ui.TUI, msgs i18n.Messages, dbPath string) (core.InboundSelection, error) {
	combos := []core.Combo{
		// VLESS
		{Name: "vless + tcp + none", Protocol: "vless", Network: "tcp", Security: "none"},
		{Name: "vless + tcp + tls", Protocol: "vless", Network: "tcp", Security: "tls", WithTLS: true},
		{Name: "vless + tcp + reality", Protocol: "vless", Network: "tcp", Security: "reality", WithReality: true},
		{Name: "vless + ws + none", Protocol: "vless", Network: "ws", Security: "none"},
		{Name: "vless + ws + tls", Protocol: "vless", Network: "ws", Security: "tls", WithTLS: true},
		{Name: "vless + xhttp + tls", Protocol: "vless", Network: "xhttp", Security: "tls", WithTLS: true},
		{Name: "vless + xhttp + reality", Protocol: "vless", Network: "xhttp", Security: "reality", WithReality: true},
		{Name: "vless + http + none", Protocol: "vless", Network: "http", Security: "none"},
		{Name: "vless + http + tls", Protocol: "vless", Network: "http", Security: "tls", WithTLS: true},

		// Trojan
		{Name: "trojan + tcp + tls", Protocol: "trojan", Network: "tcp", Security: "tls", WithTLS: true},
		{Name: "trojan + ws + tls", Protocol: "trojan", Network: "ws", Security: "tls", WithTLS: true},
		{Name: "trojan + xhttp + tls", Protocol: "trojan", Network: "xhttp", Security: "tls", WithTLS: true},
		{Name: "trojan + http + tls", Protocol: "trojan", Network: "http", Security: "tls", WithTLS: true},
	}

	var choiceStr string = "1"
	term.Batch(func() {
		term.Reset()
		term.Println(msgs.Get("select_combo"))
		for i, c := range combos {
			term.Println(fmt.Sprintf("  %d) %s", i+1, c.Name))
		}
		choiceStr = term.Prompt(msgs.Get("prompt_choice"), "1")
	})

	choice := ui.ClampChoice(choiceStr, len(combos))
	selected := combos[choice-1]

	selection := core.InboundSelection{
		Combo:      selected,
		ListenPort: term.Prompt(msgs.Get("prompt_port"), "443"),
		InboundTag: term.Prompt(msgs.Get("prompt_tag"), "inbound-0"),
	}

	switch selected.Protocol {
	case "vless":
		selection.UserID = term.Prompt(msgs.Get("prompt_uuid"), core.NewUUID())
	case "trojan":
		for {
			selection.Password = strings.TrimSpace(term.Prompt(msgs.Get("prompt_trojan_password"), core.NewPassword()))
			if selection.Password == "" {
				term.Println(msgs.Get("trojan_password_required"))
				continue
			}
			break
		}
	}

	// Only prompt for fields relevant to the selected transport/security.
	if selected.WithTLS {
		selection.SNI = term.Prompt(msgs.Get("prompt_sni"), "example.com")

		// Keep TLS cert mode selection early so users can complete certificate
		// provisioning before answering transport-specific prompts.
		mode := ui.ClampChoice(term.Prompt(msgs.Get("prompt_tls_mode"), "1"), 2)
		if mode == 1 {
			for {
				selection.TLSCert = strings.TrimSpace(term.Prompt(msgs.Get("prompt_tls_cert"), ""))
				if selection.TLSCert == "" {
					term.Println(msgs.Get("tls_cert_required"))
					continue
				}
				break
			}
			for {
				selection.TLSKey = strings.TrimSpace(term.Prompt(msgs.Get("prompt_tls_key"), ""))
				if selection.TLSKey == "" {
					term.Println(msgs.Get("tls_key_required"))
					continue
				}
				break
			}
		} else {
			domain := strings.TrimSpace(selection.SNI)
			email := strings.TrimSpace(term.Prompt(msgs.Get("prompt_tls_email"), ""))
			for email == "" {
				term.Println(msgs.Get("tls_email_required"))
				email = strings.TrimSpace(term.Prompt(msgs.Get("prompt_tls_email"), ""))
			}
			challenge := ui.ClampChoice(term.Prompt(msgs.Get("prompt_tls_challenge"), "1"), 2)
			provider := ""
			if challenge == 2 {
				for {
					provider = strings.TrimSpace(term.Prompt(msgs.Get("prompt_tls_dns_provider"), ""))
					if provider == "?" {
						acme.PrintDNSProviderList(term)
						continue
					}
					if provider != "" && provider != "manual" {
						term.Println(msgs.Get("tls_dns_manual_only"))
						continue
					}
					if provider == "" {
						term.Println(msgs.Get("tls_dns_provider_required"))
						continue
					}
					break
				}
			} else {
				term.Batch(func() {
					term.Println(msgs.Get("tls_http_notice"))
					term.Println(msgs.Get("tls_http_notice_root"))
				})
			}
			term.Println(msgs.Get("tls_auto_start"))
			certPath, keyPath, err := acme.EnsureCertificate(term, msgs, domain, email, challenge, provider)
			if err != nil {
				return core.InboundSelection{}, err
			}
			selection.TLSCert = certPath
			selection.TLSKey = keyPath
			term.Println(fmt.Sprintf("%s %s", msgs.Get("tls_auto_done"), certPath))
		}
	}

	if selected.WithReality {
		names, err := promptRealityServerNames(term, msgs, dbPath)
		if err != nil {
			return core.InboundSelection{}, err
		}
		// `names[0]` is the user's chosen decoy domain for this generation.
		selection.SNI = names[0]
		selection.RealityServerNames = names
	}

	// WS commonly needs a Host header (even without TLS). Reuse SNI prompt for it.
	if selected.Network == "ws" && strings.TrimSpace(selection.SNI) == "" {
		selection.SNI = term.Prompt(msgs.Get("prompt_sni"), "example.com")
	}
	if selected.Network == "ws" {
		selection.WSPath = term.Prompt(msgs.Get("prompt_ws_path"), "/ws")
	}
	if selected.Network == "xhttp" {
		selection.XHTTPMode = "auto"
		defPath := "/x" + core.GenerateShortID()
		selection.XHTTPPath = strings.TrimSpace(term.Prompt(msgs.Get("prompt_xhttp_path"), defPath))
		selection.XHTTPHost = strings.TrimSpace(term.Prompt(msgs.Get("prompt_xhttp_host"), ""))
	}
	if selected.Network == "http" {
		selection.HTTPPath = term.Prompt(msgs.Get("prompt_http_path"), "/")
		defHost := strings.TrimSpace(selection.SNI)
		if defHost == "" {
			defHost = "example.com"
		}
		selection.HTTPHost = term.Prompt(msgs.Get("prompt_http_host"), defHost)
	}

	if selected.WithReality {
		destPort := strings.TrimSpace(term.Prompt(msgs.Get("prompt_reality_dest_port"), "443"))
		if destPort == "" {
			destPort = "443"
		}
		selection.Dest = selection.SNI + ":" + destPort
		keyMode := term.Prompt(msgs.Get("prompt_reality_key_mode"), "2")
		if ui.ClampChoice(keyMode, 2) == 1 {
			for {
				selection.RealityKey = strings.TrimSpace(term.Prompt(msgs.Get("prompt_reality_private_key"), ""))
				if selection.RealityKey != "" {
					break
				}
				term.Println(msgs.Get("reality_private_key_required"))
			}
		} else {
			privateKey, publicKey, err := core.GenerateRealityKeyPair()
			if err != nil {
				return core.InboundSelection{}, err
			}
			selection.RealityKey = privateKey
			term.Println(fmt.Sprintf("%s %s", msgs.Get("reality_public_key"), publicKey))
		}
		shortIDs := term.Prompt(msgs.Get("prompt_reality_short_ids"), core.GenerateShortID())
		selection.ShortIDs = core.SplitShortIDs(shortIDs)
	}

	return selection, nil
}

func promptRealityServerNames(term *ui.TUI, msgs i18n.Messages, dbPath string) ([]string, error) {
	common := []string{
		"www.cloudflare.com",
		"aws.amazon.com",
		"wordpress.org",
		"www.whitehouse.com",
		"www.ubuntu.com",
		"www.blender.org",
		"www.python.org",
		"www.digitalocean.com",
		"www.autodesk.com",
		"nodejs.org",
		"www.oracle.com",
		"www.github.com",
		"www.google.com",
	}

	// No "source" selection UI: show a list (built-in list),
	// and allow typing either a number or a custom domain.
	base := append([]string(nil), common...)

	term.Batch(func() {
		term.Println(msgs.Get("reality_decoy_title"))
		for i, name := range base {
			term.Println(fmt.Sprintf("  %d) %s", i+1, name))
		}
	})

	input := strings.TrimSpace(term.Prompt(msgs.Get("reality_decoy_prompt"), "1"))
	if input == "" {
		input = "1"
	}

	chosen := ""
	if n, err := strconv.Atoi(input); err == nil {
		if n < 1 || n > len(base) {
			return nil, fmt.Errorf("%s", msgs.Get("reality_names_required"))
		}
		chosen = base[n-1]
	} else {
		chosen = input
	}
	chosen = strings.TrimSpace(chosen)
	if chosen == "" {
		return nil, fmt.Errorf("%s", msgs.Get("reality_names_required"))
	}

	// Keep a list for server-side config (Xray expects serverNames list).
	// Put the chosen domain in front so the caller can use it deterministically.
	names := dedupServerNames(append([]string{chosen}, base...))

	// Persist the latest user-provided list for next time.
	if db, err := config.OpenDB(dbPath); err == nil {
		// Save the full list (without losing items) so next time it can be picked from.
		_ = config.SetSetting(db, settingRealityServerNames, strings.Join(dedupServerNames(base), ","))
		_ = db.Close()
	}

	return names, nil
}

func parseServerNames(raw string) []string {
	var names []string
	for _, part := range strings.FieldsFunc(strings.TrimSpace(raw), func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t' || r == ';'
	}) {
		s := strings.TrimSpace(part)
		if s != "" {
			names = append(names, s)
		}
	}
	return dedupServerNames(names)
}

func dedupServerNames(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func runInitWizard(term *ui.TUI, msgs i18n.Messages, db *sql.DB) error {
	defaultPool := ""
	var pools []config.PoolConfig

	for {
		poolName := term.Prompt(msgs.Get("prompt_pool_name"), "")
		if poolName == "" {
			if len(pools) == 0 {
				term.Println(msgs.Get("need_pool"))
				continue
			}
			break
		}
		links, err := collectLinks(term, msgs, db)
		if err != nil {
			if errors.Is(err, errCancelPool) {
				term.Println(msgs.Get("pool_cancelled"))
				continue
			}
			return err
		}
		if defaultPool == "" {
			defaultPool = poolName
		}
		pools = append(pools, config.PoolConfig{
			Name:   poolName,
			Policy: "round_robin",
			Links:  links,
		})

		more := strings.ToLower(term.Prompt(msgs.Get("prompt_more_pools"), "n"))
		if more != "y" && more != "yes" {
			break
		}
	}

	cfg := config.Config{
		DefaultPool: defaultPool,
		Pools:       pools,
	}
	return config.SaveDB(db, cfg)
}

func LoadConfigOrInit(term *ui.TUI, msgs i18n.Messages, dbPath string, forceInit bool) (config.Config, bool, error) {
	dbExists := config.DBExists(dbPath)
	didInit := false
	db, err := config.OpenDB(dbPath)
	if err != nil {
		return config.Config{}, false, fmt.Errorf("%s: %w", msgs.Get("open_db_failed"), err)
	}
	defer db.Close()

	if forceInit {
		if err := runInitWizard(term, msgs, db); err != nil {
			return config.Config{}, false, err
		}
		term.Println(msgs.Get("init_ok"))
		didInit = true
		cfg, err := config.LoadDB(db)
		if err != nil {
			return config.Config{}, false, err
		}
		return cfg, didInit, nil
	}

	if !dbExists {
		if err := runInitWizard(term, msgs, db); err != nil {
			return config.Config{}, false, err
		}
		term.Println(msgs.Get("init_ok"))
		didInit = true
	}

	cfg, err := config.LoadDB(db)
	if err != nil {
		if errors.Is(err, config.ErrNoPools) {
			if err := runInitWizard(term, msgs, db); err != nil {
				return config.Config{}, false, err
			}
			term.Println(msgs.Get("init_ok"))
			didInit = true
			cfg, err = config.LoadDB(db)
		}
		if err != nil {
			return config.Config{}, false, err
		}
	}

	return cfg, didInit, nil
}

func RunXraySetup(term *ui.TUI, msgs i18n.Messages, cfg config.Config, dbPath, configPath, xrayBin string) error {
	pool := cfg.DefaultPool
	if pool == "" && len(cfg.Pools) > 0 {
		pool = cfg.Pools[0].Name
	}
	var links []string
	for _, p := range cfg.Pools {
		if p.Name == pool {
			links = p.Links
			break
		}
	}
	if len(links) == 0 {
		return fmt.Errorf("%s", msgs.Get("need_links"))
	}

	selection, err := promptInbound(term, msgs, dbPath)
	if err != nil {
		return err
	}

	// Ask (or confirm) the public domain/IP used for share links and persist it.
	// This is intentionally prompted during regeneration so users can correct it.
	if _, err := promptAndSavePublicHost(term, msgs, selection.SNI); err != nil {
		return err
	}

	outbounds, err := core.BuildOutboundsFromLinks(links)
	if err != nil {
		return err
	}

	xrayCfg := core.BuildXrayConfig(selection, outbounds)
	if err := core.WriteJSONFile(configPath, xrayCfg); err != nil {
		return err
	}

	didRestart := false
	var restartErr error
	wasRunning := runtime.IsXrayRunning(dbPath)
	if wasRunning {
		// Restart proxy to apply inbound changes; only do this when proxy is already
		// running to avoid unexpected side effects.
		proxyPath, err := runtime.ResolveXrayBinary(xrayBin)
		if err != nil {
			if p, err2 := runtime.ResolveRunningProxyBinary(dbPath); err2 == nil {
				proxyPath = p
			} else {
				restartErr = err
				proxyPath = ""
			}
		}
		if proxyPath != "" {
			if err := runtime.StopXray(dbPath); err != nil {
				restartErr = err
			} else if err := runtime.StartXray(dbPath, proxyPath, configPath); err != nil {
				restartErr = err
			} else {
				didRestart = true
			}
		}
	}

	isRunning := runtime.IsXrayRunning(dbPath) || runtime.IsProxyListening(configPath)

	term.Batch(func() {
		term.Printf("%s %s", msgs.Get("proxy_config_written"), configPath)
		term.Println("")
		if didRestart {
			term.Println(msgs.Get("proxy_restarted"))
		} else if restartErr != nil {
			term.Println(msgs.Get("proxy_restart_failed") + ": " + restartErr.Error())
		} else if isRunning {
			term.Println(msgs.Get("proxy_running"))
		} else {
			term.Println(msgs.Get("proxy_config_not_started"))
		}

		printShareLinkFromXray(term, msgs, configPath)
		term.WaitEnter(msgs.Get("press_enter_return"))
	})

	return nil
}
