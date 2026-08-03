package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/dpws/berth/internal/agent"
	"github.com/dpws/berth/internal/tmux"
)

// minListRows is what the session list keeps for itself before the usage block
// is offered anything. A session takes two rows, so this is one of them.
const minListRows = 2

// sidebarLines renders the session list as exactly h lines, each exactly w
// cells wide.
func (m *Model) sidebarLines(w, h int) []string {
	lines := make([]string, 0, h)
	pad := lipgloss.NewStyle().Width(w).MaxWidth(w)

	// rowSessions records which session each rendered row belongs to, so mouse
	// clicks can be resolved without duplicating this layout arithmetic.
	m.rowSessions = make([]int, 0, h)
	blank := func() { m.rowSessions = append(m.rowSessions, -1) }

	lines = append(lines, pad.Render(m.headerLine(w)))
	blank()
	lines = append(lines, pad.Render(dividerStyle.Render(strings.Repeat("─", max(0, w)))))
	blank()

	if m.filter != "" {
		lines = append(lines, pad.Render(" "+labelActiveStyle.Render("/")+itemStyle.Render(m.filter)))
		blank()
	}

	// Reserve the last two rows for the legend, plus whatever the usage block
	// needs above it. The block only gets what is left once the list has room
	// for a session or two: a sidebar showing rate limits and no sessions is
	// not a sidebar, and a short window used to produce exactly that.
	usage := m.usageBlock(w, h-len(lines)-2-minListRows)
	reserved := 2 + len(usage)
	listHeight := h - len(lines) - reserved
	visible := m.visibleSessions()

	switch {
	case len(visible) == 0 && m.filter != "":
		lines = append(lines, pad.Render(" "+itemMutedStyle.Render("no matches")))
		blank()
	case len(visible) == 0:
		lines = append(lines, pad.Render(" "+itemMutedStyle.Render("no sessions yet")))
		blank()
		lines = append(lines, pad.Render(" "+itemMutedStyle.Render("press n to create one")))
		blank()
	default:
		// A session can take more than one row, so the list is laid out first
		// and then scrolled by row rather than by session.
		rows := m.listRows(visible, w)
		start := scrollOffset(cursorRow(rows, m.cursor), len(rows), listHeight)
		for i := start; i < len(rows) && len(lines) < h-reserved; i++ {
			lines = append(lines, pad.Render(rows[i].text))
			m.rowSessions = append(m.rowSessions, rows[i].session)
		}
	}

	for len(lines) < h-reserved {
		lines = append(lines, pad.Render(""))
		blank()
	}
	for _, line := range usage {
		lines = append(lines, pad.Render(line))
		blank()
	}
	if h >= 2 {
		lines = append(lines, pad.Render(dividerStyle.Render(strings.Repeat("─", max(0, w)))))
		blank()
		lines = append(lines, pad.Render(" "+m.sidebarLegend()))
		blank()
	}

	for len(lines) < h {
		lines = append(lines, pad.Render(""))
		blank()
	}
	m.rowSessions = m.rowSessions[:min(len(m.rowSessions), h)]
	return lines[:h]
}

// headerLine is the top row: what this is and how many sessions on the left,
// which build it is on the right. A newer release is named beside it, since a
// message that fades is easy to miss.
func (m *Model) headerLine(w int) string {
	left := titleStyle.Render("BERTH") +
		itemMutedStyle.Render(fmt.Sprintf(" %d", len(m.sessions)))
	leftW := 5 + 1 + len(fmt.Sprintf("%d", len(m.sessions)))

	// The version reads two ways at once: an arrow and red mean there is a
	// newer release, and a "+" means this build is ahead of its tag. They
	// cannot both apply - a build from source is never told it is out of date
	// - so neither has to make room for the other.
	version := shortVersion(Version)
	style := faintStyle
	if m.newerVersion != "" {
		version = "↑" + version
		style = lipgloss.NewStyle().Foreground(colDanger)
	}
	right := style.Render(version)
	rightW := lipgloss.Width(version)

	gap := w - 1 - leftW - rightW
	if gap < 1 {
		// Too narrow for both; the count is the part worth keeping.
		return truncate(" "+left, w)
	}
	return " " + left + strings.Repeat(" ", gap) + right
}

// shortVersion keeps a version to something a sidebar can hold. A build with
// commits or changes on top of a tag is shown as that tag with a "+", which
// says it is ahead without spelling out the hash.
func shortVersion(v string) string {
	if v == "" {
		return "dev"
	}
	base, _, extra := strings.Cut(strings.TrimPrefix(v, "v"), "-")
	if base == "" || base == "dev" {
		return "dev"
	}
	if extra {
		return "v" + base + "+"
	}
	return "v" + base
}

// listRow is one rendered line of the session list, and the session it belongs
// to so a click on it can be resolved.
type listRow struct {
	session int
	text    string
}

