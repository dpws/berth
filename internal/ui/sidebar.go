package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/dpws/berth/internal/tmux"
)

// sidebarLines renders the session list as exactly h lines, each exactly w
// cells wide.
func (m *Model) sidebarLines(w, h int) []string {
	lines := make([]string, 0, h)
	pad := lipgloss.NewStyle().Width(w).MaxWidth(w)

	// rowSessions records which session each rendered row belongs to, so mouse
	// clicks can be resolved without duplicating this layout arithmetic.
	m.rowSessions = make([]int, 0, h)
	blank := func() { m.rowSessions = append(m.rowSessions, -1) }

	header := titleStyle.Render("BERTH")
	count := itemMutedStyle.Render(fmt.Sprintf(" %d", len(m.sessions)))
	lines = append(lines, pad.Render(" "+header+count))
	blank()
	lines = append(lines, pad.Render(dividerStyle.Render(strings.Repeat("─", max(0, w)))))
	blank()

	if m.filter != "" {
		lines = append(lines, pad.Render(" "+labelActiveStyle.Render("/")+itemStyle.Render(m.filter)))
		blank()
	}

	// Reserve the last two rows for the legend.
	listHeight := h - len(lines) - 2
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
		start := m.scrollOffset(len(visible), listHeight)
		for i := start; i < len(visible) && len(lines) < h-2; i++ {
			lines = append(lines, pad.Render(m.sessionLine(visible[i], i == m.cursor, w)))
			m.rowSessions = append(m.rowSessions, i)
		}
	}

	for len(lines) < h-2 {
		lines = append(lines, pad.Render(""))
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

// sessionLine renders one row: marker, status dot, name, and a right-aligned
// hint about what is running.
func (m *Model) sessionLine(s tmux.Session, selected bool, w int) string {
	marker := "  "
	if selected {
		marker = "▸ "
	}
	dot := "○"
	if s.Attached > 0 {
		dot = "●"
	}

	hint := s.Command
	if !s.Managed {
		hint = "ext"
	}
	if hint == "" {
		hint = sessionKind(s)
	}

	// Layout: marker(2) + dot(1) + space(1) + name + gap + hint + space(1).
	const fixed = 5
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
		lipgloss.NewStyle().Foreground(kindColor(sessionKind(s))).Render(dot) + " " +
		itemStyle.Render(name) +
		strings.Repeat(" ", gap) +
		itemMutedStyle.Render(hint)
}

func (m *Model) sidebarLegend() string {
	if m.focus == focusTerminal {
		return footerStyle.Render("terminal has keys")
	}
	return footerStyle.Render("n new  x kill  ? help")
}

// scrollOffset keeps the cursor inside the visible window.
func (m *Model) scrollOffset(total, height int) int {
	if height <= 0 || total <= height {
		return 0
	}
	offset := m.cursor - height/2
	if offset < 0 {
		offset = 0
	}
	if offset > total-height {
		offset = total - height
	}
	return offset
}

func sessionKind(s tmux.Session) string { return s.DetectedKind() }
