// Package tmux wraps the tmux CLI with the few operations berth needs.
package tmux

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Session kinds. The kind is stored on the tmux session itself as a user
// option, so it survives berth restarts.
const (
	KindClaude = "claude"
	KindCodex  = "codex"
	KindShell  = "shell"
)

// Kinds are the session kinds berth can create, in the order the new
// session form cycles through them.
var Kinds = []string{KindClaude, KindCodex, KindShell}

// How an agent session starts: fresh, carrying on where it left off, or
// asking which earlier conversation to pick up.
const (
	StartNew      = "new"
	StartContinue = "continue"
	StartResume   = "resume"
)

// Starts are the start modes the new session form cycles through.
var Starts = []string{StartNew, StartContinue, StartResume}

// tmux user options used to tag sessions berth created.
const (
	optManaged = "@berth"
	optKind    = "@berth_kind"
)

// sep delimits fields in -F output. A raw tab is unambiguous: tmux escapes
// control characters that appear inside values (a tab in a name is printed as
// "\011"), so the only literal tab in the output is the one we asked for.
// Non-printable separators such as 0x1f are no good — tmux escapes those too,
// including the ones in our own format string.
const sep = "\t"

var listFormat = strings.Join([]string{
	"#{session_name}",
	"#{session_windows}",
	"#{session_attached}",
	"#{session_created}",
	"#{session_activity}",
	"#{session_path}",
	"#{pane_current_command}",
	"#{pane_pid}",
	"#{" + optManaged + "}",
	"#{" + optKind + "}",
}, sep)

// Session is a tmux session as berth cares about it.
type Session struct {
	Name     string
	Windows  int
	Attached int
	Created  time.Time
	Activity time.Time
	Dir      string
	Command  string // command running in the session's active pane
	PanePID  int    // pid of the process the pane was started with
	Kind     string // KindClaude, KindShell, or "" for foreign sessions
	Managed  bool   // created by berth
}

// IsClaude reports whether the session is running Claude Code.
func (s Session) IsClaude() bool { return s.DetectedKind() == KindClaude }

// IsAgent reports whether the session runs a coding agent rather than a plain
// shell, which is what image pasting is meant for.
func (s Session) IsAgent() bool {
	k := s.DetectedKind()
	return k == KindClaude || k == KindCodex
}

// DetectedKind returns the session's tagged kind, falling back to sniffing the
// running command for sessions berth did not create.
func (s Session) DetectedKind() string {
	if s.Kind != "" {
		return s.Kind
	}
	switch cmd := strings.ToLower(s.Command); {
	case strings.Contains(cmd, "claude"):
		return KindClaude
	case strings.Contains(cmd, "codex"):
		return KindCodex
	default:
		return KindShell
	}
}

// Available reports whether a usable tmux binary is on PATH.
func Available() error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return errors.New("tmux not found on PATH")
	}
	return nil
}

// List returns every session on the tmux server, oldest first. A server that
// is not running yields an empty list rather than an error.
func List() ([]Session, error) {
	out, err := run("list-sessions", "-F", listFormat)
	if err != nil {
		if noServer(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []Session
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, sep)
		if len(f) < 10 {
			continue
		}
		s := Session{
			Name:     f[0],
			Windows:  atoi(f[1]),
			Attached: atoi(f[2]),
			Created:  unix(f[3]),
			Activity: unix(f[4]),
			Dir:      f[5],
			Command:  f[6],
			PanePID:  atoi(f[7]),
			Managed:  f[8] == "1",
			Kind:     f[9],
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

// Exists reports whether a session with the exact given name is present.
func Exists(name string) bool {
	_, err := run("has-session", "-t", target(name))
	return err == nil
}

// NewOptions describes a session to create.
type NewOptions struct {
	Name          string
	Dir           string
	Kind          string
	Command       string // shell command to run; empty means the user's shell
	HideStatusBar bool
	// Options are extra "set-option" arguments applied to the new session,
	// each a single string such as "mouse on".
	Options []string
}

// New creates a detached session and tags it as berth-managed. It returns
// the name actually used, which may be suffixed to avoid a collision.
func New(o NewOptions) (string, error) {
	name := UniqueName(SanitizeName(o.Name))
	if name == "" {
		return "", errors.New("session name is empty")
	}

	args := []string{"new-session", "-d", "-s", name}
	if o.Dir != "" {
		args = append(args, "-c", o.Dir)
	}
	if cmd := strings.TrimSpace(o.Command); cmd != "" {
		args = append(args, cmd)
	}
	if _, err := run(args...); err != nil {
		return "", err
	}

	kind := o.Kind
	if kind == "" {
		kind = KindShell
	}
	// Tagging is best-effort: a session that exists but is untagged still works.
	_, _ = run("set-option", "-t", optionTarget(name), optManaged, "1")
	_, _ = run("set-option", "-t", optionTarget(name), optKind, kind)
	if o.HideStatusBar {
		_, _ = run("set-option", "-t", optionTarget(name), "status", "off")
	}
	for _, opt := range o.Options {
		fields := strings.Fields(opt)
		if len(fields) == 0 {
			continue
		}
		args := append([]string{"set-option", "-t", optionTarget(name)}, fields...)
		if _, err := run(args...); err != nil {
			return name, fmt.Errorf("session created, but %q failed: %w", opt, err)
		}
	}
	return name, nil
}

// Kill destroys a session and everything running in it.
func Kill(name string) error {
	_, err := run("kill-session", "-t", target(name))
	return err
}

// Rename changes a session's name, returning the sanitized name used.
func Rename(old, name string) (string, error) {
	clean := SanitizeName(name)
	if clean == "" {
		return "", errors.New("session name is empty")
	}
	if clean == old {
		return clean, nil
	}
	if Exists(clean) {
		return "", fmt.Errorf("session %q already exists", clean)
	}
	if _, err := run("rename-session", "-t", target(old), clean); err != nil {
		return "", err
	}
	return clean, nil
}

// SanitizeName makes a string usable as a tmux session name. tmux treats "."
// and ":" as target separators, and whitespace makes targets awkward to type.
func SanitizeName(name string) string {
	repl := strings.NewReplacer(".", "-", ":", "-", " ", "-", "\t", "-", "\n", "")
	return strings.Trim(repl.Replace(strings.TrimSpace(name)), "-")
}

// UniqueName appends a numeric suffix until the name is free.
func UniqueName(name string) string {
	if name == "" || !Exists(name) {
		return name
	}
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d", name, i)
		if !Exists(candidate) {
			return candidate
		}
	}
	return name
}

// target builds an exact-match target-session so a name is never treated as a
// prefix or pattern.
func target(name string) string { return "=" + name }

// optionTarget builds a target for set-option, whose -t is a target-pane
// rather than a target-session. The trailing colon selects the session's
// current window, keeping the "=" exact-match semantics.
func optionTarget(name string) string { return "=" + name + ":" }

func run(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), fmt.Errorf("tmux %s: %s", args[0], msg)
	}
	return stdout.String(), nil
}

func noServer(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "no server running") ||
		strings.Contains(s, "no such file or directory") ||
		strings.Contains(s, "error connecting")
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func unix(s string) time.Time {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(n, 0)
}
