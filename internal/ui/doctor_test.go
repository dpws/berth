package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dpws/berth/internal/config"
	"github.com/dpws/berth/internal/doctor"
)

func findings() []doctor.Finding {
	return []doctor.Finding{
		{Key: "tmux_extended_keys", Subject: doctor.Tmux, Severity: doctor.Broken,
			Summary: "tmux is dropping modified keys", Detail: "shift+enter will submit"},
		{Key: "tmux_mouse", Subject: doctor.Tmux, Severity: doctor.Degraded,
			Summary: "tmux is leaving the mouse alone"},
		{Key: "all_well", Subject: doctor.Tmux, Severity: doctor.OK,
			Summary: "tmux is installed"},
	}
}

func TestTheStartupCheckOnlyRaisesWhatIsWrong(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	m.Update(doctorMsg(findings()))

	if m.mode != modeDoctor {
		t.Fatalf("mode = %v, want the doctor prompt", m.mode)
	}
	if len(m.doctor) != 2 {
		t.Fatalf("raised %d findings, want the 2 that are not already fine", len(m.doctor))
	}
	body := m.doctorPanel()
	if strings.Contains(body, "tmux is installed") {
		t.Error("the prompt is listing something that is already fine")
	}
	if !strings.Contains(body, "dropping modified keys") {
		t.Errorf("the prompt does not name the broken one:\n%s", body)
	}
}

// Nothing wrong means no prompt at all: berth opening with a dialog over it
// when there is nothing to say would be worse than saying nothing.
func TestNothingWrongRaisesNothing(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	m.Update(doctorMsg([]doctor.Finding{
		{Key: "a", Severity: doctor.OK, Summary: "fine"},
	}))
	if m.mode != modeNormal {
		t.Errorf("mode = %v, want to be left alone", m.mode)
	}
}

// The prompt must not land on top of whatever you opened berth to do.
func TestTheCheckWaitsWhileSomethingElseIsOpen(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	m.Update(typed("?")) // the help screen
	if m.mode != modeHelp {
		t.Fatalf("mode = %v, want help open first", m.mode)
	}

	m.Update(doctorMsg(findings()))
	if m.mode != modeHelp {
		t.Errorf("the check interrupted the help screen (mode = %v)", m.mode)
	}
}

// A check already skipped is not raised again, which is the whole point of
// remembering the answer.
func TestSkippedChecksAreNotRaised(t *testing.T) {
	m := newTestModel()
	m.cfg.DoctorSkipped = []string{"tmux_extended_keys", "tmux_mouse"}
	m.Update(sessions("alpha"))
	m.Update(doctorMsg(findings()))

	if m.mode != modeNormal {
		t.Errorf("mode = %v, want nothing raised when both were skipped", m.mode)
	}
}

// Turning the startup check off has to mean off.
func TestTheStartupCheckCanBeTurnedOff(t *testing.T) {
	m := New(config.Default())
	m.cfg.HideDoctor = true
	m.Update(sessions("alpha"))
	m.Update(doctorMsg(findings()))
	if m.mode == modeDoctor {
		t.Error("hide_doctor still raised the prompt")
	}
}

// "not now" leaves the config alone; only "never again" writes to it.
func TestNotNowIsNotRemembered(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	m.Update(doctorMsg(findings()))

	m.Update(key(27)) // esc
	if m.mode != modeNormal {
		t.Errorf("esc did not close the prompt, mode = %v", m.mode)
	}
	if len(m.cfg.DoctorSkipped) != 0 {
		t.Errorf("esc recorded a skip: %v", m.cfg.DoctorSkipped)
	}
}

// Moving through the list changes which explanation is shown, so a list of
// summaries can be read one at a time without leaving the prompt.
func TestMovingThroughTheFindings(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	m.Update(doctorMsg(findings()))

	if got := m.selectedFinding(); got == nil || got.Key != "tmux_extended_keys" {
		t.Fatalf("the worst one is not selected first: %+v", got)
	}
	m.Update(typed("j"))
	if got := m.selectedFinding(); got == nil || got.Key != "tmux_mouse" {
		t.Errorf("j did not move to the next finding: %+v", got)
	}
	m.Update(typed("j")) // already at the end
	if got := m.selectedFinding(); got == nil || got.Key != "tmux_mouse" {
		t.Errorf("j ran off the end of the list: %+v", got)
	}
	m.Update(typed("k"))
	if got := m.selectedFinding(); got == nil || got.Key != "tmux_extended_keys" {
		t.Errorf("k did not move back: %+v", got)
	}
}

