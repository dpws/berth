package ui

import (
	"fmt"
	"math"
	"strconv"

	"github.com/charmbracelet/lipgloss"
)

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
	// colIdle is the quiet end of the status colours: white on a dark
	// terminal, and dark on a light one so it stays legible either way.
	colIdle = lipgloss.AdaptiveColor{Light: "#3A3A3A", Dark: "#FFFFFF"}
	// colBranch names the branch in the git bar. Yellow is the one hue the
	// palette had not spent, so the branch reads as its own thing rather than
	// as another session kind.
	colBranch = lipgloss.AdaptiveColor{Light: "#8A6A1F", Dark: "#D8B863"}
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

// fadeColor blends c toward the terminal background, with t running 0 (as
// written) to 1 (gone). Both halves of the adaptive pair are faded toward
// their own background, so the caller does not have to know which one the
// terminal will pick.
func fadeColor(c lipgloss.AdaptiveColor, t float64) lipgloss.AdaptiveColor {
	if t <= 0 {
		return c
	}
	if t > 1 {
		t = 1
	}
	return lipgloss.AdaptiveColor{
		Light: blendHex(c.Light, "#FFFFFF", t),
		Dark:  blendHex(c.Dark, "#000000", t),
	}
}

// blendHex mixes two "#rrggbb" colors, returning from at t=0 and to at t=1.
func blendHex(from, to string, t float64) string {
	fr, fg, fb, ok := parseHex(from)
	if !ok {
		return from
	}
	tr, tg, tb, ok := parseHex(to)
	if !ok {
		return from
	}
	// math.Round, not a +0.5 truncation: the deltas here go negative when
	// fading toward black, and truncation would stop a channel one short of
	// the background instead of reaching it.
	mix := func(a, b int) int { return int(math.Round(float64(a) + float64(b-a)*t)) }
	return fmt.Sprintf("#%02X%02X%02X", mix(fr, tr), mix(fg, tg), mix(fb, tb))
}

// parseHex reads "#rrggbb".
func parseHex(s string) (r, g, b int, ok bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return int(v>>16) & 0xFF, int(v>>8) & 0xFF, int(v) & 0xFF, true
}

// spinnerFrames animates a working session. Braille dots read as motion at
// small sizes better than a rotating bar, which is why every agent CLI uses
// them.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

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
