// Package config loads user configuration for berth.
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Config controls how berth creates and displays sessions.
type Config struct {
	// ClaudeCommand is run inside sessions of kind "claude".
	ClaudeCommand string `json:"claude_command"`
	// CodexCommand is run inside sessions of kind "codex".
	CodexCommand string `json:"codex_command"`
	// ClaudeContinueArgs and ClaudeResumeArgs are appended to ClaudeCommand
	// when a session is started to carry on an earlier conversation: the first
	// picks up the most recent one in the directory, the second asks which.
	ClaudeContinueArgs string `json:"claude_continue_args"`
	ClaudeResumeArgs   string `json:"claude_resume_args"`
	// CodexContinueArgs and CodexResumeArgs do the same for Codex.
	CodexContinueArgs string `json:"codex_continue_args"`
	CodexResumeArgs   string `json:"codex_resume_args"`
	// ShellCommand is run inside sessions of kind "shell". Empty means $SHELL.
	ShellCommand string `json:"shell_command"`
	// DefaultDir is the working directory suggested for new sessions.
	DefaultDir string `json:"default_dir"`
	// SidebarWidth is the preferred width of the session list, in columns.
	SidebarWidth int `json:"sidebar_width"`
	// RefreshMillis is how often the session list is polled from tmux.
	RefreshMillis int `json:"refresh_millis"`
	// HideStatusBar turns tmux's own status bar off in sessions we create.
	HideStatusBar bool `json:"hide_status_bar"`
	// Mouse forwards clicks and the scroll wheel to the focused session, and
	// lets you click a row in the session list. Turning it off gives the
	// outer terminal its native text selection back.
	Mouse bool `json:"mouse"`
	// SessionOptions are tmux "set-option" arguments applied to sessions
	// berth creates, for example ["mouse on"]. Sessions started elsewhere
	// are never touched, and server-wide options belong in ~/.tmux.conf.
	SessionOptions []string `json:"session_options"`
	// ImageDropDir is scanned for images when the clipboard has none. Over
	// SSH this is the only workable source, so it is on by default.
	ImageDropDir string `json:"image_drop_dir"`
	// PasteImageKey inserts the path of an image into the focused session.
	PasteImageKey string `json:"paste_image_key"`
	// QuitKey quits berth from anywhere, including while a session has the
	// keyboard - the only other way out from there is ctrl+o then q. Set it to
	// "" to give the key back to your sessions; emacs in particular wants
	// ctrl+x for itself.
	QuitKey string `json:"quit_key"`
	// ClipAgentURL is a berth-clipd serving the clipboard of the machine
	// you are sitting at. Empty disables the remote clipboard entirely.
	ClipAgentURL string `json:"clip_agent_url"`
	// ClipAgentToken is sent to the agent when it was started with -token.
	ClipAgentToken string `json:"clip_agent_token"`
	// HideUsage turns off the rate limit block under the session list.
	HideUsage bool `json:"hide_usage"`
	// HideAgentStatus turns off the busy / waiting / idle indicator in the
	// session list, and with it the marker berth puts in the terminal title
	// when a session needs you.
	HideAgentStatus bool `json:"hide_agent_status"`
	// HideTask drops the second line under each session that says what its
	// agent was last asked to do.
	HideTask bool `json:"hide_task"`
	// HideWindowTitle stops berth naming the selected session in the
	// terminal's title bar, for terminals or window managers where the title
	// is set from elsewhere.
	HideWindowTitle bool `json:"hide_window_title"`
	// UsageRefreshSeconds is how often the limits are re-read. Reading them
	// touches the agents' log files, so it is much slower than the tmux poll.
	UsageRefreshSeconds int `json:"usage_refresh_seconds"`
}

// Default returns the configuration used when no config file exists.
func Default() Config {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	return Config{
		ClaudeCommand: "claude",
		CodexCommand:  "codex",

		ClaudeContinueArgs: "--continue",
		ClaudeResumeArgs:   "--resume",
		CodexContinueArgs:  "resume --last",
		CodexResumeArgs:    "resume",
		ShellCommand:       shell,
		DefaultDir:         home,
		SidebarWidth:       28,
		RefreshMillis:      2000,
		HideStatusBar:      true,
		Mouse:              true,
		ImageDropDir:       filepath.Join(home, "berth-drop"),
		PasteImageKey:      "ctrl+y",
		QuitKey:            "ctrl+x",
		ClipAgentURL:       "http://127.0.0.1:8377",

		UsageRefreshSeconds: 30,
	}
}

// ImageCacheDir is where clipboard images are written before being handed to
// a session.
func ImageCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(dir, "berth", "images")
}

// Path returns the location berth reads its config from.
func Path() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "berth", "config.json")
}

// Load reads the config file, falling back to defaults for anything missing.
// A missing file is not an error.
func Load() (Config, error) {
	cfg := Default()
	path := Path()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), err
	}
	return cfg.withDefaults(), nil
}

// Save writes cfg to the config path, creating parent directories.
func (c Config) Save() error {
	path := Path()
	if path == "" {
		return errors.New("no user config directory available")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// withDefaults fills in anything the config file left out.
//
// QuitKey is deliberately absent: Load starts from Default and unmarshals over
// it, so a file that omits the key keeps the default, while one that sets it
// to "" turns the key off. Filling it in here would make it undisablable.
func (c Config) withDefaults() Config {
	d := Default()
	if c.ClaudeCommand == "" {
		c.ClaudeCommand = d.ClaudeCommand
	}
	if c.CodexCommand == "" {
		c.CodexCommand = d.CodexCommand
	}
	if c.PasteImageKey == "" {
		c.PasteImageKey = d.PasteImageKey
	}
	if c.ShellCommand == "" {
		c.ShellCommand = d.ShellCommand
	}
	if c.DefaultDir == "" {
		c.DefaultDir = d.DefaultDir
	}
	if c.SidebarWidth <= 0 {
		c.SidebarWidth = d.SidebarWidth
	}
	if c.RefreshMillis <= 0 {
		c.RefreshMillis = d.RefreshMillis
	}
	if c.UsageRefreshSeconds <= 0 {
		c.UsageRefreshSeconds = d.UsageRefreshSeconds
	}
	return c
}
