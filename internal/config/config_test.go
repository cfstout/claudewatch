package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	got := Load(filepath.Join(t.TempDir(), "missing.toml"))
	def := Defaults()
	if got.Port != def.Port || got.DebounceSeconds != def.DebounceSeconds ||
		got.DefaultSnoozeMinutes != def.DefaultSnoozeMinutes ||
		got.NotificationCommand != def.NotificationCommand ||
		got.NotificationsEnabled == nil || *got.NotificationsEnabled != true {
		t.Fatalf("missing file should yield Defaults(), got %+v", got)
	}
}

func TestLoadHonorsNotificationsEnabledFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`notifications_enabled = false`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Load(path)
	if got.NotificationsEnabled == nil || *got.NotificationsEnabled != false {
		t.Fatalf("notifications_enabled = false not respected: %+v", got.NotificationsEnabled)
	}
}

func TestLoadMergesPartialOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`port = 9000
default_snooze_minutes = 30`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Load(path)
	if got.Port != 9000 {
		t.Fatalf("Port = %d, want 9000", got.Port)
	}
	if got.DefaultSnoozeMinutes != 30 {
		t.Fatalf("DefaultSnoozeMinutes = %d, want 30", got.DefaultSnoozeMinutes)
	}
	// Untouched fields keep defaults.
	if got.DebounceSeconds != 10 {
		t.Fatalf("DebounceSeconds = %d, want 10 (default)", got.DebounceSeconds)
	}
}

func TestLoadMalformedReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`this is not = valid TOML [[`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Load(path)
	def := Defaults()
	if got.Port != def.Port || got.DebounceSeconds != def.DebounceSeconds {
		t.Fatalf("malformed file should yield Defaults(), got %+v", got)
	}
}