// listRows renders every visible session, which is one row plus a second for
// the task when the agent inside is working on something we can name.
func (m *Model) listRows(visible []tmux.Session, w int) []listRow {
	rows := make([]listRow, 0, len(visible)+1)
	for i, s := range visible {
		selected := i == m.cursor
		rows = append(rows, listRow{session: i, text: m.sessionLine(s, selected, w)})
		if task := m.taskLine(s, selected, w); task != "" {
			rows = append(rows, listRow{session: i, text: task})
		}
	}
	return rows
}

// cursorRow finds the first row belonging to the selected session.
func cursorRow(rows []listRow, cursor int) int {
	for i, r := range rows {
		if r.session == cursor {
			return i
		}
	}
	return 0
}

// taskLine renders what the agent was last asked to do, indented under its
// session. It returns "" for a session that will never have one.
//
// An agent session keeps its row even when there is nothing to put in it. The
// row appearing and disappearing moves every session below it, which is a lot
// of movement to report that berth briefly does not know what something is
// working on.
func (m *Model) taskLine(s tmux.Session, selected bool, w int) string {
	if m.cfg.HideTask {
		return ""
	}
	kind := sessionKind(s)
	if kind != tmux.KindClaude && kind != tmux.KindCodex {
		return ""
	}

	info := m.agents[s.Name]
	text := info.Task
	if info.Status.NeedsInput() && info.Detail != "" {
		// What it is waiting for beats what it was asked - that is the thing
		// standing between you and the session continuing.
		text = info.Detail
	}
	if text == "" {
		// A blank row, not no row: the space is held so nothing below it moves.
		if selected {
			return itemSelectedStyle.Render(padTo("", w))
		}
		return strings.Repeat(" ", max(1, w))
	}

	// Indented to sit under the name rather than the status glyph.
	const indent = 3
	body := truncate(text, max(1, w-indent-1))
	if selected {
		return itemSelectedStyle.Render(padTo(strings.Repeat(" ", indent)+body, w))
	}
	return strings.Repeat(" ", indent) + faintStyle.Render(body)
}

// sessionLine renders one row: marker, status dot, name, and a right-aligned
// hint about what is running.
func (m *Model) sessionLine(s tmux.Session, selected bool, w int) string {
	// No marker for the selected row: the highlight already spans it, and an
	// arrow beside it only takes width from the name.
	const marker = " "
	dot, dotColor := m.statusDot(s)

	hint := s.Command
	if !s.Managed {
		hint = "ext"
	}
	if hint == "" {
		hint = sessionKind(s)
	}

	// Layout: space(1) + dot(1) + space(1) + name + gap + hint + space(1).
	const fixed = 4
	nameW := w - fixed - lipgloss.Width(hint) - 1
	if nameW < 6 {
		hint = ""
		nameW = w - fixed
	}
	name := truncate(s.Name, max(1, nameW))
	gap := max(1, w-fixed-lipgloss.Width(name)-lipgloss.Width(hint))

	if selected {
		// The highlight has to cover the full row, so pad before styling.
		return itemSelectedStyle.Render(
			padTo(marker+dot+" "+name+strings.Repeat(" ", gap)+hint, w))
	}
	return marker +
		lipgloss.NewStyle().Foreground(dotColor).Render(dot) + " " +
		lipgloss.NewStyle().Foreground(sessionNameColor(s)).Render(name) +
		strings.Repeat(" ", gap) +
		itemMutedStyle.Render(hint)
}

// statusDot picks the glyph in front of a session's name. For a session with
// an agent in it that is what the agent is doing; otherwise it falls back to
// whether tmux has a client attached.
//
// The three states are meant to be told apart at a glance and without colour:
// a spinner turning means work is happening, a question mark means the session
// is stuck until you answer it, and a hollow circle means neither.
func (m *Model) statusDot(s tmux.Session) (string, lipgloss.TerminalColor) {
	switch m.agents[s.Name].Status {
	case agent.Waiting:
		return "?", colDanger
	case agent.Busy, agent.Shell:
		return spinnerFrames[m.spinner%len(spinnerFrames)], workingColor(s)
	case agent.Idle:
		return "○", colIdle
	}

	kind := sessionKind(s)
	if s.Attached > 0 {
		return "●", kindColor(kind)
	}
	return "○", kindColor(kind)
}

func (m *Model) sidebarLegend() string {
	if m.focus == focusTerminal {
		return footerStyle.Render("typing goes to the session")
	}
	return footerStyle.Render("n new  x kill  ? help")
}

// scrollOffset keeps the given row inside the visible window.
func scrollOffset(row, total, height int) int {
	if height <= 0 || total <= height {
		return 0
	}
	offset := row - height/2
	if offset < 0 {
		offset = 0
	}
	if offset > total-height {
		offset = total - height
	}
	return offset
}

func sessionKind(s tmux.Session) string { return s.DetectedKind() }
