package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const configDir = ".config/dbx"
const configFile = "config.json"

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, configDir, configFile), nil
}

// EnsureDir creates ~/.config/dbx if it does not exist. Call at startup so history can be saved.
func EnsureDir() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

// Load reads the config from disk. Returns defaults if the file does not exist.
func Load() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaults(), nil
	}
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	applyDefaults(&cfg)
	return &cfg, nil
}

// Save writes the config to disk, creating directories as needed.
func Save(cfg *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func defaults() *Config {
	return &Config{
		Connections: []Connection{},
		Layout: Layout{
			ExplorerWidthPct: 25,
			EditorHeightPct:  50,
		},
		Theme: "terminal",
		StatusMessageSeconds: 5,
	}
}

func applyDefaults(cfg *Config) {
	if cfg.Layout.ExplorerWidthPct == 0 {
		cfg.Layout.ExplorerWidthPct = 25
	}
	if cfg.Layout.EditorHeightPct == 0 {
		cfg.Layout.EditorHeightPct = 50
	}
	if cfg.Theme == "" {
		cfg.Theme = "terminal"
	}
	if cfg.StatusMessageSeconds <= 0 {
		cfg.StatusMessageSeconds = 5
	}
}
