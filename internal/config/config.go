package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	AppDir     = ".kawarimi"
	ConfigFile = "config.json"
)

// Config holds non-sensitive application configuration.
type Config struct {
	VaultDir        string     `json:"vault_dir"`
	CheckinInterval int        `json:"checkin_interval_days"`
	AutoSync        AutoSync   `json:"auto_sync"`
	SyncTargets     SyncTargets `json:"sync_targets"`
}

type AutoSync struct {
	Git bool `json:"git"`
	USB bool `json:"usb"`
}

type SyncTargets struct {
	GitRemote string `json:"git_remote,omitempty"`
	USBPath   string `json:"usb_path,omitempty"`
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig(vaultDir string) *Config {
	return &Config{
		VaultDir:        vaultDir,
		CheckinInterval: 7,
		AutoSync: AutoSync{
			Git: false,
			USB: false,
		},
	}
}

// AppDirPath returns the path to ~/.kawarimi.
func AppDirPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, AppDir), nil
}

// ConfigPath returns the full path to the config file.
func ConfigPath() (string, error) {
	appDir, err := AppDirPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(appDir, ConfigFile), nil
}

// Load reads the config from disk.
func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no config found at %s — run 'kawarimi init' first", path)
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return &cfg, nil
}

// Save writes the config to disk, creating the app directory if needed.
func Save(cfg *Config) error {
	appDir, err := AppDirPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(appDir, 0700); err != nil {
		return fmt.Errorf("creating app directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	path := filepath.Join(appDir, ConfigFile)
	return os.WriteFile(path, data, 0600)
}
