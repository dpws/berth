package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/dpws/berth/internal/host"
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

// hostBarKind is what the host meters are tinted as. It is not an agent, so it
// falls through to the colour a plain session gets - the machine is the thing
// underneath all of them rather than one of them. Filling up still turns the
// bar red, which is the whole point of drawing it.
const hostBarKind = "host"

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
	rows := usageRows(limits, kind, w)
	if len(rows) == 0 {
		return nil
	}
	// The divider costs a row, so the block can only be as tall as its budget.
	// Every row is a window now, and they are cut from the bottom: the first is
	// the plainest, and on a plan with both it is the one you run into first.
	if room := budget - 1; len(rows) > room {
		rows = rows[:room]
	}

	out := make([]string, 0, len(rows)+1)
	out = append(out, dividerStyle.Render(strings.Repeat("─", max(0, w))))
	return append(out, rows...)
}

// usageRows renders the body of the block, without the divider: one row per
// window, or the one line saying why there are none.
func usageRows(l usage.Limits, kind string, w int) []string {
	if l.Empty() {
		if l.Err == nil {
			return nil
		}
		return []string{" " + faintStyle.Render(truncate(l.Err.Error(), max(1, w-1)))}
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

	rows := make([]string, 0, len(l.Windows))
	for _, win := range l.Windows {
		rows = append(rows, usageRow(win, kind, w, labelW, resetW, now))
	}
	return rows
}

// usageRow lays out one window as: label, meter, value, and how long the window
// has left.
func usageRow(win usage.Window, kind string, w, labelW, resetW int, now time.Time) string {
	tail := ""
	if resetW > 0 {
		tail = padLeft(untilReset(win, now), resetW)
	}
	return usageRowWith(win, kind, w, labelW, resetW, tail)
}

// usageRowWith is that layout with the right-hand column supplied, so the host
// block can borrow it: label, meter, percentage, and whatever is left of the
// thing, right-aligned to w. The meter is dropped when there is no room for it,
// and the right-hand column before the number is - a percentage with nothing
// beside it still says something, the other way round says nothing.
//
// tailW is what that column has been padded to, and 0 means there is none.
func usageRowWith(win usage.Window, kind string, w, labelW, tailW int, right string) string {
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

	// The right-hand column is a column: a "52m" beside a "6d 23h" would
	// otherwise walk the percentages out of line with each other.
	tail, styled := value, styledValue
	if tailW > 0 {
		tail += " " + right
		styled += " " + faintStyle.Render(right)
	}

	for _, attempt := range []struct{ bar, tail, styled string }{
		{usageBar(win.Percent, kind, barWidthFor(w, labelW, tailW)), tail, styled},
		{"", tail, styled},
		// No room for both: the meter goes before the figures do, and the
		// right-hand column before the percentage.
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

// hostBlock renders the machine berth is running on, in the same shape as the
// rate limits above it: a divider, then a meter per thing with what is left of
// it on the right. It returns nil when the block is off, when the machine keeps
// its accounting somewhere berth cannot read, or when there is no room.
//
// It sits under the limits because it answers a different question. The limits
// are about the plan you are spending; this is about the box the work runs on,
// and it is the second thing you want to know when an agent has gone quiet.
func (m *Model) hostBlock(w, budget int) []string {
	if !m.cfg.ShowHost || budget < usageMinRows || m.host.Empty() {
		return nil
	}

	// The labels are fixed, so the column is too - no need to measure them.
	const labelW = 4
	meters := []struct {
		label string
		meter host.Meter
	}{
		{"cpu", m.host.CPU},
		{"mem", m.host.Mem},
		{"disk", m.host.Disk},
	}

	leftW := 0
	for _, x := range meters {
		if x.meter.Known && ansi.StringWidth(x.meter.Left) > leftW {
			leftW = ansi.StringWidth(x.meter.Left)
		}
	}

	rows := make([]string, 0, len(meters))
	for _, x := range meters {
		if !x.meter.Known {
			// A number the machine would not give up is left out rather than
			// drawn as a full or empty bar, either of which would be a lie.
			continue
		}
		rows = append(rows, hostRow(x.label, x.meter, w, labelW, leftW))
	}
	if len(rows) == 0 {
		return nil
	}
	if room := budget - 1; len(rows) > room {
		rows = rows[:room]
	}

	out := make([]string, 0, len(rows)+1)
	out = append(out, dividerStyle.Render(strings.Repeat("─", max(0, w))))
	return append(out, rows...)
}

// hostRow lays one meter out the way a usage row is laid out, so the two blocks
// read as one column of figures rather than two designs stacked.
func hostRow(label string, meter host.Meter, w, labelW, leftW int) string {
	win := usage.Window{Label: label, Percent: meter.Percent}
	return usageRowWith(win, hostBarKind, w, labelW, leftW, padLeft(meter.Left, leftW))
}
