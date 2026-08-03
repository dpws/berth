package ui

import "github.com/charmbracelet/lipgloss"

// Palette. Kept small on purpose: the session list is chrome around someone
// else's terminal output, so it should stay quiet.
var (
	colClaude  = lipgloss.AdaptiveColor{Light: "#C1502E", Dark: "#D97757"}
	colCodex   = lipgloss.AdaptiveColor{Light: "#2E7D6F", Dark: "#79C8B4"}
	colShell   = lipgloss.AdaptiveColor{Light: "#2F6F9F", Dark: "#7AA2D6"}
	colMuted   = lipgloss.AdaptiveColor{Light: "#6C6C6C", Dark: "#8A8A8A"}
	colFaint   = lipgloss.AdaptiveColor{Light: "#9A9A9A", Dark: "#5F5F5F"}
	colText    = lipgloss.AdaptiveColor{Light: "#1C1C1C", Dark: "#E4E4E4"}
	colSelBg   = lipgloss.AdaptiveColor{Light: "#DDE6F2", Dark: "#2A3040"}
	colBorder  = lipgloss.AdaptiveColor{Light: "#C8C8C8", Dark: "#3A3A3A"}
	colFocus   = lipgloss.AdaptiveColor{Light: "#2F6F9F", Dark: "#7AA2D6"}
	colDanger  = lipgloss.AdaptiveColor{Light: "#B3261E", Dark: "#F2846B"}
	colSuccess = lipgloss.AdaptiveColor{Light: "#2E7D32", Dark: "#8BC48F"}
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(colText)

	itemStyle         = lipgloss.NewStyle().Foreground(colText)
	itemSelectedStyle = lipgloss.NewStyle().Foreground(colText).Background(colSelBg).Bold(true)
	itemMutedStyle    = lipgloss.NewStyle().Foreground(colMuted)

	dividerStyle    = lipgloss.NewStyle().Foreground(colBorder)
	focusedDivStyle = lipgloss.NewStyle().Foreground(colFocus)

	footerStyle    = lipgloss.NewStyle().Foreground(colMuted)
	footerKeyStyle = lipgloss.NewStyle().Foreground(colText).Bold(true)

	errorStyle   = lipgloss.NewStyle().Foreground(colDanger)
	successStyle = lipgloss.NewStyle().Foreground(colSuccess)
	faintStyle   = lipgloss.NewStyle().Foreground(colFaint)

	dialogStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colFocus).
			Padding(1, 2)

	labelStyle       = lipgloss.NewStyle().Foreground(colMuted)
	labelActiveStyle = lipgloss.NewStyle().Foreground(colFocus).Bold(true)

	chipStyle       = lipgloss.NewStyle().Padding(0, 1).Foreground(colMuted)
	chipActiveStyle = lipgloss.NewStyle().Padding(0, 1).Foreground(colText).Background(colSelBg).Bold(true)
)

func kindColor(kind string) lipgloss.TerminalColor {
	switch kind {
	case "claude":
		return colClaude
	case "codex":
		return colCodex
	default:
		return colShell
	}
}
