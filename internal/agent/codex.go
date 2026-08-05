package agent

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dpws/berth/internal/jsonl"
	"github.com/dpws/berth/internal/tmux"
)

// codexScanFiles bounds how many rollouts are considered. Only sessions open
// right now matter, and those are the most recently written.
const codexScanFiles = 16

// staleAfter is how long a turn with no end recorded is believed.
//
// This is Codex's alone. A rollout is written to all through a turn, so one
// that has gone quiet really has stopped - where Claude Code writes its status
// file on a change and never again, and quiet there says nothing at all. Codex
// records no pid either, so there is nothing to ask instead: the clock is all
// berth has.
const staleAfter = 10 * time.Minute

// codexWatcher follows Codex rollouts, which record the start and end of every
// turn as well as the prompts that drove them.
type codexWatcher struct {
	root string
	// rollouts caches what has been read out of each rollout so far.
	rollouts map[string]*rollout
}

type rollout struct {
	tail jsonl.Tailer
	cwd  string
	task string
	// working tracks whether a turn is in flight: task_started with no
	// task_complete after it.
	working bool
	updated time.Time
	// since is when working last changed - the start of the turn, or the end of
	// the last one. A rollout is written to throughout a turn, so updated says
	// only that Codex is still there.
	since time.Time
}

func newCodexWatcher(root string) *codexWatcher {
	return &codexWatcher{root: root, rollouts: make(map[string]*rollout)}
}

func (w *codexWatcher) refresh(sessions []tmux.Session, out map[string]Info) {
	if w.root == "" {
		return
	}
	paths := newestFiles(w.root, ".jsonl", codexScanFiles)
	if len(paths) == 0 {
		return
	}

	live := make(map[string]bool, len(paths))
	byDir := make(map[string]*rollout, len(paths))
	for _, path := range paths {
		live[path] = true
		r := w.read(path)
		if r == nil || r.cwd == "" {
			continue
		}
		dir := filepath.Clean(r.cwd)
		if prev, ok := byDir[dir]; !ok || better(r, prev) {
			byDir[dir] = r
		}
	}
	for path := range w.rollouts {
		if !live[path] {
			delete(w.rollouts, path)
		}
	}

	for _, s := range sessions {
		// Codex records no pid, so the working directory is the only link back
		// to a tmux session.
		r, ok := byDir[filepath.Clean(s.Dir)]
		if !ok {
			continue
		}
		// A rollout only stops being "working" on an explicit task_complete, so
		// a Codex killed mid-turn leaves one saying it is busy for good. A
		// fresh session in the same directory has no task yet and loses to it
		// in better(), and berth then reports the new session as busy with the
		// dead one's work. Nothing that quiet is still running.
		status := Idle
		if r.working && time.Since(r.updated) < staleAfter {
			status = Busy
		}
		// A turn abandoned mid-flight is reported idle, and its start is not
		// the age of that idleness; the last thing the rollout recorded is.
		since := r.since
		if status == Idle && r.working {
			since = r.updated
		}
		out[s.Name] = Info{Status: status, Task: r.task, Updated: r.updated, Since: since}
	}
}

// better reports whether a is the rollout to believe about a directory.
//
// Recency alone is not enough. Codex leaves rollouts that never receive a
// prompt - a dozen of them can pile up in one directory - and they are written
// to, so one of those can be the newest file while the session you are looking
// at is the one with something to say. Picking by recency made the task under
// a session flicker away and back as the two took turns being newest.
func better(a, b *rollout) bool {
	if (a.task != "") != (b.task != "") {
		return a.task != ""
	}
	return a.updated.After(b.updated)
}

// read follows one rollout, returning what is known about it.
func (w *codexWatcher) read(path string) *rollout {
	r := w.rollouts[path]
	if r == nil {
		r = &rollout{}
		w.rollouts[path] = r
	}
	_ = r.tail.Read(path, jsonl.DefaultMaxLine, func(line []byte) {
		var rec struct {
			Timestamp time.Time `json:"timestamp"`
			Type      string    `json:"type"`
			Payload   struct {
				Type    string `json:"type"`
				CWD     string `json:"cwd"`
				Message string `json:"message"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			return
		}
		if !rec.Timestamp.IsZero() {
			r.updated = rec.Timestamp
		}
		switch {
		case rec.Type == "session_meta":
			r.cwd = rec.Payload.CWD
		case rec.Payload.Type == "user_message":
			if m := cleanTask(rec.Payload.Message); m != "" {
				r.task = m
			}
		case rec.Payload.Type == "task_started":
			if !r.working {
				r.since = rec.Timestamp
			}
			r.working = true
		case rec.Payload.Type == "task_complete":
			if r.working {
				r.since = rec.Timestamp
			}
			r.working = false
		}
	})
	return r
}

// newestFiles returns up to n paths under root with the given extension, most
// recently modified first.
func newestFiles(root, ext string, n int) []string {
	type entry struct {
		path string
		mod  time.Time
	}
	var entries []entry

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ext) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		entries = append(entries, entry{path: path, mod: info.ModTime()})
		return nil
	})

	sort.Slice(entries, func(i, j int) bool { return entries[i].mod.After(entries[j].mod) })
	if len(entries) > n {
		entries = entries[:n]
	}
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		paths = append(paths, e.path)
	}
	return paths
}
