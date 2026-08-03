package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Codex records the server's own rate limit snapshot in its session rollout
// logs, once per turn alongside the token counts. The newest snapshot is
// therefore whatever the most recently written rollout file ends with.
const (
	// codexScanFiles bounds how many rollout files we open looking for
	// snapshots. Codex leaves rollouts that never make a request, and a dozen
	// can pile up in a day, so a handful is not enough to be sure of finding
	// the session that did.
	codexScanFiles = 16
	// codexMaxLine is the scanner buffer. Rollout lines carry whole messages,
	// so they can be far longer than bufio's default 64 KiB.
	codexMaxLine = 8 << 20
	// codexTailBytes is how much of the end of a rollout is read looking for
	// the newest snapshot before falling back to the whole file.
	codexTailBytes = 512 << 10
)

// codexRollout is the subset of a rollout line berth reads.
type codexRollout struct {
	Timestamp time.Time `json:"timestamp"`
	Payload   struct {
		Type       string           `json:"type"`
		RateLimits *codexRateLimits `json:"rate_limits"`
	} `json:"payload"`
}

type codexRateLimits struct {
	// LimitID names the bucket. Codex meters some models separately, so a
	// session that switches model starts reporting against a different one.
	LimitID   string       `json:"limit_id"`
	LimitName string       `json:"limit_name"`
	Primary   *codexWindow `json:"primary"`
	Secondary *codexWindow `json:"secondary"`
	PlanType  string       `json:"plan_type"`
}

type codexWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int     `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

// readCodex returns the most recent rate limit snapshot Codex wrote.
func readCodex() (Limits, error) {
	dir := codexSessionsDir()
	if dir == "" {
		return Limits{}, errors.New("no home directory")
	}
	return readCodexDir(dir)
}

// readCodexDir is readCodex against an explicit sessions directory.
//
// Every bucket is kept, not just the last one written. Codex meters some
// models separately, so a session that switches model starts reporting against
// a different limit - and showing only the newest snapshot meant a freshly
// touched bucket at nothing used hid a main quota most of the way gone.
func readCodexDir(dir string) (Limits, error) {
	files, err := newestFiles(dir, ".jsonl", codexScanFiles)
	if err != nil {
		return Limits{}, err
	}
	if len(files) == 0 {
		return Limits{}, errors.New("no codex sessions yet")
	}

	newest := map[string]codexSnapshot{}
	for _, path := range files {
		for id, snap := range readCodexFile(path) {
			if prev, ok := newest[id]; !ok || snap.at.After(prev.at) {
				newest[id] = snap
			}
		}
	}
	if len(newest) == 0 {
		return Limits{}, errors.New("no rate limits recorded yet")
	}
	return codexLimits(newest), nil
}

// codexSnapshot is one bucket's most recent reading.
type codexSnapshot struct {
	at      time.Time
	limits  codexRateLimits
	windows []*codexWindow
}

// codexLimits turns the buckets into what the sidebar draws, plainest first so
// the main quota leads.
func codexLimits(byID map[string]codexSnapshot) Limits {
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := ids[i], ids[j]
		// The unadorned "codex" bucket is the one most people mean.
		if (a == "codex") != (b == "codex") {
			return a == "codex"
		}
		return a < b
	})

	out := Limits{}
	named := len(byID) > 1
	for _, id := range ids {
		snap := byID[id]
		if out.Plan == "" {
			out.Plan = snap.limits.PlanType
		}
		// The oldest reading on show, not the newest. A bucket the current
		// model reports against is written every few seconds and would
		// otherwise vouch for one that stopped being written an hour ago. A
		// bucket whose lines carried no timestamp has nothing to say about
		// age, and must not pass for the oldest of all.
		if !snap.at.IsZero() && (out.Sampled.IsZero() || snap.at.Before(out.Sampled)) {
			out.Sampled = snap.at
		}
		for _, w := range snap.windows {
			label := windowLabel(w.WindowMinutes)
			if named {
				// With more than one bucket in play, "week" twice says
				// nothing; the bucket is what tells them apart. A bucket that
				// meters two periods still needs the window as well, or it
				// names both of its own rows the same.
				label = codexLimitLabel(id, snap.limits.LimitName)
				if len(snap.windows) > 1 {
					label += " " + windowLabel(w.WindowMinutes)
				}
			}
			out.Windows = append(out.Windows, Window{
				Label:    label,
				Percent:  w.UsedPercent,
				ResetsAt: unixOrZero(w.ResetsAt),
			})
		}
	}
	return out
}

// codexLimitLabel shortens a bucket to something a sidebar can hold. The
// published name is friendlier than the id when there is one.
func codexLimitLabel(id, name string) string {
	if name != "" {
		parts := strings.Split(name, "-")
		return strings.ToLower(parts[len(parts)-1])
	}
	return strings.ToLower(strings.TrimPrefix(id, "codex_"))
}

// readCodexFile returns the last snapshot per bucket in one rollout file.
func readCodexFile(path string) map[string]codexSnapshot {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	// Codex records a snapshot every turn, so the newest one is close to the
	// end of the file. Reading the tail keeps the cost flat as an active
	// session's rollout grows; the whole file is only read if that misses.
	if info, err := f.Stat(); err == nil && info.Size() > codexTailBytes {
		if _, err := f.Seek(info.Size()-codexTailBytes, io.SeekStart); err == nil {
			r := bufio.NewReaderSize(f, 64*1024)
			// The seek lands mid-line; drop the remainder of it.
			if _, err := r.ReadString('\n'); err == nil {
				if found := scanCodexLines(r); len(found) > 0 {
					return found
				}
			}
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil
		}
	}
	return scanCodexLines(f)
}

// scanCodexLines returns the last snapshot per bucket in a stream of lines.
func scanCodexLines(r io.Reader) map[string]codexSnapshot {
	needle := []byte(`"rate_limits"`)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), codexMaxLine)

	found := map[string]codexSnapshot{}
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.Contains(line, needle) {
			continue
		}
		var rec codexRollout
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		rl := rec.Payload.RateLimits
		if rl == nil {
			continue
		}
		var windows []*codexWindow
		for _, w := range []*codexWindow{rl.Primary, rl.Secondary} {
			if w != nil && w.WindowMinutes > 0 {
				windows = append(windows, w)
			}
		}
		if len(windows) == 0 {
			continue
		}
		// Codex does not guarantee primary is the shorter window — a plan with
		// only a weekly limit reports it as primary — so order them the way
		// they will be read: shortest period first.
		sort.SliceStable(windows, func(i, j int) bool {
			return windows[i].WindowMinutes < windows[j].WindowMinutes
		})

		id := rl.LimitID
		if id == "" {
			id = "codex"
		}
		if prev, ok := found[id]; !ok || !rec.Timestamp.Before(prev.at) {
			found[id] = codexSnapshot{at: rec.Timestamp, limits: *rl, windows: windows}
		}
	}
	return found
}

// newestFiles returns up to n paths under root with the given extension, most
// recently modified first.
func newestFiles(root, ext string, n int) ([]string, error) {
	type entry struct {
		path string
		mod  time.Time
	}
	var entries []entry

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subdirectory should not abandon the whole walk.
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
	if err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].mod.After(entries[j].mod) })
	if len(entries) > n {
		entries = entries[:n]
	}
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		paths = append(paths, e.path)
	}
	return paths, nil
}

func unixOrZero(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
