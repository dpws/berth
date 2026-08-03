package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/dpws/berth/internal/agent"
	"github.com/dpws/berth/internal/tmux"
)

func TestColorNamed(t *testing.T) {
	if _, ok := colorNamed("purple"); !ok {
		t.Error("purple is in the palette but did not resolve")
	}
	// No colour chosen, and the entry that means exactly that.
	for _, name := range []string{"", "default"} {
		if _, ok := colorNamed(name); ok {
			t.Errorf("%q resolved to a colour", name)
		}
	}
	// A tag from a later version, or written by hand, is not an error.
	if _, ok := colorNamed("chartreuse"); ok {
		t.Error("an unknown colour name resolved")
	}
}

func TestSessionColourAppliesToNameAndSpinner(t *testing.T) {
	plain := tmux.Session{Name: "api", Kind: tmux.KindClaude}
	coloured := tmux.Session{Name: "api", Kind: tmux.KindClaude, Color: "purple"}
	purple, _ := colorNamed("purple")

	if got := sessionNameColor(coloured); got != purple {
		t.Errorf("name colour = %v, want the session's", got)
	}
	if got := sessionNameColor(plain); got != colText {
		t.Errorf("an uncoloured session's name = %v, want the usual", got)
	}
	if got := workingColor(coloured); got != purple {
		t.Errorf("spinner colour = %v, want the session's", got)
	}
	if got := workingColor(plain); got != colSuccess {
		t.Errorf("an uncoloured spinner = %v, want green", got)
	}
}

// A chosen colour says which session this is; waiting and idle say what it is
// doing. The first must not be able to drown out the second.
func TestStateColoursSurviveASessionColour(t *testing.T) {
	m := newTestModel()
	s := tmux.Session{Name: "api", Kind: tmux.KindClaude, Color: "purple"}

	withAgents(m, map[string]agent.Info{"api": {Status: agent.Waiting}})
	if _, got := m.statusDot(s); got != colDanger {
		t.Errorf("waiting drew %v, want it left red", got)
	}
	withAgents(m, map[string]agent.Info{"api": {Status: agent.Idle}})
	if _, got := m.statusDot(s); got != colIdle {
		t.Errorf("idle drew %v, want it left alone", got)
	}

	purple, _ := colorNamed("purple")
	withAgents(m, map[string]agent.Info{"api": {Status: agent.Busy}})
	if _, got := m.statusDot(s); got != purple {
		t.Errorf("working drew %v, want the session's colour", got)
	}
}

func TestPaletteOpensOnTheCurrentColour(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsMsg([]tmux.Session{
		{Name: "api", Kind: tmux.KindClaude, Managed: true, Color: "teal"},
	}))

	m.Update(keyRune('c'))
	if m.mode != modeColor {
		t.Fatal("c did not open the palette")
	}
	if sessionColors[m.colorCursor].name != "teal" {
		t.Errorf("cursor on %q, want the session's current colour",
			sessionColors[m.colorCursor].name)
	}

	// It wraps rather than stopping at the ends.
	m.colorCursor = 0
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.colorCursor != len(sessionColors)-1 {
		t.Errorf("cursor = %d, want it wrapped to the end", m.colorCursor)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.colorCursor != 0 {
		t.Errorf("cursor = %d, want it wrapped back", m.colorCursor)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeNormal {
		t.Error("esc did not close the palette")
	}
}

// Every entry needs both halves of the pair, or a session coloured on a dark
// terminal turns invisible on a light one.
func TestPaletteIsAdaptive(t *testing.T) {
	for _, c := range sessionColors {
		if c.name == "default" {
			continue
		}
		if c.color.Light == "" || c.color.Dark == "" {
			t.Errorf("%s has no colour for one background: %+v", c.name, c.color)
		}
		if !strings.HasPrefix(c.color.Light, "#") || !strings.HasPrefix(c.color.Dark, "#") {
			t.Errorf("%s is not a hex pair: %+v", c.name, c.color)
		}
	}
}

// The header's version says two different things, and they must not be
// mistaken for each other: red with an arrow means a newer release exists, a
// "+" means this build is ahead of its tag. A build from source is never told
// it is out of date, so the two can never appear at once.
func TestHeaderVersionMarkers(t *testing.T) {
	withColour(t)
	m := newTestModel()
	m.Update(sessions("api"))
	before := Version
	t.Cleanup(func() { Version = before })

	Version = "v0.3.0"
	if got := m.headerLine(28); !strings.Contains(got, faintStyle.Render("v0.3.0")) {
		t.Error("an up-to-date version is not drawn quietly")
	}

	m.newerVersion = "v0.4.0"
	behind := lipgloss.NewStyle().Foreground(colDanger).Render("↑v0.3.0")
	if got := m.headerLine(28); !strings.Contains(got, behind) {
		t.Error("a version behind a release is not drawn in red with an arrow")
	}

	// The two markers cannot collide: update.Available says nothing to a build
	// from source, so a "+" version never also carries an arrow.
	Version = "v0.3.0-9-gabc-dirty"
	m.newerVersion = ""
	if got := ansi.Strip(m.headerLine(28)); !strings.Contains(got, "v0.3.0+") {
		t.Errorf("header = %q, want the ahead-of-tag marker", got)
	}
}
