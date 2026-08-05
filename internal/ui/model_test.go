package ui

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/dpws/berth/internal/agent"
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
	m.Update(typed("j"))
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
	m.Update(typed("G"))
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
	// 30 rows, less the rule and the hotkey line under the body, and the bar
	// and its own rule over it.
	if termH != 26 {
		t.Fatalf("terminal height should leave the rule and the hotkeys room, got %d", termH)
	}
	if m.topBarHeight()+termH+2 != 30 {
		t.Fatalf("the rows do not add up: bar %d + body %d + footer 2 != 30",
			m.topBarHeight(), termH)
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

	m.Update(click(2, row+m.topBarHeight())) // rowSessions is indexed from below the bar
	if got := selectedName(t, m); got != "charlie" {
		t.Fatalf("clicking charlie's row selected %q", got)
	}
}

func TestMouseWheelScrollsTheList(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha", "bravo", "charlie"))

	wheel := func(button tea.MouseButton) {
		m.Update(wheelAt(2, 3, button))
	}
	wheel(tea.MouseWheelDown)
	if got := selectedName(t, m); got != "bravo" {
		t.Fatalf("wheel down selected %q, want bravo", got)
	}
	wheel(tea.MouseWheelUp)
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
	m.Update(click(m.sidebarWidth(), 3))
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

		_, cmd := m.Update(key('x', tea.ModCtrl))
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

	m.Update(key('x', tea.ModCtrl))
	if m.quitting {
		t.Error("ctrl+x quit even though quit_key is empty")
	}
}

// A dialog owns the keyboard, so the quit key waits its turn behind esc rather
// than dropping the form out from under a half-typed name.
func TestQuitKeyDoesNotFireInsideADialog(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	m.Update(typed("n"))
	if m.mode != modeNew {
		t.Fatalf("n should open the new-session form, mode = %v", m.mode)
	}

	m.Update(key('x', tea.ModCtrl))
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

	m.Update(typed("j"))
	if got := m.windowTitle(); got != "web (codex) — berth" {
		t.Errorf("after moving, title = %q, want %q", got, "web (codex) — berth")
	}
}

// The title is a property of the view now, so what matters is that it always
// says what is selected. Writing it only when it changes is Bubble Tea's job,
// which is the whole reason it stopped being a command.
func TestTitleFollowsTheSelection(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha", "bravo"))
	if m.title == "" {
		t.Fatal("title was never set")
	}

	// A refresh that changes nothing leaves it alone.
	before := m.title
	m.Update(sessions("alpha", "bravo"))
	if m.title != before {
		t.Errorf("a no-op refresh moved the title from %q to %q", before, m.title)
	}

	m.Update(typed("j"))
	if m.title != "bravo (shell) — berth" {
		t.Errorf("title = %q, want it to follow the cursor", m.title)
	}
	if got := m.View().WindowTitle; got != m.title {
		t.Errorf("the view asks for %q, want %q", got, m.title)
	}
}

