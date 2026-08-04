package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"
	"github.com/dpws/berth/internal/doctor"
)

// doctorMsg carries what the checks found, off the update loop: each one shells
// out to tmux or reads a config file, which is not work for a redraw to wait on.
type doctorMsg []doctor.Finding

// runDoctor looks over the software berth sits on.
func runDoctor() tea.Cmd {
	return func() tea.Msg { return doctorMsg(doctor.Run()) }
}

// doctorPrompt is what the startup check has to say, once it has something to
// say. It is deliberately not shown while anything else is: berth interrupting
// whatever you opened it to do, to talk about a tmux option, would be worse
// than the option being wrong.
func (m *Model) showDoctor(findings []doctor.Finding) {
	m.doctor = doctor.Actionable(findings, m.cfg.DoctorSkipped)
	if len(m.doctor) == 0 || m.cfg.HideDoctor || m.mode != modeNormal {
		return
	}
	m.mode = modeDoctor
	m.doctorCursor = 0
}

// handleDoctorKey drives the startup prompt.
//
// The three answers are fix it, leave it alone for good, and not now. Not now
// is the one escape gives, because that is what escape means everywhere else,
// and it is the answer someone gives by reflex when a prompt appears over the
// thing they were about to do.
func (m *Model) handleDoctorKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		m.mode = modeNormal
		return nil

	case "up", "k":
		if m.doctorCursor > 0 {
			m.doctorCursor--
		}
		return nil
	case "down", "j":
		if m.doctorCursor < len(m.doctor)-1 {
			m.doctorCursor++
		}
		return nil

	case "f":
		return m.fixDoctor()

	case "s":
		// Skipping is remembered, or berth would ask the same question at every
		// start and the answer would stop being read.
		for _, f := range m.doctor {
			m.cfg.DoctorSkipped = append(m.cfg.DoctorSkipped, f.Key)
		}
		m.mode = modeNormal
		m.doctor = nil
		if err := m.cfg.Save(); err != nil {
			m.setStatus("could not remember that: "+err.Error(), true)
			return nil
		}
		m.setStatus("left alone; berth will not ask again", false)
		return nil
	}
	return nil
}

// fixDoctor applies everything berth can do itself, and says plainly what is
// left for someone to do by hand rather than reporting a clean sweep it did not
// manage.
func (m *Model) fixDoctor() tea.Cmd {
	var fixed, manual int
	var failed []string
	var restart bool
	for _, f := range m.doctor {
		if !f.Fixable() {
			manual++
			continue
		}
		if err := f.Fix(); err != nil {
			failed = append(failed, f.Key)
			continue
		}
		fixed++
		if f.NeedsRestart {
			restart = true
		}
	}

	m.mode = modeNormal
	m.doctor = nil

	switch {
	case len(failed) > 0:
		m.setStatus(fmt.Sprintf("fixed %d, could not fix %s", fixed, strings.Join(failed, ", ")), true)
	case restart:
		// tmux settles what it will send a client when that client attaches, so
		// berth goes on being sent the old, flattened keys until it is started
		// again. Saying the option is fixed without saying that would send
		// someone off to press shift+enter and find it still does nothing.
		m.setStatus(fmt.Sprintf("fixed %d - restart berth for the key changes to reach it", fixed), false)
	case manual > 0:
		m.setStatus(fmt.Sprintf("fixed %d; %d need doing by hand - see berth doctor", fixed, manual), false)
	default:
		m.setStatus(fmt.Sprintf("fixed %d", fixed), false)
	}
	return nil
}

// doctorChrome is what the dialog needs besides the findings themselves: a
// title and a blank under it, and a blank and the key line at the foot.
const doctorChrome = 2 + 2

// doctorFrame is what the border and its padding cost on top of that. Below
// the height where a framed dialog would leave no room for anything to say,
// the frame is what goes: a border around nothing is worth less than a line of
// text inside no border.
const doctorFrame = 4

// doctorPanel is the dialog with or without its frame, depending on whether the
// window can afford one.
func (m *Model) doctorPanel() string {
	// Two rows for the list, not one: with more findings than fit, one of them
	// goes to saying how many were left out, and a frame drawn around a list
	// with no room for either is a frame that gets cut instead.
	if m.height < doctorChrome+doctorFrame+2 {
		return m.doctorDialog(m.height - doctorChrome)
	}
	return dialogStyle.Render(m.doctorDialog(m.height - doctorChrome - doctorFrame))
}

// doctorDialog lists what was found, worst first, with what each one costs.
//
// It is built to the height it has rather than to its full length and then cut.
// A dialog cut from the bottom loses its last line, and the last line here is
// the one naming the keys that dismiss it - which would leave someone holding a
// modal with no visible way out of it. So the findings are what gives way,
// saying how many were not shown, and the keys are never what goes.
func (m *Model) doctorDialog(room int) string {
	if room < 1 {
		room = 1
	}

	shown := m.doctor
	var hidden int
	if len(shown) > room {
		// One of the rows the list would have had goes to saying so.
		keep := room - 1
		if keep < 1 {
			keep = 1
		}
		hidden = len(shown) - keep
		shown = shown[:keep]
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("berth doctor"))
	b.WriteString(itemMutedStyle.Render(fmt.Sprintf("  %d to look at", len(m.doctor))))
	b.WriteString("\n\n")

	for i, f := range shown {
		mark := itemMutedStyle.Render("  ")
		if i == m.doctorCursor {
			mark = labelActiveStyle.Render("▸ ")
		}

		style := faintStyle
		word := "could be better"
		switch f.Severity {
		case doctor.Broken:
			style = lipgloss.NewStyle().Foreground(colDanger)
			word = "broken"
		case doctor.Unknown:
			word = "cannot tell"
		}

		// Subject and severity ride on the same row as the summary. A second
		// line each read more clearly with three findings and pushed the
		// prompt off a 24-row terminal with six.
		b.WriteString(mark + style.Render(f.Summary))
		tail := "  " + string(f.Subject) + " · " + word
		if !f.Fixable() {
			tail += " · by hand"
		}
		b.WriteString(itemMutedStyle.Render(tail) + "\n")
	}

	if hidden > 0 {
		b.WriteString(itemMutedStyle.Render(fmt.Sprintf("  … and %d more, see berth doctor\n", hidden)))
	}

	// The explanation is the first thing to go: it is the longest part and the
	// least use to someone who cannot see the whole list anyway.
	if f := m.selectedFinding(); f != nil && f.Detail != "" && hidden == 0 {
		detail := wrapTo(f.Detail, 60)
		if lines := strings.Count(detail, "\n") + 1; room-len(shown) > lines {
			b.WriteString("\n" + faintStyle.Render(detail) + "\n")
		}
	}

	b.WriteString("\n" + footerStyle.Render(
		"f fix what berth can · s never ask again · esc not now"))
	return b.String()
}

func (m *Model) selectedFinding() *doctor.Finding {
	if m.doctorCursor < 0 || m.doctorCursor >= len(m.doctor) {
		return nil
	}
	return &m.doctor[m.doctorCursor]
}

// wrapTo folds text to a width, so a dialog holds its shape rather than growing
// to the length of the longest thing a check had to say.
func wrapTo(s string, width int) string {
	var b strings.Builder
	col := 0
	for i, word := range strings.Fields(s) {
		switch {
		case i == 0:
		case col+1+len(word) > width:
			b.WriteString("\n")
			col = 0
		default:
			b.WriteString(" ")
			col++
		}
		b.WriteString(word)
		col += len(word)
	}
	return b.String()
}
