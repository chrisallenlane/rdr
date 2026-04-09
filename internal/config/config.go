// Package config handles application configuration via environment variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Config holds all application configuration values.
type Config struct {
	DataPath      string
	DatabasePath  string
	FaviconsDir   string
	ListenAddr    string
	PollInterval  time.Duration
	RetentionDays int
}

// defaultDataPath returns the default data directory using the OS-standard
// config location (e.g. ~/.config/rdr on Linux, ~/Library/Application Support/rdr
// on macOS). Respects $XDG_CONFIG_HOME on Linux.
func defaultDataPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("determining config directory: %w", err)
	}
	return filepath.Join(configDir, "rdr"), nil
}

// Load reads configuration from environment variables and returns a Config.
// Missing variables are filled with sensible defaults.
func Load() (*Config, error) {
	dataPath, err := defaultDataPath()
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		DataPath:     dataPath,
		ListenAddr:   ":8080",
		PollInterval: 6 * time.Hour,
	}

	if v := os.Getenv("RDR_DATA_PATH"); v != "" {
		cfg.DataPath = v
	}

	cfg.DatabasePath = filepath.Join(cfg.DataPath, "rdr.db")
	cfg.FaviconsDir = filepath.Join(cfg.DataPath, "favicons")

	if v := os.Getenv("RDR_DATABASE_PATH"); v != "" {
		cfg.DatabasePath = v
	}

	if v := os.Getenv("RDR_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}

	if v := os.Getenv("RDR_POLL_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid RDR_POLL_INTERVAL %q: %w", v, err)
		}
		if d < time.Minute {
			return nil, fmt.Errorf("RDR_POLL_INTERVAL must be >= 1m, got %s", d)
		}
		cfg.PollInterval = d
	}

	if v := os.Getenv("RDR_RETENTION_DAYS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid RDR_RETENTION_DAYS %q: %w", v, err)
		}
		cfg.RetentionDays = n
	}

	if err := os.MkdirAll(cfg.FaviconsDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating favicons directory: %w", err)
	}

	return cfg, nil
}
