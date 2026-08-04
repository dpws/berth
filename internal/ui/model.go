package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/dpws/berth/internal/agent"
	"github.com/dpws/berth/internal/clip"
	"github.com/dpws/berth/internal/config"
	"github.com/dpws/berth/internal/doctor"
	"github.com/dpws/berth/internal/git"
	"github.com/dpws/berth/internal/term"
	"github.com/dpws/berth/internal/tmux"
	"github.com/dpws/berth/internal/update"
	"github.com/dpws/berth/internal/usage"
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
	modeSettings
	modePresets
	modeSavePreset
	modeColor
	modeDoctor
)

// attachDelay debounces attaching while the user scrolls the list.
const attachDelay = 120 * time.Millisecond

// How long a message in the footer lasts. It holds at full strength, then
// fades out and gives the row back to the key hints. Errors hold longer: a
// missed error costs more than a missed "created api".
const (
	statusHold      = 4 * time.Second
	statusErrorHold = 8 * time.Second
	statusFade      = 900 * time.Millisecond
	// statusFrame is how often the fade is redrawn, and only while one is
	// running - nothing ticks at this rate when the footer is idle.
	statusFrame = 80 * time.Millisecond
	// spinnerFrame is how often a working session's glyph advances.
	spinnerFrame = 120 * time.Millisecond
)

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
	newStart  string
	// newColor is the palette name a session will be created with, and
	// newSavePreset whether creating it should also save a preset.
	newColor      string
	newSavePreset bool
	// dirMatches is what the directory field would complete to, shown under it
	// so tab is not a guess in the dark.
	dirMatches []string
	// settingInput edits one config value at a time. It is separate from the
	// others so opening settings cannot disturb a half-typed session name.
	settingInput   textinput.Model
	settings       []setting
	settingsCursor int
	// settingsEditing is true while a value is being typed rather than the
	// list being walked.
	settingsEditing bool
	// settingsDirty marks changes made but not yet written to disk. They are
	// already live: the screen edits the running config so you can see what a
	// change does, and saving is what makes it survive a restart.
	settingsDirty bool
	newKind       string
	formField     int

	attachGen int
	pending   string
	// selectName pulls the cursor onto a session as soon as it shows up in a
	// refresh, so a session you just created becomes the selected one.
	selectName string

	status      string
	statusIsErr bool
	// statusAt is when the message landed, which is what the fade is measured
	// from. statusTicking keeps one redraw chain running rather than starting
	// a fresh one on every message that arrives mid-fade.
	statusAt      time.Time
	statusTicking bool

	// usageTracker reads the agents' own logs; it holds per-file state between
	// refreshes, so it is created once and reused. usageGen names the poll
	// chain it belongs to, so a tick left over from an earlier one is dropped
	// rather than starting a second chain beside the current one.
	usageTracker *usage.Tracker
	usageGen     int
	usage        map[string]usage.Limits

	// agentWatcher reads what the agents are doing; like the usage tracker it
	// keeps its place in their logs, so it is created once and reused.
	agentWatcher *agent.Watcher
	agents       map[string]agent.Info

	// clock is what the view asks for the time, so a test can render an age
	// without racing the wall. Nil means the wall clock.
	clock func() time.Time

	// gitStatus is what the bar knows, by directory. It is kept per directory
	// rather than only for the selection so that moving back to a session shows
	// its branch at once instead of blanking until the next read lands.
	// gitDir is the directory last asked about, and gitGen names the poll chain,
	// the same way usageGen does.
	// doctor is what the startup check found and has not been dealt with, and
	// doctorCursor is the one being read about.
	doctor       []doctor.Finding
	doctorCursor int

	// gitTicking says a poll chain is alive, so that saving some unrelated
	// setting does not start a second one beside it.
	gitStatus  map[string]gitEntry
	gitDir     string
	gitGen     int
	gitTicking bool

	// sel is the run of cells being dragged over the session, or the last one
	// dragged. Selection is berth's own rather than the terminal's: the
	// terminal would happily select straight across the sidebar and the
	// divider, because it knows nothing about where berth drew them.
	sel *term.Selection
	// dragging is set once a press has moved far enough to be a drag rather
	// than a click.
	dragging bool
	// pressed holds a press that has not been forwarded yet, because until the
	// button comes up berth does not know whether it was a click for the
	// session or the start of a selection.
	pressed tea.MouseMsg
	// clipboard is a copy waiting to be handed to the terminal on the next
	// frame, as an OSC 52 sequence.
	clipboard string

	// presets are the saved session shapes, kept in their own file next to the
	// config.
	presets      []config.Preset
	presetCursor int
	// presetReturn is the screen the preset list was opened from, so closing
	// it goes back there rather than always to the session list.
	presetReturn uiMode
	// colorCursor is the palette entry highlighted while choosing a colour.
	colorCursor int
	// newerVersion is the tag of a release newer than this build, once the
	// check has found one.
	newerVersion string

	// spinner advances the glyph on working sessions. It only ticks while
	// something is working, so an idle berth redraws no more than before.
	spinner        int
	spinnerRunning bool

	// mouseOn tracks whether berth is capturing the mouse. Capturing it is what
	// lets you click a row and scroll a session, and also what stops your
	// terminal doing its own text selection, so it can be turned off and on
	// again without restarting.
	mouseOn bool

	// title is the last window title berth set, so the escape sequence is only
	// written when it actually changes rather than on every refresh tick.
	title string

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

	value := textinput.New()
	value.CharLimit = 512
	value.Prompt = ""
	value.SetWidth(settingsWidth - 24)

	m := &Model{
		cfg:          cfg,
		nameInput:    name,
		dirInput:     dir,
		settingInput: value,
		settings:     settingsList(),
		newKind:      tmux.KindClaude,
		newStart:     tmux.StartNew,
		appFocused:   true,
		mouseOn:      cfg.Mouse,
	}
	if !cfg.HideUsage {
		m.usageTracker = usage.NewTracker()
	}
	if !cfg.HideAgentStatus {
		m.agentWatcher = agent.NewWatcher()
	}
	// A presets file that will not parse should not stop berth starting; the
	// list simply comes up empty and saving one writes a good file over it.
	m.presets, _ = config.LoadPresets()
	return m
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

// statusTickMsg advances the fade of the message in the footer.
type statusTickMsg struct{}

// spinnerTickMsg advances the glyph on working sessions.
type spinnerTickMsg struct{}

