package cli

import "strings"

type linkInputCommand struct {
	Name string
	Arg  string
}

func parseLinkInputCommand(input string) (linkInputCommand, bool) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return linkInputCommand{}, false
	}
	// Backward compatible: allow commands with or without leading ':'.
	if strings.HasPrefix(raw, ":") {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, ":"))
	}
	if raw == "" {
		return linkInputCommand{}, false
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return linkInputCommand{}, false
	}
	name := strings.ToLower(fields[0])
	arg := strings.TrimSpace(strings.TrimPrefix(raw, fields[0]))
	return linkInputCommand{Name: name, Arg: arg}, true
}

func isCmd(name string, aliases ...string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, a := range aliases {
		if name == strings.ToLower(strings.TrimSpace(a)) {
			return true
		}
	}
	return false
}