func TestWindowTitleCanBeDisabled(t *testing.T) {
	m := New(config.Default())
	m.cfg.HideWindowTitle = true
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(sessions("alpha"))

	if m.title != "" {
		t.Errorf("title recorded as %q, want it left alone", m.title)
	}
	if got := m.View().WindowTitle; got != "" {
		t.Errorf("hide_window_title still had the view ask for %q", got)
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
	m.Update(typed("m"))
	if m.mouseOn {
		t.Error("m did not release the mouse")
	}
	if !strings.Contains(m.status, "select") {
		t.Errorf("status = %q, want it to say selection is back", m.status)
	}

	// While released, stray events are ignored rather than acted on.
	before := m.cursor
	m.Update(wheelAt(2, 3, tea.MouseWheelDown))
	if m.cursor != before {
		t.Error("a mouse event moved the cursor after the mouse was released")
	}

	m.Update(typed("m"))
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

// termAt converts the pane's own coordinates into the screen coordinates a
// mouse event would carry: past the sidebar, the divider and the gutter, and
// below the bar above the body.
func termAt(m *Model, x, y int) (int, int) {
	return m.sidebarWidth() + 1 + gutter + x, y + m.topBarHeight()
}

func terminalClick(m *Model, x, y int) tea.MouseClickMsg {
	sx, sy := termAt(m, x, y)
	return click(sx, sy)
}

func terminalRelease(m *Model, x, y int) tea.MouseReleaseMsg {
	sx, sy := termAt(m, x, y)
	return release(sx, sy)
}

func terminalMotion(m *Model, x, y int) tea.MouseMotionMsg {
	sx, sy := termAt(m, x, y)
	return motion(sx, sy)
}

// A drag over the session selects text; berth has the mouse, and the terminal
// would otherwise select straight across the sidebar.
func TestDragOverTheSessionSelects(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	withPane(t, m)

	m.Update(terminalClick(m, 2, 1))
	if m.selection() != nil {
		t.Error("a press alone should not select anything yet")
	}
	m.Update(terminalMotion(m, 9, 3))

	sel := m.selection()
	if sel == nil {
		t.Fatal("dragging did not start a selection")
	}
	if sel.AnchorX != 2 || sel.AnchorY != 1 || sel.CursorX != 9 || sel.CursorY != 3 {
		t.Errorf("selection = %+v, want it to span the drag", *sel)
	}
}

// The copy has to leave as a command. A frame is no longer a string the
// terminal receives - it is parsed into cells, and those are drawn - so an OSC
// 52 written into the view is dropped on the way out, silently, since a copy
// that never happened looks exactly like one that did.
func TestCopyingSelectionLeavesAsACommand(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	withPane(t, m)

	const marker = "berth-copy-marker"
	m.pane.SendText("echo " + marker + "\n")
	row := -1
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && row < 0 {
		for i, line := range m.pane.Render(false, nil) {
			// The echoed command counts: what matters is that the row berth
			// selects has known text on it.
			if strings.Contains(ansi.Strip(line), marker) {
				row = i
				break
			}
		}
		if row < 0 {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if row < 0 {
		t.Skip("the shell never echoed the marker")
	}

	sel := term.Selection{AnchorX: 0, AnchorY: row, CursorX: 39, CursorY: row}
	m.sel = &sel
	text := m.pane.SelectedText(sel)
	if !strings.Contains(text, marker) {
		t.Fatalf("selected %q, want the marker row", text)
	}

	cmd := m.copySelection()
	if cmd == nil {
		t.Fatal("copying produced no command")
	}
	if got, want := cmd(), tea.SetClipboard(text)(); got != want {
		t.Errorf("command sent %#v, want the clipboard set to the selection", got)
	}

	// And the frame must not be carrying it: that route does not arrive.
	if body := m.View().Content; strings.Contains(body, "\x1b]52") {
		t.Error("the view is still carrying an OSC 52 sequence")
	}
}

// A click is not a drag, and still belongs to the program in the session.
func TestClickOverTheSessionIsNotASelection(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	withPane(t, m)

	m.Update(terminalClick(m, 4, 2))
	m.Update(terminalRelease(m, 4, 2))

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

	m.Update(terminalClick(m, 1, 1))
	m.Update(terminalMotion(m, 8, 1))
	if m.selection() == nil {
		t.Fatal("no selection to clear")
	}

	m.Update(typed("x"))
	if m.selection() != nil {
		t.Error("typing into the session left the highlight behind")
	}
}

// The wheel is the session's, selection or not.
func TestWheelIsNotASelection(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	withPane(t, m)
	m.Update(func() tea.MouseMsg { x, y := termAt(m, 3, 3); return wheelAt(x, y, tea.MouseWheelDown) }())
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

	rows := strings.Split(m.screen(), "\n")
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
	m.Update(click(sideW+1, 3))
	if got := selectedName(t, m); got != before {
		t.Errorf("a click in the gutter changed the selection to %q", got)
	}
	if m.focus != focusSidebar {
		t.Error("a click in the gutter moved the focus")
	}
}

// The vertical divider and the rule beneath it are lit together while a
// session has the keyboard; a dim join would break the line in half.
func TestCornerIsLitWithTheSession(t *testing.T) {
	withColour(t)
	m := newTestModel()
	m.Update(sessions("alpha"))
	sideW := m.sidebarWidth()

	m.focus = focusTerminal
	if !strings.Contains(m.footerRule(sideW), focusedDivStyle.Render("┴")) {
		t.Error("the corner is dim while the session has the keyboard")
	}
	m.focus = focusSidebar
	if !strings.Contains(m.footerRule(sideW), dividerStyle.Render("┴")) {
		t.Error("the corner is lit while the list has the keyboard")
	}
}

// ordered builds sessions already carrying berth's own positions.
func ordered(names ...string) sessionsMsg {
	out := make([]tmux.Session, 0, len(names))
	for i, n := range names {
		out = append(out, tmux.Session{
			Name: n, Kind: tmux.KindShell, Managed: true, Order: i,
		})
	}
	return sessionsMsg(out)
}

func sessionOrder(m *Model) []string {
	out := make([]string, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s.Name)
	}
	return out
}

// tmux lists sessions by name and has no notion of order, so berth's own has
// to survive the list being refreshed under it.
func TestSessionsAreShownInBerthsOrder(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsMsg([]tmux.Session{
		{Name: "alpha", Order: 2}, {Name: "bravo", Order: 0}, {Name: "charlie", Order: 1},
	}))
	if got := sessionOrder(m); !slices.Equal(got, []string{"bravo", "charlie", "alpha"}) {
		t.Errorf("order = %q, want berth's", got)
	}
}

// A session never moved keeps tmux's arrangement, and sits after any that have
// been placed, rather than jumping to the front.
func TestUnplacedSessionsKeepTmuxsOrder(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsMsg([]tmux.Session{
		{Name: "new-a", Order: tmux.NoOrder},
		{Name: "placed", Order: 0},
		{Name: "new-b", Order: tmux.NoOrder},
	}))
	if got := sessionOrder(m); !slices.Equal(got, []string{"placed", "new-a", "new-b"}) {
		t.Errorf("order = %q, want the placed one first", got)
	}
}

func TestMovingASessionUpAndDown(t *testing.T) {
	m := newTestModel()
	m.Update(ordered("alpha", "bravo", "charlie"))

	m.cursor = 2 // charlie
	m.Update(typed("K"))
	if got := sessionOrder(m); !slices.Equal(got, []string{"alpha", "charlie", "bravo"}) {
		t.Errorf("after K: %q", got)
	}
	// The cursor follows the session, not the position.
	if got := selectedName(t, m); got != "charlie" {
		t.Errorf("cursor left on %q, want it following charlie", got)
	}

	m.Update(typed("J"))
	if got := sessionOrder(m); !slices.Equal(got, []string{"alpha", "bravo", "charlie"}) {
		t.Errorf("after J: %q", got)
	}
}

// shift with the arrows is the other way to say J and K, and has to reach the
// list rather than being taken for a plain arrow and only moving the cursor.
func TestMovingASessionWithShiftedArrows(t *testing.T) {
	m := newTestModel()
	m.Update(ordered("alpha", "bravo", "charlie"))

	m.cursor = 2 // charlie
	m.Update(key(tea.KeyUp, tea.ModShift))
	if got := sessionOrder(m); !slices.Equal(got, []string{"alpha", "charlie", "bravo"}) {
		t.Errorf("after shift+up: %q", got)
	}
	if got := selectedName(t, m); got != "charlie" {
		t.Errorf("cursor left on %q, want it following charlie", got)
	}

	m.Update(key(tea.KeyDown, tea.ModShift))
	if got := sessionOrder(m); !slices.Equal(got, []string{"alpha", "bravo", "charlie"}) {
		t.Errorf("after shift+down: %q", got)
	}
}

func TestMovingStopsAtTheEnds(t *testing.T) {
	m := newTestModel()
	m.Update(ordered("alpha", "bravo"))

	m.cursor = 0
	m.Update(typed("K"))
	if got := sessionOrder(m); !slices.Equal(got, []string{"alpha", "bravo"}) {
		t.Errorf("moving the first session up changed the order: %q", got)
	}
	m.cursor = 1
	m.Update(typed("J"))
	if got := sessionOrder(m); !slices.Equal(got, []string{"alpha", "bravo"}) {
		t.Errorf("moving the last session down changed the order: %q", got)
	}
}

// With a filter on, a move should land where it looked like it would: past the
// next session you can see, not the next one in the full list.
func TestMovingPastAFilteredOutSession(t *testing.T) {
	m := newTestModel()
	m.Update(ordered("api-one", "hidden", "api-two"))
	m.filter = "api"

	m.cursor = 0 // api-one, with api-two next on screen
	m.Update(typed("J"))

	if got := sessionOrder(m); !slices.Equal(got, []string{"api-two", "hidden", "api-one"}) {
		t.Errorf("order = %q, want the two visible sessions swapped", got)
	}
	if got := selectedName(t, m); got != "api-one" {
		t.Errorf("cursor on %q, want it still on the moved session", got)
	}
}

func TestShortVersion(t *testing.T) {
	cases := map[string]string{
		"v0.3.0":                  "v0.3.0",
		"0.3.0":                   "v0.3.0",
		"v0.3.0-9-gabc1234":       "v0.3.0+",
		"v0.3.0-9-gabc1234-dirty": "v0.3.0+",
		"dev":                     "dev",
		"":                        "dev",
	}
	for in, want := range cases {
		if got := shortVersion(in); got != want {
			t.Errorf("shortVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHeaderShowsTheVersionOnTheRight(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("api", "web"))

	before := Version
	t.Cleanup(func() { Version = before })
	Version = "v0.3.0"

	line := ansi.Strip(m.brandBar(28))
	if ansi.StringWidth(m.brandBar(28)) != 28 {
		t.Errorf("header is %d cells, want 28", ansi.StringWidth(m.brandBar(28)))
	}
	if !strings.HasPrefix(line, " BERTH 2") {
		t.Errorf("header = %q, want the count on the left", line)
	}
	if !strings.HasSuffix(line, "v0.3.0") {
		t.Errorf("header = %q, want the version on the right", line)
	}

	// An update turns it into an arrow, which fits where a second version
	// would not have.
	m.newerVersion = "v0.4.0"
	line = ansi.Strip(m.brandBar(28))
	if !strings.HasSuffix(line, "↑v0.3.0") {
		t.Errorf("header = %q, want the version marked as behind", line)
	}
	if ansi.StringWidth(m.brandBar(18)) != 18 {
		t.Error("the marked version does not fit a narrow sidebar")
	}

	// And a header with room for neither still fits its column.
	for _, w := range []int{16, 12, 8, 4} {
		if got := ansi.StringWidth(m.brandBar(w)); got > w {
			t.Errorf("header at %d is %d cells", w, got)
		}
	}
}

// Bubble Tea drops lines off the top of a view taller than the terminal, so a
// help screen too tall for the window lost its border, its title and its first
// bindings with nothing to say so.
func TestDialogsAreCutToTheWindowNotOffTheTop(t *testing.T) {
	m := New(config.Default())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m.mode = modeHelp

	view := m.screen()
	lines := strings.Split(view, "\n")
	if len(lines) > m.height {
		t.Fatalf("help is %d lines in a %d-row window", len(lines), m.height)
	}
	if !strings.Contains(ansi.Strip(view), "berth") {
		t.Error("the title was cut; want the top of the dialog kept")
	}
	if !strings.Contains(ansi.Strip(view), "window too short") {
		t.Error("the dialog was cut without saying so")
	}

	// With room to spare nothing is cut.
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if strings.Contains(ansi.Strip(m.screen()), "window too short") {
		t.Error("a dialog that fits was marked as cut")
	}
}

// key builds a key press the way a terminal that can disambiguate reports one:
// a key code, plus whatever was held down with it. It stands in for the struct
// literals these tests used before, which named a key by a constant per
// combination rather than by the key and its modifiers.
func key(code rune, mods ...tea.KeyMod) tea.KeyPressMsg {
	var mod tea.KeyMod
	for _, m := range mods {
		mod |= m
	}
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

// typed builds the key press for a printable character, which carries the text
// it produced as well as the key that produced it.
func typed(s string) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

// click, release, motion and wheel build the four kinds of mouse message. In
// v2 the kind of event is the message's own type rather than an Action field,
// which is why these are four constructors and not one with an argument.
func click(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func release(x, y int) tea.MouseReleaseMsg {
	return tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func motion(x, y int) tea.MouseMotionMsg {
	return tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func wheelAt(x, y int, button tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg{X: x, Y: y, Button: button}
}

// shift is part of how a character was made, not a modifier that changes what
// the key means. Insisting on an unmodified key here dropped every capital
// letter and every symbol typed with shift, which made berth unusable for
// typing into a session.
func TestShiftedCharactersAreTypedThrough(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.Key
		want string
	}{
		{"a", tea.Key{Code: 'a', Text: "a"}, "a"},
		{"shift+a", tea.Key{Code: 'a', Mod: tea.ModShift, Text: "A"}, "A"},
		{"shift+9 on a US layout", tea.Key{Code: '9', Mod: tea.ModShift, Text: "("}, "("},
		{"an accented letter", tea.Key{Code: 'é', Text: "é"}, "é"},
		{"a character outside the BMP", tea.Key{Code: '𝄞', Text: "𝄞"}, "𝄞"},
	} {
		got, ok := typedText(tc.key)
		if !ok {
			t.Errorf("%s was not typed through at all", tc.name)
			continue
		}
		if got != tc.want {
			t.Errorf("%s arrived as %q, want %q", tc.name, got, tc.want)
		}
	}

	// ctrl and alt change what a key means rather than which character it is,
	// so they are left to be encoded as key presses instead.
	for _, tc := range []struct {
		name string
		key  tea.Key
	}{
		{"ctrl+a", tea.Key{Code: 'a', Mod: tea.ModCtrl}},
		{"alt+a", tea.Key{Code: 'a', Mod: tea.ModAlt, Text: "a"}},
		{"ctrl+shift+a", tea.Key{Code: 'a', Mod: tea.ModCtrl | tea.ModShift, Text: "A"}},
		{"enter", tea.Key{Code: tea.KeyEnter}},
		{"shift+enter", tea.Key{Code: tea.KeyEnter, Mod: tea.ModShift}},
	} {
		if _, ok := typedText(tc.key); ok {
			t.Errorf("%s was treated as plain typing", tc.name)
		}
	}
}

// notifyStrings runs a reading through the model and returns what it would send
// the terminal, if anything.
func notifyStrings(t *testing.T, m *Model, infos map[string]agent.Info) string {
	t.Helper()
	cmd := m.notifyFor(infos)
	m.agents = infos
	if cmd == nil {
		return ""
	}
	msg, ok := cmd().(tea.RawMsg)
	if !ok {
		t.Fatalf("notify sent %T, want a raw sequence", cmd())
	}
	return fmt.Sprint(msg.Msg)
}

// Notifying is about the moment a session changes, so berth has to know what it
// was doing before. The first reading is where it learns that - announcing it
// would ring for news that is hours old every time berth starts.
func TestNotifyOnlyOnAChange(t *testing.T) {
	m := newTestModel()
	m.cfg.NotifyBell, m.cfg.NotifyDesktop = true, true
	m.cfg.NotifyWaiting, m.cfg.NotifyIdle = true, true

	first := map[string]agent.Info{
		"api": {Status: agent.Busy},
		"web": {Status: agent.Busy},
	}
	if got := notifyStrings(t, m, first); got != "" {
		t.Errorf("the first reading said %q, want nothing", got)
	}
	// Still waiting, still working: nothing has changed, so nothing is said.
	if got := notifyStrings(t, m, first); got != "" {
		t.Errorf("an unchanged reading said %q, want nothing", got)
	}

	got := notifyStrings(t, m, map[string]agent.Info{
		"api": {Status: agent.Idle},    // answered, and now finished
		"web": {Status: agent.Waiting}, // has come to a question
	})
	if !strings.Contains(got, "web is waiting on you") {
		t.Errorf("said %q, want the session that came to a question", got)
	}
	if !strings.Contains(got, "api has finished") {
		t.Errorf("said %q, want the session that finished", got)
	}
	// One ring for the pair of them: a terminal cannot ring twice as loudly.
	// The ring leads, and is the only bare bell - OSC 9 ends in one of its own,
	// which is a string terminator rather than a sound.
	if !strings.HasPrefix(got, "\a") || strings.Contains(strings.TrimPrefix(got, "\a"), "\a\a") {
		t.Errorf("said %q, want exactly one ring, leading", got)
	}
}

// Only work turning into idle is a turn that finished. A session berth has just
// started following, or one going back to the prompt after a question, has not.
func TestFinishedMeansWorkThatStopped(t *testing.T) {
	m := newTestModel()
	m.cfg.NotifyBell = true
	m.cfg.NotifyWaiting, m.cfg.NotifyIdle = false, true

	notifyStrings(t, m, map[string]agent.Info{"api": {Status: agent.Waiting}})
	if got := notifyStrings(t, m, map[string]agent.Info{"api": {Status: agent.Idle}}); got != "" {
		t.Errorf("waiting to idle said %q, want nothing - no turn ended there", got)
	}
	notifyStrings(t, m, map[string]agent.Info{"api": {Status: agent.Busy}})
	if got := notifyStrings(t, m, map[string]agent.Info{"api": {Status: agent.Idle}}); got == "" {
		t.Error("work turning into idle said nothing, want a notification")
	}
}

// The two halves are separate: a bell is a sequence that draws nothing, a
// desktop notification carries the name of the session.
func TestNotifyHonoursHowAndWhen(t *testing.T) {
	waiting := map[string]agent.Info{"api": {Status: agent.Waiting}}
	start := map[string]agent.Info{"api": {Status: agent.Busy}}

	cases := []struct {
		bell, desktop bool
		waiting, idle bool
		wantBell      bool
		wantWords     bool
	}{
		// No way of saying it: the moment is set and nothing happens.
		{false, false, true, false, false, false},
		{true, false, true, false, true, false},
		{false, true, true, false, false, true},
		{true, true, true, false, true, true},
		// Both ways of saying it, but not about this moment.
		{true, true, false, true, false, false},
		{true, true, false, false, false, false},
	}
	for _, c := range cases {
		m := newTestModel()
		m.cfg.NotifyBell, m.cfg.NotifyDesktop = c.bell, c.desktop
		m.cfg.NotifyWaiting, m.cfg.NotifyIdle = c.waiting, c.idle
		notifyStrings(t, m, start)
		got := notifyStrings(t, m, waiting)
		if strings.HasPrefix(got, "\a") != c.wantBell {
			t.Errorf("bell=%v desktop=%v waiting=%v: bell in %q, want %v",
				c.bell, c.desktop, c.waiting, got, c.wantBell)
		}
		if strings.Contains(got, "api is waiting on you") != c.wantWords {
			t.Errorf("bell=%v desktop=%v waiting=%v: words in %q, want %v",
				c.bell, c.desktop, c.waiting, got, c.wantWords)
		}
	}
}

// A session that has gone takes its history with it, so a name used again later
// is not compared against a session that no longer exists.
func TestNotifyForgetsSessionsThatEnd(t *testing.T) {
	m := newTestModel()
	m.cfg.NotifyBell, m.cfg.NotifyWaiting = true, true

	notifyStrings(t, m, map[string]agent.Info{"api": {Status: agent.Busy}})
	notifyStrings(t, m, map[string]agent.Info{}) // api is killed
	if _, ok := m.seen["api"]; ok {
		t.Error("a session that ended was still remembered")
	}
	// A new session of the same name starts over: its first reading teaches
	// berth where it stands rather than announcing it.
	if got := notifyStrings(t, m, map[string]agent.Info{"api": {Status: agent.Waiting}}); got != "" {
		t.Errorf("a session seen for the first time said %q, want nothing", got)
	}
}
