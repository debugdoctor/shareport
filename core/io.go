package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func WriteJSONFile(path string, v any) error {
	if path == "" {
		return fmt.Errorf("empty xray config path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
