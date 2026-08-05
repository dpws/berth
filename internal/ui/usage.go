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

// A meter row is " label bar pct  tail": the percentage sits against the meter
// it belongs to rather than out at the edge, and the bar takes whatever the row
// can spare, so a wider sidebar buys a longer meter rather than more blank.
const (
	// labelMax is the widest a row label may be. Windows are "5h" and "week",
	// but Codex meters some models separately, and a row then carries the
	// bucket and the window both ("spark week"). The column only takes what its
	// widest row asks for, so this is a ceiling rather than a cost.
	labelMax = 12
	// pctMin is what a percentage costs at its narrowest: "100%". A load
	// average past 999% is the only thing that asks for more.
	pctMin = 4
	// barMin is the shortest meter worth drawing; below it the number alone
	// says more than six characters of block glyph.
	barMin = 6
	// barMax stops a very wide sidebar turning the block into a ruler.
	barMax = 24
)

// meterLayout is the shape every meter row is drawn to - the rate limits and
// the machine alike.
//
// One layout for both blocks is what keeps them reading as a single column of
// figures. Measured separately they settle on different widths for the same
// sidebar, since their right-hand columns differ - "2d 12h" against "7.1G" -
// and the bars and the percentages then sit a cell or two out of line with each
// other for no reason a reader could name.
type meterLayout struct {
	label int // the label column
	pct   int // the percentage
	tail  int // what is left of the thing, on the right; 0 when nothing says
	bar   int // the meter, or 0 when the row is too narrow for one
}

// barWidthFor is how many cells the meter gets in a row w cells wide, or 0 when
// there is not enough room for one. What is left over after " label bar pct"
// and the right-hand column is the bar.
func barWidthFor(w int, lay meterLayout) int {
	n := w - (1 + lay.label + 1 + 1 + lay.pct)
	if lay.tail > 0 {
		n -= 1 + lay.tail
	}
	if n > barMax {
		n = barMax
	}
	if n < barMin {
		return 0
	}
	return n
}

// meterLayout measures everything about to be drawn in the sidebar's meter rows
// - both blocks - and settles the columns for all of it.
func (m *Model) meterLayout(w int, now time.Time) meterLayout {
	lay := meterLayout{label: 2, pct: pctMin}
	measure := func(label string, percent float64, tail string) {
		if n := lipgloss.Width(label); n > lay.label {
			lay.label = n
		}
		if n := lipgloss.Width(pctText(percent)); n > lay.pct {
			lay.pct = n
		}
		if n := lipgloss.Width(tail); n > lay.tail {
			lay.tail = n
		}
	}

	if l, _, ok := m.selectedLimits(); ok {
		for _, win := range l.Windows {
			measure(win.Label, win.Percent, untilReset(win, now))
		}
	}
	if m.cfg.ShowHost {
		for _, x := range hostMeters(m.host) {
			if x.meter.Known {
				measure(x.label, x.meter.Percent, x.meter.Left)
			}
		}
	}

	if lay.label > labelMax {
		lay.label = labelMax
	}
	lay.bar = barWidthFor(w, lay)
	return lay
}

func pctText(percent float64) string { return fmt.Sprintf("%.0f%%", percent) }

// usageMinRows is the smallest block worth drawing: a divider plus one line.
const usageMinRows = 2

// hostBarKind is what the host meters are tinted as. It is not an agent, so it
// falls through to the colour a plain session gets - the machine is the thing
// underneath all of them rather than one of them. Filling up still turns the
// bar red, which is the whole point of drawing it.
const hostBarKind = "host"

// selectedLimits is the rate limits the block would draw, the agent they
// belong to, and whether there are any: a plain shell has none, and one berth
// has not read yet has none yet.
func (m *Model) selectedLimits() (usage.Limits, string, bool) {
	if m.cfg.HideUsage {
		return usage.Limits{}, "", false
	}
	s, ok := m.selected()
	if !ok {
		return usage.Limits{}, "", false
	}
	kind := s.DetectedKind()
	if kind != tmux.KindClaude && kind != tmux.KindCodex {
		return usage.Limits{}, "", false
	}
	l, ok := m.usage[kind]
	return l, kind, ok
}

// usageBlock renders the selected session's rate limits as w-wide lines,
// including the divider above them. It returns nil when there is nothing to
// show or fewer than budget rows to show it in.
func (m *Model) usageBlock(w, budget int) []string {
	if budget < usageMinRows {
		return nil
	}
	limits, kind, ok := m.selectedLimits()
	if !ok {
		return nil
	}
	now := time.Now()
	rows := usageRows(limits, kind, w, m.meterLayout(w, now), now)
	if len(rows) == 0 {
		return nil
	}
	// The divider costs a row, so the block can only be as tall as its budget.
	// Every row is a window, and they are cut from the bottom: the first is the
	// plainest, and on a plan with both it is the one you run into first.
	if room := budget - 1; len(rows) > room {
		rows = rows[:room]
	}

	out := make([]string, 0, len(rows)+1)
	out = append(out, dividerStyle.Render(strings.Repeat("─", max(0, w))))
	return append(out, rows...)
}

