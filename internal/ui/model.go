package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/dpws/berth/internal/clip"
	"github.com/dpws/berth/internal/config"
	"github.com/dpws/berth/internal/term"
	"github.com/dpws/berth/internal/tmux"
)

type focusArea int

const (
	focusSidebar focusArea = iota
	focusTerminal
)

type uiMode int

const (
	modeNormal uiMode = iota
	modeNew
	modeRename
	modeConfirmKill
	modeFilter
	modeHelp
)

// attachDelay debounces attaching while the user scrolls the list.
const attachDelay = 120 * time.Millisecond

// Model is the root Bubble Tea model.
type Model struct {
	cfg config.Config

	width, height int
	ready         bool

	sessions []tmux.Session
	cursor   int
	filter   string

	focus focusArea
	mode  uiMode
	// appFocused tracks whether berth's own window has the keyboard, so a
	// session is only told it is focused when both it and we are.
	appFocused bool
	// rowSessions maps a sidebar row to the session drawn on it, filled in
	// while rendering so click handling cannot drift from the layout.
	rowSessions []int

	pane *term.Pane

	nameInput textinput.Model
	dirInput  textinput.Model
	newKind   string
	formField int

	attachGen int
	pending   string
	// selectName pulls the cursor onto a session as soon as it shows up in a
	// refresh, so a session you just created becomes the selected one.
	selectName string

	status      string
	statusIsErr bool

	quitting bool
}

// New builds the root model.
func New(cfg config.Config) *Model {
	name := textinput.New()
	name.Placeholder = "session name"
	name.CharLimit = 64
	name.Prompt = ""

	dir := textinput.New()
	dir.Placeholder = cfg.DefaultDir
	dir.CharLimit = 512
	dir.Prompt = ""

	return &Model{
		cfg:        cfg,
		nameInput:  name,
		dirInput:   dir,
		newKind:    tmux.KindClaude,
		appFocused: true,
	}
}

// ---------------------------------------------------------------- messages

type sessionsMsg []tmux.Session

type errMsg struct{ err error }

type statusMsg struct {
	text  string
	isErr bool
	// selectName, when set, moves the cursor to that session once the list
	// refresh that follows this message lands.
	selectName string
}

type tickMsg time.Time

type attachMsg struct {
	name string
	gen  int
}

type paneMsg struct{ pane *term.Pane }

// imagePasteMsg carries the result of looking for an image to hand to the
// focused session. The lookup shells out, so it runs as a command.
type imagePasteMsg struct {
	result clip.Result
	err    error
}

// ---------------------------------------------------------------- lifecycle

func (m *Model) Init() tea.Cmd {
	return tea.Batch(listSessions(), tickCmd(m.cfg.RefreshMillis))
}

// Close releases the attached tmux client. Safe to call more than once.
func (m *Model) Close() {
	if m.pane != nil {
		_ = m.pane.Close()
		m.pane = nil
	}
}

func listSessions() tea.Cmd {
	return func() tea.Msg {
		sessions, err := tmux.List()
		if err != nil {
			return errMsg{err}
		}
		return sessionsMsg(sessions)
	}
}

