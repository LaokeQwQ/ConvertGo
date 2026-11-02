package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultsForLocaleAndPlatform(t *testing.T) {
	for _, test := range []struct{ locale, want string }{
		{"zh_CN.UTF-8", "zh-CN"}, {"en_US.UTF-8", "en-US"},
		{"en-US", "en-US"}, {"fr_FR.UTF-8", "zh-CN"}, {"", "zh-CN"},
	} {
		t.Run(test.locale, func(t *testing.T) {
			got := defaultsFor(test.locale, "Test UI Font")
			if got.Locale != test.want || got.FontFamily != "Test UI Font" || got.Theme != "system" || got.MaxConcurrentJobs != 1 || got.PreventSleep || got.UpdateSource != "github" || got.UpdateMirrorURL != "" || got.AutoCheckUpdates || got.FFmpegPath != "" {
				t.Fatalf("unexpected defaults: %#v", got)
			}
			if err := got.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMissingFilesDoNotCreatePersistentState(t *testing.T) {
	dir := t.TempDir()
	settings, err := loadSettings(filepath.Join(dir, "settings.json"))
	if err != nil || !reflect.DeepEqual(settings, DefaultSettings()) {
		t.Fatalf("settings=%#v err=%v", settings, err)
	}
	profiles, err := loadProfiles(filepath.Join(dir, "profiles.json"))
	if err != nil || profiles == nil || len(profiles) != 0 {
		t.Fatalf("profiles=%#v err=%v", profiles, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("missing-file read wrote state: %v %v", entries, err)
	}
}

func TestMalformedSettingsPreservedOnLoadAndSave(t *testing.T) {
	for _, raw := range []string{`{"locale":`, `null`, `[]`, `{} {}`, `{"locale":7}`, `{"max_concurrent_jobs":null}`, `{"history":[]}`} {
		t.Run(raw, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadSettings(path); err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("expected actionable load error, got %v", err)
			}
			if err := saveSettings(path, DefaultSettings()); err == nil {
				t.Fatal("save overwrote malformed settings")
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != raw {
				t.Fatalf("malformed file changed: %q err=%v", got, err)
			}
		})
	}
}

func TestPartialSettingsUseDefaultsAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"theme":"dark","max_concurrent_jobs":3}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := loadSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultSettings()
	want.Theme, want.MaxConcurrentJobs = "dark", 3
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%#v want=%#v", got, want)
	}
	got.AccentHex, got.PreventSleep, got.AutoCheckUpdates = "#123aBC", true, true
	if err := saveSettings(path, got); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := loadSettings(path)
	if err != nil || !reflect.DeepEqual(roundTrip, got) {
		t.Fatalf("got=%#v err=%v", roundTrip, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != "settings.json" {
		t.Fatalf("unexpected persisted files: %v err=%v", entries, err)
	}
}

func TestSettingsValidationRejectsInvalidFields(t *testing.T) {
	cases := map[string]func(*Settings){
		"locale":          func(s *Settings) { s.Locale = "fr-FR" },
		"theme":           func(s *Settings) { s.Theme = "neon" },
		"accent":          func(s *Settings) { s.AccentHex = "red" },
		"font":            func(s *Settings) { s.FontFamily = " " },
		"jobs":            func(s *Settings) { s.MaxConcurrentJobs = 0 },
		"source":          func(s *Settings) { s.UpdateSource = "other" },
		"mirror missing":  func(s *Settings) { s.UpdateSource = "mirror" },
		"mirror insecure": func(s *Settings) { s.UpdateSource = "mirror"; s.UpdateMirrorURL = "http://example.com/releases" },
		"mirror credentials": func(s *Settings) {
			s.UpdateSource = "mirror"
			s.UpdateMirrorURL = "https://user:pass@example.com/releases"
		},
		"mirror query": func(s *Settings) {
			s.UpdateSource = "mirror"
			s.UpdateMirrorURL = "https://example.com/releases?token=secret"
		},
		"mirror fragment": func(s *Settings) {
			s.UpdateSource = "mirror"
			s.UpdateMirrorURL = "https://example.com/releases#section"
		},
		"ffmpeg relative":  func(s *Settings) { s.FFmpegPath = "ffmpeg" },
		"ffmpeg missing":   func(s *Settings) { s.FFmpegPath = filepath.Join(t.TempDir(), "missing-ffmpeg") },
		"ffmpeg directory": func(s *Settings) { s.FFmpegPath = t.TempDir() },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := DefaultSettings()
			mutate(&s)
			if err := s.Validate(); err == nil {
				t.Fatal("invalid setting accepted")
			}
		})
	}
	valid := DefaultSettings()
	valid.UpdateSource, valid.UpdateMirrorURL = "mirror", "https://example.com/releases"
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidSettingsSaveLeavesExistingValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := saveSettings(path, DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	invalid := DefaultSettings()
	invalid.MaxConcurrentJobs = -1
	if err := saveSettings(path, invalid); err == nil {
		t.Fatal("invalid settings saved")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatal("failed save changed settings")
	}
}

func TestProfilesRoundTripWithEncoderOptions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "profiles.json")
	want := []Profile{{ID: "web", Name: "Web video", Container: "mp4", VideoEncoder: "libx264", AudioEncoder: "aac", VideoOptions: map[string]string{"crf": "22", "preset": "medium"}, AudioOptions: map[string]string{"b:a": "128k"}}}
	if err := saveProfiles(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadProfiles(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var jsonProfiles []map[string]any
	if err := json.Unmarshal(data, &jsonProfiles); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "name", "container", "video_encoder", "audio_encoder", "video_options", "audio_options"} {
		if _, ok := jsonProfiles[0][key]; !ok {
			t.Fatalf("missing JSON key %s", key)
		}
	}
}

func TestMalformedProfilesPreservedOnLoadAndSave(t *testing.T) {
	for _, raw := range []string{`[`, `null`, `{}`, `[{},{}]`, `[] []`} {
		path := filepath.Join(t.TempDir(), "profiles.json")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadProfiles(path); err == nil || !strings.Contains(err.Error(), path) {
			t.Fatalf("expected actionable error, got %v", err)
		}
		if err := saveProfiles(path, []Profile{}); err == nil {
			t.Fatal("save overwrote malformed profiles")
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != raw {
			t.Fatalf("malformed profiles changed: %q %v", got, err)
		}
	}
}

func TestProfilesRejectDuplicateIDsAndInvalidOptions(t *testing.T) {
	profile := Profile{ID: "one", Name: "One", Container: "mp4"}
	path := filepath.Join(t.TempDir(), "profiles.json")
	if err := saveProfiles(path, []Profile{profile, profile}); err == nil {
		t.Fatal("duplicate IDs saved")
	}
	profile.VideoOptions = map[string]string{"": "value"}
	if err := profile.Validate(); err == nil {
		t.Fatal("blank option accepted")
	}
	profile.VideoOptions = map[string]string{"preset": "medium\x00extra"}
	if err := profile.Validate(); err == nil {
		t.Fatal("NUL option accepted")
	}
}

func TestNilProfilesPersistAsEmptyArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	if err := saveProfiles(path, nil); err != nil {
		t.Fatal(err)
	}
	got, err := loadProfiles(path)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}
