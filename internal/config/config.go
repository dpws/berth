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
	// berth creates. Sessions started elsewhere are never touched, and
	// server-wide options belong in ~/.tmux.conf.
	//
	// "mouse on" is there by default because berth forwards the wheel into
	// the pane, and without it nothing downstream acts on it: an agent that
	// does not ask for mouse reporting - Codex does not - simply cannot be
	// scrolled. With it, tmux scrolls its own scrollback for those, and hands
	// the wheel to agents that do want it.
	SessionOptions []string `json:"session_options"`
	// ImageDropDir is scanned for images when the clipboard has none. Over
	// SSH this is the only workable source, so it is on by default.
	ImageDropDir string `json:"image_drop_dir"`
	// PasteImageKey inserts the path of an image into the focused session.
	//
	// ctrl+v by default, which is the key everyone reaches for. berth takes it
	// from the session to do so: a shell loses quoted-insert and vim loses
	// visual block, both of which are ctrl+v there. Set it to ctrl+y, or to
	// anything else, to give the key back.
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
	// HideAgentAge drops the how-long-it-has-been-doing-that time from the
	// right of that line.
	HideAgentAge bool `json:"hide_agent_age"`
	// ShowHost adds a block under the rate limits with the load, memory and
	// disk of the machine berth is running on. It is off by default: berth is
	// a session list first, and most of the time the machine is not the thing
	// in question.
	ShowHost bool `json:"show_host"`
	// Notify is how berth gets your attention when a session reaches one of
	// the moments in NotifyOn: "bell" rings the terminal, "desktop" asks it to
	// raise a real notification, "both" does the two, "off" neither.
	Notify string `json:"notify"`
	// NotifyOn is which moments are worth it: "waiting" for a session blocked
	// on you, "idle" for one that has just finished. Empty means none, which
	// is another way of saying off.
	NotifyOn []string `json:"notify_on"`
	// CheckUpdates asks GitHub once a day whether there is a newer release and
	// says so in the header. It is the only request berth makes on its own;
	// nothing is sent but the ask, and nothing is ever installed without
	// "berth update".
	CheckUpdates bool `json:"check_updates"`
	// HideWindowTitle stops berth naming the selected session in the
	// terminal's title bar, for terminals or window managers where the title
	// is set from elsewhere.
	HideWindowTitle bool `json:"hide_window_title"`
	// UsageRefreshSeconds is how often the limits are re-read. Reading them
	// touches the agents' log files, so it is much slower than the tmux poll.
	UsageRefreshSeconds int `json:"usage_refresh_seconds"`
	// HideGitBar turns off the half of the bar above the session that says
	// which branch its directory is on and what has changed there. The bar's
	// other half, over the list, carries berth's own name and build and stays
	// either way, so hiding this gives no room back.
	HideGitBar bool `json:"hide_git_bar"`
	// GitRefreshSeconds is how often that bar is re-read. Reading it walks the
	// worktree, so like the rate limits it runs far slower than the tmux poll.
	GitRefreshSeconds int `json:"git_refresh_seconds"`
	// HideDoctor stops berth checking the software it sits on when it starts,
	// and offering to put right whatever is not set the way it needs.
	HideDoctor bool `json:"hide_doctor"`
	// DoctorSkipped names the checks to stop asking about. Skipping one is a
	// decision about your own setup, so it is remembered rather than asked
	// again every start.
	DoctorSkipped []string `json:"doctor_skipped"`
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
		PasteImageKey:      "ctrl+v",
		QuitKey:            "ctrl+x",
		ClipAgentURL:       "http://127.0.0.1:8377",
		SessionOptions:     []string{"mouse on"},

		CheckUpdates:        true,
		UsageRefreshSeconds: 30,
		GitRefreshSeconds:   5,

		// Off, like everything that interrupts you. Turned on, the default is
		// the moment that is actually blocking - a session that has finished
		// is not waiting for anything.
		Notify:   NotifyOff,
		NotifyOn: []string{NotifyWaiting},
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
//
// The file is written beside the real one and renamed over it. Writing in place
// truncates first, so a crash or a full disk mid-write would leave a config
// that no longer parses - and Load treats that as a config to start over from,
// losing every setting the user ever changed. It is written 0600 because
// clip_agent_token is a shared secret; the settings screen masks it on the way
// in, which is worth little if the file is readable by everyone on the box.
func (c Config) Save() error {
	path := Path()
	if path == "" {
		return errors.New("no user config directory available")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// withDefaults fills in anything the config file left out.
//
// QuitKey is deliberately absent: Load starts from Default and unmarshals over
// it, so a file that omits the key keeps the default, while one that sets it
// to "" turns the key off. Filling it in here would make it undisablable.
// The ways berth can get your attention, and the moments worth doing it at.
const (
	NotifyOff     = "off"
	NotifyBell    = "bell"
	NotifyDesktop = "desktop"
	NotifyBoth    = "both"

	NotifyWaiting = "waiting"
	NotifyIdle    = "idle"
)

// Rings reports whether the terminal should be rung.
func (c Config) Rings() bool { return c.Notify == NotifyBell || c.Notify == NotifyBoth }

// Raises reports whether the terminal should be asked for a desktop
// notification.
func (c Config) Raises() bool { return c.Notify == NotifyDesktop || c.Notify == NotifyBoth }

// NotifiesOn reports whether a moment is one berth was asked about.
func (c Config) NotifiesOn(moment string) bool {
	if c.Notify == NotifyOff {
		return false
	}
	for _, m := range c.NotifyOn {
		if m == moment {
			return true
		}
	}
	return false
}

func (c Config) withDefaults() Config {
	d := Default()
	// A way of being told that berth does not know is no way at all, and
	// silently doing nothing is better than a config typo ringing all day.
	switch c.Notify {
	case NotifyOff, NotifyBell, NotifyDesktop, NotifyBoth:
	default:
		c.Notify = NotifyOff
	}
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
	if c.GitRefreshSeconds <= 0 {
		c.GitRefreshSeconds = d.GitRefreshSeconds
	}
	return c
}
