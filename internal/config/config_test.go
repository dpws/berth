package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfig points the config path at a temp directory and puts body there.
func writeConfig(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))

	path := Path()
	if path == "" {
		t.Fatal("no config path available")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// An omitted key keeps its default, but a key set to "" turns the shortcut
// off. Adding QuitKey to withDefaults would quietly break the second half.
func TestQuitKeyCanBeOmittedOrDisabled(t *testing.T) {
	t.Run("omitted", func(t *testing.T) {
		writeConfig(t, `{"sidebar_width": 20}`)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.QuitKey != "ctrl+x" {
			t.Errorf("QuitKey = %q, want the default", cfg.QuitKey)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		writeConfig(t, `{"quit_key": ""}`)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.QuitKey != "" {
			t.Errorf("QuitKey = %q, want it left empty", cfg.QuitKey)
		}
	})

	t.Run("remapped", func(t *testing.T) {
		writeConfig(t, `{"quit_key": "ctrl+q"}`)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.QuitKey != "ctrl+q" {
			t.Errorf("QuitKey = %q, want ctrl+q", cfg.QuitKey)
		}
	})
}

func TestMissingConfigIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.QuitKey != "ctrl+x" || cfg.PasteImageKey != "ctrl+v" {
		t.Errorf("defaults not applied: %+v", cfg)
	}
}

// berth forwards the wheel into the pane whether or not anything downstream
// listens. Codex does not ask for mouse reporting, so without tmux's own mouse
// mode its sessions cannot be scrolled at all.
func TestNewSessionsCanBeScrolled(t *testing.T) {
	var found bool
	for _, opt := range Default().SessionOptions {
		if opt == "mouse on" {
			found = true
		}
	}
	if !found {
		t.Errorf("SessionOptions = %q, want tmux mouse mode among them",
			Default().SessionOptions)
	}
}

// The default is only for a config that does not mention the key. Someone who
// asks for no options gets none.
func TestSessionOptionsCanBeEmptied(t *testing.T) {
	writeConfig(t, `{"session_options": []}`)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.SessionOptions) != 0 {
		t.Errorf("SessionOptions = %q, want none", cfg.SessionOptions)
	}

	writeConfig(t, `{"sidebar_width": 30}`)
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.SessionOptions) == 0 {
		t.Error("a config that does not mention the key lost the default")
	}
}

// The config holds clip_agent_token, a shared secret, and is replaced by a
// rename so a crash mid-write cannot leave a file that no longer parses.
func TestSaveIsPrivateAndReplacesTheFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	cfg := Default()
	cfg.ClipAgentToken = "hunter2"
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config is mode %o, want 600: it holds a shared secret", perm)
	}
	// And nothing is left behind from the write.
	entries, err := os.ReadDir(filepath.Dir(Path()))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d files, want only the config", len(entries))
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ClipAgentToken != "hunter2" {
		t.Errorf("token = %q, want it round-tripped", got.ClipAgentToken)
	}
}