// updateMsg carries the tag of a newer release, when there is one.
type updateMsg string

// usageTickMsg drives the slow poll of the agents' rate limits. It carries the
// generation of the chain that scheduled it: a tea.Tick cannot be cancelled,
// so turning the block off and on again would otherwise leave the tick that
// was already in flight running alongside the chain that replaced it, and the
// logs would be re-read once more per interval for every toggle.
type usageTickMsg struct{ gen int }

type usageMsg map[string]usage.Limits

// gitTickMsg drives the poll of the selected session's repository. Like
// usageTickMsg it carries the generation that scheduled it, so a tick left in
// flight by a chain that has since been replaced is dropped rather than
// doubling the rate every time the bar is turned off and on again.
type gitTickMsg struct{ gen int }

// gitMsg is one directory's answer, named by the directory it is about: the
// cursor can move while a read is in flight, and the reply has to be filed
// under what was asked rather than under whatever is selected when it lands.
type gitMsg struct {
	dir    string
	status git.Status
	ok     bool
}

type attachMsg struct {
	name string
	gen  int
}

type paneMsg struct{ pane *term.Pane }

// sessionCreatedMsg carries the result of creating a session back to the
// update loop, since what follows - remembering a preset - is model state and
// has no business being touched from a command's goroutine.
type sessionCreatedMsg struct {
	name   string
	preset *config.Preset
	err    error
}

// imagePasteMsg carries the result of looking for an image to hand to the
// focused session. The lookup shells out, so it runs as a command.
type imagePasteMsg struct {
	result clip.Result
	err    error
}

// ---------------------------------------------------------------- lifecycle

func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{listSessions(), tickCmd(m.cfg.RefreshMillis)}
	if m.usageTracker != nil {
		cmds = append(cmds, m.readUsage())
	}
	if !m.cfg.HideGitBar {
		m.gitTicking = true
		cmds = append(cmds, gitTickCmd(m.cfg.GitRefreshSeconds, m.gitGen))
	}
	if m.cfg.CheckUpdates {
		cmds = append(cmds, checkForUpdate(Version))
	}
	if !m.cfg.HideDoctor {
		cmds = append(cmds, runDoctor())
	}
	return tea.Batch(cmds...)
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

func usageTickCmd(sec, gen int) tea.Cmd {
	if sec <= 0 {
		sec = 30
	}
	return tea.Tick(time.Duration(sec)*time.Second, func(time.Time) tea.Msg {
		return usageTickMsg{gen: gen}
	})
}

func gitTickCmd(sec, gen int) tea.Cmd {
	if sec <= 0 {
		sec = 5
	}
	return tea.Tick(time.Duration(sec)*time.Second, func(time.Time) tea.Msg {
		return gitTickMsg{gen: gen}
	})
}

// readGit reads one directory's repository state off the update loop, since it
// shells out to git and walks a worktree.
func readGit(dir string) tea.Cmd {
	return func() tea.Msg {
		st, ok := git.Read(dir)
		return gitMsg{dir: dir, status: st, ok: ok}
	}
}

// gitCmd reads the selected session's directory when the cursor has moved to a
// different one than was last asked about. Riding on Update covers every way
// the selection can change - a key, a click, a filter that leaves something
// else selected - without each of them having to remember to ask.
func (m *Model) gitCmd() tea.Cmd {
	if m.cfg.HideGitBar {
		return nil
	}
	dir := m.selectedDir()
	if dir == "" || dir == m.gitDir {
		return nil
	}
	m.gitDir = dir
	return readGit(dir)
}

// readUsage re-reads the agents' logs off the update loop.
func (m *Model) readUsage() tea.Cmd {
	tracker := m.usageTracker
	if tracker == nil {
		return nil
	}
	return func() tea.Msg { return usageMsg(tracker.Refresh()) }
}

// Version is the build berth is running, set from main so the update check
// and the header can see it.
var Version = "dev"

// checkForUpdate asks whether there is a newer release, off the update loop.
// The answer is cached for a day, so this is usually no request at all, and a
// failed check says nothing rather than complaining.
func checkForUpdate(current string) tea.Cmd {
	return func() tea.Msg {
		return updateMsg(update.Available(context.Background(), current, ""))
	}
}

// readAgents refreshes what each session's agent is doing. It reads small
// files and the tails of logs it is already following, so it rides along with
// the session list rather than polling on its own.
func (m *Model) readAgents(sessions []tmux.Session) {
	if m.agentWatcher == nil {
		return
	}
	m.agents = m.agentWatcher.Refresh(sessions)
}

// syncTitle keeps the title the view will ask for in step with what is
// selected. The title is a property of the view now rather than something
// written by a command, so Bubble Tea writes it only when it changes and the
// flicker that repeating an OSC caused on some terminals cannot come back.
func (m *Model) syncTitle() {
	if m.cfg.HideWindowTitle {
		m.title = ""
		return
	}
	m.title = m.windowTitle()
}

// windowTitle names the selected session, so berth is identifiable among a row
// of terminal tabs rather than showing up as another anonymous shell.
func (m *Model) windowTitle() string {
	// A session blocked on you is worth knowing about from the tab bar alone,
	// so it goes first, where it survives the tab being truncated.
	prefix := ""
	switch n := agent.AnyWaiting(m.agents); {
	case n == 1:
		prefix = "● "
	case n > 1:
		prefix = fmt.Sprintf("●%d ", n)
	}

	s, ok := m.selected()
	if !ok {
		return prefix + "berth"
	}
	return fmt.Sprintf("%s%s (%s) — berth", prefix, safeTitle(s.Name), sessionKind(s))
}

// titleNameMax bounds the session name in the title. Terminals truncate long
// titles themselves, but not always gracefully.
const titleNameMax = 64

// safeTitle makes a session name fit to interpolate into an escape sequence.
// The title is written as OSC 2, which a control character in the name could
// cut short or steer - tmux escapes those on the way out, but a name berth did
// not create is not berth's to trust.
func safeTitle(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			continue
		}
		if b.Len() >= titleNameMax {
			b.WriteString("…")
			break
		}
		b.WriteRune(r)
	}
	return b.String()
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

