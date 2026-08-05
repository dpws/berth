package ui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/dpws/berth/internal/agent"
	"github.com/dpws/berth/internal/config"
	"github.com/dpws/berth/internal/usage"
)

// settingsWidth is the inner width of the settings panel. Wide enough for a
// path or a command without wrapping, narrow enough to stay readable.
const settingsWidth = 62

// setting is one editable line of the config.
//
// Booleans toggle in place; everything else opens a one-line editor. The pair
// of get and set is deliberately over a string rather than the field itself,
// so the screen does not need to know one field's type from another's.
type setting struct {
	key   string // the name it has in config.json
	label string
	help  string

	// toggle is set for booleans, and flips the value in place.
	toggle func(*config.Config)
	// get renders the value for display.
	get func(config.Config) string
	// edit returns the text the editor should open with. Nil means get.
	edit func(config.Config) string
	// set parses edited text back into the config, or explains why it cannot.
	set func(*config.Config, string) error
	// secret hides the value until it is being edited.
	secret bool
	// restart marks a setting that only takes effect on new sessions or a
	// fresh start, so the screen can say so rather than appearing to lie.
	restart string
}

// onOff renders a boolean the way its label reads.
func onOff(v bool, yes, no string) string {
	if v {
		return yes
	}
	return no
}

// text builds a plain string setting.
func text(key, label, help string, get func(config.Config) string, set func(*config.Config, string)) setting {
	return setting{
		key: key, label: label, help: help,
		get: get,
		set: func(c *config.Config, s string) error { set(c, strings.TrimSpace(s)); return nil },
	}
}

// number builds a positive-integer setting.
func number(key, label, help string, get func(config.Config) int, set func(*config.Config, int)) setting {
	return setting{
		key: key, label: label, help: help,
		get: func(c config.Config) string { return strconv.Itoa(get(c)) },
		set: func(c *config.Config, s string) error {
			n, err := strconv.Atoi(strings.TrimSpace(s))
			if err != nil {
				return fmt.Errorf("%s wants a number", key)
			}
			if n <= 0 {
				return fmt.Errorf("%s must be greater than zero", key)
			}
			set(c, n)
			return nil
		},
	}
}

// flag builds a boolean setting, rendered in the words of its label rather
// than as true and false.
func flag(key, label, help, yes, no string, get func(config.Config) bool, set func(*config.Config, bool)) setting {
	return setting{
		key: key, label: label, help: help,
		get:    func(c config.Config) string { return onOff(get(c), yes, no) },
		toggle: func(c *config.Config) { set(c, !get(*c)) },
	}
}