// A modal that hides the keys which dismiss it is a trap. The dialog is built
// to the height it has rather than cut to it, and the key line is never what
// gives way.
func TestTheDoctorDialogAlwaysShowsItsWayOut(t *testing.T) {
	many := make([]doctor.Finding, 0, 9)
	for i := 0; i < 9; i++ {
		many = append(many, doctor.Finding{
			Key:      fmt.Sprintf("check_%d", i),
			Subject:  doctor.Tmux,
			Severity: doctor.Degraded,
			Summary:  fmt.Sprintf("something is not set up right, number %d", i),
			Detail:   "A long explanation that would happily fill several lines of the dialog on its own, given the chance.",
		})
	}

	for _, h := range []int{40, 24, 20, 16, 12, 8, 6} {
		m := newTestModel()
		m.Update(tea.WindowSizeMsg{Width: 100, Height: h})
		m.Update(sessions("alpha"))
		m.Update(doctorMsg(many))

		body := ansi.Strip(m.dialogView())
		if !strings.Contains(body, "esc not now") {
			t.Errorf("at height %d the dialog does not say how to dismiss it:\n%s", h, body)
		}
		if !strings.Contains(body, "berth doctor") {
			t.Errorf("at height %d the dialog lost its title:\n%s", h, body)
		}
		if rows := strings.Count(m.dialogView(), "\n") + 1; rows > h {
			t.Errorf("at height %d the dialog is %d rows tall", h, rows)
		}
		// Fitting means fitting: the doctor is never the dialog that gets cut,
		// because what a cut takes is the line naming the way out.
		if strings.Contains(body, "window too short") {
			t.Errorf("at height %d the dialog was cut instead of fitting:\n%s", h, body)
		}
	}
}

// What is left out has to be said, or a short window quietly under-reports.
func TestALeftOutFindingIsCountedOut(t *testing.T) {
	many := make([]doctor.Finding, 0, 9)
	for i := 0; i < 9; i++ {
		many = append(many, doctor.Finding{
			Key: fmt.Sprintf("check_%d", i), Subject: doctor.Tmux,
			Severity: doctor.Degraded, Summary: fmt.Sprintf("finding %d", i),
		})
	}
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	m.Update(sessions("alpha"))
	m.Update(doctorMsg(many))

	body := ansi.Strip(m.doctorPanel())
	if !strings.Contains(body, "more") {
		t.Errorf("findings were dropped without saying so:\n%s", body)
	}
	// And the full count is still named at the top.
	if !strings.Contains(body, "9 to look at") {
		t.Errorf("the dialog does not say how many there really are:\n%s", body)
	}
}

// A paste that arrived with nothing in it is what a terminal sends when its own
// paste key was pressed over a clipboard holding no text - an image, most
// often. That is the one thing berth can fetch and the terminal cannot, and on
// Windows Terminal, which keeps ctrl+v for itself, it is the only way the key
// reaches berth at all.
func TestAnEmptyPasteReachesForAnImage(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	withPane(t, m)
	m.focus = focusTerminal

	_, cmd := m.Update(tea.PasteMsg{Content: ""})
	if cmd == nil {
		t.Fatal("an empty paste did nothing; it should reach for the clipboard")
	}

	// A paste with text in it is the terminal's own business and goes straight
	// through, without asking the clipboard anything.
	if _, cmd := m.Update(tea.PasteMsg{Content: "some text"}); cmd != nil {
		t.Error("a paste carrying text went looking for an image as well")
	}
}

// The list has nothing to paste into, so neither kind of paste means anything
// there.
func TestPastingIntoTheListDoesNothing(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	withPane(t, m)
	m.focus = focusSidebar

	if _, cmd := m.Update(tea.PasteMsg{Content: ""}); cmd != nil {
		t.Error("an empty paste reached for an image while the list had the keyboard")
	}
}
