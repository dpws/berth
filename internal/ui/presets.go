package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dpws/berth/internal/config"
	"github.com/dpws/berth/internal/tmux"
)

// presetWidth is the inner width of the preset list.
const presetWidth = 58

// presetFrom describes the selected session as a preset, which is the whole
// point of saving one: you have a session set up the way you want it and would
// like another like it later.
func presetFrom(s tmux.Session, label string) config.Preset {
	return config.Preset{
		Label:   strings.TrimSpace(label),
		Session: s.Name,
		Kind:    s.DetectedKind(),
		Dir:     s.Dir,
		Start:   tmux.StartNew,
	}
}

// openPresets shows the saved presets.
func (m *Model) openPresets() tea.Cmd {
	if len(m.presets) == 0 {
		m.setStatus("no presets yet - P saves the selected session as one", false)
		return nil
	}
	m.mode = modePresets
	if m.presetCursor >= len(m.presets) {
		m.presetCursor = len(m.presets) - 1
	}
	return nil
}

// openSavePreset asks what to call a preset made from the selected session.
func (m *Model) openSavePreset() tea.Cmd {
	s, ok := m.selected()
	if !ok {
		m.setStatus("no session to save", true)
		return nil
	}
	m.mode = modeSavePreset
	m.nameInput.SetValue(s.Name)
	m.nameInput.CursorEnd()
	m.nameInput.Focus()
	return textinput.Blink
}

// handlePresetsKey drives the list of saved presets.
func (m *Model) handlePresetsKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q", "p":
		m.mode = modeNormal
		return nil

	case "up", "k", "ctrl+p":
		m.movePreset(-1)
	case "down", "j", "ctrl+n":
		m.movePreset(1)
	case "home", "g":
		m.movePreset(-len(m.presets))
	case "end", "G":
		m.movePreset(len(m.presets))

	case "enter", "l", "right":
		return m.usedPreset()

	case "x", "d":
		return m.deletePreset()
	}
	return nil
}

func (m *Model) movePreset(delta int) {
	m.presetCursor += delta
	if m.presetCursor < 0 {
		m.presetCursor = 0
	}
	if m.presetCursor >= len(m.presets) {
		m.presetCursor = len(m.presets) - 1
	}
}

// usedPreset opens the new session form filled in from the preset, rather than
// creating the session outright: the point of a preset is to save the typing,
// not to take away the last look before something starts.
func (m *Model) usedPreset() tea.Cmd {
	if m.presetCursor < 0 || m.presetCursor >= len(m.presets) {
		return nil
	}
	p := m.presets[m.presetCursor]

	m.mode = modeNew
	m.formField = fieldName
	m.dirMatches = nil

	m.newKind = p.Kind
	if m.newKind == "" {
		m.newKind = tmux.KindClaude
	}
	m.newStart = p.Start
	if m.newStart == "" {
		m.newStart = tmux.StartNew
	}

	m.nameInput.SetValue(p.Session)
	m.nameInput.CursorEnd()
	m.nameInput.Focus()
	m.dirInput.Blur()

	dir := p.Dir
	if dir == "" {
		dir = m.cfg.DefaultDir
	}
	m.dirInput.SetValue(defaultDirValue(dir))
	m.dirInput.Placeholder = m.cfg.DefaultDir
	m.dirInput.CursorEnd()
	return textinput.Blink
}

func (m *Model) deletePreset() tea.Cmd {
	if m.presetCursor < 0 || m.presetCursor >= len(m.presets) {
		return nil
	}
	gone := m.presets[m.presetCursor].Label
	m.presets = config.RemovePreset(m.presets, m.presetCursor)
	if m.presetCursor >= len(m.presets) {
		m.presetCursor = max(0, len(m.presets)-1)
	}
	if err := config.SavePresets(m.presets); err != nil {
		m.setStatus("could not save presets: "+err.Error(), true)
		return nil
	}
	m.setStatus("removed preset "+gone, false)
	if len(m.presets) == 0 {
		m.mode = modeNormal
	}
	return nil
}

// handleSavePresetKey drives the little dialog that names a new preset.
func (m *Model) handleSavePresetKey(msg tea.KeyMsg) tea.Cmd {
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
		label := strings.TrimSpace(m.nameInput.Value())
		m.mode = modeNormal
		m.nameInput.Blur()
		if label == "" {
			m.setStatus("a preset needs a name", true)
			return nil
		}

		m.presets = config.AddPreset(m.presets, presetFrom(s, label))
		if err := config.SavePresets(m.presets); err != nil {
			m.setStatus("could not save presets: "+err.Error(), true)
			return nil
		}
		m.setStatus("saved preset "+label, false)
		return nil
	}

	var cmd tea.Cmd
	m.nameInput, cmd = m.nameInput.Update(msg)
	return cmd
}

// presetsView draws the list.
func (m *Model) presetsView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Presets"))
	b.WriteString(itemMutedStyle.Render(fmt.Sprintf("  %d", len(m.presets))))
	b.WriteString("\n\n")

	for i, p := range m.presets {
		b.WriteString(m.presetRow(i, p, i == m.presetCursor))
		b.WriteByte('\n')
	}

	b.WriteString("\n")
	b.WriteString(joinHelp("enter", "use", "x", "remove", "esc", "close"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
		dialogStyle.Render(b.String()))
}

// presetRow renders one preset as its label, then where and how it starts.
func (m *Model) presetRow(i int, p config.Preset, selected bool) string {
	start := p.Start
	if start == "" || start == tmux.StartNew {
		start = ""
	} else {
		start = " " + start
	}
	detail := p.Kind + start
	if p.Dir != "" {
		detail += "  " + shortenPath(p.Dir)
	}

	label := truncate(p.Label, 20)
	detail = truncate(detail, presetWidth-lipgloss.Width(label)-3)
	gap := max(1, presetWidth-lipgloss.Width(label)-lipgloss.Width(detail))

	if selected {
		return itemSelectedStyle.Render(
			padTo(label+strings.Repeat(" ", gap)+detail, presetWidth))
	}
	return itemStyle.Render(label) + strings.Repeat(" ", gap) + itemMutedStyle.Render(detail)
}

// shortenPath puts the ~ back, so a list of presets under one home directory
// does not repeat it on every line.
func shortenPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !strings.HasPrefix(p, home) {
		return p
	}
	if p == home {
		return "~"
	}
	if rest := strings.TrimPrefix(p, home+"/"); rest != p {
		return "~/" + rest
	}
	return p
}
