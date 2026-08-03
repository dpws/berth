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
	// codexScanFiles bounds how many rollout files we open looking for a
	// snapshot. One is almost always enough; a handful covers the case where
	// the newest session has not made a request yet.
	codexScanFiles = 4
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
func readCodexDir(dir string) (Limits, error) {
	files, err := newestFiles(dir, ".jsonl", codexScanFiles)
	if err != nil {
		return Limits{}, err
	}
	if len(files) == 0 {
		return Limits{}, errors.New("no codex sessions yet")
	}

	for _, path := range files {
		limits, ok := readCodexFile(path)
		if ok {
			return limits, nil
		}
	}
	return Limits{}, errors.New("no rate limits recorded yet")
}

// readCodexFile returns the last snapshot in one rollout file.
func readCodexFile(path string) (Limits, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Limits{}, false
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
				if limits, ok := scanCodexLines(r); ok {
					return limits, true
				}
			}
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return Limits{}, false
		}
	}
	return scanCodexLines(f)
}

// scanCodexLines returns the last snapshot in a stream of rollout lines.
func scanCodexLines(r io.Reader) (Limits, bool) {
	needle := []byte(`"rate_limits"`)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), codexMaxLine)

	var (
		found  Limits
		haveIt bool
	)
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

		limits := Limits{Plan: rl.PlanType, Sampled: rec.Timestamp}
		for _, w := range windows {
			limits.Windows = append(limits.Windows, Window{
				Label:    windowLabel(w.WindowMinutes),
				Percent:  w.UsedPercent,
				ResetsAt: unixOrZero(w.ResetsAt),
			})
		}
		found, haveIt = limits, true
	}
	return found, haveIt
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
