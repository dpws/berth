package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
	m.Update(key(tea.KeyUp))
	if m.colorCursor != len(sessionColors)-1 {
		t.Errorf("cursor = %d, want it wrapped to the end", m.colorCursor)
	}
	m.Update(key(tea.KeyDown))
	if m.colorCursor != 0 {
		t.Errorf("cursor = %d, want it wrapped back", m.colorCursor)
	}

	m.Update(key(tea.KeyEscape))
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
	if got := m.brandBar(28); !strings.Contains(got, faintStyle.Render("v0.3.0")) {
		t.Error("an up-to-date version is not drawn quietly")
	}

	m.newerVersion = "v0.4.0"
	behind := lipgloss.NewStyle().Foreground(colDanger).Render("↑v0.3.0")
	if got := m.brandBar(28); !strings.Contains(got, behind) {
		t.Error("a version behind a release is not drawn in red with an arrow")
	}

	// The two markers cannot collide: update.Available says nothing to a build
	// from source, so a "+" version never also carries an arrow.
	Version = "v0.3.0-9-gabc-dirty"
	m.newerVersion = ""
	if got := ansi.Strip(m.brandBar(28)); !strings.Contains(got, "v0.3.0+") {
		t.Errorf("header = %q, want the ahead-of-tag marker", got)
	}
}

// Reading the background out of the environment is what keeps lipgloss from
// asking the terminal and waiting five seconds for an answer Bubble Tea has
// already read. The numbers are ANSI colours: 0-6 and 8 are dark, 7 and 9-15
// are light.
func TestDarkBackgroundFromTheEnvironment(t *testing.T) {
	for _, tc := range []struct {
		fgbg string
		want bool
		why  string
	}{
		{"15;0", true, "white on black"},
		{"0;15", false, "black on white"},
		{"7;0", true, "grey on black"},
		{"0;7", false, "black on grey"},
		{"15;8", true, "8 is the bright black, still dark"},
		{"15;9", false, "9 upwards is a bright colour, treated as light"},

		// Some terminals write three fields, foreground;cursor;background.
		{"15;default;0", true, "three fields, dark"},
		{"0;default;15", false, "three fields, light"},

		// Anything berth cannot read is taken as dark, which is what a terminal
		// running a coding agent overwhelmingly is.
		{"", true, "unset"},
		{"15", true, "no background field"},
		{"15;", true, "empty background field"},
		{"15;grey", true, "not a number"},
	} {
		if got := darkBackground(tc.fgbg); got != tc.want {
			t.Errorf("darkBackground(%q) = %v, want %v (%s)", tc.fgbg, got, tc.want, tc.why)
		}
	}
}

// The whole point of settling them up front is that it happens before a frame
// is drawn, so it must not depend on a terminal being there to answer.
func TestSettleStylesNeedsNoTerminal(t *testing.T) {
	t.Setenv("COLORFGBG", "15;0")
	done := make(chan struct{})
	go func() { SettleStyles(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SettleStyles blocked; it is meant to ask nobody anything")
	}
	if !lipgloss.HasDarkBackground() {
		t.Error("a dark COLORFGBG did not reach lipgloss")
	}
}