func tickCmd(ms int) tea.Cmd {
	if ms <= 0 {
		ms = 2000
	}
	return tea.Tick(time.Duration(ms)*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// waitForPane blocks until the pane reports a change, then asks for a redraw.
func waitForPane(p *term.Pane) tea.Cmd {
	if p == nil {
		return nil
	}
	return func() tea.Msg {
		<-p.Dirty()
		return paneMsg{pane: p}
	}
}

// ---------------------------------------------------------------- update

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		if m.pane != nil {
			w, h := m.terminalSize()
			m.pane.Resize(w, h)
		}
		return m, nil

	case tickMsg:
		return m, tea.Batch(listSessions(), tickCmd(m.cfg.RefreshMillis))

	case sessionsMsg:
		return m, m.applySessions(msg)

	case errMsg:
		m.setStatus(msg.err.Error(), true)
		return m, nil

	case statusMsg:
		m.setStatus(msg.text, msg.isErr)
		if msg.selectName != "" {
			m.selectName = msg.selectName
		}
		return m, listSessions()

	case attachMsg:
		if msg.gen != m.attachGen {
			return m, nil // superseded by a newer selection
		}
		m.pending = ""
		return m, m.attach(msg.name)

	case imagePasteMsg:
		if msg.err != nil {
			m.setStatus("no image to paste - "+msg.err.Error(), true)
			return m, nil
		}
		if m.pane == nil {
			m.setStatus("no session to paste into", true)
			return m, nil
		}
		// A trailing space separates the path from whatever is typed next.
		m.pane.SendText(msg.result.Path + " ")
		m.focus = focusTerminal
		m.syncPaneFocus()
		m.setStatus(fmt.Sprintf("pasted %s from %s",
			filepath.Base(msg.result.Path), msg.result.Source), false)
		return m, nil

	case paneMsg:
		if msg.pane != m.pane {
			return m, nil // a pane we already replaced
		}
		if msg.pane.Exited() {
			// The tmux client went away: the session was killed or the server
			// detached us. Refresh and let the list decide what to show.
			m.pane = nil
			m.focus = focusSidebar
			return m, listSessions()
		}
		return m, waitForPane(msg.pane)

	case tea.FocusMsg:
		m.appFocused = true
		m.syncPaneFocus()
		return m, nil

	case tea.BlurMsg:
		m.appFocused = false
		m.syncPaneFocus()
		return m, nil

	case tea.MouseMsg:
		return m, m.handleMouse(msg)

	case tea.KeyMsg:
		return m, m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	switch m.mode {
	case modeNew:
		return m.handleNewKey(msg)
	case modeRename:
		return m.handleRenameKey(msg)
	case modeFilter:
		return m.handleFilterKey(msg)
	case modeConfirmKill:
		return m.handleConfirmKey(msg)
	case modeHelp:
		m.mode = modeNormal
		return nil
	}

	// Ctrl+O toggles which half of the screen owns the keyboard. Everything
	// else, when the terminal has focus, belongs to the session.
	if msg.Type == tea.KeyCtrlO {
		m.toggleFocus()
		return nil
	}

	// The paste key is the one other combination berth keeps for itself
	// while a session has focus, so it is configurable.
	if m.cfg.PasteImageKey != "" && msg.String() == m.cfg.PasteImageKey {
		return m.pasteImage()
	}

	if m.focus == focusTerminal {
		if m.pane == nil {
			m.focus = focusSidebar
			return nil
		}
		switch {
		case msg.Paste:
			m.pane.Paste(string(msg.Runes))
		case msg.Type == tea.KeyRunes && !msg.Alt:
			// Bubble Tea hands us printable input in runs; forwarding the whole
			// run as text keeps every character instead of just the first.
			m.pane.SendText(string(msg.Runes))
		default:
			if key, ok := term.ToKeyPress(msg); ok {
				m.pane.SendKey(key)
			}
		}
		return nil
	}

	return m.handleSidebarKey(msg)
}

// handleMouse routes a click or wheel event to whichever half of the screen it
// landed on. Sidebar events drive the list; terminal events are translated
// into the pane's coordinate space and handed to the session.
func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if m.mode != modeNormal {
		return nil
	}

	sideW := m.sidebarWidth()
	if msg.X < sideW {
		return m.handleSidebarMouse(msg)
	}
	if msg.X == sideW || m.pane == nil {
		return nil // the divider column
	}

	x, y := msg.X-sideW-1, msg.Y
	if y >= m.bodyHeight() {
		return nil // the footer
	}
	if msg.Action == tea.MouseActionPress && m.focus != focusTerminal {
		m.focus = focusTerminal
		m.syncPaneFocus()
	}
	if ev, ok := term.ToMouse(msg, x, y); ok {
		m.pane.SendMouse(ev)
	}
	return nil
}

func (m *Model) handleSidebarMouse(msg tea.MouseMsg) tea.Cmd {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return m.moveCursor(-1)
	case tea.MouseButtonWheelDown:
		return m.moveCursor(1)
	}

	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	if msg.Y < 0 || msg.Y >= len(m.rowSessions) {
		return nil
	}
	target := m.rowSessions[msg.Y]
	if target < 0 {
		return nil
	}
	if target == m.cursor && m.pane != nil {
		// A second click on the selected session hands over the keyboard.
		m.focus = focusTerminal
		m.syncPaneFocus()
		return nil
	}
	return m.moveCursor(target - m.cursor)
}

