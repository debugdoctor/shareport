package cli

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"aimerick.com/shareport/components"
	"aimerick.com/shareport/config"
	"aimerick.com/shareport/i18n"
	"aimerick.com/shareport/ui"
)

var errCancelPool = errors.New("pool cancelled")

func saveConfig(dbPath string, cfg config.Config) error {
	db, err := config.OpenDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	return config.SaveDB(db, cfg)
}

func collectLinks(term *ui.TUI, msgs i18n.Messages, db *sql.DB) ([]string, error) {
	var linksList []string
	term.Println(msgs.Get("links_help"))
	for {
		if len(linksList) > 0 {
			term.Batch(func() {
				term.Println(msgs.Get("links_current"))
				for i, link := range linksList {
					term.Println(fmt.Sprintf("  %d) %s", i+1, link))
				}
			})
		}
		prompt := msgs.Get("prompt_link_single")
		if len(linksList) == 0 {
			prompt = msgs.Get("prompt_link_single_required")
		}
		input := strings.TrimSpace(term.Prompt(prompt, ""))
		if input == "" {
			if len(linksList) == 0 {
				term.Println(msgs.Get("need_links"))
				continue
			}
			break
		}

		if cmd, ok := parseLinkInputCommand(input); ok {
			switch {
			case isCmd(cmd.Name, "q", "quit", "exit"):
				return nil, errCancelPool
			case isCmd(cmd.Name, "del", "rm", "d", "delete"):
				idx := ui.ClampChoice(cmd.Arg, len(linksList)) - 1
				if idx < 0 || idx >= len(linksList) {
					term.Println(msgs.Get("links_delete_invalid"))
					continue
				}
				linksList = append(linksList[:idx], linksList[idx+1:]...)
				continue
			}
		}

		if _, err := components.Parse(strings.TrimSpace(input)); err != nil {
			term.Println(fmt.Sprintf("%s: %v", msgs.Get("invalid_link"), err))
			continue
		}
		linksList = append(linksList, strings.TrimSpace(input))
		if db != nil {
			if meta, ok, err := promptLinkTrafficMeta(term, msgs); err != nil {
				return nil, err
			} else if ok {
				if err := config.SetLinkTrafficMeta(db, strings.TrimSpace(input), meta); err != nil {
					return nil, err
				}
			}
		}
	}
	return linksList, nil
}

func promptLinkTrafficMeta(term *ui.TUI, msgs i18n.Messages) (config.LinkTrafficMeta, bool, error) {
	raw := strings.TrimSpace(term.Prompt(msgs.Get("prompt_traffic_limit_gb"), ""))
	if raw == "" {
		return config.LinkTrafficMeta{}, false, nil
	}
	rawLower := strings.ToLower(raw)
	rawLower = strings.TrimSuffix(rawLower, "gb")
	rawLower = strings.TrimSuffix(rawLower, "g")
	rawLower = strings.TrimSpace(rawLower)
	gb, err := strconv.ParseFloat(rawLower, 64)
	if err != nil || gb <= 0 {
		term.Println(msgs.Get("traffic_limit_invalid"))
		return config.LinkTrafficMeta{}, false, nil
	}

	dayStr := strings.TrimSpace(term.Prompt(msgs.Get("prompt_traffic_reset_day"), "1"))
	day, err := strconv.Atoi(dayStr)
	if err != nil || day < 1 || day > 31 {
		term.Println(msgs.Get("traffic_reset_day_invalid"))
		return config.LinkTrafficMeta{}, false, nil
	}

	resetTime := strings.TrimSpace(term.Prompt(msgs.Get("prompt_traffic_reset_time"), "00:00"))
	hhmm, ok := normalizeHHMM(resetTime)
	if !ok {
		term.Println(msgs.Get("traffic_reset_time_invalid"))
		return config.LinkTrafficMeta{}, false, nil
	}

	const giB = int64(1024 * 1024 * 1024)
	limitBytes := int64(gb * float64(giB))
	if limitBytes <= 0 {
		term.Println(msgs.Get("traffic_limit_invalid"))
		return config.LinkTrafficMeta{}, false, nil
	}
	return config.LinkTrafficMeta{LimitBytes: limitBytes, ResetDay: day, ResetTime: hhmm}, true, nil
}

func normalizeHHMM(in string) (string, bool) {
	in = strings.TrimSpace(in)
	if in == "" {
		return "", false
	}
	parts := strings.Split(in, ":")
	if len(parts) != 2 {
		return "", false
	}
	h, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return "", false
	}
	return fmt.Sprintf("%02d:%02d", h, m), true
}

func selectPolicy(term *ui.TUI, msgs i18n.Messages) string {
	term.Reset()
	options := []string{"round_robin", "random", "least_conn"}
	term.Batch(func() {
		term.Println(msgs.Get("policy_options_title"))
		for i, opt := range options {
			term.Println(fmt.Sprintf("  %d) %s", i+1, opt))
		}
	})
	choiceStr := term.Prompt(msgs.Get("prompt_policy_choice"), "1")
	choice := ui.ClampChoice(choiceStr, len(options))
	return options[choice-1]
}

func promptAndSavePublicHost(term *ui.TUI, msgs i18n.Messages, fallback string) (string, error) {
	current := ""
	if cached, err := components.LoadMeta(); err == nil {
		current = strings.TrimSpace(cached.PublicHost)
	}
	def := strings.TrimSpace(current)
	if def == "" {
		def = strings.TrimSpace(fallback)
	}
	if def == "" {
		def = components.DetectPublicHost()
	}
	host := strings.TrimSpace(term.Prompt(msgs.Get("prompt_public_host"), def))
	if host == "" {
		return "", fmt.Errorf("%s", msgs.Get("public_host_required"))
	}
	if err := components.SaveMeta(components.ShareportMeta{PublicHost: host}); err != nil {
		return "", err
	}
	return host, nil
}