// settings is every field of the config, in the order it is shown.
func settingsList() []setting {
	return []setting{
		text("claude_command", "Claude command", "Run in sessions of kind claude",
			func(c config.Config) string { return c.ClaudeCommand },
			func(c *config.Config, s string) { c.ClaudeCommand = s }),
		text("codex_command", "Codex command", "Run in sessions of kind codex",
			func(c config.Config) string { return c.CodexCommand },
			func(c *config.Config, s string) { c.CodexCommand = s }),
		text("claude_continue_args", "Claude continue", "Added when a session carries on the most recent conversation",
			func(c config.Config) string { return c.ClaudeContinueArgs },
			func(c *config.Config, s string) { c.ClaudeContinueArgs = s }),
		text("claude_resume_args", "Claude resume", "Added when a session picks an earlier conversation",
			func(c config.Config) string { return c.ClaudeResumeArgs },
			func(c *config.Config, s string) { c.ClaudeResumeArgs = s }),
		text("codex_continue_args", "Codex continue", "Added when a session carries on the most recent conversation",
			func(c config.Config) string { return c.CodexContinueArgs },
			func(c *config.Config, s string) { c.CodexContinueArgs = s }),
		text("codex_resume_args", "Codex resume", "Added when a session picks an earlier conversation",
			func(c config.Config) string { return c.CodexResumeArgs },
			func(c *config.Config, s string) { c.CodexResumeArgs = s }),
		text("shell_command", "Shell command", "Run in sessions of kind shell",
			func(c config.Config) string { return c.ShellCommand },
			func(c *config.Config, s string) { c.ShellCommand = s }),
		text("default_dir", "Default directory", "Offered when creating a session",
			func(c config.Config) string { return c.DefaultDir },
			func(c *config.Config, s string) { c.DefaultDir = s }),

		number("sidebar_width", "Sidebar width", "Columns; never more than half the screen",
			func(c config.Config) int { return c.SidebarWidth },
			func(c *config.Config, n int) { c.SidebarWidth = n }),
		number("refresh_millis", "Session poll", "Milliseconds between tmux list-sessions",
			func(c config.Config) int { return c.RefreshMillis },
			func(c *config.Config, n int) { c.RefreshMillis = n }),
		number("usage_refresh_seconds", "Limits poll", "Seconds between rate limit reads",
			func(c config.Config) int { return c.UsageRefreshSeconds },
			func(c *config.Config, n int) { c.UsageRefreshSeconds = n }),
		number("git_refresh_seconds", "Git poll", "Seconds between reads of the branch and its changes",
			func(c config.Config) int { return c.GitRefreshSeconds },
			func(c *config.Config, n int) { c.GitRefreshSeconds = n }),

		flag("mouse", "Mouse", "Click rows to switch, drag a session to copy",
			"captured by berth", "left to the terminal",
			func(c config.Config) bool { return c.Mouse },
			func(c *config.Config, v bool) { c.Mouse = v }),
		flag("hide_usage", "Rate limits", "The meters under the session list",
			"hidden", "shown",
			func(c config.Config) bool { return c.HideUsage },
			func(c *config.Config, v bool) { c.HideUsage = v }),
		flag("hide_git_bar", "Git bar", "Branch and uncommitted work, above the session",
			"hidden", "shown",
			func(c config.Config) bool { return c.HideGitBar },
			func(c *config.Config, v bool) { c.HideGitBar = v }),
		flag("hide_agent_status", "Agent status", "Working, waiting on you, idle",
			"hidden", "shown",
			func(c config.Config) bool { return c.HideAgentStatus },
			func(c *config.Config, v bool) { c.HideAgentStatus = v }),
		flag("hide_task", "Task line", "What each agent was last asked to do",
			"hidden", "shown",
			func(c config.Config) bool { return c.HideTask },
			func(c *config.Config, v bool) { c.HideTask = v }),
		flag("hide_agent_age", "Agent age", "How long it has been working, waiting or idle",
			"hidden", "shown",
			func(c config.Config) bool { return c.HideAgentAge },
			func(c *config.Config, v bool) { c.HideAgentAge = v }),
		flag("show_host", "Host meters", "Load, memory and disk on the machine berth is running on",
			"shown", "hidden",
			func(c config.Config) bool { return c.ShowHost },
			func(c *config.Config, v bool) { c.ShowHost = v }),
		text("notify", "Notify", "How berth gets your attention: off, bell, desktop, both",
			func(c config.Config) string { return c.Notify },
			func(c *config.Config, v string) { c.Notify = strings.ToLower(strings.TrimSpace(v)) }),
		text("notify_on", "Notify on", "Which moments are worth it: waiting, idle",
			func(c config.Config) string { return strings.Join(c.NotifyOn, ", ") },
			func(c *config.Config, v string) { c.NotifyOn = splitList(v) }),
		text("doctor_skipped", "Checks skipped", "Startup checks you told berth to stop asking about; clear it to hear them again",
			func(c config.Config) string { return strings.Join(c.DoctorSkipped, ", ") },
			func(c *config.Config, v string) { c.DoctorSkipped = splitList(v) }),
		flag("hide_doctor", "Startup check", "Looking over tmux, the terminal and the agents when berth starts",
			"off", "on",
			func(c config.Config) bool { return c.HideDoctor },
			func(c *config.Config, v bool) { c.HideDoctor = v }),
		flag("check_updates", "Update check", "Asks GitHub once a day for the newest release; installs nothing",
			"on", "off",
			func(c config.Config) bool { return c.CheckUpdates },
			func(c *config.Config, v bool) { c.CheckUpdates = v }),
		flag("hide_window_title", "Window title", "Naming the session in the terminal's title bar",
			"left alone", "set by berth",
			func(c config.Config) bool { return c.HideWindowTitle },
			func(c *config.Config, v bool) { c.HideWindowTitle = v }),

		text("paste_image_key", "Paste image key", "Types an image path into the focused session",
			func(c config.Config) string { return c.PasteImageKey },
			func(c *config.Config, s string) { c.PasteImageKey = s }),
		text("quit_key", "Quit key", "Quits from anywhere; empty gives the key back to sessions",
			func(c config.Config) string { return c.QuitKey },
			func(c *config.Config, s string) { c.QuitKey = s }),
		text("image_drop_dir", "Image drop dir", "Searched when the clipboard has no image",
			func(c config.Config) string { return c.ImageDropDir },
			func(c *config.Config, s string) { c.ImageDropDir = s }),
		text("clip_agent_url", "Clip agent URL", "berth-clipd on the machine you are sitting at",
			func(c config.Config) string { return c.ClipAgentURL },
			func(c *config.Config, s string) { c.ClipAgentURL = s }),
		{
			key: "clip_agent_token", label: "Clip agent token",
			help:   "Sent to the agent when it was started with -token",
			secret: true,
			get:    func(c config.Config) string { return c.ClipAgentToken },
			set: func(c *config.Config, s string) error {
				c.ClipAgentToken = strings.TrimSpace(s)
				return nil
			},
		},

		{
			key: "hide_status_bar", label: "tmux status bar",
			help:    "In sessions berth creates",
			restart: "applies to new sessions",
			get: func(c config.Config) string {
				return onOff(c.HideStatusBar, "hidden", "shown")
			},
			toggle: func(c *config.Config) { c.HideStatusBar = !c.HideStatusBar },
		},
		{
			key: "session_options", label: "Session options",
			help:    "tmux set-option arguments, comma separated",
			restart: "applies to new sessions",
			get: func(c config.Config) string {
				if len(c.SessionOptions) == 0 {
					return "none"
				}
				return strings.Join(c.SessionOptions, ", ")
			},
			edit: func(c config.Config) string { return strings.Join(c.SessionOptions, ", ") },
			set: func(c *config.Config, s string) error {
				var out []string
				for _, part := range strings.Split(s, ",") {
					if p := strings.TrimSpace(part); p != "" {
						out = append(out, p)
					}
				}
				c.SessionOptions = out
				return nil
			},
		},
	}
}

