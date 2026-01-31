package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"aimerick.com/shareport/config"
	"aimerick.com/shareport/i18n"
	"aimerick.com/shareport/runtime"
	"aimerick.com/shareport/ui"
)

func manageBalancer(term *ui.TUI, msgs i18n.Messages, cfg config.Config, dbPath, xrayConfigPath, xrayBin string) error {
	term.Batch(func() {
		term.Reset()
		term.Println(msgs.Get("balancer_menu_title"))
	})
	if !runtime.IsXrayRunning(dbPath) {
		term.Batch(func() {
			term.Println("  1) " + msgs.Get("balancer_menu_start"))
			term.Println("  2) " + msgs.Get("menu_back"))
		})
		choice := ui.ClampChoice(term.Prompt(msgs.Get("menu_prompt"), "2"), 2)
		switch choice {
		case 1:
			xrayPath, err := runtime.EnsureXrayInstalled(xrayBin)
			if err != nil {
				return err
			}
			if err := runtime.StartXray(dbPath, xrayPath, xrayConfigPath); err != nil {
				return err
			}
			term.Println(msgs.Get("proxy_started"))
			if err := runtime.EnsureDefaultOutbound(xrayPath); err != nil {
				term.Println(msgs.Get("balancer_default_failed") + ": " + err.Error())
			}
			pid, err := runtime.StartBalancerDaemon(dbPath, xrayBin)
			if err != nil {
				term.Println(msgs.Get("balancer_start_failed") + ": " + err.Error())
			} else if err := runtime.ReloadBalancerDaemon(dbPath); err != nil {
				term.Println(msgs.Get("balancer_daemon_reload_failed") + ": " + err.Error())
			} else {
				term.Println(msgs.Get("balancer_daemon_started") + fmt.Sprintf(" (pid: %d)", pid))
			}
			term.WaitEnter(msgs.Get("press_enter_return"))
		default:
		}
		return nil
	}

	daemonRunning := runtime.IsBalancerDaemonRunning(dbPath)
	switchCfg, _ := runtime.LoadBalancerSwitchConfigWithRetry(dbPath, 1*time.Second)
	if !daemonRunning || switchCfg.Mode == runtime.BalancerSwitchOff {
		// If switching is off (or daemon isn't running), enforce the default (node-1).
		if xrayPath, err := runtime.ResolveXrayBinary(xrayBin); err == nil {
			if err := runtime.EnsureDefaultOutbound(xrayPath); err != nil {
				term.Println(msgs.Get("balancer_default_failed") + ": " + err.Error())
			}
		}
	}

	term.Batch(func() {
		term.Println("  1) " + msgs.Get("balancer_menu_stop"))
		term.Println("  2) " + msgs.Get("balancer_switch_strategy") + " (" + switchStrategySummary(switchCfg) + ")")
		term.Println("  3) " + msgs.Get("menu_back"))
	})
	choice := ui.ClampChoice(term.Prompt(msgs.Get("menu_prompt"), "3"), 3)
	switch choice {
	case 1:
		if daemonRunning {
			if runtime.StopBalancerDaemon(dbPath) {
				term.Println(msgs.Get("balancer_daemon_stopped"))
			} else {
				term.Println(msgs.Get("balancer_daemon_not_running"))
			}
		} else {
			term.Println(msgs.Get("balancer_daemon_not_running"))
		}
		if err := runtime.StopXray(dbPath); err != nil {
			return err
		}
		term.Println(msgs.Get("proxy_stopped"))
		term.WaitEnter(msgs.Get("press_enter_return"))
	case 2:
		configureSwitchStrategy(term, msgs, dbPath, xrayBin)
		term.WaitEnter(msgs.Get("press_enter_return"))
	default:
	}
	return nil
}

