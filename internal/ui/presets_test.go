package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/dpws/berth/internal/config"
	"github.com/dpws/berth/internal/tmux"
)

func presetModel(t *testing.T) *Model {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))

	m := newTestModel()
	m.Update(sessionsMsg([]tmux.Session{
		{Name: "api", Kind: tmux.KindClaude, Managed: true, Dir: "/code/api"},
		{Name: "web", Kind: tmux.KindCodex, Managed: true, Dir: "/code/web"},
	}))
	return m
}

func savePreset(t *testing.T, m *Model, label string) {
	t.Helper()
	m.Update(keyRune('P'))
	m.nameInput.SetValue(label)
	m.Update(keyType(tea_KeyEnter))
}

func TestSavingAPresetCapturesTheSession(t *testing.T) {
	m := presetModel(t)
	savePreset(t, m, "api on claude")

	if len(m.presets) != 1 {
		t.Fatalf("got %d presets, want 1", len(m.presets))
	}
	p := m.presets[0]
	if p.Label != "api on claude" || p.Session != "api" ||
		p.Kind != tmux.KindClaude || p.Dir != "/code/api" {
		t.Errorf("preset = %+v, want it to describe the selected session", p)
	}

	// It has to survive berth being closed, which is the point of saving.
	onDisk, err := config.LoadPresets()
	if err != nil {
		t.Fatalf("LoadPresets: %v", err)
	}
	if len(onDisk) != 1 || onDisk[0].Label != "api on claude" {
		t.Errorf("on disk = %+v", onDisk)
	}
}

// Saving under a name already in use should replace it, not quietly leave two
// presets that look identical in the list.
func TestSavingOverAPresetReplacesIt(t *testing.T) {
	m := presetModel(t)
	savePreset(t, m, "work")

	m.cursor = 1 // the codex session
	savePreset(t, m, "WORK")

	if len(m.presets) != 1 {
		t.Fatalf("got %d presets, want the first replaced", len(m.presets))
	}
	if m.presets[0].Kind != tmux.KindCodex {
		t.Errorf("preset kind = %q, want the newer session's", m.presets[0].Kind)
	}
}

func TestAPresetNeedsAName(t *testing.T) {
	m := presetModel(t)
	m.Update(keyRune('P'))
	m.nameInput.SetValue("   ")
	m.Update(keyType(tea_KeyEnter))

	if len(m.presets) != 0 {
		t.Error("a blank name was saved as a preset")
	}
	if m.status == "" {
		t.Error("nothing said why it was not saved")
	}
}

// Using a preset opens the form filled in, rather than starting the session
// outright: the preset saves the typing, not the last look before it runs.
func TestUsingAPresetFillsTheForm(t *testing.T) {
	m := presetModel(t)
	m.presets = []config.Preset{{
		Label: "api", Session: "api-2", Kind: tmux.KindCodex,
		Dir: "/code/api", Start: tmux.StartContinue,
	}}

	m.Update(keyRune('p'))
	if m.mode != modePresets {
		t.Fatal("p did not open the preset list")
	}
	m.Update(keyType(tea_KeyEnter))

	if m.mode != modeNew {
		t.Fatalf("mode = %v, want the new session form", m.mode)
	}
	if m.nameInput.Value() != "api-2" {
		t.Errorf("name = %q, want the preset's session name", m.nameInput.Value())
	}
	if m.newKind != tmux.KindCodex {
		t.Errorf("kind = %q, want codex", m.newKind)
	}
	if m.newStart != tmux.StartContinue {
		t.Errorf("start = %q, want continue", m.newStart)
	}
	if got := m.dirInput.Value(); got != "/code/api/" {
		t.Errorf("dir = %q, want the preset's directory", got)
	}
}