// settingsRows is how many settings fit on screen at once.
func (m *Model) settingsRows() int {
	// Title, blank, help line, blank, footer, and the dialog's own border and
	// padding come out of the height before any setting does.
	n := m.height - 10
	if n < 3 {
		n = 3
	}
	if n > len(m.settings) {
		n = len(m.settings)
	}
	return n
}

// settingsView draws the whole screen.
func (m *Model) settingsView() string {
	rows := m.settingsRows()
	start := scrollOffset(m.settingsCursor, len(m.settings), rows)

	var b strings.Builder
	b.WriteString(titleStyle.Render("Settings"))
	b.WriteString(itemMutedStyle.Render(fmt.Sprintf("  %d of %d",
		m.settingsCursor+1, len(m.settings))))
	if m.settingsDirty {
		b.WriteString(errorStyle.Render("  unsaved"))
	}
	b.WriteString("\n\n")

	for i := start; i < start+rows && i < len(m.settings); i++ {
		b.WriteString(m.settingRow(i, i == m.settingsCursor))
		b.WriteByte('\n')
	}

	cur := m.settings[m.settingsCursor]
	b.WriteString("\n")
	help := cur.help
	if cur.restart != "" {
		help += " (" + cur.restart + ")"
	}
	b.WriteString(footerStyle.Render(truncate(help, settingsWidth)))
	b.WriteString("\n")
	b.WriteString(faintStyle.Render(truncate(cur.key, settingsWidth)))
	b.WriteString("\n\n")

	if m.settingsEditing {
		b.WriteString(joinHelp("enter", "apply", "esc", "cancel"))
	} else {
		b.WriteString(joinHelp(
			"↑/↓", "move",
			"enter", "change",
			"d", "default",
			"ctrl+s", "save",
			"esc", "close",
		))
	}
	return m.placeDialog(dialogStyle.Render(b.String()))
}

// settingRow renders one setting as "label   value", right-aligned.
func (m *Model) settingRow(i int, selected bool) string {
	s := m.settings[i]

	if selected && m.settingsEditing {
		label := labelActiveStyle.Render(padTo(truncate(s.label, 22), 22))
		return label + " " + m.settingInput.View()
	}

	value := s.get(m.cfg)
	if s.secret && value != "" {
		value = strings.Repeat("•", min(len(value), 12))
	}
	if value == "" {
		value = "—"
	}

	label := padTo(truncate(s.label, 22), 22)
	value = truncate(value, settingsWidth-24)
	gap := max(1, settingsWidth-lipgloss.Width(label)-lipgloss.Width(value))

	if selected {
		return itemSelectedStyle.Render(padTo(label+strings.Repeat(" ", gap)+value, settingsWidth))
	}
	return labelStyle.Render(label) + strings.Repeat(" ", gap) + itemStyle.Render(value)
}

// handleSettingsKey drives the settings screen.
func (m *Model) handleSettingsKey(msg tea.KeyPressMsg) tea.Cmd {
	if m.settingsEditing {
		return m.handleSettingEdit(msg)
	}
	cur := &m.settings[m.settingsCursor]

	switch msg.String() {
	case "esc", "q", ",":
		m.mode = modeNormal
		if m.settingsDirty {
			m.setStatus("settings changed but not saved - ctrl+s in settings to keep them", true)
		}
		return nil

	case "up", "k", "ctrl+p":
		m.moveSetting(-1)
	case "down", "j", "ctrl+n":
		m.moveSetting(1)
	case "pgup":
		m.moveSetting(-5)
	case "pgdown":
		m.moveSetting(5)
	case "home", "g":
		m.moveSetting(-len(m.settings))
	case "end", "G":
		m.moveSetting(len(m.settings))

	case "enter", " ", "right", "l":
		if cur.toggle != nil {
			cur.toggle(&m.cfg)
			m.settingsDirty = true
			return m.applyConfig()
		}
		return m.startEditing()

	case "left", "h":
		if cur.toggle != nil {
			cur.toggle(&m.cfg)
			m.settingsDirty = true
			return m.applyConfig()
		}

	case "d":
		return m.resetSetting()

	case "ctrl+s", "s":
		return m.saveSettings()
	}
	return nil
}