func (m *Model) handleSidebarKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return tea.Quit

	case "up", "k", "ctrl+p":
		return m.moveCursor(-1)
	case "down", "j", "ctrl+n":
		return m.moveCursor(1)
	case "pgup":
		return m.moveCursor(-5)
	case "pgdown":
		return m.moveCursor(5)
	case "home", "g":
		return m.moveCursor(-len(m.visibleSessions()))
	case "end", "G":
		return m.moveCursor(len(m.visibleSessions()))

	case "enter", "l", "right", "tab":
		if m.pane != nil {
			m.focus = focusTerminal
			m.syncPaneFocus()
			m.setStatus("", false)
		}
		return nil

	case "n":
		m.mode = modeNew
		m.formField = 0
		m.newKind = tmux.KindClaude
		m.nameInput.SetValue("")
		m.dirInput.SetValue("")
		m.nameInput.Focus()
		m.dirInput.Blur()
		return textinput.Blink

	case "r":
		s, ok := m.selected()
		if !ok {
			return nil
		}
		m.mode = modeRename
		m.nameInput.SetValue(s.Name)
		m.nameInput.CursorEnd()
		m.nameInput.Focus()
		return textinput.Blink

	case "x", "d":
		if _, ok := m.selected(); !ok {
			return nil
		}
		m.mode = modeConfirmKill
		return nil

	case "/":
		m.mode = modeFilter
		m.nameInput.SetValue(m.filter)
		m.nameInput.CursorEnd()
		m.nameInput.Focus()
		return textinput.Blink

	case "?":
		m.mode = modeHelp
		return nil

	case "R":
		return listSessions()
	}
	return nil
}

func (m *Model) handleNewKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.nameInput.Blur()
		m.dirInput.Blur()
		return nil

	case "tab", "down":
		m.formField = (m.formField + 1) % 3
		m.syncFormFocus()
		return textinput.Blink

	case "shift+tab", "up":
		m.formField = (m.formField + 2) % 3
		m.syncFormFocus()
		return textinput.Blink

	case "left":
		if m.formField == 1 {
			m.newKind = cycleKind(m.newKind, -1)
			return nil
		}

	case "right", " ":
		if m.formField == 1 {
			m.newKind = cycleKind(m.newKind, 1)
			return nil
		}

	case "enter":
		return m.createSession()
	}

	var cmd tea.Cmd
	switch m.formField {
	case 0:
		m.nameInput, cmd = m.nameInput.Update(msg)
	case 2:
		m.dirInput, cmd = m.dirInput.Update(msg)
	}
	return cmd
}

func (m *Model) handleRenameKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.nameInput.Blur()
		return nil
	case "enter":
		s, ok := m.selected()
		if !ok {
			m.mode = modeNormal
			return nil
		}
		newName := m.nameInput.Value()
		m.mode = modeNormal
		m.nameInput.Blur()
		return func() tea.Msg {
			name, err := tmux.Rename(s.Name, newName)
			if err != nil {
				return errMsg{err}
			}
			return statusMsg{text: "renamed to " + name}
		}
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return cmd
}

func (m *Model) handleFilterKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.filter = ""
		m.mode = modeNormal
		m.nameInput.Blur()
		return m.clampCursor()
	case "enter":
		m.filter = m.nameInput.Value()
		m.mode = modeNormal
		m.nameInput.Blur()
		return m.clampCursor()
	}
	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	m.filter = m.nameInput.Value()
	return tea.Batch(cmd, m.clampCursor())
}

func (m *Model) handleConfirmKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "y", "Y", "enter":
		m.mode = modeNormal
		s, ok := m.selected()
		if !ok {
			return nil
		}
		// Drop the client first so the pty does not race the session teardown.
		if m.pane != nil && m.pane.Session == s.Name {
			m.pane.Close()
			m.pane = nil
			m.pending = ""
			m.focus = focusSidebar
		}
		return func() tea.Msg {
			if err := tmux.Kill(s.Name); err != nil {
				return errMsg{err}
			}
			return statusMsg{text: "killed " + s.Name}
		}
	default:
		m.mode = modeNormal
		return nil
	}
}

