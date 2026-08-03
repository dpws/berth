package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/dpws/berth/internal/agent"
	"github.com/dpws/berth/internal/config"
	"github.com/dpws/berth/internal/tmux"
)

func openSettings(t *testing.T) *Model {
	t.Helper()
	m := newTestModel()
	m.Update(sessions("alpha"))
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})
	if m.mode != modeSettings {
		t.Fatal("',' did not open the settings screen")
	}
	return m
}

// cursorTo moves the cursor onto the setting with the given config key.
func cursorTo(t *testing.T, m *Model, key string) setting {
	t.Helper()
	for i, s := range m.settings {
		if s.key == key {
			m.settingsCursor = i
			return s
		}
	}
	t.Fatalf("no setting named %q", key)
	return setting{}
}

func typeInto(m *Model, s string) {
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// Every field of the config should be reachable, or the screen is quietly
// hiding something you can only change by editing the file.
func TestEverySettingIsListed(t *testing.T) {
	m := openSettings(t)
	listed := map[string]bool{}
	for _, s := range m.settings {
		listed[s.key] = true
	}

	// The json tags are the source of truth for what a config has.
	data, err := os.ReadFile("../config/config.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		i := strings.Index(line, `json:"`)
		if i < 0 {
			continue
		}
		key := line[i+6:]
		key = key[:strings.Index(key, `"`)]
		if !listed[key] {
			t.Errorf("%s is in the config but not on the settings screen", key)
		}
	}
}

func TestTogglingASettingAppliesImmediately(t *testing.T) {
	m := openSettings(t)
	cursorTo(t, m, "hide_usage")

	if m.cfg.HideUsage {
		t.Fatal("expected rate limits to start shown")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !m.cfg.HideUsage {
		t.Error("enter did not toggle the setting")
	}
	if !m.settingsDirty {
		t.Error("the change was not marked unsaved")
	}
	// Hiding the block should let go of what fed it, not just stop drawing it.
	if m.usageTracker != nil || m.usage != nil {
		t.Error("the usage tracker outlived the setting that wanted it")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.usageTracker == nil {
		t.Error("turning it back on did not rebuild the tracker")
	}
}

func TestEditingAValueAppliesImmediately(t *testing.T) {
	m := openSettings(t)
	cursorTo(t, m, "sidebar_width")
	before := m.sidebarWidth()

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.settingsEditing {
		t.Fatal("enter did not open the editor")
	}
	m.settingInput.SetValue("36")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if m.settingsEditing {
		t.Error("the editor stayed open after a good value")
	}
	if m.cfg.SidebarWidth != 36 {
		t.Errorf("SidebarWidth = %d, want 36", m.cfg.SidebarWidth)
	}
	if m.sidebarWidth() == before {
		t.Error("the change did not reach the layout")
	}
}

// A value that will not parse must be refused where it was typed, not
// swallowed while the old one silently stays.
func TestABadValueIsRefusedInTheEditor(t *testing.T) {
	m := openSettings(t)
	cursorTo(t, m, "refresh_millis")
	before := m.cfg.RefreshMillis

	for _, bad := range []string{"soon", "0", "-5"} {
		m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m.settingInput.SetValue(bad)
		m.Update(tea.KeyMsg{Type: tea.KeyEnter})

		if !m.settingsEditing {
			t.Errorf("%q closed the editor", bad)
			m.settingsEditing = false
		}
		if m.cfg.RefreshMillis != before {
			t.Errorf("%q was accepted, giving %d", bad, m.cfg.RefreshMillis)
		}
		if m.status == "" {
			t.Errorf("%q was refused without saying why", bad)
		}
		m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	}
}

func TestEscapeLeavesTheEditorWithoutChanging(t *testing.T) {
	m := openSettings(t)
	cursorTo(t, m, "claude_command")
	before := m.cfg.ClaudeCommand

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	typeInto(m, "-x")
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if m.settingsEditing {
		t.Error("esc did not leave the editor")
	}
	if m.cfg.ClaudeCommand != before {
		t.Errorf("ClaudeCommand = %q, want it untouched", m.cfg.ClaudeCommand)
	}
}

func TestSaveWritesTheConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))

	m := openSettings(t)
	cursorTo(t, m, "sidebar_width")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.settingInput.SetValue("33")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if m.settingsDirty {
		t.Error("still marked unsaved after saving")
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.SidebarWidth != 33 {
		t.Errorf("saved SidebarWidth = %d, want 33", loaded.SidebarWidth)
	}
}

// Leaving with changes that were never written should say so: they are live,
// so nothing looks wrong until the next start.
func TestClosingWithUnsavedChangesWarns(t *testing.T) {
	m := openSettings(t)
	cursorTo(t, m, "hide_task")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if m.mode != modeNormal {
		t.Fatal("esc did not close the settings screen")
	}
	if !strings.Contains(m.status, "not saved") {
		t.Errorf("status = %q, want a warning about unsaved changes", m.status)
	}
}

func TestResetToDefault(t *testing.T) {
	m := openSettings(t)

	cursorTo(t, m, "sidebar_width")
	m.cfg.SidebarWidth = 99
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if m.cfg.SidebarWidth != config.Default().SidebarWidth {
		t.Errorf("SidebarWidth = %d, want the default", m.cfg.SidebarWidth)
	}

	// Booleans reset too, and resetting one already at its default is a no-op.
	cursorTo(t, m, "hide_task")
	m.cfg.HideTask = true
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if m.cfg.HideTask {
		t.Error("HideTask did not go back to its default")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if m.cfg.HideTask {
		t.Error("resetting an already-default setting flipped it")
	}
}

func TestSessionOptionsRoundTrip(t *testing.T) {
	m := openSettings(t)
	cursorTo(t, m, "session_options")

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.settingInput.SetValue("mouse on,  status off ,")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	want := []string{"mouse on", "status off"}
	if len(m.cfg.SessionOptions) != len(want) {
		t.Fatalf("SessionOptions = %q, want %q", m.cfg.SessionOptions, want)
	}
	for i, v := range want {
		if m.cfg.SessionOptions[i] != v {
			t.Errorf("SessionOptions[%d] = %q, want %q", i, m.cfg.SessionOptions[i], v)
		}
	}
}

// The token is a secret; it should not sit on screen in the clear.
func TestTokenIsMasked(t *testing.T) {
	m := openSettings(t)
	i := 0
	for j, s := range m.settings {
		if s.key == "clip_agent_token" {
			i = j
		}
	}
	m.cfg.ClipAgentToken = "hunter2-and-then-some"

	row := ansi.Strip(m.settingRow(i, false))
	if strings.Contains(row, "hunter2") {
		t.Errorf("the token is shown in the clear: %q", row)
	}
	if !strings.Contains(row, "•") {
		t.Errorf("row = %q, want the value masked", row)
	}

	// Editing it shows the real value, or it could not be corrected.
	m.settingsCursor = i
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.settingInput.Value() != "hunter2-and-then-some" {
		t.Errorf("editor opened with %q", m.settingInput.Value())
	}
}

// The screen has to survive a terminal too short to show it all.
func TestSettingsFitsAnySize(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {60, 10}, {40, 6}, {30, 4}, {20, 3}} {
		m := newTestModel()
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m.Update(sessions("alpha"))
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})

		for _, cursor := range []int{0, len(m.settings) / 2, len(m.settings) - 1} {
			m.settingsCursor = cursor
			view := m.View() // must not panic or index out of range
			if view == "" {
				t.Errorf("%dx%d cursor %d rendered nothing", size[0], size[1], cursor)
			}
		}
	}
}

