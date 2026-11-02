package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// fileConfig is persisted at <user config dir>/certwatch/config.json.
type fileConfig struct {
	APIURL string `json:"api_url,omitempty"`
	APIKey string `json:"api_key,omitempty"`
}

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "certwatch", "config.json")
}

func loadConfig(path string) (fileConfig, error) {
	var c fileConfig
	b, err := os.ReadFile(path) //nolint:gosec // path is the user's own config file
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(b, &c)
}

func saveConfig(path string, c fileConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}