// Update runs the message through the model, then keeps the terminal's title
// in step with whatever the result left selected.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := tea.Batch(m.update(msg), m.statusCmd(), m.spinnerCmd(), m.gitCmd())
	m.syncTitle()
	return m, cmd
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		if m.pane != nil {
			w, h := m.terminalSize()
			m.pane.Resize(w, h)
		}
		return nil

	case tickMsg:
		return tea.Batch(listSessions(), tickCmd(m.cfg.RefreshMillis))

	case spinnerTickMsg:
		m.spinnerRunning = false
		m.spinner++
		return nil

	case statusTickMsg:
		m.statusTicking = false
		if m.statusLife() >= 1 {
			m.status = ""
		}
		return nil

	case usageTickMsg:
		if msg.gen != m.usageGen {
			return nil // a chain that has been replaced
		}
		return m.readUsage()

	case usageMsg:
		m.usage = msg
		return usageTickCmd(m.cfg.UsageRefreshSeconds, m.usageGen)

	case gitTickMsg:
		if m.cfg.HideGitBar {
			// Only the current chain reports its own death: an older one is
			// already dead and has no business clearing the flag for the chain
			// that replaced it.
			if msg.gen == m.gitGen {
				m.gitTicking = false
			}
			return nil
		}
		if msg.gen != m.gitGen {
			return nil // a chain that has been replaced
		}
		// The tick reschedules itself rather than waiting for the read it
		// starts. A read is also fired whenever the cursor lands on a new
		// directory, and if those replies scheduled ticks too there would soon
		// be a chain per session visited.
		next := gitTickCmd(m.cfg.GitRefreshSeconds, m.gitGen)
		if m.gitDir == "" {
			return next
		}
		return tea.Batch(next, readGit(m.gitDir))

	case doctorMsg:
		m.showDoctor(msg)
		return nil

	case gitMsg:
		if m.gitStatus == nil {
			m.gitStatus = make(map[string]gitEntry)
		}
		m.gitStatus[msg.dir] = gitEntry{status: msg.status, ok: msg.ok}
		return nil

	case updateMsg:
		if msg == "" {
			return nil
		}
		m.newerVersion = string(msg)
		m.setStatus(string(msg)+" is out - run berth update", false)
		return nil

	case sessionsMsg:
		m.readAgents(msg)
		return m.applySessions(msg)

	case errMsg:
		m.setStatus(msg.err.Error(), true)
		return nil

	case sessionCreatedMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			m.selectName = msg.name
			return listSessions()
		}
		text := "created " + msg.name
		if msg.preset != nil {
			m.presets = config.AddPreset(m.presets, *msg.preset)
			if err := config.SavePresets(m.presets); err != nil {
				m.setStatus("created "+msg.name+", but the preset did not save: "+
					err.Error(), true)
				m.selectName = msg.name
				return listSessions()
			}
			text += ", saved as a preset"
		}
		m.setStatus(text, false)
		m.selectName = msg.name
		return listSessions()

	case statusMsg:
		m.setStatus(msg.text, msg.isErr)
		if msg.selectName != "" {
			m.selectName = msg.selectName
		}
		return listSessions()

	case attachMsg:
		if msg.gen != m.attachGen {
			return nil // superseded by a newer selection
		}
		m.pending = ""
		return m.attach(msg.name)

	case imagePasteMsg:
		if msg.err != nil {
			m.setStatus("no image to paste - "+msg.err.Error(), true)
			return nil
		}
		if m.pane == nil {
			m.setStatus("no session to paste into", true)
			return nil
		}
		// A trailing space separates the path from whatever is typed next.
		m.pane.SendText(msg.result.Path + " ")
		m.focus = focusTerminal
		m.syncPaneFocus()
		m.setStatus(fmt.Sprintf("pasted %s from %s",
			filepath.Base(msg.result.Path), msg.result.Source), false)
		return nil

	case paneMsg:
		if msg.pane != m.pane {
			return nil // a pane we already replaced
		}
		if msg.pane.Exited() {
			// The tmux client went away: the session was killed or the server
			// detached us. Refresh and let the list decide what to show.
			//
			// Dropping the reference is not enough: the pty and the emulator
			// stay open, and writeLoop sits in the emulator's pipe forever, so
			// every session killed or detached would cost a goroutine and two
			// descriptors for as long as berth runs.
			msg.pane.Close()
			m.pane = nil
			m.focus = focusSidebar
			return listSessions()
		}
		return waitForPane(msg.pane)

	case tea.FocusMsg:
		m.appFocused = true
		m.syncPaneFocus()
		return nil

	case tea.BlurMsg:
		m.appFocused = false
		m.syncPaneFocus()
		return nil

	case tea.MouseMsg:
		if !m.mouseOn {
			return nil // a leftover event from before the mouse was released
		}
		return m.handleMouse(msg)

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.PasteMsg:
		// A paste is its own message now rather than a flag on a key, and it
		// only means anything to the session: berth's own fields take typing a
		// character at a time, and the list has nothing to paste into.
		if m.mode != modeNormal || m.focus != focusTerminal || m.pane == nil {
			return nil
		}
		if msg.Content == "" {
			// A paste that arrived with nothing in it is what a terminal sends
			// when its own paste key was pressed over a clipboard holding no
			// text - an image, most often, which is exactly what berth can
			// fetch and the terminal cannot. Windows Terminal keeps ctrl+v for
			// itself, so this is the only way that key reaches berth there.
			return m.pasteImage()
		}
		m.pane.Paste(msg.Content)
		return nil
	}

	return nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
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
	case modeSettings:
		return m.handleSettingsKey(msg)
	case modePresets:
		return m.handlePresetsKey(msg)
	case modeSavePreset:
		return m.handleSavePresetKey(msg)
	case modeColor:
		return m.handleColorKey(msg)
	case modeDoctor:
		return m.handleDoctorKey(msg)
	}

	// Ctrl+O toggles which half of the screen owns the keyboard. Everything
	// else, when the terminal has focus, belongs to the session.
	if msg.String() == "ctrl+o" {
		m.toggleFocus()
		return nil
	}

	// The paste and quit keys are the other combinations berth keeps for
	// itself while a session has focus, so both are configurable.
	if m.cfg.PasteImageKey != "" && msg.String() == m.cfg.PasteImageKey {
		return m.pasteImage()
	}
	if m.cfg.QuitKey != "" && msg.String() == m.cfg.QuitKey {
		// Quitting is not destructive - the sessions are tmux's, and they keep
		// running - so it does not ask first.
		m.quitting = true
		return tea.Quit
	}

	if m.focus == focusTerminal {
		if m.pane == nil {
			m.focus = focusSidebar
			return nil
		}
		m.clearSelection()
		if text, ok := typedText(msg.Key()); ok {
			m.pane.SendText(text)
			return nil
		}
		if key, ok := term.ToKeyPress(msg); ok {
			m.pane.SendKey(key)
		}
		return nil
	}

	return m.handleSidebarKey(msg)
}

