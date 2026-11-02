package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"ConvertGo/internal/platform"
)

type Settings struct {
	Locale            string `json:"locale"`
	Theme             string `json:"theme"`
	AccentHex         string `json:"accent_hex"`
	FontFamily        string `json:"font_family"`
	MaxConcurrentJobs int    `json:"max_concurrent_jobs"`
	PreventSleep      bool   `json:"prevent_sleep"`
	UpdateSource      string `json:"update_source"`
	UpdateMirrorURL   string `json:"update_mirror_url"`
	AutoCheckUpdates  bool   `json:"auto_check_updates"`
	FFmpegPath        string `json:"ffmpeg_path"`
}

type Profile struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Container    string            `json:"container"`
	VideoEncoder string            `json:"video_encoder"`
	AudioEncoder string            `json:"audio_encoder"`
	VideoOptions map[string]string `json:"video_options"`
	AudioOptions map[string]string `json:"audio_options"`
}

var persistenceMu sync.Mutex

func DefaultSettings() Settings {
	return defaultsFor(platform.SystemLocale(), platform.DefaultFontFamily())
}

func defaultsFor(locale, font string) Settings {
	locale = strings.ReplaceAll(strings.SplitN(strings.SplitN(locale, ".", 2)[0], "@", 2)[0], "_", "-")
	switch strings.ToLower(locale) {
	case "en-us":
		locale = "en-US"
	default:
		locale = "zh-CN"
	}
	return Settings{Locale: locale, Theme: "system", FontFamily: font, MaxConcurrentJobs: 1, UpdateSource: "github"}
}

func (s Settings) Validate() error {
	if s.Locale != "zh-CN" && s.Locale != "en-US" {
		return errors.New("locale must be zh-CN or en-US")
	}
	if s.Theme != "system" && s.Theme != "light" && s.Theme != "dark" {
		return errors.New("theme must be system, light, or dark")
	}
	if s.AccentHex != "" {
		if len(s.AccentHex) != 7 || s.AccentHex[0] != '#' {
			return errors.New("accent_hex must be empty or #RRGGBB")
		}
		for _, c := range s.AccentHex[1:] {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return errors.New("accent_hex must be empty or #RRGGBB")
			}
		}
	}
	if strings.TrimSpace(s.FontFamily) == "" || strings.ContainsRune(s.FontFamily, 0) {
		return errors.New("font_family must contain a font family name")
	}
	if s.MaxConcurrentJobs < 1 {
		return errors.New("max_concurrent_jobs must be at least 1")
	}
	if s.UpdateSource != "github" && s.UpdateSource != "mirror" {
		return errors.New("update_source must be github or mirror")
	}
	if s.UpdateSource == "mirror" && s.UpdateMirrorURL == "" {
		return errors.New("update_mirror_url is required for mirror updates")
	}
	if s.UpdateMirrorURL != "" {
		mirror, err := url.Parse(s.UpdateMirrorURL)
		if err != nil || mirror.Scheme != "https" || mirror.Hostname() == "" || mirror.User != nil || mirror.RawQuery != "" || mirror.ForceQuery || mirror.Fragment != "" || mirror.Opaque != "" {
			return errors.New("update_mirror_url must be an absolute HTTPS URL without credentials, query, or fragment")
		}
	}
	if s.FFmpegPath != "" {
		if err := validateExecutable(s.FFmpegPath); err != nil {
			return fmt.Errorf("ffmpeg_path %q is unavailable; choose an executable or clear the override: %w", s.FFmpegPath, err)
		}
	}
	return nil
}

func (p Profile) Validate() error {
	for _, field := range []struct{ name, value string }{{"id", p.ID}, {"name", p.Name}, {"container", p.Container}} {
		if strings.TrimSpace(field.value) == "" || strings.ContainsRune(field.value, 0) {
			return fmt.Errorf("profile %s must be nonempty and contain no NUL bytes", field.name)
		}
	}
	if strings.ContainsRune(p.VideoEncoder, 0) || strings.ContainsRune(p.AudioEncoder, 0) {
		return errors.New("profile encoder names must not contain NUL bytes")
	}
	for _, options := range []map[string]string{p.VideoOptions, p.AudioOptions} {
		for key, value := range options {
			if strings.TrimSpace(key) == "" || strings.ContainsRune(key, 0) || strings.ContainsRune(value, 0) {
				return errors.New("profile options require nonempty keys and no NUL bytes")
			}
		}
	}
	return nil
}