// A preset saved before start modes existed has no Start, and must not create
// a session with an empty one.
func TestAPresetWithoutAStartModeDefaultsToNew(t *testing.T) {
	m := presetModel(t)
	m.presets = []config.Preset{{Label: "old", Kind: "", Dir: ""}}

	m.Update(keyRune('p'))
	m.Update(keyType(tea_KeyEnter))

	if m.newStart != tmux.StartNew {
		t.Errorf("start = %q, want new", m.newStart)
	}
	if m.newKind != tmux.KindClaude {
		t.Errorf("kind = %q, want a sensible default", m.newKind)
	}
	if m.dirInput.Value() == "" {
		t.Error("dir was left empty rather than falling back to the default")
	}
}

func TestRemovingAPreset(t *testing.T) {
	m := presetModel(t)
	savePreset(t, m, "one")
	m.cursor = 1
	savePreset(t, m, "two")

	m.Update(keyRune('p'))
	m.Update(keyRune('x'))

	if len(m.presets) != 1 || m.presets[0].Label != "two" {
		t.Errorf("presets = %+v, want only the second left", m.presets)
	}
	onDisk, _ := config.LoadPresets()
	if len(onDisk) != 1 {
		t.Errorf("on disk = %+v, want the removal saved", onDisk)
	}

	// Removing the last one closes the list rather than leaving it empty.
	m.Update(keyRune('x'))
	if m.mode != modeNormal {
		t.Error("the empty list stayed open")
	}
}

func TestOpeningAnEmptyPresetListSaysSo(t *testing.T) {
	m := presetModel(t)
	m.Update(keyRune('p'))

	if m.mode == modePresets {
		t.Error("an empty preset list was opened")
	}
	if m.status == "" {
		t.Error("nothing explained that there are no presets")
	}
}

func TestAddAndRemovePreset(t *testing.T) {
	var list []config.Preset
	list = config.AddPreset(list, config.Preset{Label: "a", Kind: "claude"})
	list = config.AddPreset(list, config.Preset{Label: "b", Kind: "codex"})
	if len(list) != 2 {
		t.Fatalf("got %d, want 2", len(list))
	}

	list = config.RemovePreset(list, 0)
	if len(list) != 1 || list[0].Label != "b" {
		t.Errorf("after removing the first: %+v", list)
	}
	// Out of range is a no-op rather than a panic.
	if got := config.RemovePreset(list, 9); len(got) != 1 {
		t.Errorf("removing out of range changed the list: %+v", got)
	}
	if got := config.RemovePreset(list, -1); len(got) != 1 {
		t.Errorf("removing a negative index changed the list: %+v", got)
	}
}

func TestMissingPresetsFileIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))

	got, err := config.LoadPresets()
	if err != nil {
		t.Errorf("LoadPresets on a fresh machine: %v", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nothing", got)
	}
}

func TestFormCarriesColourAndPresetIntoTheSession(t *testing.T) {
	m := presetModel(t)
	m.Update(keyRune('n'))

	m.formField = fieldColor
	m.Update(keyType(tea_KeyRight))
	if m.newColor == "" {
		t.Fatal("the colour row did not move off the default")
	}
	m.formField = fieldSavePreset
	m.Update(keyType(tea_KeyEnter))
	if !m.newSavePreset {
		t.Fatal("enter did not tick save-as-preset")
	}

	// Creating carries both through, and the preset is remembered on the
	// update loop rather than from the command that made the session.
	m.Update(sessionCreatedMsg{
		name: "api",
		preset: &config.Preset{
			Label: "api", Session: "api", Kind: tmux.KindClaude, Color: "red",
		},
	})
	if len(m.presets) != 1 || m.presets[0].Color != "red" {
		t.Errorf("presets = %+v, want the colour kept", m.presets)
	}
	if !strings.Contains(m.status, "preset") {
		t.Errorf("status = %q, want it to mention the preset", m.status)
	}

	onDisk, _ := config.LoadPresets()
	if len(onDisk) != 1 {
		t.Errorf("on disk = %+v, want the preset saved", onDisk)
	}
}