func (m *Model) createSession() tea.Cmd {
	name := strings.TrimSpace(m.nameInput.Value())
	dir := strings.TrimSpace(m.dirInput.Value())
	kind := m.newKind

	if name == "" {
		name = defaultName(kind, dir)
	}
	if dir == "" {
		dir = m.cfg.DefaultDir
	}

	command := m.commandFor(kind)

	m.mode = modeNormal
	m.nameInput.Blur()
	m.dirInput.Blur()

	return func() tea.Msg {
		// New can return both a name and an error: the session exists but one
		// of the configured tmux options was rejected. Report it and still
		// select what was created.
		created, err := tmux.New(tmux.NewOptions{
			Name:          name,
			Dir:           dir,
			Kind:          kind,
			Command:       command,
			HideStatusBar: m.cfg.HideStatusBar,
			Options:       m.cfg.SessionOptions,
		})
		if err != nil {
			return statusMsg{text: err.Error(), isErr: true, selectName: created}
		}
		return statusMsg{text: "created " + created, selectName: created}
	}
}

// commandFor returns the command a session of the given kind should run.
func (m *Model) commandFor(kind string) string {
	switch kind {
	case tmux.KindClaude:
		return m.cfg.ClaudeCommand
	case tmux.KindCodex:
		return m.cfg.CodexCommand
	default:
		return m.cfg.ShellCommand
	}
}