func LoadSettings() (Settings, error) {
	persistenceMu.Lock()
	defer persistenceMu.Unlock()
	path, err := configPath("settings.json")
	if err != nil {
		return DefaultSettings(), err
	}
	return loadSettings(path)
}

func SaveSettings(settings Settings) error {
	persistenceMu.Lock()
	defer persistenceMu.Unlock()
	path, err := configPath("settings.json")
	if err != nil {
		return err
	}
	return saveSettings(path, settings)
}

func ListProfiles() ([]Profile, error) {
	persistenceMu.Lock()
	defer persistenceMu.Unlock()
	path, err := configPath("profiles.json")
	if err != nil {
		return nil, err
	}
	return loadProfiles(path)
}

func SaveProfiles(profiles []Profile) error {
	persistenceMu.Lock()
	defer persistenceMu.Unlock()
	path, err := configPath("profiles.json")
	if err != nil {
		return err
	}
	return saveProfiles(path, profiles)
}

func configPath(name string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate ConvertGo configuration directory: %w", err)
	}
	return filepath.Join(dir, "ConvertGo", name), nil
}

func loadSettings(path string) (Settings, error) {
	settings, err := readSettings(path)
	if err != nil {
		return settings, err
	}
	if err := settings.Validate(); err != nil {
		return settings, fmt.Errorf("settings %q are invalid and unchanged; correct the values before encoding: %w", path, err)
	}
	return settings, nil
}

func readSettings(path string) (Settings, error) {
	defaults := DefaultSettings()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return defaults, nil
	}
	if err != nil {
		return defaults, fmt.Errorf("read settings %q: %w", path, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return defaults, invalidFile(path, err)
	}
	if fields == nil {
		return defaults, invalidFile(path, errors.New("expected a JSON settings object, not null"))
	}
	for name, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return defaults, invalidFile(path, fmt.Errorf("%s must not be null", name))
		}
	}
	settings := defaults
	if err := decodeJSON(data, &settings); err != nil {
		return defaults, invalidFile(path, err)
	}
	return settings, nil
}

func saveSettings(path string, settings Settings) error {
	if err := settings.Validate(); err != nil {
		return fmt.Errorf("settings not saved: %w", err)
	}
	if _, err := readSettings(path); err != nil {
		return err
	}
	return atomicJSON(path, settings)
}

func loadProfiles(path string) ([]Profile, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Profile{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read profiles %q: %w", path, err)
	}
	var profiles []Profile
	if err := decodeJSON(data, &profiles); err != nil {
		return nil, invalidFile(path, err)
	}
	if profiles == nil {
		return nil, invalidFile(path, errors.New("expected a JSON profile array, not null"))
	}
	if err := validateProfiles(profiles); err != nil {
		return nil, invalidFile(path, err)
	}
	return profiles, nil
}

func saveProfiles(path string, profiles []Profile) error {
	if profiles == nil {
		profiles = []Profile{}
	}
	if err := validateProfiles(profiles); err != nil {
		return fmt.Errorf("profiles not saved: %w", err)
	}
	if _, err := loadProfiles(path); err != nil {
		return err
	}
	return atomicJSON(path, profiles)
}

func validateProfiles(profiles []Profile) error {
	ids := make(map[string]bool, len(profiles))
	for index, profile := range profiles {
		if err := profile.Validate(); err != nil {
			return fmt.Errorf("profile %d: %w", index+1, err)
		}
		if ids[profile.ID] {
			return fmt.Errorf("profile ID %q is duplicated", profile.ID)
		}
		ids[profile.ID] = true
	}
	return nil
}

func decodeJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("expected exactly one JSON value")
	}
	return nil
}

func invalidFile(path string, err error) error {
	return fmt.Errorf("configuration file %q is invalid and was left unchanged; repair it or move it aside before saving: %w", path, err)
}

func atomicJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("serialize %q: %w", path, err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create configuration directory %q: %w", dir, err)
	}
	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("create temporary configuration for %q: %w", path, err)
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)
	defer file.Close()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write temporary configuration for %q: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync temporary configuration for %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary configuration for %q: %w", path, err)
	}
	if err := replaceFile(tempPath, path); err != nil {
		return fmt.Errorf("replace configuration %q atomically: %w", path, err)
	}
	return nil
}
