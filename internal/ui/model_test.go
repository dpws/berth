package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/dpws/berth/internal/config"
	"github.com/dpws/berth/internal/term"
	"github.com/dpws/berth/internal/tmux"
	"github.com/muesli/termenv"
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
	if sideW+1+gutter+termW != 100 {
		t.Fatalf("columns do not add up: %d + 1 + %d + %d != 100",
			sideW, gutter, termW)
	}
	if termH != 28 {
		t.Fatalf("terminal height should leave the rule and the hotkeys room, got %d", termH)
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
		if got := m.commandFor(kind, tmux.StartNew); got != want {
			t.Errorf("commandFor(%q) = %q, want %q", kind, got, want)
		}
	}
}

// Resuming appends to the configured command rather than replacing it, so a
// command carrying options of its own keeps them.
func TestCommandForResumeModes(t *testing.T) {
	m := newTestModel()
	m.cfg.ClaudeCommand = "claude --model opus"
	m.cfg.ClaudeContinueArgs = "--continue"
	m.cfg.ClaudeResumeArgs = "--resume"
	m.cfg.CodexCommand = "codex"
	m.cfg.CodexContinueArgs = "resume --last"
	m.cfg.ShellCommand = "/bin/bash"

	cases := []struct {
		kind, start, want string
	}{
		{tmux.KindClaude, tmux.StartNew, "claude --model opus"},
		{tmux.KindClaude, tmux.StartContinue, "claude --model opus --continue"},
		{tmux.KindClaude, tmux.StartResume, "claude --model opus --resume"},
		{tmux.KindCodex, tmux.StartContinue, "codex resume --last"},
		// A shell has no conversation to carry on.
		{tmux.KindShell, tmux.StartResume, "/bin/bash"},
	}
	for _, c := range cases {
		if got := m.commandFor(c.kind, c.start); got != c.want {
			t.Errorf("commandFor(%q, %q) = %q, want %q", c.kind, c.start, got, c.want)
		}
	}

	// An empty args string means the mode adds nothing rather than a space.
	m.cfg.ClaudeResumeArgs = ""
	if got := m.commandFor(tmux.KindClaude, tmux.StartResume); got != "claude --model opus" {
		t.Errorf("with no resume args, got %q", got)
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

func TestQuitKeyWorksFromEitherHalf(t *testing.T) {
	for _, focus := range []focusArea{focusSidebar, focusTerminal} {
		m := newTestModel()
		m.Update(sessions("alpha"))
		m.focus = focus

		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
		if !m.quitting {
			t.Errorf("focus %v: ctrl+x did not quit", focus)
		}
		if cmd == nil {
			t.Errorf("focus %v: ctrl+x returned no command", focus)
		}
	}
}

func TestQuitKeyCanBeDisabled(t *testing.T) {
	m := newTestModel()
	m.cfg.QuitKey = ""
	m.Update(sessions("alpha"))
	m.focus = focusTerminal

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if m.quitting {
		t.Error("ctrl+x quit even though quit_key is empty")
	}
}

// A dialog owns the keyboard, so the quit key waits its turn behind esc rather
// than dropping the form out from under a half-typed name.
func TestQuitKeyDoesNotFireInsideADialog(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if m.mode != modeNew {
		t.Fatalf("n should open the new-session form, mode = %v", m.mode)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	if m.quitting {
		t.Error("ctrl+x quit from inside the new-session form")
	}
}

func TestWindowTitleFollowsTheSelection(t *testing.T) {
	m := newTestModel()
	if got := m.windowTitle(); got != "berth" {
		t.Errorf("with no sessions, title = %q, want %q", got, "berth")
	}

	m.Update(sessionsMsg([]tmux.Session{
		{Name: "api", Kind: tmux.KindClaude, Managed: true},
		{Name: "web", Kind: tmux.KindCodex, Managed: true},
	}))
	if got := m.windowTitle(); got != "api (claude) — berth" {
		t.Errorf("title = %q, want %q", got, "api (claude) — berth")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if got := m.windowTitle(); got != "web (codex) — berth" {
		t.Errorf("after moving, title = %q, want %q", got, "web (codex) — berth")
	}
}

// The title is rewritten on every message, so it has to stay quiet when
// nothing about the selection changed.
func TestTitleIsOnlySetWhenItChanges(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha", "bravo"))
	if m.title == "" {
		t.Fatal("title was never set")
	}

	if cmd := m.titleCmd(); cmd != nil {
		t.Error("titleCmd fired again for an unchanged title")
	}
	// A refresh that changes nothing must not touch the title either.
	m.Update(sessions("alpha", "bravo"))
	if cmd := m.titleCmd(); cmd != nil {
		t.Error("titleCmd fired after a no-op refresh")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if m.title != "bravo (shell) — berth" {
		t.Errorf("title = %q, want it to follow the cursor", m.title)
	}
}

func TestWindowTitleCanBeDisabled(t *testing.T) {
	m := New(config.Default())
	m.cfg.HideWindowTitle = true
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(sessions("alpha"))

	if cmd := m.titleCmd(); cmd != nil {
		t.Error("hide_window_title still set the terminal title")
	}
	if m.title != "" {
		t.Errorf("title recorded as %q, want it left alone", m.title)
	}
}

// The title is written as an OSC escape sequence, so a session berth did not
// create must not be able to smuggle control characters into it.
func TestWindowTitleStripsControlCharacters(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsMsg([]tmux.Session{
		{Name: "ok\x1b]0;pwned\x07\x0aname", Kind: tmux.KindShell},
	}))

	title := m.windowTitle()
	for _, r := range title {
		if r < 0x20 || r == 0x7f {
			t.Fatalf("title carries control character %q: %q", r, title)
		}
	}
	// The printable remains are harmless: without the ESC that opens a sequence
	// and the BEL that closes one, "]0;" is just text.
	if !strings.HasPrefix(title, "ok") || !strings.Contains(title, "name") {
		t.Errorf("title = %q, want the printable characters kept", title)
	}
}

func TestWindowTitleBoundsLongNames(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsMsg([]tmux.Session{
		{Name: strings.Repeat("x", 400), Kind: tmux.KindShell},
	}))

	if got := len([]rune(m.windowTitle())); got > titleNameMax+32 {
		t.Errorf("title is %d runes, want it bounded", got)
	}
}

// Capturing the mouse is what stops the terminal doing its own selection, so
// it has to be releasable without a restart.
func TestMouseCanBeToggledAtRuntime(t *testing.T) {
	m := newTestModel()
	if !m.mouseOn {
		t.Fatal("mouse should start on with the default config")
	}

	m.Update(sessions("alpha"))
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if m.mouseOn {
		t.Error("m did not release the mouse")
	}
	if !strings.Contains(m.status, "select") {
		t.Errorf("status = %q, want it to say selection is back", m.status)
	}

	// While released, stray events are ignored rather than acted on.
	before := m.cursor
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if m.cursor != before {
		t.Error("a mouse event moved the cursor after the mouse was released")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if !m.mouseOn {
		t.Error("m did not take the mouse back")
	}
}

func TestMouseStartsOffWhenConfigured(t *testing.T) {
	cfg := config.Default()
	cfg.Mouse = false
	m := New(cfg)
	if m.mouseOn {
		t.Error("mouse should start off when the config disables it")
	}
}

// withPane attaches the model to a real tmux session, since mouse events over
// the terminal half are ignored when nothing is attached - there is nothing
// there to select or to forward to.
func withPane(t *testing.T, m *Model) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not on PATH")
	}
	name, err := tmux.New(tmux.NewOptions{
		Name:    fmt.Sprintf("berth-uitest-%d", os.Getpid()),
		Kind:    tmux.KindShell,
		Command: "sh",
	})
	if err != nil {
		t.Skipf("tmux new-session: %v", err)
	}
	t.Cleanup(func() { _ = tmux.Kill(name) })

	pane, err := term.Attach(name, 40, 10)
	if err != nil {
		t.Skipf("attach: %v", err)
	}
	t.Cleanup(func() { _ = pane.Close() })
	m.pane = pane
}

// terminalPress builds a mouse event over the terminal half of the screen.
func terminalMouse(m *Model, x, y int, action tea.MouseAction, button tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{X: m.sidebarWidth() + 1 + gutter + x, Y: y, Action: action, Button: button}
}

// A drag over the session selects text; berth has the mouse, and the terminal
// would otherwise select straight across the sidebar.
func TestDragOverTheSessionSelects(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	withPane(t, m)

	m.Update(terminalMouse(m, 2, 1, tea.MouseActionPress, tea.MouseButtonLeft))
	if m.selection() != nil {
		t.Error("a press alone should not select anything yet")
	}
	m.Update(terminalMouse(m, 9, 3, tea.MouseActionMotion, tea.MouseButtonLeft))

	sel := m.selection()
	if sel == nil {
		t.Fatal("dragging did not start a selection")
	}
	if sel.AnchorX != 2 || sel.AnchorY != 1 || sel.CursorX != 9 || sel.CursorY != 3 {
		t.Errorf("selection = %+v, want it to span the drag", *sel)
	}
}

// A click is not a drag, and still belongs to the program in the session.
func TestClickOverTheSessionIsNotASelection(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	withPane(t, m)

	m.Update(terminalMouse(m, 4, 2, tea.MouseActionPress, tea.MouseButtonLeft))
	m.Update(terminalMouse(m, 4, 2, tea.MouseActionRelease, tea.MouseButtonLeft))

	if m.selection() != nil {
		t.Error("a click left a selection behind")
	}
	if m.focus != focusTerminal {
		t.Error("a click should hand the keyboard to the session")
	}
}

// Typing moves the text the highlight was drawn over, so it has to go.
func TestSelectionClearsOnInput(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	withPane(t, m)
	m.focus = focusTerminal

	m.Update(terminalMouse(m, 1, 1, tea.MouseActionPress, tea.MouseButtonLeft))
	m.Update(terminalMouse(m, 8, 1, tea.MouseActionMotion, tea.MouseButtonLeft))
	if m.selection() == nil {
		t.Fatal("no selection to clear")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if m.selection() != nil {
		t.Error("typing into the session left the highlight behind")
	}
}

// The wheel is the session's, selection or not.
func TestWheelIsNotASelection(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	withPane(t, m)
	m.Update(terminalMouse(m, 3, 3, tea.MouseActionPress, tea.MouseButtonWheelDown))
	if m.selection() != nil {
		t.Error("the wheel started a selection")
	}
}

func TestBlendHex(t *testing.T) {
	cases := []struct {
		from, to string
		t        float64
		want     string
	}{
		{"#000000", "#FFFFFF", 0, "#000000"},
		{"#000000", "#FFFFFF", 1, "#FFFFFF"},
		{"#000000", "#FFFFFF", 0.5, "#808080"},
		{"#B3261E", "#FFFFFF", 0, "#B3261E"},
		{"#204080", "#000000", 0.5, "#102040"},
	}
	for _, c := range cases {
		if got := blendHex(c.from, c.to, c.t); got != c.want {
			t.Errorf("blendHex(%s,%s,%v) = %s, want %s", c.from, c.to, c.t, got, c.want)
		}
	}
	// Anything unparseable is returned untouched rather than turned to mush.
	if got := blendHex("red", "#FFFFFF", 0.5); got != "red" {
		t.Errorf("blendHex on a non-hex colour = %q", got)
	}
}

// Each half of the pair fades toward its own background, so the caller does
// not need to know which the terminal will use.
func TestFadeColorMovesTowardBothBackgrounds(t *testing.T) {
	c := colDanger
	if got := fadeColor(c, 0); got != c {
		t.Errorf("fadeColor at t=0 changed the colour: %+v", got)
	}
	gone := fadeColor(c, 1)
	if gone.Light != "#FFFFFF" || gone.Dark != "#000000" {
		t.Errorf("fadeColor at t=1 = %+v, want the two backgrounds", gone)
	}
	// Past the end is clamped, not extrapolated into nonsense.
	if fadeColor(c, 5) != gone {
		t.Error("fadeColor did not clamp beyond t=1")
	}
}

func TestStatusHoldsThenFades(t *testing.T) {
	m := newTestModel()
	m.setStatus("created api", false)

	if got := m.statusLife(); got != 0 {
		t.Errorf("a fresh message is already fading: %v", got)
	}
	m.statusAt = time.Now().Add(-statusHold - statusFade/2)
	if got := m.statusLife(); got <= 0 || got >= 1 {
		t.Errorf("mid-fade life = %v, want between 0 and 1", got)
	}
	m.statusAt = time.Now().Add(-statusHold - statusFade)
	if got := m.statusLife(); got < 1 {
		t.Errorf("life = %v, want the message spent", got)
	}
}

// An error is worth more than a confirmation, so it stays up longer.
func TestErrorsHoldLongerThanNotices(t *testing.T) {
	m := newTestModel()
	m.setStatus("boom", true)
	m.statusAt = time.Now().Add(-statusHold - statusFade)
	if got := m.statusLife(); got != 0 {
		t.Errorf("an error faded on the notice schedule: life = %v", got)
	}
}

func TestStatusClearsItselfOnceSpent(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	m.setStatus("created api", false)

	m.Update(statusTickMsg{})
	if m.status == "" {
		t.Fatal("the message went before it had been read")
	}

	m.statusAt = time.Now().Add(-statusErrorHold - statusFade)
	m.Update(statusTickMsg{})
	if m.status != "" {
		t.Errorf("status = %q, want it cleared once spent", m.status)
	}
	// With the footer quiet again, nothing should keep ticking.
	if cmd := m.statusCmd(); cmd != nil {
		t.Error("a redraw was scheduled with no message to fade")
	}
}

// A newer message restarts the clock rather than inheriting the old one's age.
func TestNewMessageResetsTheClock(t *testing.T) {
	m := newTestModel()
	m.setStatus("first", false)
	m.statusAt = time.Now().Add(-statusHold)

	m.setStatus("second", false)
	if got := m.statusLife(); got != 0 {
		t.Errorf("life = %v, want the clock restarted", got)
	}
}

// One chain of redraws, however many messages arrive during it.
func TestOnlyOneFadeChainRuns(t *testing.T) {
	m := newTestModel()
	m.setStatus("first", false)

	if cmd := m.statusCmd(); cmd == nil {
		t.Fatal("no redraw scheduled for a new message")
	}
	if cmd := m.statusCmd(); cmd != nil {
		t.Error("a second redraw chain started while one was already running")
	}
}

// The footer must never be wider than the screen: it wraps if it is, and a
// wrapped footer pushes the whole frame up off the top.
func TestFooterNeverOverflows(t *testing.T) {
	for _, w := range []int{200, 100, 80, 64, 40, 20, 10} {
		m := newTestModel()
		m.Update(tea.WindowSizeMsg{Width: w, Height: 20})
		m.Update(sessions("a-session-with-a-long-name"))

		for _, focus := range []focusArea{focusSidebar, focusTerminal} {
			m.focus = focus
			if got := ansi.StringWidth(m.footerView()); got != w {
				t.Errorf("width %d focus %v: footer is %d cells", w, focus, got)
			}
		}

		m.setStatus(strings.Repeat("a long message that will not fit ", 8), true)
		if got := ansi.StringWidth(m.footerView()); got != w {
			t.Errorf("width %d: a long status made the footer %d cells", w, got)
		}
	}
}

// Every row of the frame has to be exactly the width of the screen, or the
// terminal wraps one and the layout slides.
func TestFrameRowsAreExactlyTheScreenWidth(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 72, Height: 14})
	m.Update(sessions("alpha", "bravo"))

	rows := strings.Split(m.View(), "\n")
	if len(rows) != 14 {
		t.Fatalf("frame is %d rows, want 14", len(rows))
	}
	for i, r := range rows {
		if got := ansi.StringWidth(r); got != 72 {
			t.Errorf("row %d is %d cells wide: %q", i, got, ansi.Strip(r))
		}
	}
}

// withColour makes lipgloss emit escape codes in a test, which it does not do
// by default because nothing here is a terminal. Without it, assertions about
// what is highlighted pass whether or not anything is.
func withColour(t *testing.T) {
	t.Helper()
	before := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(before) })
}

