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
		// Several rollouts can share a directory; the one written last is the
		// session still in front of you.
		if prev, ok := byDir[dir]; !ok || r.updated.After(prev.updated) {
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
		status := Idle
		if r.working {
			status = Busy
		}
		out[s.Name] = Info{Status: status, Task: r.task, Updated: r.updated}
	}
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
			r.working = true
		case rec.Payload.Type == "task_complete":
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