// The spinner should only be running when something is actually working, or
// an idle berth redraws ten times a second for a glyph that is not moving.
func TestSpinnerRunsOnlyWhileSomethingWorks(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsMsg([]tmux.Session{{Name: "api", Kind: tmux.KindClaude, Managed: true}}))

	withAgents(m, map[string]agent.Info{"api": {Status: agent.Idle}})
	if cmd := m.spinnerCmd(); cmd != nil {
		t.Error("the spinner ticks with nothing working")
	}

	withAgents(m, map[string]agent.Info{"api": {Status: agent.Busy}})
	if cmd := m.spinnerCmd(); cmd == nil {
		t.Fatal("the spinner does not tick while a session works")
	}
	if cmd := m.spinnerCmd(); cmd != nil {
		t.Error("a second spinner chain started alongside the first")
	}

	// Advancing a frame moves the glyph on.
	before, _ := m.statusDot(tmux.Session{Name: "api", Kind: tmux.KindClaude})
	m.Update(spinnerTickMsg{})
	after, _ := m.statusDot(tmux.Session{Name: "api", Kind: tmux.KindClaude})
	if before == after {
		t.Error("the spinner did not advance")
	}

	m.cfg.HideAgentStatus = true
	m.spinnerRunning = false
	if cmd := m.spinnerCmd(); cmd != nil {
		t.Error("the spinner ticks even with agent status hidden")
	}
}

// A tea.Tick cannot be cancelled, so turning the rate limit block off and on
// again left the tick already in flight running beside the chain that replaced
// it. Every toggle added one more read of both agents' logs per interval.
func TestTogglingUsageDoesNotLeaveASecondPollChain(t *testing.T) {
	m := newTestModel()
	inFlight := usageTickMsg{gen: m.usageGen}

	m.cfg.HideUsage = true
	m.applyConfig()
	m.cfg.HideUsage = false
	m.applyConfig()

	if cmd := m.update(inFlight); cmd != nil {
		t.Error("the tick from the replaced chain kept polling")
	}
	if cmd := m.update(usageTickMsg{gen: m.usageGen}); cmd == nil {
		t.Error("the current chain stopped polling")
	}
}