// The lit half of the rule under the columns answers the same question as the
// words below it - where does typing go - and answers it faster.
func TestFooterRuleLightsTheFocusedHalf(t *testing.T) {
	withColour(t)
	m := newTestModel()
	m.Update(sessions("alpha"))
	sideW := m.sidebarWidth()

	left := strings.Repeat("─", sideW)
	right := strings.Repeat("─", m.width-sideW-1)

	m.focus = focusSidebar
	rule := m.footerRule(sideW)
	if !strings.Contains(rule, focusedDivStyle.Render(left)) {
		t.Error("the list has the keyboard but its half of the rule is not lit")
	}
	if !strings.Contains(rule, dividerStyle.Render(right)) {
		t.Error("the session's half should be dim while the list has the keyboard")
	}

	m.focus = focusTerminal
	rule = m.footerRule(sideW)
	if !strings.Contains(rule, focusedDivStyle.Render(right)) {
		t.Error("the session has the keyboard but its half of the rule is not lit")
	}
	if !strings.Contains(rule, dividerStyle.Render(left)) {
		t.Error("the list's half should be dim while the session has the keyboard")
	}

	// A window too narrow to hold both columns still draws a rule.
	m.width = 4
	if got := ansi.StringWidth(m.footerRule(0)); got != 4 {
		t.Errorf("degenerate rule is %d cells, want 4", got)
	}
}

// The session's own output should not start hard against the divider.
func TestGutterSeparatesTheDividerFromTheSession(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))

	sideW := m.sidebarWidth()
	termW, _ := m.terminalSize()
	if sideW+1+gutter+termW != m.width {
		t.Errorf("%d + 1 + %d + %d != %d", sideW, gutter, termW, m.width)
	}

	// A click landing in the gutter belongs to neither half.
	before := selectedName(t, m)
	m.Update(tea.MouseMsg{
		X: sideW + 1, Y: 3,
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	if got := selectedName(t, m); got != before {
		t.Errorf("a click in the gutter changed the selection to %q", got)
	}
	if m.focus != focusSidebar {
		t.Error("a click in the gutter moved the focus")
	}
}