// handleSettingEdit drives the one-line editor.
func (m *Model) handleSettingEdit(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.settingsEditing = false
		m.settingInput.Blur()
		return nil

	case "enter":
		cur := m.settings[m.settingsCursor]
		if err := cur.set(&m.cfg, m.settingInput.Value()); err != nil {
			// Refuse the value and stay in the editor, rather than closing it
			// and quietly keeping the old one.
			m.setStatus(err.Error(), true)
			return nil
		}
		m.settingsEditing = false
		m.settingInput.Blur()
		m.settingsDirty = true
		return m.applyConfig()
	}

	var cmd tea.Cmd
	m.settingInput, cmd = m.settingInput.Update(msg)
	return cmd
}

func (m *Model) moveSetting(delta int) {
	m.settingsCursor += delta
	if m.settingsCursor < 0 {
		m.settingsCursor = 0
	}
	if m.settingsCursor >= len(m.settings) {
		m.settingsCursor = len(m.settings) - 1
	}
}

// startEditing opens the editor on the selected setting.
func (m *Model) startEditing() tea.Cmd {
	cur := m.settings[m.settingsCursor]
	if cur.set == nil {
		return nil
	}
	value := cur.get(m.cfg)
	if cur.edit != nil {
		value = cur.edit(m.cfg)
	}
	m.settingInput.SetValue(value)
	m.settingInput.CursorEnd()
	m.settingInput.Focus()
	m.settingsEditing = true
	return textinput.Blink
}

// resetSetting puts one setting back to what berth ships with.
func (m *Model) resetSetting() tea.Cmd {
	cur := m.settings[m.settingsCursor]
	d := config.Default()
	switch {
	case cur.toggle != nil:
		// Toggle until it matches the default, which is at most one step.
		if cur.get(m.cfg) != cur.get(d) {
			cur.toggle(&m.cfg)
		}
	case cur.set != nil:
		value := cur.get(d)
		if cur.edit != nil {
			value = cur.edit(d)
		}
		if err := cur.set(&m.cfg, value); err != nil {
			m.setStatus(err.Error(), true)
			return nil
		}
	}
	m.settingsDirty = true
	m.setStatus(cur.key+" back to its default", false)
	return m.applyConfig()
}

// saveSettings writes the running config to disk.
func (m *Model) saveSettings() tea.Cmd {
	if err := m.cfg.Save(); err != nil {
		m.setStatus("could not save: "+err.Error(), true)
		return nil
	}
	m.settingsDirty = false
	m.setStatus("saved to "+config.Path(), false)
	return nil
}

// applyConfig makes a changed setting take effect now rather than on the next
// start. Most are read straight off the config while rendering, so they need
// nothing; these are the ones holding something built at startup.
func (m *Model) applyConfig() tea.Cmd {
	var cmds []tea.Cmd

	switch {
	case m.cfg.HideUsage:
		m.usageTracker, m.usage = nil, nil
	case m.usageTracker == nil:
		m.usageTracker = usage.NewTracker()
		m.usageGen++
		cmds = append(cmds, m.readUsage())
	}

	// Turning the bar back on starts a fresh poll chain, and the generation
	// bump retires whichever tick the old one left in flight rather than
	// letting the two run side by side. The gitTicking guard is what keeps this
	// from starting another chain every time any other setting is saved.
	switch {
	case m.cfg.HideGitBar:
		m.gitStatus, m.gitDir, m.gitTicking = nil, "", false
	case !m.gitTicking:
		m.gitGen++
		m.gitTicking = true
		cmds = append(cmds, gitTickCmd(m.cfg.GitRefreshSeconds, m.gitGen))
	}

	switch {
	case m.cfg.HideAgentStatus:
		m.agentWatcher, m.agents = nil, nil
	case m.agentWatcher == nil:
		m.agentWatcher = agent.NewWatcher()
		cmds = append(cmds, listSessions())
	}

	// The mouse is asked for by the view, so changing it here is only a matter
	// of recording what the next one should ask for.
	m.mouseOn = m.cfg.Mouse

	if m.pane != nil {
		w, h := m.terminalSize()
		m.pane.Resize(w, h)
	}
	return tea.Batch(cmds...)
}

// splitList reads a comma-separated setting back into a list, dropping the
// empties so that clearing the field means an empty list rather than a list
// with one blank in it.
func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
