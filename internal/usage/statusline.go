package usage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Claude Code hands its status line command a JSON payload on stdin, and for
// Pro and Max accounts that payload carries the real rate limit windows - the
// same ones /usage draws. It is the only route to those numbers that does not
// involve calling an internal endpoint with the Claude Code login token, which
// Anthropic's terms do not allow. Nothing here reaches the network: Claude Code
// pushes the numbers to a command you configured, and berth writes them down.

// stdinLimit bounds how much of the payload is read. The real thing is a few
// kilobytes; this is only here so a wedged pipe cannot grow without end.
const stdinLimit = 1 << 20

// StatusLine is the slice of Claude Code's status line payload berth reads.
type StatusLine struct {
	Model struct {
		DisplayName string `json:"display_name"`
	} `json:"model"`
	ContextWindow struct {
		UsedPercentage *float64 `json:"used_percentage"`
	} `json:"context_window"`
	RateLimits *struct {
		FiveHour *statusWindow `json:"five_hour"`
		SevenDay *statusWindow `json:"seven_day"`
	} `json:"rate_limits"`
}

type statusWindow struct {
	UsedPercentage float64 `json:"used_percentage"`
	// ResetsAt is Unix epoch seconds.
	ResetsAt int64 `json:"resets_at"`
}

// cachedLimits is what berth writes down between status line invocations.
type cachedLimits struct {
	// SampledUnix is when Claude Code handed us these numbers.
	SampledUnix int64         `json:"sampled_unix"`
	FiveHour    *statusWindow `json:"five_hour,omitempty"`
	SevenDay    *statusWindow `json:"seven_day,omitempty"`
}

// CachePath is where the status line command leaves the numbers for berth.
func CachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "berth", "claude-limits.json")
}

// ParseStatusLine reads a Claude Code status line payload.
func ParseStatusLine(data []byte) (StatusLine, error) {
	var s StatusLine
	if err := json.Unmarshal(data, &s); err != nil {
		return StatusLine{}, err
	}
	return s, nil
}

// ReadStatusLine reads a payload from r.
func ReadStatusLine(r io.Reader) (StatusLine, error) {
	data, err := io.ReadAll(io.LimitReader(r, stdinLimit))
	if err != nil {
		return StatusLine{}, err
	}
	return ParseStatusLine(data)
}

// SaveStatusLine records the rate limits from a status line payload.
//
// A payload without rate limits is ignored rather than written: they only
// appear for Claude.ai subscribers, and only after the session's first
// response, so the early calls in every session carry none. Writing those would
// erase good numbers and leave berth reporting nothing.
func SaveStatusLine(s StatusLine, now time.Time) error {
	if s.RateLimits == nil {
		return nil
	}
	if s.RateLimits.FiveHour == nil && s.RateLimits.SevenDay == nil {
		return nil
	}
	path := CachePath()
	if path == "" {
		return errors.New("no user cache directory available")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.Marshal(cachedLimits{
		SampledUnix: now.Unix(),
		FiveHour:    s.RateLimits.FiveHour,
		SevenDay:    s.RateLimits.SevenDay,
	})
	if err != nil {
		return err
	}

	// Claude Code kills the status line command if another update lands while
	// it is still running, so the file is replaced by a rename rather than
	// written in place - a half-written cache would be read as corrupt.
	tmp, err := os.CreateTemp(filepath.Dir(path), "limits-*.tmp")
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
	return os.Rename(tmp.Name(), path)
}

// errNoStatusLine is what the block says when the hook has never run. It is
// shown to the user, so it names the thing that fixes it.
var errNoStatusLine = errors.New("run berth statusline")

// readCachedClaude returns the last rate limits a status line invocation wrote.
func readCachedClaude() (Limits, error) {
	path := CachePath()
	if path == "" {
		return Limits{}, errNoStatusLine
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Limits{}, errNoStatusLine
	}
	var c cachedLimits
	if err := json.Unmarshal(data, &c); err != nil {
		return Limits{}, errNoStatusLine
	}

	// A cache written without a stamp has no age; time.Unix(0, 0) is not the
	// zero time, so it would be reported as a reading taken in 1970.
	limits := Limits{Sampled: unixOrZero(c.SampledUnix)}
	for _, w := range []struct {
		label string
		win   *statusWindow
	}{{"5h", c.FiveHour}, {"week", c.SevenDay}} {
		if w.win == nil {
			continue
		}
		limits.Windows = append(limits.Windows, Window{
			Label:    w.label,
			Percent:  w.win.UsedPercentage,
			ResetsAt: unixOrZero(w.win.ResetsAt),
		})
	}
	if len(limits.Windows) == 0 {
		return Limits{}, errNoStatusLine
	}
	return limits, nil
}

// Render draws the status line berth prints when it is not chaining to a
// command of yours. It is deliberately one short line: Claude Code gives the
// status bar limited width and truncates what does not fit.
func (s StatusLine) Render() string {
	parts := make([]string, 0, 4)
	if s.Model.DisplayName != "" {
		parts = append(parts, s.Model.DisplayName)
	}
	if p := s.ContextWindow.UsedPercentage; p != nil {
		parts = append(parts, fmt.Sprintf("ctx %.0f%%", *p))
	}
	if s.RateLimits != nil {
		if w := s.RateLimits.FiveHour; w != nil {
			parts = append(parts, fmt.Sprintf("5h %.0f%%", w.UsedPercentage))
		}
		if w := s.RateLimits.SevenDay; w != nil {
			parts = append(parts, fmt.Sprintf("week %.0f%%", w.UsedPercentage))
		}
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " · "
		}
		out += p
	}
	return out
}