// handleMouse routes a click or wheel event to whichever half of the screen it
// landed on. Sidebar events drive the list; terminal events are translated
// into the pane's coordinate space and handed to the session.
// typedText reports the characters a key produced, when it produced any.
//
// Text is set only for a key that made characters, so it is what separates
// typing from a key that has to be encoded; forwarding the whole of it keeps a
// multi-byte character or a dead-key sequence whole rather than sending its
// first rune.
//
// Shift is not a modifier here, it is part of how the character was made:
// shift+a arrives as "A" with the shift still noted beside it, and insisting on
// an unmodified key drops every capital letter and every symbol typed with
// shift. Alt and ctrl are different - they change what a key means rather than
// which character it is - so those are left to be encoded.
func typedText(k tea.Key) (string, bool) {
	if k.Text == "" || k.Mod&^tea.ModShift != 0 {
		return "", false
	}
	return k.Text, true
}

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if m.mode != modeNormal {
		return nil
	}

	at := msg.Mouse()

	// Everything below the git bar sits one row lower than the screen says, so
	// the offset comes off before any of this decides what was clicked.
	top := m.topBarHeight()
	if at.Y < top {
		return nil // the bar itself
	}

	sideW := m.sidebarWidth()
	if at.X < sideW {
		return m.handleSidebarMouse(msg, at.Y-top)
	}
	if at.X < sideW+1+gutter || m.pane == nil {
		return nil // the divider and the blank column beside it
	}

	x, y := at.X-sideW-1-gutter, at.Y-top
	if y >= m.bodyHeight() {
		return nil // the footer
	}
	return m.handleTerminalMouse(msg, x, y)
}

// handleTerminalMouse decides between selecting text and handing the event to
// the session.
//
// A drag selects, because that is what a drag does everywhere else and the
// terminal cannot do it here - berth is holding the mouse, and were it not,
// the terminal would select across the sidebar and divider as well. A click
// still belongs to the session, so a press is held back until the button comes
// up and it is clear which of the two happened.
func (m *Model) handleTerminalMouse(msg tea.MouseMsg, x, y int) tea.Cmd {
	forward := func(ev tea.MouseMsg, ex, ey int) {
		if e, ok := term.ToMouse(ev, ex, ey); ok {
			m.pane.SendMouse(e)
		}
	}

	// The kind of event is its own type now rather than a field, so what used
	// to be a chain of conditions is a switch on the message itself.
	switch msg.(type) {
	case tea.MouseClickMsg:
		if msg.Mouse().Button != tea.MouseLeft {
			break
		}
		m.pressed = msg
		m.dragging = false
		m.sel = &term.Selection{AnchorX: x, AnchorY: y, CursorX: x, CursorY: y}
		return nil

	case tea.MouseMotionMsg:
		if m.pressed == nil || m.sel == nil {
			break
		}
		m.sel.CursorX, m.sel.CursorY = x, y
		if !m.sel.Empty() {
			m.dragging = true
		}
		return nil

	case tea.MouseReleaseMsg:
		if m.pressed == nil {
			break
		}
		press := m.pressed
		m.pressed = nil
		if !m.dragging {
			// It was a click after all: give the session the press it never
			// saw, then the release, and take the focus as before.
			m.sel = nil
			if m.focus != focusTerminal {
				m.focus = focusTerminal
				m.syncPaneFocus()
			}
			// The press was held in screen coordinates, so it crosses the same
			// divider, gutter and git bar the release already did. Missing the
			// gutter put the press one column right of the release beside it.
			held := press.Mouse()
			forward(press, held.X-m.sidebarWidth()-1-gutter, held.Y-m.topBarHeight())
			forward(msg, x, y)
			return nil
		}
		m.dragging = false
		return m.copySelection()
	}

	// Anything else - the wheel, the other buttons - is the session's.
	forward(msg, x, y)
	return nil
}

// copySelection puts the selected text on the clipboard.
func (m *Model) copySelection() tea.Cmd {
	if m.pane == nil || m.sel == nil {
		return nil
	}
	text := m.pane.SelectedText(*m.sel)
	if text == "" {
		m.sel = nil
		return nil
	}
	// OSC 52 hands the text to whatever terminal berth is talking to, which
	// over SSH is the one in front of you rather than the remote machine.
	m.clipboard = ansi.SetSystemClipboard(text)
	lines := strings.Count(text, "\n") + 1
	m.setStatus(fmt.Sprintf("copied %d %s", lines, plural(lines, "line", "lines")), false)
	return nil
}

// selection is the range to draw highlighted, if any.
func (m *Model) selection() *term.Selection {
	if m.sel == nil || m.sel.Empty() {
		return nil
	}
	return m.sel
}

// clearSelection drops the highlight once it stops meaning anything - a new
// keystroke, or a different session.
func (m *Model) clearSelection() {
	m.sel, m.pressed, m.dragging = nil, nil, false
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// row is msg.Y already brought into the sidebar's own coordinates, which start
// below the git bar rather than at the top of the screen.
func (m *Model) handleSidebarMouse(msg tea.MouseMsg, row int) tea.Cmd {
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		return m.moveCursor(-1)
	case tea.MouseWheelDown:
		return m.moveCursor(1)
	}

	if _, ok := msg.(tea.MouseClickMsg); !ok || msg.Mouse().Button != tea.MouseLeft {
		return nil
	}
	if row < 0 || row >= len(m.rowSessions) {
		return nil
	}
	target := m.rowSessions[row]
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

func (m *Model) handleSidebarKey(msg tea.KeyPressMsg) tea.Cmd {
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
		m.formField = fieldName
		m.dirMatches = nil
		m.newStart = tmux.StartNew
		m.newColor = ""
		m.newSavePreset = false
		m.newKind = tmux.KindClaude
		m.nameInput.SetValue("")
		m.dirInput.SetValue(defaultDirValue(m.cfg.DefaultDir))
		m.dirInput.Placeholder = m.cfg.DefaultDir
		m.dirInput.CursorEnd()
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

	case "c":
		return m.openColors()

	case "p":
		return m.openPresets()

	case "P":
		return m.openSavePreset()

	case ",":
		m.mode = modeSettings
		m.settingsEditing = false
		return nil

	case "?":
		m.mode = modeHelp
		return nil

	case "m":
		return m.toggleMouse()

	// Shift with the arrows says the same thing as shift with the letters, and
	// is the pair most people reach for first. Both are kept: the arrows are
	// the guessable one, J/K the one that stays under the home row.
	case "K", "shift+up":
		return m.moveSession(-1)
	case "J", "shift+down":
		return m.moveSession(1)

	case "R":
		return listSessions()
	}
	return nil
}

