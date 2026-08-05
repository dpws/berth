package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dpws/berth/internal/jsonl"
	"github.com/dpws/berth/internal/tmux"
)

// claudeStatus is the status file Claude Code keeps at
// ~/.claude/sessions/<pid>.json for each running process.
type claudeStatus struct {
	PID        int    `json:"pid"`
	SessionID  string `json:"sessionId"`
	CWD        string `json:"cwd"`
	Status     string `json:"status"`
	WaitingFor string `json:"waitingFor"`
	UpdatedAt  int64  `json:"updatedAt"`
	StatusAt   int64  `json:"statusUpdatedAt"`
}

// claudeWatcher maps tmux sessions to Claude Code processes and their work.
type claudeWatcher struct {
	root string
	// tasks caches the last prompt seen in each transcript, along with how far
	// into the file that reading got.
	tasks map[string]*transcript
	// alive reports whether a process is still running. It is a field so tests
	// can describe processes they have not started.
	alive func(pid int) bool
}

type transcript struct {
	tail jsonl.Tailer
	task string
	seen time.Time
}

func newClaudeWatcher(root string) *claudeWatcher {
	return &claudeWatcher{
		root:  root,
		tasks: make(map[string]*transcript),
		alive: processAlive,
	}
}

// processAlive reports whether a pid is still running. Signal 0 is the ordinary
// way to ask: it delivers nothing and only reports whether it could have.
// "Not permitted" is a yes - the process is there, it just is not ours.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM)
}

func (w *claudeWatcher) refresh(sessions []tmux.Session, out map[string]Info) {
	if w.root == "" {
		return
	}
	byPID, byDir := w.readStatuses()
	if len(byPID) == 0 && len(byDir) == 0 {
		return
	}

	live := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		st, ok := byPID[s.PanePID]
		if !ok {
			// Claude started from a shell inside the pane is not the pane's own
			// process, so fall back to the working directory.
			st, ok = byDir[filepath.Clean(s.Dir)]
		}
		if !ok {
			continue
		}

		// Since comes from the status timestamp alone: it is the one that stays
		// put while the agent keeps doing the same thing, which is what makes
		// it an age rather than a heartbeat.
		since := msTime(st.StatusAt)
		if since.IsZero() {
			since = msTime(st.UpdatedAt)
		}
		info := Info{
			Status:  claudeStatusOf(st.Status),
			Detail:  cleanTask(st.WaitingFor),
			Updated: msTime(maxInt64(st.StatusAt, st.UpdatedAt)),
			Since:   since,
		}
		if path := w.transcriptPath(st.SessionID); path != "" {
			live[path] = true
			info.Task = w.task(path)
		}
		out[s.Name] = info
	}

	// Stop following transcripts whose sessions have gone.
	for path := range w.tasks {
		if !live[path] {
			delete(w.tasks, path)
		}
	}
}

// readStatuses loads every status file, indexed by pid and by directory. A
// stale file is skipped so a killed session does not read as busy forever.
func (w *claudeWatcher) readStatuses() (map[int]claudeStatus, map[string]claudeStatus) {
	dir := filepath.Join(w.root, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}

	byPID := make(map[int]claudeStatus, len(entries))
	byDir := make(map[string]claudeStatus, len(entries))

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var st claudeStatus
		if err := json.Unmarshal(data, &st); err != nil {
			continue
		}
		if st.PID == 0 {
			// The file is named for the pid, so recover it if the body lacks one.
			st.PID, _ = strconv.Atoi(strings.TrimSuffix(e.Name(), ".json"))
		}
		// Whether the process is still there is the whole test. Claude Code
		// writes this file when its status changes and at no other time - no
		// heartbeat, so "busy" an hour old is an agent an hour into a turn,
		// not a dead one. A clock cannot tell those apart; a pid can, and a
		// file whose process has gone is a leftover whatever it says.
		if !w.alive(st.PID) {
			continue
		}
		byPID[st.PID] = st
		if st.CWD != "" {
			// Several sessions can share a directory; the newest wins.
			if prev, ok := byDir[filepath.Clean(st.CWD)]; !ok || st.UpdatedAt > prev.UpdatedAt {
				byDir[filepath.Clean(st.CWD)] = st
			}
		}
	}
	return byPID, byDir
}

// transcriptPath finds the conversation log for a session id. Transcripts live
// under a directory named for the project, which berth does not know, so the
// lookup is a glob rather than a path it can build.
func (w *claudeWatcher) transcriptPath(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	matches, err := filepath.Glob(filepath.Join(w.root, "projects", "*", sessionID+".jsonl"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	return matches[0]
}

// task returns the last prompt written to a transcript, reading only what has
// been appended since the last look.
func (w *claudeWatcher) task(path string) string {
	state := w.tasks[path]
	if state == nil {
		state = &transcript{}
		w.tasks[path] = state
	}
	_ = state.tail.Read(path, jsonl.DefaultMaxLine, func(line []byte) {
		var rec struct {
			Type       string `json:"type"`
			LastPrompt string `json:"lastPrompt"`
			AITitle    string `json:"aiTitle"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			return
		}
		switch rec.Type {
		case "last-prompt":
			if p := cleanTask(rec.LastPrompt); p != "" {
				state.task = p
			}
		case "ai-title":
			// Only a fallback: the title is written once, from the opening
			// request, so a prompt always beats it once one turns up.
			if state.task == "" {
				state.task = cleanTask(rec.AITitle)
			}
		}
	})
	return state.task
}

// claudeStatusOf maps Claude Code's own vocabulary onto berth's.
func claudeStatusOf(s string) Status {
	switch s {
	case "busy":
		return Busy
	case "waiting":
		return Waiting
	case "idle":
		return Idle
	case "shell":
		return Shell
	default:
		return Unknown
	}
}

func msTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
