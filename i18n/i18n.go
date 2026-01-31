package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

type Messages struct {
	Lang string
	Map  map[string]string
}

func (m Messages) Get(key string) string {
	if val, ok := m.Map[key]; ok {
		return val
	}
	return key
}

//go:embed i18n.json
var i18nFS embed.FS

func LoadMessages(lang string) (Messages, error) {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "" {
		lang = "zh"
	}

	data, err := i18nFS.ReadFile("i18n.json")
	if err != nil {
		return Messages{}, err
	}

	var bundle map[string]map[string]string
	if err := json.Unmarshal(data, &bundle); err != nil {
		return Messages{}, err
	}

	key := "zh"
	if strings.HasPrefix(lang, "en") {
		key = "en"
	}
	msgs, ok := bundle[key]
	if !ok {
		return Messages{}, fmt.Errorf("missing language: %s", key)
	}

	return Messages{Lang: key, Map: msgs}, nil
}
