package cli

import (
	"errors"
	"fmt"
	"strings"

	"aimerick.com/shareport/acme"
	"aimerick.com/shareport/config"
	"aimerick.com/shareport/i18n"
	"aimerick.com/shareport/links"
	"aimerick.com/shareport/runtime"
	"aimerick.com/shareport/ui"
)

func ManageConfig(term *ui.TUI, msgs i18n.Messages, cfg config.Config, dbPath, xrayConfig, xrayBin string) error {
	for {
		term.Reset()
		term.Batch(func() {
			term.Println(msgs.Get("menu_title"))
			term.Println("  1) " + msgs.Get("menu_view"))
			term.Println("  2) " + msgs.Get("menu_manage_pools"))
			term.Println("  3) " + msgs.Get("menu_manage_links"))
			term.Println("  4) " + msgs.Get("menu_manage_runtime"))
			term.Println("  5) " + msgs.Get("menu_generate"))
			term.Println("  6) " + msgs.Get("menu_renew_certs"))
			term.Println("  7) " + msgs.Get("menu_exit"))
		})

		choice := ui.ClampChoice(term.Prompt(msgs.Get("menu_prompt"), "7"), 7)

		term.Reset()
		switch choice {
		case 1:
			printConfig(term, msgs, cfg, xrayConfig)
		case 2:
			if err := managePools(term, msgs, &cfg, dbPath); err != nil {
				return err
			}
			if err := saveConfig(dbPath, cfg); err != nil {
				return err
			}
			if runtime.IsBalancerDaemonRunning(dbPath) {
				if err := runtime.ReloadBalancerDaemon(dbPath); err != nil {
					term.Println(msgs.Get("balancer_daemon_reload_failed") + ": " + err.Error())
				} else {
					term.Println(msgs.Get("balancer_daemon_reloaded"))
				}
			}
		case 3:
			if err := manageLinks(term, msgs, &cfg, dbPath); err != nil {
				return err
			}
			if err := saveConfig(dbPath, cfg); err != nil {
				return err
			}
			if runtime.IsBalancerDaemonRunning(dbPath) {
				if err := runtime.ReloadBalancerDaemon(dbPath); err != nil {
					term.Println(msgs.Get("balancer_daemon_reload_failed") + ": " + err.Error())
				} else {
					term.Println(msgs.Get("balancer_daemon_reloaded"))
				}
			}
		case 4:
			if err := manageRuntime(term, msgs, &cfg, dbPath, xrayConfig, xrayBin); err != nil {
				return err
			}
		case 5:
			if err := RunXraySetup(term, msgs, cfg, dbPath, xrayConfig, xrayBin); err != nil {
				return err
			}
			term.WaitEnter(msgs.Get("press_enter_return"))
		case 6:
			if err := acme.RenewCertificates(term, msgs); err != nil {
				return err
			}
			term.WaitEnter(msgs.Get("press_enter_return"))
		default:
			return nil
		}
	}
}