// The form is a way into the presets as well as a way past them.
func TestFormOpensThePresetList(t *testing.T) {
	m := presetModel(t)
	m.presets = []config.Preset{{Label: "api", Kind: tmux.KindCodex, Color: "teal"}}
	m.Update(keyRune('n'))

	m.formField = fieldPreset
	m.Update(keyType(tea_KeyEnter))
	if m.mode != modePresets {
		t.Fatalf("mode = %v, want the preset list", m.mode)
	}

	// Choosing one comes back to the form with its colour in place.
	m.Update(keyType(tea_KeyEnter))
	if m.mode != modeNew {
		t.Fatalf("mode = %v, want the form again", m.mode)
	}
	if m.newColor != "teal" {
		t.Errorf("colour = %q, want the preset's", m.newColor)
	}
	if m.newSavePreset {
		t.Error("a session started from a preset defaults to saving another")
	}
}

func TestCycleColor(t *testing.T) {
	// Forward from no colour, and back to it.
	first := cycleColor("", 1)
	if first == "" || first == "default" {
		t.Fatalf("cycleColor forward gave %q", first)
	}
	if got := cycleColor(first, -1); got != "" {
		t.Errorf("cycling back gave %q, want no colour", got)
	}
	// Wraps rather than running off the end.
	last := cycleColor("", -1)
	if last == "" {
		t.Error("cycling back from the default did not wrap")
	}
}

// Closing the preset list has to go back where it was opened from. Reached
// from the new session form, dropping to the session list would throw away a
// half-filled form.
func TestClosingPresetsReturnsWhereItCameFrom(t *testing.T) {
	m := presetModel(t)
	m.presets = []config.Preset{{Label: "api", Kind: tmux.KindCodex}}

	// From the session list.
	m.Update(keyRune('p'))
	m.Update(keyType(tea_KeyEsc))
	if m.mode != modeNormal {
		t.Errorf("mode = %v, want the session list", m.mode)
	}

	// From the new session form.
	m.Update(keyRune('n'))
	m.nameInput.SetValue("half-typed")
	m.formField = fieldPreset
	m.Update(keyType(tea_KeyEnter))
	if m.mode != modePresets {
		t.Fatal("the form did not open the preset list")
	}
	m.Update(keyType(tea_KeyEsc))
	if m.mode != modeNew {
		t.Fatalf("mode = %v, want the form back", m.mode)
	}
	if m.nameInput.Value() != "half-typed" {
		t.Errorf("the form lost what was typed: %q", m.nameInput.Value())
	}

	// And "q", which also closes the list.
	m.formField = fieldPreset
	m.Update(keyType(tea_KeyEnter))
	m.Update(keyRune('q'))
	if m.mode != modeNew {
		t.Errorf("q from the form dropped to %v", m.mode)
	}
}

// Removing the last preset closes the list, and that too should land where it
// came from.
func TestRemovingTheLastPresetFromTheFormReturnsToIt(t *testing.T) {
	m := presetModel(t)
	m.presets = []config.Preset{{Label: "only", Kind: tmux.KindClaude}}

	m.Update(keyRune('n'))
	m.formField = fieldPreset
	m.Update(keyType(tea_KeyEnter))
	m.Update(keyRune('x'))

	if len(m.presets) != 0 {
		t.Fatalf("presets = %+v, want it removed", m.presets)
	}
	if m.mode != modeNew {
		t.Errorf("mode = %v, want the form back", m.mode)
	}
}

// The tick has no label column of its own, so the highlight has to land on the
// text. Styling the empty label showed nothing, and the row looked unreachable
// while the cursor was sitting on it.
func TestSavePresetRowShowsItHasTheCursor(t *testing.T) {
	withColour(t)
	m := presetModel(t)
	m.Update(keyRune('n'))

	row := func(field int) string {
		m.formField = field
		for _, line := range strings.Split(m.newDialog(), "\n") {
			if strings.Contains(ansi.Strip(line), "save as a preset") {
				return line
			}
		}
		t.Fatal("the form has no save-as-preset row")
		return ""
	}

	off, on := row(fieldName), row(fieldSavePreset)
	if off == on {
		t.Error("the row looks the same whether or not it has the cursor")
	}
	if !strings.Contains(on, labelActiveStyle.Render("[ ] save as a preset")) {
		t.Error("the focused row is not drawn the way every other focused row is")
	}
}
