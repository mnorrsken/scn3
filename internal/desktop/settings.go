package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Settings are the saved preferences.
type Settings struct {
	Theme  string `json:"theme"` // system, light or dark
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// DefaultSettings is used when nothing has been saved yet.
func DefaultSettings() Settings {
	return Settings{Theme: "system", Width: 1100, Height: 780}
}

func settingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding config directory: %w", err)
	}
	return filepath.Join(dir, "scn3", "settings.json"), nil
}

// LoadSettings reads the settings file. A missing or broken file gives the
// defaults: losing preferences must not stop the app from starting.
func LoadSettings() Settings {
	s := DefaultSettings()
	path, err := settingsPath()
	if err != nil {
		return s
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) || err != nil {
		return s
	}
	if json.Unmarshal(data, &s) != nil {
		return DefaultSettings()
	}
	return s
}

// SaveSettings writes the settings file.
func SaveSettings(s Settings) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("saving settings: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("saving settings: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("saving settings: %w", err)
	}
	return nil
}

// Settings returns the saved preferences.
func (a *App) Settings() Settings { return LoadSettings() }

// SetTheme saves the theme choice.
func (a *App) SetTheme(theme string) error {
	s := LoadSettings()
	s.Theme = theme
	return SaveSettings(s)
}

// Platform returns the OS name, for OS-specific help text.
func (a *App) Platform() string { return runtime.GOOS }
