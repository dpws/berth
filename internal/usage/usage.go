// Package usage reports how much of an agent's rate limit has been spent.
//
// Every number here comes from the agent itself. Codex writes the server's
// answer into its session logs. Claude Code hands its own 5-hour and weekly
// windows to whatever command is configured as its status line, which is what
// "berth statusline" is for. Neither route touches the network or needs
// credentials, and berth reports nothing it was not told - if the status line
// hook is not wired up, the block says so rather than guessing.
package usage

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dpws/berth/internal/tmux"
)

// Window is one rate limit period.
type Window struct {
	// Label names the period, for example "5h" or "week".
	Label string
	// Percent is how much of the period is spent, from 0 to 100.
	Percent float64
	// ResetsAt is when the period rolls over. Zero when unknown.
	ResetsAt time.Time
}

// Limits is what berth knows about one agent's usage right now.
type Limits struct {
	Kind    string // tmux.KindClaude or tmux.KindCodex
	Plan    string // plan name, when the source names one
	Windows []Window
	// Sampled is when the underlying numbers were produced, which for Codex is
	// the timestamp of the log entry rather than when berth read it.
	Sampled time.Time
	// Err records why a source came back empty. It is shown in place of the
	// numbers rather than raised as an error, since a missing agent is normal.
	Err error
}

// Empty reports whether there is nothing worth drawing.
func (l Limits) Empty() bool { return len(l.Windows) == 0 }

// Tracker collects usage for every agent berth can report on.
type Tracker struct {
	mu sync.Mutex
}

// NewTracker builds a tracker rooted at the current user's home directory.
func NewTracker() *Tracker { return &Tracker{} }

// Refresh reads every source and returns the result keyed by session kind.
// Sources that fail contribute a Limits carrying the error rather than
// dropping out, so the UI can say why a row is blank.
func (t *Tracker) Refresh() map[string]Limits {
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make(map[string]Limits, 2)

	claude, err := readCachedClaude()
	claude.Kind = tmux.KindClaude
	claude.Err = err
	out[tmux.KindClaude] = claude

	codex, err := readCodex()
	codex.Kind = tmux.KindCodex
	codex.Err = err
	out[tmux.KindCodex] = codex

	return out
}

// windowLabel turns a window length in minutes into something that fits in a
// narrow sidebar.
func windowLabel(minutes int) string {
	switch {
	case minutes <= 0:
		return "?"
	case minutes == 10080:
		return "week"
	case minutes%1440 == 0:
		return fmt.Sprintf("%dd", minutes/1440)
	case minutes%60 == 0:
		return fmt.Sprintf("%dh", minutes/60)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func codexSessionsDir() string {
	home := homeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".codex", "sessions")
}