func managePools(term *ui.TUI, msgs i18n.Messages, cfg *config.Config, dbPath string) error {
	db, err := config.OpenDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	term.Batch(func() {
		term.Println(msgs.Get("pools_title"))
		term.Println("  1) " + msgs.Get("pools_add"))
		term.Println("  2) " + msgs.Get("pools_delete"))
		term.Println("  3) " + msgs.Get("pools_set_default"))
		term.Println("  4) " + msgs.Get("menu_back"))
	})
	choice := ui.ClampChoice(term.Prompt(msgs.Get("menu_prompt"), "4"), 4)
	switch choice {
	case 1:
		name := strings.TrimSpace(term.Prompt(msgs.Get("prompt_pool_name"), ""))
		if name == "" {
			term.Println(msgs.Get("pool_name_required"))
			term.WaitEnter(msgs.Get("press_enter_return"))
			return nil
		}
		if findPoolIndex(cfg, name) >= 0 {
			term.Println(msgs.Get("pool_exists"))
			term.WaitEnter(msgs.Get("press_enter_return"))
			return nil
		}
		links, err := collectLinks(term, msgs, db)
		if err != nil {
			if errors.Is(err, errCancelPool) {
				term.Println(msgs.Get("pool_cancelled"))
				term.WaitEnter(msgs.Get("press_enter_return"))
				return nil
			}
			return err
		}
		cfg.Pools = append(cfg.Pools, config.PoolConfig{
			Name:   name,
			Policy: "round_robin",
			Links:  links,
		})
		if cfg.DefaultPool == "" {
			cfg.DefaultPool = name
		}
		term.WaitEnter(msgs.Get("press_enter_return"))
	case 2:
		if len(cfg.Pools) == 0 {
			term.Println(msgs.Get("need_pool"))
			term.WaitEnter(msgs.Get("press_enter_return"))
			return nil
		}
		idx := selectPoolIndex(term, msgs, cfg.Pools)
		if idx < 0 {
			return nil
		}
		deleted := cfg.Pools[idx].Name
		cfg.Pools = append(cfg.Pools[:idx], cfg.Pools[idx+1:]...)
		if cfg.DefaultPool == deleted {
			if len(cfg.Pools) > 0 {
				cfg.DefaultPool = cfg.Pools[0].Name
			} else {
				cfg.DefaultPool = ""
			}
		}
		term.WaitEnter(msgs.Get("press_enter_return"))
	case 3:
		if len(cfg.Pools) == 0 {
			term.Println(msgs.Get("need_pool"))
			term.WaitEnter(msgs.Get("press_enter_return"))
			return nil
		}
		term.Batch(func() {
			term.Clear()
			term.Println(msgs.Get("prompt_default_pool"))
			for i, p := range cfg.Pools {
				term.Println(fmt.Sprintf("  %d) %s", i+1, p.Name))
			}
		})
		choice := ui.ClampChoice(term.Prompt(msgs.Get("prompt_default_pool_choice"), "1"), len(cfg.Pools))
		cfg.DefaultPool = cfg.Pools[choice-1].Name
		term.WaitEnter(msgs.Get("press_enter_return"))
	default:
		return nil
	}
	return nil
}

func manageLinks(term *ui.TUI, msgs i18n.Messages, cfg *config.Config, dbPath string) error {
	db, err := config.OpenDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	if len(cfg.Pools) == 0 {
		term.Println(msgs.Get("need_pool"))
		term.WaitEnter(msgs.Get("press_enter_return"))
		return nil
	}
	idx := selectPoolIndex(term, msgs, cfg.Pools)
	if idx < 0 {
		return nil
	}
	pool := &cfg.Pools[idx]

	for {
		term.Batch(func() {
			term.Reset()
			term.Println(msgs.Get("links_manage_title") + " " + pool.Name)
			term.Println(msgs.Get("links_help_manage"))
		})
		if len(pool.Links) > 0 {
			term.Batch(func() {
				term.Println(msgs.Get("links_current"))
				for i, link := range pool.Links {
					term.Println("")
					term.Println(fmt.Sprintf("  %d) %s: %s", i+1, msgs.Get("label_link"), link))
					if meta, ok, err := config.GetLinkTrafficMeta(db, link); err == nil && ok {
						term.Println(fmt.Sprintf("     %s: %sGB", msgs.Get("label_traffic_limit"), formatGiB(meta.LimitBytes)))
						term.Println(fmt.Sprintf("     %s: %d %s", msgs.Get("label_traffic_reset_day"), meta.ResetDay, meta.ResetTime))
					} else {
						term.Println(fmt.Sprintf("     %s: %s", msgs.Get("label_traffic_limit"), msgs.Get("label_unset")))
						term.Println(fmt.Sprintf("     %s: %s", msgs.Get("label_traffic_reset_day"), msgs.Get("label_unset")))
					}
				}
			})
		}
		input := strings.TrimSpace(term.Prompt(msgs.Get("prompt_link_single_manage"), ""))
		if input == "" {
			break
		}

		if cmd, ok := parseLinkInputCommand(input); ok {
			switch {
			case isCmd(cmd.Name, "q", "quit", "exit", "back"):
				return nil
			case isCmd(cmd.Name, "del", "rm", "d", "delete"):
				delIdx := ui.ClampChoice(cmd.Arg, len(pool.Links)) - 1
				if delIdx < 0 || delIdx >= len(pool.Links) {
					term.Println(msgs.Get("links_delete_invalid"))
					term.WaitEnter(msgs.Get("press_enter_continue"))
					continue
				}
				pool.Links = append(pool.Links[:delIdx], pool.Links[delIdx+1:]...)
				continue
			case isCmd(cmd.Name, "set", "traffic"):
				setIdx := ui.ClampChoice(cmd.Arg, len(pool.Links)) - 1
				if setIdx < 0 || setIdx >= len(pool.Links) {
					term.Println(msgs.Get("links_set_invalid"))
					term.WaitEnter(msgs.Get("press_enter_continue"))
					continue
				}
				link := pool.Links[setIdx]
				meta, ok, err := promptLinkTrafficMeta(term, msgs)
				if err != nil {
					return err
				}
				if ok {
					if err := config.SetLinkTrafficMeta(db, link, meta); err != nil {
						return err
					}
					term.Println(msgs.Get("traffic_saved"))
					term.WaitEnter(msgs.Get("press_enter_continue"))
				}
				continue
			case isCmd(cmd.Name, "clr", "clear"):
				clrIdx := ui.ClampChoice(cmd.Arg, len(pool.Links)) - 1
				if clrIdx < 0 || clrIdx >= len(pool.Links) {
					term.Println(msgs.Get("links_set_invalid"))
					term.WaitEnter(msgs.Get("press_enter_continue"))
					continue
				}
				link := pool.Links[clrIdx]
				if err := config.ClearLinkTrafficMeta(db, link); err != nil {
					return err
				}
				term.Println(msgs.Get("traffic_cleared"))
				term.WaitEnter(msgs.Get("press_enter_continue"))
				continue
			}
		}

		if input == "" {
			term.Println(msgs.Get("invalid_link"))
			term.WaitEnter(msgs.Get("press_enter_continue"))
			continue
		}

		if _, err := links.Parse(input); err != nil {
			term.Println(fmt.Sprintf("%s: %v", msgs.Get("invalid_link"), err))
			term.WaitEnter(msgs.Get("press_enter_continue"))
			continue
		}
		pool.Links = append(pool.Links, input)
		if meta, ok, err := promptLinkTrafficMeta(term, msgs); err != nil {
			return err
		} else if ok {
			if err := config.SetLinkTrafficMeta(db, input, meta); err != nil {
				return err
			}
		}
	}
	return nil
}

