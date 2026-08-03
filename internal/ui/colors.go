package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dpws/berth/internal/tmux"
)

// A session can be given a colour of its own, which marks its name in the list
// and the spinner it shows while working. Waiting and idle keep their own
// colours: those say something about the session's state, and a colour you
// chose says which session it is - the two should not be able to drown each
// other out.
//
// Colours are stored by name rather than as a value, so one choice reads
// correctly on a light terminal and a dark one.
type sessionColor struct {
	name  string
	color lipgloss.AdaptiveColor
}

// sessionColors is the palette, with the kind's own colour first so there is
// always a way back to no choice at all.
var sessionColors = []sessionColor{
	{"default", lipgloss.AdaptiveColor{}},
	{"red", lipgloss.AdaptiveColor{Light: "#B3261E", Dark: "#F2846B"}},
	{"orange", lipgloss.AdaptiveColor{Light: "#B35C00", Dark: "#F0A868"}},
	{"yellow", lipgloss.AdaptiveColor{Light: "#8A6D00", Dark: "#E3C567"}},
	{"green", lipgloss.AdaptiveColor{Light: "#2E7D32", Dark: "#8BC48F"}},
	{"teal", lipgloss.AdaptiveColor{Light: "#2E7D6F", Dark: "#79C8B4"}},
	{"blue", lipgloss.AdaptiveColor{Light: "#2F6F9F", Dark: "#7AA2D6"}},
	{"purple", lipgloss.AdaptiveColor{Light: "#6A4C9C", Dark: "#B79CE0"}},
	{"pink", lipgloss.AdaptiveColor{Light: "#A63D6B", Dark: "#E38FB4"}},
}

// colorNamed returns the palette entry for a name, and whether it is one.
// An unknown name - a tag written by hand, or from a later version - reads as
// no colour rather than as an error.
func colorNamed(name string) (lipgloss.TerminalColor, bool) {
	if name == "" {
		return nil, false
	}
	for _, c := range sessionColors {
		if c.name == name && c.name != "default" {
			return c.color, true
		}
	}
	return nil, false
}

// sessionNameColor is the colour a session's name is drawn in.
func sessionNameColor(s tmux.Session) lipgloss.TerminalColor {
	if c, ok := colorNamed(s.Color); ok {
		return c
	}
	return colText
}

// workingColor is the colour of the spinner on a working session.
func workingColor(s tmux.Session) lipgloss.TerminalColor {
	if c, ok := colorNamed(s.Color); ok {
		return c
	}
	return colSuccess
}

// cycleColor steps through the palette, wrapping in either direction. The
// empty string is the entry that means "no colour of its own".
func cycleColor(name string, delta int) string {
	at := 0
	for i, c := range sessionColors {
		if c.name == name {
			at = i
		}
	}
	next := sessionColors[(at+delta+len(sessionColors))%len(sessionColors)].name
	if next == "default" {
		return ""
	}
	return next
}

// colorChip renders a colour as a swatch and its name, for a form row that has
// no space for the whole palette.
func colorChip(name string) string {
	if name == "" {
		return itemMutedStyle.Render("── default")
	}
	c, ok := colorNamed(name)
	if !ok {
		return itemMutedStyle.Render("── " + name)
	}
	return lipgloss.NewStyle().Foreground(c).Render("●● " + name)
}

// openColors shows the palette for the selected session.
func (m *Model) openColors() tea.Cmd {
	s, ok := m.selected()
	if !ok {
		m.setStatus("no session to colour", true)
		return nil
	}
	m.mode = modeColor
	m.colorCursor = 0
	for i, c := range sessionColors {
		if c.name == s.Color {
			m.colorCursor = i
		}
	}
	return nil
}

// handleColorKey drives the palette.
func (m *Model) handleColorKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q", "c":
		m.mode = modeNormal
		return nil

	case "up", "k", "left", "h", "ctrl+p":
		m.colorCursor = (m.colorCursor - 1 + len(sessionColors)) % len(sessionColors)
	case "down", "j", "right", "l", "ctrl+n":
		m.colorCursor = (m.colorCursor + 1) % len(sessionColors)

	case "enter", " ":
		return m.applyColor()
	}
	return nil
}

// applyColor tags the selected session and asks tmux to remember it.
func (m *Model) applyColor() tea.Cmd {
	s, ok := m.selected()
	if !ok {
		m.mode = modeNormal
		return nil
	}
	name := sessionColors[m.colorCursor].name
	if name == "default" {
		name = ""
	}
	m.mode = modeNormal

	return func() tea.Msg {
		if err := tmux.SetColor(s.Name, name); err != nil {
			return errMsg{err}
		}
		if name == "" {
			return statusMsg{text: s.Name + " back to its usual colour"}
		}
		return statusMsg{text: s.Name + " is now " + name}
	}
}

// colorsView draws the palette, each row in the colour it offers.
func (m *Model) colorsView() string {
	name := ""
	if s, ok := m.selected(); ok {
		name = s.Name
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("Colour"))
	b.WriteString(itemMutedStyle.Render("  " + name))
	b.WriteString("\n\n")

	for i, c := range sessionColors {
		marker := "  "
		if i == m.colorCursor {
			marker = "▸ "
		}
		swatch := "●●●"
		label := c.name
		if c.name == "default" {
			swatch = "───"
			label = "default (by kind)"
		}

		row := marker + swatch + "  " + padTo(label, 18)
		if c.name == "default" {
			row = marker + itemMutedStyle.Render(swatch) + "  " +
				itemMutedStyle.Render(padTo(label, 18))
		} else {
			row = marker + lipgloss.NewStyle().Foreground(c.color).Render(swatch) +
				"  " + lipgloss.NewStyle().Foreground(c.color).Render(padTo(label, 18))
		}
		if i == m.colorCursor {
			row = itemSelectedStyle.Render(padTo(marker+swatch+"  "+label, 26))
		}
		b.WriteString(row)
		b.WriteByte('\n')
	}

	b.WriteString("\n")
	b.WriteString(joinHelp("↑/↓", "choose", "enter", "apply", "esc", "cancel"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		dialogStyle.Render(b.String()))
}
