package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dpws/berth/internal/config"
	"github.com/dpws/berth/internal/tmux"
)

func newTestModel() *Model {
	m := New(config.Default())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return m
}

func sessions(names ...string) sessionsMsg {
	out := make([]tmux.Session, 0, len(names))
	for _, n := range names {
		out = append(out, tmux.Session{Name: n, Kind: tmux.KindShell, Managed: true})
	}
	return sessionsMsg(out)
}

func selectedName(t *testing.T, m *Model) string {
	t.Helper()
	s, ok := m.selected()
	if !ok {
		return ""
	}
	return s.Name
}

func TestCreatedSessionBecomesSelected(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	if got := selectedName(t, m); got != "alpha" {
		t.Fatalf("first session should be selected, got %q", got)
	}

	m.Update(statusMsg{text: "created bravo", selectName: "bravo"})
	m.Update(sessions("alpha", "bravo"))

	if got := selectedName(t, m); got != "bravo" {
		t.Fatalf("newly created session should be selected, got %q", got)
	}
}

func TestSelectionSurvivesRefresh(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha", "bravo", "charlie"))
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got := selectedName(t, m); got != "bravo" {
		t.Fatalf("j should move to bravo, got %q", got)
	}

	// A session sorted before the cursor disappears: the cursor must follow
	// the name, not the index.
	m.Update(sessions("bravo", "charlie"))
	if got := selectedName(t, m); got != "bravo" {
		t.Fatalf("selection should stay on bravo, got %q", got)
	}
}

func TestFilterNarrowsAndClamps(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha", "bravo", "charlie"))
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if got := selectedName(t, m); got != "charlie" {
		t.Fatalf("G should jump to the last session, got %q", got)
	}

	m.filter = "bra"
	m.clampCursor()
	if got := selectedName(t, m); got != "bravo" {
		t.Fatalf("filter should clamp the cursor onto bravo, got %q", got)
	}
}

func TestSidebarAndTerminalFillTheScreen(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))

	sideW := m.sidebarWidth()
	termW, termH := m.terminalSize()
	if sideW+1+termW != 100 {
		t.Fatalf("columns do not add up: %d + 1 + %d != 100", sideW, termW)
	}
	if termH != 29 {
		t.Fatalf("terminal height should leave one row for the footer, got %d", termH)
	}

	lines := m.sidebarLines(sideW, m.bodyHeight())
	if len(lines) != m.bodyHeight() {
		t.Fatalf("sidebar rendered %d lines, want %d", len(lines), m.bodyHeight())
	}
}

func TestMouseSelectsSidebarRow(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha", "bravo", "charlie"))

	// View populates the row-to-session mapping that click handling relies on.
	m.View()

	row := -1
	for y, idx := range m.rowSessions {
		if idx == 2 {
			row = y
		}
	}
	if row < 0 {
		t.Fatal("charlie was not rendered on any row")
	}

	m.Update(tea.MouseMsg{
		X:      2,
		Y:      row,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if got := selectedName(t, m); got != "charlie" {
		t.Fatalf("clicking charlie's row selected %q", got)
	}
}

func TestMouseWheelScrollsTheList(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha", "bravo", "charlie"))

	wheel := func(button tea.MouseButton) {
		m.Update(tea.MouseMsg{X: 2, Y: 3, Action: tea.MouseActionPress, Button: button})
	}
	wheel(tea.MouseButtonWheelDown)
	if got := selectedName(t, m); got != "bravo" {
		t.Fatalf("wheel down selected %q, want bravo", got)
	}
	wheel(tea.MouseButtonWheelUp)
	if got := selectedName(t, m); got != "alpha" {
		t.Fatalf("wheel up selected %q, want alpha", got)
	}
}

// Clicks in the sidebar must never be treated as clicks in the session, and
// vice versa - the divider column belongs to neither.
func TestMouseIgnoresTheDividerColumn(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha", "bravo"))
	m.View()

	before := selectedName(t, m)
	m.Update(tea.MouseMsg{
		X:      m.sidebarWidth(),
		Y:      3,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	})
	if got := selectedName(t, m); got != before {
		t.Fatalf("a click on the divider changed the selection to %q", got)
	}
	if m.focus != focusSidebar {
		t.Error("a click on the divider should not move focus")
	}
}

func TestKindCyclesThroughEveryKind(t *testing.T) {
	seen := map[string]bool{}
	kind := tmux.KindClaude
	for range len(tmux.Kinds) {
		seen[kind] = true
		kind = cycleKind(kind, 1)
	}
	if kind != tmux.KindClaude {
		t.Errorf("cycling should wrap back to the first kind, ended on %q", kind)
	}
	for _, want := range tmux.Kinds {
		if !seen[want] {
			t.Errorf("cycling never reached %q", want)
		}
	}
	if got := cycleKind(tmux.KindClaude, -1); got != tmux.Kinds[len(tmux.Kinds)-1] {
		t.Errorf("cycling backwards from the first kind gave %q", got)
	}
}

func TestCommandPerKind(t *testing.T) {
	m := newTestModel()
	m.cfg.ClaudeCommand = "claude --model opus"
	m.cfg.CodexCommand = "codex"
	m.cfg.ShellCommand = "/bin/bash"

	cases := map[string]string{
		tmux.KindClaude: "claude --model opus",
		tmux.KindCodex:  "codex",
		tmux.KindShell:  "/bin/bash",
	}
	for kind, want := range cases {
		if got := m.commandFor(kind); got != want {
			t.Errorf("commandFor(%q) = %q, want %q", kind, got, want)
		}
	}
}

func TestExternalSessionKindIsSniffedFromTheCommand(t *testing.T) {
	cases := map[string]string{
		"claude": tmux.KindClaude,
		"codex":  tmux.KindCodex,
		"bash":   tmux.KindShell,
		"node":   tmux.KindShell,
	}
	for command, want := range cases {
		s := tmux.Session{Name: "x", Command: command}
		if got := sessionKind(s); got != want {
			t.Errorf("a session running %q was classed as %q, want %q", command, got, want)
		}
	}
}
