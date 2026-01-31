package cli

import (
	"fmt"
	"strings"

	"aimerick.com/shareport/config"
	"aimerick.com/shareport/i18n"
	"aimerick.com/shareport/ui"
)

func selectPoolIndex(term *ui.TUI, msgs i18n.Messages, pools []config.PoolConfig) int {
	term.Reset()
	term.Batch(func() {
		term.Reset()
		term.Println(msgs.Get("prompt_select_pool"))
		for i, p := range pools {
			term.Println(fmt.Sprintf("  %d) %s", i+1, p.Name))
		}
	})
	choice := strings.TrimSpace(term.Prompt(msgs.Get("prompt_pool_choice"), ""))
	if choice == "" || choice == ":q" {
		return -1
	}
	idx := ui.ClampChoice(choice, len(pools)) - 1
	return idx
}

func findPoolIndex(cfg *config.Config, name string) int {
	for i, p := range cfg.Pools {
		if p.Name == name {
			return i
		}
	}
	return -1
}