func formatGiB(bytes int64) string {
	const giB = float64(1024 * 1024 * 1024)
	gb := float64(bytes) / giB
	gbStr := fmt.Sprintf("%.2f", gb)
	return strings.TrimRight(strings.TrimRight(gbStr, "0"), ".")
}

func manageRuntime(term *ui.TUI, msgs i18n.Messages, cfg *config.Config, dbPath, xrayConfigPath, xrayBin string) error {
	term.Batch(func() {
		term.Println(msgs.Get("runtime_menu_title"))
		term.Println("  1) " + msgs.Get("runtime_menu_balancer"))
		term.Println("  2) " + msgs.Get("runtime_menu_policy"))
		term.Println("  3) " + msgs.Get("menu_back"))
	})
	choice := ui.ClampChoice(term.Prompt(msgs.Get("menu_prompt"), "3"), 3)
	switch choice {
	case 1:
		return manageBalancer(term, msgs, *cfg, dbPath, xrayConfigPath, xrayBin)
	case 2:
		if err := setBalancerPolicy(term, msgs, cfg); err != nil {
			return err
		}
		if err := saveConfig(dbPath, *cfg); err != nil {
			return err
		}
		if runtime.IsBalancerDaemonRunning(dbPath) {
			if err := runtime.ReloadBalancerDaemon(dbPath); err != nil {
				term.Println(msgs.Get("balancer_daemon_reload_failed") + ": " + err.Error())
			} else {
				term.Println(msgs.Get("balancer_daemon_reloaded"))
			}
		}
		term.WaitEnter(msgs.Get("press_enter_return"))
		return nil
	default:
		return nil
	}
}

func setBalancerPolicy(term *ui.TUI, msgs i18n.Messages, cfg *config.Config) error {
	if len(cfg.Pools) == 0 {
		term.Println(msgs.Get("need_pool"))
		return nil
	}
	poolIdx := selectPoolIndex(term, msgs, cfg.Pools)
	if poolIdx < 0 {
		return nil
	}
	cfg.Pools[poolIdx].Policy = selectPolicy(term, msgs)
	return nil
}