func configureSwitchStrategy(term *ui.TUI, msgs i18n.Messages, dbPath, xrayBin string) {
	current, _ := runtime.LoadBalancerSwitchConfigWithRetry(dbPath, 1*time.Second)
	current = current.Normalize()

	term.Batch(func() {
		term.Reset()
		term.Println(msgs.Get("balancer_switch_strategy_title"))
		term.Println("  1) " + msgs.Get("balancer_switch_off"))
		term.Println("  2) " + msgs.Get("balancer_switch_interval_fixed"))
		term.Println("  3) " + msgs.Get("balancer_switch_interval_random"))
		term.Println("  4) " + msgs.Get("balancer_switch_per_connection"))
		term.Println("  5) " + msgs.Get("menu_back"))
	})

	choice := ui.ClampChoice(term.Prompt(msgs.Get("menu_prompt"), "5"), 5)
	if choice == 5 {
		return
	}

	next := current
	switch choice {
	case 1:
		next.Mode = runtime.BalancerSwitchOff
	case 2:
		next.Mode = runtime.BalancerSwitchInterval
		next.IntervalMode = runtime.BalancerIntervalFixed
		sec := promptIntervalSeconds(term, msgs, "prompt_interval_seconds", int(current.Interval.Seconds()))
		if sec <= 0 {
			return
		}
		next.Interval = time.Duration(sec) * time.Second
	case 3:
		next.Mode = runtime.BalancerSwitchInterval
		next.IntervalMode = runtime.BalancerIntervalRandom
		minSec := promptIntervalSeconds(term, msgs, "prompt_interval_min_seconds", int(current.MinInterval.Seconds()))
		if minSec <= 0 {
			return
		}
		maxSec := promptIntervalSeconds(term, msgs, "prompt_interval_max_seconds", int(current.MaxInterval.Seconds()))
		if maxSec <= 0 {
			return
		}
		next.MinInterval = time.Duration(minSec) * time.Second
		next.MaxInterval = time.Duration(maxSec) * time.Second
	case 4:
		next.Mode = runtime.BalancerSwitchPerConnection
	}

	if err := runtime.SaveBalancerSwitchConfig(dbPath, next); err != nil {
		term.Println(msgs.Get("balancer_switch_save_failed") + ": " + err.Error())
		return
	}

	if next.Mode == runtime.BalancerSwitchOff {
		_ = runtime.StopBalancerDaemon(dbPath)
		if xrayPath, err := runtime.ResolveXrayBinary(xrayBin); err == nil {
			if err := runtime.EnsureDefaultOutbound(xrayPath); err != nil {
				term.Println(msgs.Get("balancer_default_failed") + ": " + err.Error())
			}
		}
		term.Println(msgs.Get("balancer_switch_applied"))
		return
	}

	pid, err := runtime.StartBalancerDaemon(dbPath, xrayBin)
	if err != nil {
		term.Println(msgs.Get("balancer_start_failed") + ": " + err.Error())
		return
	}
	if err := runtime.ReloadBalancerDaemon(dbPath); err != nil {
		term.Println(msgs.Get("balancer_daemon_reload_failed") + ": " + err.Error())
		return
	}
	term.Println(msgs.Get("balancer_switch_applied") + fmt.Sprintf(" (pid: %d)", pid))
}

func promptIntervalSeconds(term *ui.TUI, msgs i18n.Messages, promptKey string, def int) int {
	if def <= 0 {
		def = 60
	}
	input := strings.TrimSpace(term.Prompt(msgs.Get(promptKey), strconv.Itoa(def)))
	if input == "" {
		return def
	}
	sec, err := strconv.Atoi(input)
	if err != nil {
		term.Println(msgs.Get("invalid_interval_seconds"))
		return 0
	}
	if sec < 10 || sec > 30*60 {
		term.Println(msgs.Get("invalid_interval_range"))
		return 0
	}
	return sec
}

func switchStrategySummary(cfg runtime.BalancerSwitchConfig) string {
	cfg = cfg.Normalize()
	switch cfg.Mode {
	case runtime.BalancerSwitchOff:
		return "off"
	case runtime.BalancerSwitchPerConnection:
		return "per_connection"
	default:
		if cfg.IntervalMode == runtime.BalancerIntervalRandom {
			return fmt.Sprintf("random %ds-%ds", int(cfg.MinInterval.Seconds()), int(cfg.MaxInterval.Seconds()))
		}
		return fmt.Sprintf("fixed %ds", int(cfg.Interval.Seconds()))
	}
}
