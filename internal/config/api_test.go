package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPublicAPIUsesOnlyUserConfigurationFiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("APPDATA", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(root, dir)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf("unsafe test config directory: %q", dir)
	}
	settings, err := LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Theme = "dark"
	if err := SaveSettings(settings); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings()
	if err != nil || !reflect.DeepEqual(settings, got) {
		t.Fatalf("settings=%#v err=%v", got, err)
	}
	wantProfiles := []Profile{{ID: "user", Name: "User profile", Container: "mp4"}}
	if err := SaveProfiles(wantProfiles); err != nil {
		t.Fatal(err)
	}
	profiles, err := ListProfiles()
	if err != nil || !reflect.DeepEqual(profiles, wantProfiles) {
		t.Fatalf("profiles=%v err=%v", profiles, err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "ConvertGo"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name() != "profiles.json" || entries[1].Name() != "settings.json" {
		t.Fatalf("unexpected persisted artifacts: %v", entries)
	}
}

func TestExecutableOverrideRoundTrip(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	settings := DefaultSettings()
	settings.FFmpegPath = executable
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := saveSettings(path, settings); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings(path)
	if err != nil || got.FFmpegPath != executable {
		t.Fatalf("override=%q err=%v", got.FFmpegPath, err)
	}
}

func TestNonExecutableOverrideRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-an-executable")
	if err := os.WriteFile(path, []byte("not executable"), 0600); err != nil {
		t.Fatal(err)
	}
	settings := DefaultSettings()
	settings.FFmpegPath = path
	if err := settings.Validate(); err == nil || !strings.Contains(err.Error(), "ffmpeg_path") {
		t.Fatalf("expected override error, got %v", err)
	}
}

func TestMissingOverrideIsRetainedAndCanBeExplicitlyRepaired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	settings := DefaultSettings()
	settings.FFmpegPath = filepath.Join(t.TempDir(), "missing-ffmpeg")
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings(path)
	if err == nil || !strings.Contains(err.Error(), "ffmpeg_path") || got.FFmpegPath != settings.FFmpegPath {
		t.Fatalf("invalid override silently defaulted: %#v err=%v", got, err)
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || string(unchanged) != string(data) {
		t.Fatal("loading changed invalid settings")
	}
	got.FFmpegPath = ""
	if err := saveSettings(path, got); err != nil {
		t.Fatalf("explicit repair failed: %v", err)
	}
	repaired, err := loadSettings(path)
	if err != nil || repaired.FFmpegPath != "" {
		t.Fatalf("repair=%#v err=%v", repaired, err)
	}
}

func TestSemanticValidationFailurePreservesLoadedSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"locale":"fr-FR","theme":"dark"}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings(path)
	if err == nil || got.Locale != "fr-FR" || got.Theme != "dark" {
		t.Fatalf("invalid settings silently replaced: %#v err=%v", got, err)
	}
	got.Locale = "en-US"
	if err := saveSettings(path, got); err != nil {
		t.Fatal(err)
	}
}