func (m *Model) handleNewKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.mode = modeNormal
		m.nameInput.Blur()
		m.dirInput.Blur()
		return nil

	case "tab":
		// On the directory field tab completes, the way it would in a shell.
		// Everything else, and a path with nothing left to add, moves on.
		if m.formField == fieldDir {
			if m.completeDirField() {
				return nil
			}
		}
		return m.moveFormField(1)

	case "down":
		return m.moveFormField(1)

	case "shift+tab", "up":
		return m.moveFormField(-1)

	case "left":
		switch m.formField {
		case fieldKind:
			m.newKind = cycleKind(m.newKind, -1)
			return nil
		case fieldStart:
			m.newStart = cycleStart(m.newStart, -1)
			return nil
		case fieldColor:
			m.newColor = cycleColor(m.newColor, -1)
			return nil
		}

	case "right":
		switch m.formField {
		case fieldPreset:
			return m.openPresets()
		case fieldKind:
			m.newKind = cycleKind(m.newKind, 1)
			return nil
		case fieldStart:
			m.newStart = cycleStart(m.newStart, 1)
			return nil
		case fieldColor:
			m.newColor = cycleColor(m.newColor, 1)
			return nil
		case fieldSavePreset:
			m.newSavePreset = !m.newSavePreset
			return nil
		}

	case " ":
		switch m.formField {
		case fieldKind:
			m.newKind = cycleKind(m.newKind, 1)
			return nil
		case fieldStart:
			m.newStart = cycleStart(m.newStart, 1)
			return nil
		case fieldColor:
			m.newColor = cycleColor(m.newColor, 1)
			return nil
		case fieldSavePreset, fieldPreset:
			if m.formField == fieldSavePreset {
				m.newSavePreset = !m.newSavePreset
				return nil
			}
			return m.openPresets()
		}

	case "enter":
		switch m.formField {
		case fieldPreset:
			return m.openPresets()
		case fieldSavePreset:
			m.newSavePreset = !m.newSavePreset
			return nil
		}
		return m.createSession()
	}

	var cmd tea.Cmd
	switch m.formField {
	case fieldName:
		m.nameInput, cmd = m.nameInput.Update(msg)
	case fieldDir:
		before := m.dirInput.Value()
		m.dirInput, cmd = m.dirInput.Update(msg)
		if m.dirInput.Value() != before {
			// Offer what the new text could become, without changing it.
			_, m.dirMatches = completeDir(m.dirInput.Value())
		}
	}
	return cmd
}

// moveFormField steps between the fields of the new session form.
func (m *Model) moveFormField(delta int) tea.Cmd {
	m.formField = (m.formField + delta + formFields) % formFields
	m.syncFormFocus()
	return textinput.Blink
}

// defaultDirValue is what the directory field starts with: the configured
// default, ready to be completed further rather than shown as a hint you have
// to retype. The trailing separator means the first tab lists what is inside
// it instead of completing the name of the directory itself.
func defaultDirValue(dir string) string {
	if dir == "" {
		return ""
	}
	if strings.HasSuffix(dir, string(os.PathSeparator)) {
		return dir
	}
	return dir + string(os.PathSeparator)
}

// completeDirField extends the directory to the longest path it certainly
// starts with, and reports whether anything changed.
func (m *Model) completeDirField() bool {
	typed := m.dirInput.Value()
	if typed == "" {
		typed = defaultDirValue(m.cfg.DefaultDir)
	}
	completed, matches := completeDir(typed)
	m.dirMatches = matches
	if completed == m.dirInput.Value() {
		return false
	}
	m.dirInput.SetValue(completed)
	m.dirInput.CursorEnd()
	return true
}

func (m *Model) handleRenameKey(msg tea.KeyPressMsg) tea.Cmd {
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

func (m *Model) handleFilterKey(msg tea.KeyPressMsg) tea.Cmd {
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

func (m *Model) handleConfirmKey(msg tea.KeyPressMsg) tea.Cmd {
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

	command := m.commandFor(kind, m.newStart)
	color, start, savePreset := m.newColor, m.newStart, m.newSavePreset
	opts := m.cfg.SessionOptions
	hideStatus := m.cfg.HideStatusBar

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
			HideStatusBar: hideStatus,
			Options:       opts,
		})
		if err != nil {
			return sessionCreatedMsg{name: created, err: err}
		}
		if color != "" {
			// A colour that will not stick is not worth losing the session
			// over, so it is reported rather than raised.
			if err := tmux.SetColor(created, color); err != nil {
				return sessionCreatedMsg{name: created, err: err}
			}
		}

		msg := sessionCreatedMsg{name: created}
		if savePreset {
			msg.preset = &config.Preset{
				Label: created, Session: created, Kind: kind,
				Dir: dir, Color: color, Start: start,
			}
		}
		return msg
	}
}

// commandFor returns the command a session of the given kind should run.
//
// The resume arguments are appended to the configured command rather than
// replacing it, so a command with options of its own keeps them.
func (m *Model) commandFor(kind, start string) string {
	var base, cont, resume string
	switch kind {
	case tmux.KindClaude:
		base, cont, resume = m.cfg.ClaudeCommand, m.cfg.ClaudeContinueArgs, m.cfg.ClaudeResumeArgs
	case tmux.KindCodex:
		base, cont, resume = m.cfg.CodexCommand, m.cfg.CodexContinueArgs, m.cfg.CodexResumeArgs
	default:
		return m.cfg.ShellCommand // a shell has no conversation to carry on
	}

	args := ""
	switch start {
	case tmux.StartContinue:
		args = cont
	case tmux.StartResume:
		args = resume
	}
	if args = strings.TrimSpace(args); args == "" {
		return base
	}
	return strings.TrimSpace(base) + " " + args
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

	// tmux lists sessions by name; berth shows them in the order you put them
	// in, with anything never moved keeping tmux's arrangement at the end.
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].Order < sessions[j].Order
	})
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

