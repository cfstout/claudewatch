// Package config loads ~/.config/claudewatch/config.toml. All fields are
// optional; defaults are applied for anything missing or zero. A missing file
// is not an error. A malformed file logs a warning and falls back to defaults
// rather than crashing the daemon.
package config

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the on-disk + runtime config for claudewatch.
type Config struct {
	Port                       int    `toml:"port"`
	NotificationCommand        string `toml:"notification_command"`
	DebounceSeconds            int    `toml:"debounce_seconds"`
	DefaultSnoozeMinutes       int    `toml:"default_snooze_minutes"`
	AutoArchiveCompleteMinutes int    `toml:"auto_archive_complete_minutes"`
}

// Defaults returns the baseline config used when the file is missing or
// fields are unset.
func Defaults() Config {
	return Config{
		Port:                       7777,
		NotificationCommand:        "auto", // pick terminal-notifier if present, else osascript
		DebounceSeconds:            10,
		DefaultSnoozeMinutes:       10,
		AutoArchiveCompleteMinutes: 0, // 0 = never
	}
}

// DefaultPath is the standard config location.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "claudewatch", "config.toml")
}

// Load reads the config from `path` (or DefaultPath() if empty), merging on
// top of Defaults(). Returns Defaults() with no error if the file doesn't
// exist.
func Load(path string) Config {
	if path == "" {
		path = DefaultPath()
	}
	cfg := Defaults()
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("read config", "path", path, "err", err)
		}
		return cfg
	}
	var loaded Config
	if _, err := toml.Decode(string(data), &loaded); err != nil {
		slog.Warn("parse config; using defaults", "path", path, "err", err)
		return cfg
	}
	return merge(cfg, loaded)
}

// merge applies any non-zero fields from loaded onto base.
func merge(base, loaded Config) Config {
	if loaded.Port != 0 {
		base.Port = loaded.Port
	}
	if loaded.NotificationCommand != "" {
		base.NotificationCommand = loaded.NotificationCommand
	}
	if loaded.DebounceSeconds != 0 {
		base.DebounceSeconds = loaded.DebounceSeconds
	}
	if loaded.DefaultSnoozeMinutes != 0 {
		base.DefaultSnoozeMinutes = loaded.DefaultSnoozeMinutes
	}
	if loaded.AutoArchiveCompleteMinutes != 0 {
		base.AutoArchiveCompleteMinutes = loaded.AutoArchiveCompleteMinutes
	}
	return base
}
