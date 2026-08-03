package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// A preset is the shape of a session you start often: what runs in it, where,
// and how it begins. They live in their own file rather than in the config,
// because the config is a flat list of settings and this is a list of things.
type Preset struct {
	// Label is what the preset is called in the list.
	Label string `json:"label"`
	// Session is the name offered for the session itself, which is not the
	// same thing: "api" started from the preset "api on claude".
	Session string `json:"session,omitempty"`
	Kind    string `json:"kind"`
	Dir     string `json:"dir,omitempty"`
	// Start is new, continue or resume; empty means new.
	Start string `json:"start,omitempty"`
}

// PresetsPath returns the file presets are kept in.
func PresetsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "berth", "presets.json")
}

// LoadPresets reads the saved presets. A missing file is not an error: it just
// means none have been saved.
func LoadPresets() ([]Preset, error) {
	path := PresetsPath()
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Preset
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SavePresets writes the presets, creating parent directories.
func SavePresets(presets []Preset) error {
	path := PresetsPath()
	if path == "" {
		return errors.New("no user config directory available")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(presets, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// AddPreset returns the list with p added, replacing any preset of the same
// label rather than letting two of them accumulate.
func AddPreset(presets []Preset, p Preset) []Preset {
	p.Label = strings.TrimSpace(p.Label)
	for i, existing := range presets {
		if strings.EqualFold(strings.TrimSpace(existing.Label), p.Label) {
			presets[i] = p
			return presets
		}
	}
	return append(presets, p)
}

// RemovePreset returns the list with the preset at i taken out.
func RemovePreset(presets []Preset, i int) []Preset {
	if i < 0 || i >= len(presets) {
		return presets
	}
	return append(presets[:i:i], presets[i+1:]...)
}