// moveSession moves the selected session past its neighbour in the list.
//
// It writes a position for every session on screen rather than only the two
// that swapped: until something is moved they all share "never placed", and
// swapping two of those would change nothing. Numbering everything once, the
// first time, is what makes the second move behave like the first.
func (m *Model) moveSession(delta int) tea.Cmd {
	visible := m.visibleSessions()
	to := m.cursor + delta
	if m.cursor < 0 || m.cursor >= len(visible) || to < 0 || to >= len(visible) {
		return nil
	}

	// Swap where the two sit in the full list, so a move past a filtered-out
	// session still lands where it looked like it would.
	a := indexOfSession(m.sessions, visible[m.cursor].Name)
	b := indexOfSession(m.sessions, visible[to].Name)
	if a < 0 || b < 0 {
		return nil
	}
	order := append([]tmux.Session(nil), m.sessions...)
	order[a], order[b] = order[b], order[a]

	moved := visible[m.cursor].Name
	m.sessions = order
	m.cursor = to
	m.selectName = moved

	names := make([]string, len(order))
	for i, s := range order {
		names[i] = s.Name
	}
	return func() tea.Msg {
		for i, name := range names {
			if err := tmux.SetOrder(name, i); err != nil {
				return errMsg{err}
			}
		}
		return statusMsg{selectName: moved}
	}
}

// indexOfSession finds a session by name.
func indexOfSession(sessions []tmux.Session, name string) int {
	for i, s := range sessions {
		if s.Name == name {
			return i
		}
	}
	return -1
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
	// The generation moves even when there is nothing to attach, because an
	// attach already in flight has to be superseded either way: stepping to the
	// next session and straight back within the delay would otherwise let the
	// scheduled one land and leave the pane on a session the cursor is not on.
	m.attachGen++
	gen := m.attachGen
	if m.pane != nil && m.pane.Session == name && !m.pane.Exited() {
		m.pending = ""
		return nil
	}
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

	m.clearSelection()
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

// toggleMouse hands the mouse back to your terminal, and takes it again.
//
// While berth is capturing it, dragging over the session selects nothing: the
// drag is forwarded to the program in the pane instead of being the terminal's
// own selection. Rather than making that a restart-and-edit-the-config affair,
// it is a key.
func (m *Model) toggleMouse() tea.Cmd {
	m.mouseOn = !m.mouseOn
	if m.mouseOn {
		m.setStatus("mouse on - berth takes clicks and the wheel", false)
	} else {
		m.setStatus("mouse off - your terminal selects, berth stops clicking", false)
	}
	// Nothing to send: the next view asks for the mode it wants.
	return nil
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
	case fieldName:
		m.nameInput.Focus()
	case fieldDir:
		m.dirInput.Focus()
	}
}

func (m *Model) setStatus(text string, isErr bool) {
	m.status = text
	m.statusIsErr = isErr
	m.statusAt = time.Now()
}

// statusLife returns how far through its life the message is, from 0 when it
// arrives to 1 once it has faded out entirely.
func (m *Model) statusLife() float64 {
	if m.status == "" {
		return 1
	}
	hold := statusHold
	if m.statusIsErr {
		hold = statusErrorHold
	}
	age := time.Since(m.statusAt) - hold
	if age <= 0 {
		return 0
	}
	return float64(age) / float64(statusFade)
}