// applySessions folds a fresh session list into the model, keeping the cursor
// on the same session where possible and attaching when the selection moves.
func (m *Model) applySessions(sessions []tmux.Session) tea.Cmd {
	selectedName := m.selectName
	if selectedName == "" {
		if s, ok := m.selected(); ok {
			selectedName = s.Name
		}
	}

	m.sessions = sessions

	visible := m.visibleSessions()
	if selectedName != "" {
		for i, s := range visible {
			if s.Name == selectedName {
				m.cursor = i
				m.selectName = ""
				break
			}
		}
	}
	if m.cursor >= len(visible) {
		m.cursor = len(visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}

	if len(visible) == 0 {
		m.pending = ""
		if m.pane != nil {
			m.pane.Close()
			m.pane = nil
			m.focus = focusSidebar
		}
		return nil
	}

	want := visible[m.cursor].Name
	if m.pane == nil || m.pane.Session != want {
		return m.requestAttach(want)
	}
	return nil
}

func (m *Model) moveCursor(delta int) tea.Cmd {
	visible := m.visibleSessions()
	if len(visible) == 0 {
		return nil
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(visible) {
		m.cursor = len(visible) - 1
	}
	return m.requestAttach(visible[m.cursor].Name)
}

func (m *Model) clampCursor() tea.Cmd {
	visible := m.visibleSessions()
	if len(visible) == 0 {
		return nil
	}
	if m.cursor >= len(visible) {
		m.cursor = len(visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	return m.requestAttach(visible[m.cursor].Name)
}

// requestAttach schedules an attach after a short delay so holding j/k does
// not spawn a tmux client per row.
func (m *Model) requestAttach(name string) tea.Cmd {
	if m.pane != nil && m.pane.Session == name && !m.pane.Exited() {
		return nil
	}
	m.attachGen++
	gen := m.attachGen
	m.pending = name
	return tea.Tick(attachDelay, func(time.Time) tea.Msg {
		return attachMsg{name: name, gen: gen}
	})
}

func (m *Model) attach(name string) tea.Cmd {
	if m.pane != nil {
		if m.pane.Session == name && !m.pane.Exited() {
			return nil
		}
		m.pane.Close()
		m.pane = nil
	}

	w, h := m.terminalSize()
	pane, err := term.Attach(name, w, h)
	if err != nil {
		dbg("attach %q failed: %v", name, err)
		m.setStatus(err.Error(), true)
		return nil
	}
	dbg("attached %q at %dx%d", name, w, h)
	m.pane = pane
	m.syncPaneFocus()
	return waitForPane(pane)
}

// pasteImage looks for an image and, when it finds one, types its path into
// the focused session. Agents read the file from the path; there is no way to
// push image bytes through a terminal.
func (m *Model) pasteImage() tea.Cmd {
	if m.pane == nil {
		m.setStatus("no session to paste into", true)
		return nil
	}
	opts := clip.Options{
		DropDir:    m.cfg.ImageDropDir,
		CacheDir:   config.ImageCacheDir(),
		AgentURL:   m.cfg.ClipAgentURL,
		AgentToken: m.cfg.ClipAgentToken,
	}
	return func() tea.Msg {
		result, err := clip.Fetch(opts)
		return imagePasteMsg{result: result, err: err}
	}
}

func (m *Model) toggleFocus() {
	if m.focus == focusTerminal {
		m.focus = focusSidebar
	} else if m.pane != nil {
		m.focus = focusTerminal
	}
	m.syncPaneFocus()
}

// syncPaneFocus tells the session whether it holds the keyboard. tmux only
// passes this on to the program in the pane when its focus-events option is
// on; without that the emulator drops it.
func (m *Model) syncPaneFocus() {
	if m.pane != nil {
		m.pane.SetFocus(m.focus == focusTerminal && m.appFocused)
	}
}

func (m *Model) syncFormFocus() {
	m.nameInput.Blur()
	m.dirInput.Blur()
	switch m.formField {
	case 0:
		m.nameInput.Focus()
	case 2:
		m.dirInput.Focus()
	}
}

func (m *Model) setStatus(text string, isErr bool) {
	m.status = text
	m.statusIsErr = isErr
}

func (m *Model) selected() (tmux.Session, bool) {
	visible := m.visibleSessions()
	if m.cursor < 0 || m.cursor >= len(visible) {
		return tmux.Session{}, false
	}
	return visible[m.cursor], true
}

func (m *Model) visibleSessions() []tmux.Session {
	if m.filter == "" {
		return m.sessions
	}
	needle := strings.ToLower(m.filter)
	out := make([]tmux.Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		if strings.Contains(strings.ToLower(s.Name), needle) {
			out = append(out, s)
		}
	}
	return out
}

// ---------------------------------------------------------------- layout

func (m *Model) sidebarWidth() int {
	w := m.cfg.SidebarWidth
	if w <= 0 {
		w = 28
	}
	if maxW := m.width / 2; w > maxW {
		w = maxW
	}
	if w < 16 {
		w = 16
	}
	if w > m.width-10 {
		w = max(1, m.width-10)
	}
	return w
}

func (m *Model) bodyHeight() int { return max(1, m.height-1) }

func (m *Model) terminalSize() (int, int) {
	return max(2, m.width-m.sidebarWidth()-1), m.bodyHeight()
}

// ---------------------------------------------------------------- view

func (m *Model) View() string {
	if !m.ready {
		return "starting berth…"
	}
	if m.quitting {
		return ""
	}

	switch m.mode {
	case modeNew, modeRename, modeConfirmKill, modeHelp:
		return m.dialogView()
	}

	sideW := m.sidebarWidth()
	bodyH := m.bodyHeight()
	termW, termH := m.terminalSize()

	sidebar := m.sidebarLines(sideW, bodyH)
	terminal := m.terminalLines(termW, termH)

	div := dividerStyle.Render("│")
	if m.focus == focusTerminal {
		div = focusedDivStyle.Render("│")
	}

	var b strings.Builder
	for i := 0; i < bodyH; i++ {
		b.WriteString(sidebar[i])
		b.WriteString(div)
		b.WriteString(terminal[i])
		b.WriteString("\x1b[0m")
		if i < bodyH-1 {
			b.WriteByte('\n')
		}
	}
	b.WriteByte('\n')
	b.WriteString(m.footerView())
	return b.String()
}

// terminalLines returns exactly h lines of at most w cells from the pane.
func (m *Model) terminalLines(w, h int) []string {
	out := make([]string, h)

	if m.pane == nil {
		msg := "no session attached"
		if m.pending != "" {
			msg = "attaching to " + m.pending + "…"
		} else if len(m.visibleSessions()) == 0 {
			msg = "press n to start a Claude or shell session"
		}
		for i := range out {
			out[i] = strings.Repeat(" ", w)
		}
		if h > 2 {
			out[h/2] = padTo(centerIn(faintStyle.Render(msg), w), w)
		}
		return out
	}

	lines := m.pane.Render(m.focus == focusTerminal)
	for i := 0; i < h; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		if ansi.StringWidth(line) > w {
			line = ansi.Truncate(line, w, "")
		}
		out[i] = line
	}
	return out
}

func (m *Model) footerView() string {
	var help string
	switch {
	case m.focus == focusTerminal:
		name := "session"
		if m.pane != nil {
			name = m.pane.Session
		}
		help = footerKeyStyle.Render("ctrl+o") + footerStyle.Render(" back to list") +
			footerStyle.Render("  ·  keys go to ") + footerKeyStyle.Render(name)
	default:
		help = joinHelp(
			"↑/↓", "move",
			"enter", "terminal",
			"n", "new",
			"x", "kill",
			"r", "rename",
			"/", "filter",
			"?", "help",
			"q", "quit",
		)
	}

	if m.status != "" {
		style := successStyle
		if m.statusIsErr {
			style = errorStyle
		}
		help = style.Render(truncate(m.status, m.width))
	}
	return padTo(help, m.width)
}

func joinHelp(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, footerKeyStyle.Render(pairs[i])+" "+footerStyle.Render(pairs[i+1]))
	}
	return strings.Join(parts, footerStyle.Render(" · "))
}

func (m *Model) dialogView() string {
	var body string
	switch m.mode {
	case modeNew:
		body = m.newDialog()
	case modeRename:
		body = m.renameDialog()
	case modeConfirmKill:
		body = m.killDialog()
	case modeHelp:
		body = m.helpText()
	}
	dialog := dialogStyle.Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (m *Model) newDialog() string {
	label := func(i int, s string) string {
		if m.formField == i {
			return labelActiveStyle.Render(s)
		}
		return labelStyle.Render(s)
	}

	var chips []string
	for _, k := range tmux.Kinds {
		if k == m.newKind {
			chips = append(chips, chipActiveStyle.Render(k))
		} else {
			chips = append(chips, chipStyle.Render(k))
		}
	}

	dirValue := m.dirInput.View()
	return strings.Join([]string{
		titleStyle.Render("New session"),
		"",
		label(0, "name") + "  " + m.nameInput.View(),
		label(1, "kind") + "  " + strings.Join(chips, " "),
		label(2, "dir ") + "  " + dirValue,
		"",
		footerStyle.Render("tab switch · ←/→ kind · enter create · esc cancel"),
	}, "\n")
}

func (m *Model) renameDialog() string {
	name := ""
	if s, ok := m.selected(); ok {
		name = s.Name
	}
	return strings.Join([]string{
		titleStyle.Render("Rename " + name),
		"",
		m.nameInput.View(),
		"",
		footerStyle.Render("enter rename · esc cancel"),
	}, "\n")
}

func (m *Model) killDialog() string {
	name := ""
	if s, ok := m.selected(); ok {
		name = s.Name
	}
	return strings.Join([]string{
		errorStyle.Render("Kill session " + name + "?"),
		"",
		footerStyle.Render("Everything running in it is terminated."),
		"",
		footerStyle.Render("y kill · any other key cancel"),
	}, "\n")
}

func (m *Model) helpText() string {
	rows := [][2]string{
		{"↑/↓, j/k", "move through sessions"},
		{"enter, l", "give keys to the terminal"},
		{"ctrl+o", "toggle list / terminal focus"},
		{"n", "new session (Claude or shell)"},
		{"x", "kill the selected session"},
		{"r", "rename the selected session"},
		{"/", "filter by name"},
		{m.cfg.PasteImageKey, "paste an image path into the session"},
		{"click", "select a row; click again to focus it"},
		{"wheel", "scroll the list, or the focused session"},
		{"R", "refresh the list now"},
		{"q", "quit (sessions keep running)"},
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("berth") + "\n\n")
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("%s  %s\n",
			footerKeyStyle.Render(padTo(r[0], 12)), footerStyle.Render(r[1])))
	}
	b.WriteString("\n" + footerStyle.Render("any key to close"))
	return b.String()
}

// ---------------------------------------------------------------- helpers

func defaultName(kind, dir string) string {
	base := kind
	if base == "" {
		base = tmux.KindShell
	}
	if dir != "" {
		if parts := strings.Split(strings.TrimRight(dir, "/"), "/"); len(parts) > 0 {
			if last := parts[len(parts)-1]; last != "" {
				base = last
			}
		}
	}
	return base
}

// cycleKind steps through tmux.Kinds, wrapping in either direction.
func cycleKind(k string, delta int) string {
	for i, kind := range tmux.Kinds {
		if kind == k {
			next := (i + delta + len(tmux.Kinds)) % len(tmux.Kinds)
			return tmux.Kinds[next]
		}
	}
	return tmux.Kinds[0]
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	if w <= 1 {
		return ansi.Truncate(s, w, "")
	}
	return ansi.Truncate(s, w, "…")
}

func padTo(s string, w int) string {
	gap := w - ansi.StringWidth(s)
	if gap <= 0 {
		return s
	}
	return s + strings.Repeat(" ", gap)
}

func centerIn(s string, w int) string {
	gap := w - ansi.StringWidth(s)
	if gap <= 0 {
		return s
	}
	return strings.Repeat(" ", gap/2) + s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
