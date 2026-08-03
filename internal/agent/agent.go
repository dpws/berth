// Package agent reports what the coding agent inside a session is doing:
// whether it is working, waiting on you, or idle, and what it was last asked
// to do.
//
// Both agents keep this on disk already. Claude Code writes a status file per
// process under ~/.claude/sessions, keyed by pid - the same pid tmux reports
// for the pane - and records every prompt in its transcript. Codex writes
// task_started and task_complete events into its session rollout. Neither is
// polled over the network and neither needs credentials.
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dpws/berth/internal/tmux"
)

// Status is what an agent is doing right now.
type Status string

const (
	// Unknown means berth could not find the agent's own record of itself.
	Unknown Status = ""
	// Busy is working on something.
	Busy Status = "busy"
	// Waiting is blocked on you - a permission prompt or a question. This is
	// the one worth noticing across a list of sessions.
	Waiting Status = "waiting"
	// Idle is at the prompt with nothing to do.
	Idle Status = "idle"
	// Shell is running a command.
	Shell Status = "shell"
)

// NeedsInput reports whether the session is blocked until you answer it.
func (s Status) NeedsInput() bool { return s == Waiting }

// Active reports whether the agent is doing work of its own.
func (s Status) Active() bool { return s == Busy || s == Shell }

// Info is what berth knows about one session's agent.
type Info struct {
	Status Status
	// Task is the last thing the agent was asked to do. Agents also record a
	// title for the conversation as a whole, but that is written once from the
	// opening request and goes stale the moment the session moves on to
	// something else, so berth tracks the latest prompt instead.
	Task string
	// Detail is what the agent says it is waiting for, when it says.
	Detail string
	// Updated is when the agent last wrote any of this down.
	Updated time.Time
}

// Empty reports whether there is nothing to show.
func (i Info) Empty() bool { return i.Status == Unknown && i.Task == "" }

// Watcher reads both agents' logs, keeping its place between refreshes.
type Watcher struct {
	mu     sync.Mutex
	claude *claudeWatcher
	codex  *codexWatcher
}

// NewWatcher builds a watcher rooted at the current user's home directory.
func NewWatcher() *Watcher {
	return &Watcher{
		claude: newClaudeWatcher(claudeDir()),
		codex:  newCodexWatcher(codexSessionsDir()),
	}
}

// Refresh returns what each of the given sessions is doing, keyed by session
// name. Sessions berth can say nothing about are left out.
func (w *Watcher) Refresh(sessions []tmux.Session) map[string]Info {
	w.mu.Lock()
	defer w.mu.Unlock()

	out := make(map[string]Info, len(sessions))
	var claude, codex []tmux.Session
	for _, s := range sessions {
		switch s.DetectedKind() {
		case tmux.KindClaude:
			claude = append(claude, s)
		case tmux.KindCodex:
			codex = append(codex, s)
		}
	}

	if len(claude) > 0 {
		w.claude.refresh(claude, out)
	}
	if len(codex) > 0 {
		w.codex.refresh(codex, out)
	}
	return out
}

// AnyWaiting reports whether any session is blocked on you, which is worth
// surfacing somewhere you can see without looking at berth.
func AnyWaiting(infos map[string]Info) int {
	n := 0
	for _, info := range infos {
		if info.Status.NeedsInput() {
			n++
		}
	}
	return n
}

// cleanTask tidies a prompt for display on one line. Prompts are whatever you
// typed, so they arrive with newlines, runs of spaces, and occasionally
// control characters.
func cleanTask(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if r == '\n' || r == '\t' || r == '\r' || r == ' ' {
			space = b.Len() > 0
			continue
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func claudeDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".claude")
}

func codexSessionsDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".codex", "sessions")
}
