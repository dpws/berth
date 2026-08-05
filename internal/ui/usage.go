package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/dpws/berth/internal/tmux"
	"github.com/dpws/berth/internal/usage"
)

// A usage meter takes whatever the row can spare between its label and its
// percentage, so a wider sidebar buys a longer bar rather than more blank.
const (
	// labelMax is the widest a row label may be. Windows are "5h" and "week",
	// but Codex meters some models separately, and a row then carries the
	// bucket and the window both ("spark week"). The column only takes what its
	// widest row asks for, so this is a ceiling rather than a cost.
	labelMax = 12
	// barFixed is what the rest of the row costs: a leading space, the label,
	// a space, two of gap, and "100%".
	barFixed = 1 + labelMax + 1 + 2 + 4
	// barMin is the shortest meter worth drawing; below it the number alone
	// says more than six characters of block glyph.
	barMin = 6
	// barMax stops a very wide sidebar turning the block into a ruler.
	barMax = 24
)

// barWidthFor is how many cells the meter gets in a row w cells wide, or 0
// when there is not enough room for one. resetW is the column on the far right
// holding how long the window has left, which is 0 when no window says.
func barWidthFor(w, labelW, resetW int) int {
	n := w - (barFixed - labelMax + labelW)
	if resetW > 0 {
		n -= resetW + 1
	}
	if n > barMax {
		n = barMax
	}
	if n < barMin {
		return 0
	}
	return n
}

// usageMinRows is the smallest block worth drawing: a divider plus one line.
const usageMinRows = 2

// usageStaleAfter is how long a reading is presented as current. Both agents
// only report while they are running, so a quiet hour leaves the last numbers
// standing; saying when they were taken is better than implying they are live.
const usageStaleAfter = 20 * time.Minute

// usageBlock renders the selected session's rate limits as w-wide lines,
// including the divider above them. It returns nil when there is nothing to
// show or fewer than budget rows to show it in.
func (m *Model) usageBlock(w, budget int) []string {
	if m.cfg.HideUsage || budget < usageMinRows {
		return nil
	}
	s, ok := m.selected()
	if !ok {
		return nil
	}
	kind := s.DetectedKind()
	if kind != tmux.KindClaude && kind != tmux.KindCodex {
		return nil // a plain shell has no limits to report
	}
	limits, ok := m.usage[kind]
	if !ok {
		return nil // not read yet
	}
	rows, notes := usageRows(limits, kind, w)
	if len(rows) == 0 && len(notes) == 0 {
		return nil
	}
	// The divider costs a row, so the block can only be as tall as its budget.
	// The notes are what the meters do not say - when the window rolls over,
	// and how old the reading is - so they outrank the last meters when there
	// is not room for both. Never the only meter, though: a note over nothing
	// says less than a figure with no date on it.
	room := budget - 1
	keep := len(rows)
	if len(rows)+len(notes) > room {
		keep = min(len(rows), max(1, room-len(notes)))
	}
	rows = rows[:keep:keep]
	for _, note := range notes {
		if len(rows) >= room {
			break
		}
		rows = append(rows, note)
	}
	if len(rows) > room {
		rows = rows[:room]
	}

	out := make([]string, 0, len(rows)+1)
	out = append(out, dividerStyle.Render(strings.Repeat("─", max(0, w))))
	return append(out, rows...)
}

// usageRows renders the body of the block, without the divider. The notes come
// back apart from the meters because they are the last rows and so the first
// the budget would cut, and they are worth more than the meters they displace.
func usageRows(l usage.Limits, kind string, w int) (rows []string, notes []string) {
	if l.Empty() {
		if l.Err == nil {
			return nil, nil
		}
		return []string{" " + faintStyle.Render(truncate(l.Err.Error(), max(1, w-1)))}, nil
	}

	// One label column for the block, as wide as its widest row needs.
	labelW := 2
	for _, win := range l.Windows {
		if n := lipgloss.Width(win.Label); n > labelW {
			labelW = n
		}
	}
	if labelW > labelMax {
		labelW = labelMax
	}

	now := time.Now()
	resetW := resetWidth(l, now)

	rows = make([]string, 0, len(l.Windows)+1)
	for _, win := range l.Windows {
		rows = append(rows, usageRow(win, kind, w, labelW, resetW, now))
	}
	return rows, usageNotes(l, w, now)
}