// usageRows renders the body of the block, without the divider: one row per
// window, or the one line saying why there are none.
func usageRows(l usage.Limits, kind string, w int, lay meterLayout, now time.Time) []string {
	if l.Empty() {
		if l.Err == nil {
			return nil
		}
		return []string{" " + faintStyle.Render(truncate(l.Err.Error(), max(1, w-1)))}
	}
	rows := make([]string, 0, len(l.Windows))
	for _, win := range l.Windows {
		rows = append(rows, meterRow(win.Label, win.Percent, kind, w, lay, untilReset(win, now)))
	}
	return rows
}

// meterRow lays one meter out: label, bar, percentage against the bar, and
// whatever is left of the thing out at the right edge.
//
// The percentage belongs to the meter, so it goes where the meter ends rather
// than in a column of its own on the far side of the row - a number floating a
// hand's width from the bar it describes reads as a third thing on the row
// rather than the same fact twice.
//
// What the row gives up first, as it narrows, is the meter: a bar is a picture
// of a number that is on the row anyway. Then the right-hand column, then the
// label is cut. With no bar to sit against, the percentage joins the figures on
// the right rather than trailing the label across an empty row.
func meterRow(label string, percent float64, kind string, w int, lay meterLayout, left string) string {
	name := footerStyle.Render(padTo(truncate(label, lay.label), lay.label))
	pct := padLeft(pctText(percent), lay.pct)
	styledPct := itemStyle.Render(pct)

	tail, styledTail := "", ""
	if lay.tail > 0 {
		tail = padLeft(left, lay.tail)
		styledTail = faintStyle.Render(tail)
	}

	// row assembles a candidate and reports whether it fits. head is everything
	// up to the gap; the tail is held against the right edge by it.
	row := func(head, styledHead, tail, styledTail string) (string, bool) {
		gap := w - 1 - lay.label - lipgloss.Width(head) - lipgloss.Width(tail)
		if tail != "" && gap < 1 {
			return "", false
		}
		if gap < 0 {
			return "", false
		}
		return " " + name + styledHead + strings.Repeat(" ", gap) + styledTail, true
	}

	bar := ""
	if lay.bar > 0 {
		bar = usageBar(percent, kind, lay.bar)
	}
	if bar != "" {
		head, styled := " "+strings.Repeat(" ", lay.bar)+" "+pct, " "+bar+" "+styledPct
		if out, ok := row(head, styled, tail, styledTail); ok {
			return out
		}
		if out, ok := row(head, styled, "", ""); ok {
			return out
		}
	}
	// No meter: the percentage keeps the figures company on the right instead.
	if out, ok := row("", "", pct+" "+tail, styledPct+" "+styledTail); lay.tail > 0 && ok {
		return out
	}
	if out, ok := row("", "", pct, styledPct); ok {
		return out
	}
	return truncate(" "+name+" "+styledPct, w)
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

// hostMeters is the machine's three rows, in the order they are drawn.
func hostMeters(s host.Stats) []struct {
	label string
	meter host.Meter
} {
	return []struct {
		label string
		meter host.Meter
	}{
		{"cpu", s.CPU},
		{"mem", s.Mem},
		{"disk", s.Disk},
	}
}

// hostBlock renders the machine berth is running on, in the same shape as the
// rate limits above it and to the same columns: a divider, then a meter per
// thing with what is left of it on the right. It returns nil when the block is
// off, when the machine keeps its accounting somewhere berth cannot read, or
// when there is no room.
//
// It sits under the limits because it answers a different question. The limits
// are about the plan you are spending; this is about the box the work runs on,
// and it is the second thing you want to know when an agent has gone quiet.
func (m *Model) hostBlock(w, budget int) []string {
	if !m.cfg.ShowHost || budget < usageMinRows || m.host.Empty() {
		return nil
	}
	lay := m.meterLayout(w, time.Now())

	rows := make([]string, 0, 3)
	for _, x := range hostMeters(m.host) {
		if !x.meter.Known {
			// A number the machine would not give up is left out rather than
			// drawn as a full or empty bar, either of which would be a lie.
			continue
		}
		rows = append(rows, meterRow(x.label, x.meter.Percent, hostBarKind, w, lay, x.meter.Left))
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