// spinnerCmd keeps the spinner turning while any session is working, and stops
// when they all go quiet - an idle berth should not be redrawing ten times a
// second for a glyph that is not moving.
func (m *Model) spinnerCmd() tea.Cmd {
	if m.spinnerRunning || m.cfg.HideAgentStatus {
		return nil
	}
	working := false
	for _, info := range m.agents {
		if info.Status.Active() {
			working = true
			break
		}
	}
	if !working {
		return nil
	}
	m.spinnerRunning = true
	return tea.Tick(spinnerFrame, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

// statusCmd keeps a redraw running while a message is fading, and stops as
// soon as the footer is quiet again. Riding on Update means the many places
// that set a status do not each have to remember to schedule one.
func (m *Model) statusCmd() tea.Cmd {
	if m.status == "" || m.statusTicking {
		return nil
	}
	m.statusTicking = true
	return tea.Tick(statusFrame, func(time.Time) tea.Msg { return statusTickMsg{} })
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

// bodyHeight is the room left for the sidebar and the terminal once the rule
// and the hotkey line beneath them, and the git bar above them, are taken out.
func (m *Model) bodyHeight() int { return max(1, m.height-2-m.topBarHeight()) }

// gutter is the blank column between the divider and the session, so the
// terminal's own output does not start hard against the line.
const gutter = 1

func (m *Model) terminalSize() (int, int) {
	return max(2, m.width-m.sidebarWidth()-1-gutter), m.bodyHeight()
}

// ---------------------------------------------------------------- view

// View wraps the rendered screen in the properties berth wants from the
// terminal. In v2 the alt screen, focus reporting, the mouse and the window
// title are all asked for by the view rather than set once at startup, which
// is why turning the mouse off is now just a different view rather than a
// command that has to be remembered and undone.
func (m *Model) View() tea.View {
	v := tea.NewView(m.screen())
	v.AltScreen = true
	// Focus reporting only matters once tmux has focus-events on, but asking
	// for it costs nothing when it does not.
	v.ReportFocus = true
	v.WindowTitle = m.title
	if m.mouseOn {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

// screen renders the frame itself: everything inside the window, as one string
// of exactly as many rows as the window is tall.
func (m *Model) screen() string {
	if !m.ready {
		return "starting berth…"
	}
	if m.quitting {
		return ""
	}

	switch m.mode {
	case modeSettings:
		return m.settingsView()
	case modePresets:
		return m.presetsView()
	case modeColor:
		return m.colorsView()
	case modeNew, modeRename, modeConfirmKill, modeHelp, modeSavePreset, modeDoctor:
		return m.dialogView()
	}

	sideW := m.sidebarWidth()
	bodyH := m.bodyHeight()
	termW, termH := m.terminalSize()

	sidebar := m.sidebarLines(sideW, bodyH)
	terminal := m.terminalLines(termW, termH)

	divStyle := dividerStyle
	if m.focus == focusTerminal {
		divStyle = focusedDivStyle
	}
	div := divStyle.Render("│")
	// Where one of the list's own rules reaches the divider, the divider is
	// drawn as a join instead. A plain "│" leaves the two lines a half cell
	// apart - the rule ends at the edge of its cell and the stroke stands in the
	// middle of the next one - which reads as a gap rather than a corner.
	divJoin := divStyle.Render("┤")

	gap := strings.Repeat(" ", gutter)

	var b strings.Builder
	// A pending copy rides out with this frame. OSC 52 draws nothing, so it is
	// safe ahead of the rest, and clearing it here means it is sent once.
	if m.clipboard != "" {
		b.WriteString(m.clipboard)
		m.clipboard = ""
	}
	// The bar above the body is split at the same column the body is, so its
	// two halves sit over the list and over the session they describe.
	if m.topBarHeight() > 0 {
		b.WriteString(padTo(m.brandBar(sideW), sideW))
		b.WriteString(div)
		b.WriteString(gap)
		b.WriteString(m.gitBarView(termW))
		b.WriteString("\x1b[0m")
		b.WriteByte('\n')
		b.WriteString(m.topRule(sideW))
		b.WriteString("\x1b[0m")
		b.WriteByte('\n')
	}
	for i := 0; i < bodyH; i++ {
		b.WriteString(sidebar[i])
		if isSidebarRule(sidebar[i]) {
			b.WriteString(divJoin)
		} else {
			b.WriteString(div)
		}
		b.WriteString(gap)
		b.WriteString(terminal[i])
		b.WriteString("\x1b[0m")
		if i < bodyH-1 {
			b.WriteByte('\n')
		}
	}
	b.WriteByte('\n')
	b.WriteString(m.footerRule(sideW))
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

	lines := m.pane.Render(m.focus == focusTerminal, m.selection())
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

// topRule closes the bar off from the body, mirroring the rule at the foot of
// the screen: same lit half, same meeting with the divider. Without it the
// branch runs into the session's first line of output, and berth's name sits
// straight on top of the list.
//
// The join is a cross rather than a tee, because unlike the rule at the foot of
// the screen this one has the divider running on both sides of it: the bar
// above is split by it too. A tee left the stroke above hanging.
func (m *Model) topRule(sideW int) string {
	return m.rule(sideW, "┼")
}

// footerRule closes the two columns off above the hotkeys, meeting the divider
// between them so the corner reads as one drawing rather than two lines that
// happen to cross.
func (m *Model) footerRule(sideW int) string {
	return m.rule(sideW, "┴")
}

// rule draws one horizontal rule across the whole window, joined to the divider
// between the two columns by join.
//
// The half of the rule beside whichever column has the keyboard is lit, which
// is the same question the words at the foot of the screen answer - it is just
// quicker to read a line than a sentence.
func (m *Model) rule(sideW int, join string) string {
	if m.width <= 0 {
		return ""
	}
	if sideW <= 0 || sideW >= m.width {
		return dividerStyle.Render(strings.Repeat("─", m.width))
	}

	left, right := dividerStyle, dividerStyle
	if m.focus == focusTerminal {
		right = focusedDivStyle
	} else {
		left = focusedDivStyle
	}
	corner := dividerStyle
	if m.focus == focusTerminal {
		// The divider above meets the rule here, and both are lit while the
		// session has the keyboard - leaving the join dim breaks the line.
		corner = focusedDivStyle
	}
	return left.Render(strings.Repeat("─", sideW)) +
		corner.Render(join) +
		right.Render(strings.Repeat("─", max(0, m.width-sideW-1)))
}

func (m *Model) footerView() string {
	var help string
	switch {
	case m.focus == focusTerminal:
		name := "session"
		if m.pane != nil {
			name = m.pane.Session
		}
		// Say what happens to everything that is not listed, since while a
		// session has the keyboard that is nearly every key there is.
		pairs := []string{"ctrl+o", "back to the list"}
		if m.cfg.QuitKey != "" {
			pairs = append(pairs, m.cfg.QuitKey, "quit")
		}
		pairs = append(pairs, m.cfg.PasteImageKey, "image")
		help = joinHelpWidth(max(0, m.width-2), pairs...) +
			footerStyle.Render("  ·  everything else typed goes to ") +
			footerKeyStyle.Render(name)
	default:
		// One cell each side keeps the hints off the edge of the screen.
		help = joinHelpWidth(max(0, m.width-2),
			"↑/↓", "move",
			"shift+↑/↓", "reorder",
			"enter", "terminal",
			"n", "new",
			"x", "kill",
			"r", "rename",
			"/", "filter",
			m.cfg.PasteImageKey, "image",
			"p", "presets",
			",", "settings",
			"?", "help",
			"q", "quit",
		)
	}

	if m.status != "" {
		color := colSuccess
		if m.statusIsErr {
			color = colDanger
		}
		style := lipgloss.NewStyle().Foreground(fadeColor(color, m.statusLife()))
		help = style.Render(m.status)
	}
	// A leading space lines the hotkeys up with the sidebar's own rows, and
	// the whole line is cut to the screen: an overlong footer wraps, and a
	// wrapped footer pushes the rest of the frame up off the top.
	return padTo(truncate(" "+help, m.width), m.width)
}

func joinHelp(pairs ...string) string {
	return joinHelpWidth(0, pairs...)
}

// joinHelpWidth renders as many key hints as fit in w cells, dropping whole
// hints off the end rather than letting the line wrap or be cut mid-word. A
// width of zero means no limit.
func joinHelpWidth(w int, pairs ...string) string {
	out, used := "", 0
	for i := 0; i+1 < len(pairs); i += 2 {
		hint := footerKeyStyle.Render(pairs[i]) + " " + footerStyle.Render(pairs[i+1])
		sep := ""
		if out != "" {
			sep = footerStyle.Render(" · ")
		}
		if w > 0 && used+lipgloss.Width(sep)+lipgloss.Width(hint) > w {
			break
		}
		out += sep + hint
		used += lipgloss.Width(sep) + lipgloss.Width(hint)
	}
	return out
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
	case modeSavePreset:
		body = m.savePresetDialog()
	case modeHelp:
		body = m.helpText()
	case modeDoctor:
		// The doctor draws its own panel: it is the one dialog that has to fit
		// rather than be cut, since the line it would lose names the keys that
		// dismiss it.
		return m.placeDialog(m.doctorPanel())
	}
	return m.placeDialog(dialogStyle.Render(body))
}

// placeDialog centres a dialog over the screen, cut to the height of the window
// first.
//
// Bubble Tea drops lines off the top of a view taller than the terminal, and
// lipgloss.Place will not shrink a block that already overflows. Between them,
// a help screen or a long preset list on an ordinary 24-row terminal loses its
// border and its first rows with nothing at all to say so. Cutting from the
// bottom keeps the title where it belongs and names what was lost.
func (m *Model) placeDialog(dialog string) string {
	if lines := strings.Split(dialog, "\n"); m.height > 0 && len(lines) > m.height {
		lines = lines[:m.height]
		lines[m.height-1] = faintStyle.Render(" … window too short")
		dialog = strings.Join(lines, "\n")
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (m *Model) newDialog() string {
	label := func(i int, s string) string {
		if m.formField == i {
			return labelActiveStyle.Render(s)
		}
		return labelStyle.Render(s)
	}

	chips := func(options []string, current string) string {
		var out []string
		for _, o := range options {
			if o == current {
				out = append(out, chipActiveStyle.Render(o))
			} else {
				out = append(out, chipStyle.Render(o))
			}
		}
		return strings.Join(out, " ")
	}

	start := chips(tmux.Starts, m.newStart)
	if m.newKind == tmux.KindShell {
		// A shell has no conversation to carry on, so the row stays visible
		// but says why it is doing nothing.
		start = faintStyle.Render("only for agents")
	}

	from := faintStyle.Render("no presets saved yet")
	if len(m.presets) > 0 {
		from = itemMutedStyle.Render(fmt.Sprintf("choose one of %d →", len(m.presets)))
		if m.formField == fieldPreset {
			from = labelActiveStyle.Render(fmt.Sprintf("choose one of %d →", len(m.presets)))
		}
	}

	// The tick has no label of its own, so the highlight has to go on the text
	// itself - styling the empty label column showed nothing at all.
	tick := "[ ] save as a preset"
	if m.newSavePreset {
		tick = "[✓] save as a preset"
	}
	if m.formField == fieldSavePreset {
		tick = labelActiveStyle.Render(tick)
	} else {
		tick = itemMutedStyle.Render(tick)
	}

	rows := []string{
		titleStyle.Render("New session"),
		"",
		label(fieldPreset, "from ") + "  " + from,
		"",
		label(fieldName, "name ") + "  " + m.nameInput.View(),
		label(fieldKind, "kind ") + "  " + chips(tmux.Kinds, m.newKind),
		label(fieldStart, "start") + "  " + start,
		label(fieldColor, "color") + "  " + colorChip(m.newColor),
		label(fieldDir, "dir  ") + "  " + m.dirInput.View(),
	}
	if hint := m.dirHint(); hint != "" {
		rows = append(rows, "         "+hint)
	}
	rows = append(rows, "", "       "+tick)
	return strings.Join(append(rows,
		"",
		footerStyle.Render("tab complete dir · ↑/↓ switch · ←/→ choose · enter create · esc cancel"),
	), "\n")
}

// dirHint lists what the directory field could complete to.
func (m *Model) dirHint() string {
	if m.formField != fieldDir || len(m.dirMatches) == 0 {
		return ""
	}
	if len(m.dirMatches) == 1 {
		return "" // already completed in full; saying so adds nothing
	}
	shown := m.dirMatches
	more := ""
	if len(shown) > maxCompletions {
		more = fmt.Sprintf(" +%d", len(shown)-maxCompletions)
		shown = shown[:maxCompletions]
	}
	return faintStyle.Render(strings.Join(shown, "  ") + more)
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

// savePresetDialog asks what to call the preset being saved.
func (m *Model) savePresetDialog() string {
	name := ""
	if s, ok := m.selected(); ok {
		name = s.Name + " (" + sessionKind(s) + ")"
	}
	return strings.Join([]string{
		titleStyle.Render("Save preset"),
		"",
		footerStyle.Render("from " + name),
		"",
		m.nameInput.View(),
		"",
		footerStyle.Render("enter save · esc cancel"),
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
		{"c", "give the session a colour"},
		{"shift+↑/↓", "move the session up or down the list"},
		{"J / K", "the same, without reaching for the arrows"},
		{"/", "filter by name"},
		{"p", "start from a preset"},
		{"P", "save this session as a preset"},
		{",", "settings"},
		{m.cfg.PasteImageKey, "paste an image path into the session"},
		{"click", "select a row; click again to focus it"},
		{"wheel", "scroll the list, or the focused session"},
		{"drag", "select text in the session; copied on release"},
		{"m", "hand the mouse to your terminal instead"},
		{"R", "refresh the list now"},
		{"q", "quit (sessions keep running)"},
		{m.cfg.QuitKey, "quit from a focused session too"},
	}
	if m.cfg.QuitKey == "" {
		rows = rows[:len(rows)-1]
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("berth") + "\n\n")
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("%s  %s\n",
			footerKeyStyle.Render(padTo(r[0], 12)), footerStyle.Render(r[1])))
	}
	if !m.cfg.HideAgentStatus {
		b.WriteString("\n")
		glyphs := [][2]string{
			{"◐", "the agent is working"},
			{"▲", "the agent is waiting on you"},
			{"○", "idle, or no client attached"},
		}
		if !m.cfg.HideAgentAge && !m.cfg.HideTask {
			glyphs = append(glyphs,
				[2]string{"12m", "how long it has been doing that"})
		}
		for _, r := range glyphs {
			b.WriteString(fmt.Sprintf("%s  %s\n",
				footerKeyStyle.Render(padTo(r[0], 12)), footerStyle.Render(r[1])))
		}
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

// The fields of the new session form, in the order they are shown.
const (
	// fieldPreset is a way in rather than a value: it opens the saved presets
	// and fills the rest of the form from one.
	fieldPreset = iota
	fieldName
	fieldKind
	fieldStart
	fieldColor
	fieldDir
	fieldSavePreset
	formFields
)

// cycleStart steps through the start modes, wrapping in either direction.
func cycleStart(s string, delta int) string {
	for i, start := range tmux.Starts {
		if start == s {
			return tmux.Starts[(i+delta+len(tmux.Starts))%len(tmux.Starts)]
		}
	}
	return tmux.Starts[0]
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