// usageRow lays out one window as: label, meter, value, and how long the window
// has left, right-aligned to w. The meter is dropped when there is no room for
// it, and the time left before the number is: a percentage with no idea when it
// rolls over still says something, the other way round says nothing.
func usageRow(win usage.Window, kind string, w, labelW, resetW int, now time.Time) string {
	label := footerStyle.Render(padTo(truncate(win.Label, labelW), labelW))

	// row lays out " " + label + [" " + bar] + gap + value + [" " + reset],
	// right-aligned. tail is the plain text of everything after the gap, which
	// is what the arithmetic needs; styled is the same with colour on it.
	row := func(bar, tail, styled string) (string, bool) {
		used := 1 + labelW + lipgloss.Width(tail)
		if bar != "" {
			used += 1 + lipgloss.Width(bar)
		}
		if used+1 > w {
			return "", false
		}
		if bar != "" {
			bar = " " + bar
		}
		return " " + label + bar + strings.Repeat(" ", w-used) + styled, true
	}

	value := fmt.Sprintf("%3.0f%%", win.Percent)
	styledValue := itemStyle.Render(value)

	// The time left sits in a column of its own, so a "52m" beside a "6d 23h"
	// does not walk the percentages out of line with each other.
	tail, styled := value, styledValue
	if resetW > 0 {
		left := padLeft(untilReset(win, now), resetW)
		tail += " " + left
		styled += " " + faintStyle.Render(left)
	}

	for _, attempt := range []struct{ bar, tail, styled string }{
		{usageBar(win.Percent, kind, barWidthFor(w, labelW, resetW)), tail, styled},
		{"", tail, styled},
		// No room for both: the meter goes before the figures do, and the time
		// left before the percentage.
		{usageBar(win.Percent, kind, barWidthFor(w, labelW, 0)), value, styledValue},
		{"", value, styledValue},
	} {
		if attempt.bar == "" && attempt.tail == "" {
			continue
		}
		if out, ok := row(attempt.bar, attempt.tail, attempt.styled); ok {
			return out
		}
	}
	return truncate(" "+label+" "+styledValue, w)
}

// padLeft right-aligns s in w cells.
func padLeft(s string, w int) string {
	if gap := w - ansi.StringWidth(s); gap > 0 {
		return strings.Repeat(" ", gap) + s
	}
	return s
}

// untilReset is how long this window has left, or "" when it does not say -
// including when the moment it named has passed, since the window has rolled
// over and the agent has simply not run since to report the next one.
func untilReset(win usage.Window, now time.Time) string {
	if win.ResetsAt.IsZero() || !win.ResetsAt.After(now) {
		return ""
	}
	return shortUntil(win.ResetsAt.Sub(now))
}

// shortUntil writes a wait as at most two units. A weekly window is days out
// and a five-hour one is minutes, and both have to read at a glance in a column
// a few cells wide - "2h 52m" says more there than "17:40" ever did, which left
// you working out what that meant from the clock on the wall.
func shortUntil(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h, m := int(d.Hours()), int(d.Minutes())%60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		days, h := int(d.Hours())/24, int(d.Hours())%24
		if h == 0 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dd %dh", days, h)
	}
}

// resetWidth is how wide the column of times left has to be, and 0 when not one
// window says - which is what keeps the block exactly as it was for a source
// that reports no reset at all.
func resetWidth(l usage.Limits, now time.Time) int {
	n := 0
	for _, win := range l.Windows {
		if s := untilReset(win, now); ansi.StringWidth(s) > n {
			n = ansi.StringWidth(s)
		}
	}
	return n
}

// usageBar draws a meter tinted by the agent it belongs to, turning red as the
// window fills up.
func usageBar(percent float64, kind string, width int) string {
	if percent < 0 {
		percent = 0
	}
	filled := int(percent/100*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}

	color := kindColor(kind)
	if percent >= 90 {
		color = colDanger
	}
	return lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("▓", filled)) +
		faintStyle.Render(strings.Repeat("░", width-filled))
}

// usageNotes are the lines under the meters. Only one is left: how old the
// reading is. When a window rolls over is on the window's own row now, where it
// belongs - a block metering two of them had to pick one to report, and it
// picked the soonest, which is not the same as the one you are up against.
func usageNotes(l usage.Limits, w int, now time.Time) []string {
	// Numbers only arrive while an agent is running, so old ones say when they
	// were taken rather than pretending to be current.
	if l.Sampled.IsZero() || now.Sub(l.Sampled) <= usageStaleAfter {
		return nil
	}
	return []string{" " + faintStyle.Render(
		truncate("as of "+dayTime(l.Sampled, now), max(1, w-1)))}
}

// dayTime writes a moment as a clock time, naming the day when it is not
// today. Both sides of the block need that. A reading easily is not from today:
// Codex meters some models separately and berth keeps the last word on every
// bucket, so the age of the block is the age of whichever one has gone longest
// untouched - days, for a model tried once. A reset easily is not either: a
// weekly window rolls over up to seven days out, and "resets 10:15" for
// something three days away is worse than saying nothing.
//
// Codex stamps its rollouts in UTC and gives reset times as unix seconds, so
// either is moved into the zone the clock on the wall is in before the day or
// the time is read off it.
func dayTime(at, now time.Time) string {
	const day = "2006-01-02"
	at = at.In(now.Location())
	if at.Format(day) == now.Format(day) {
		return at.Format("15:04")
	}
	return at.Format("Jan 2 15:04")
}
