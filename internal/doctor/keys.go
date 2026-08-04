package doctor

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// keyProbe is the model behind "berth doctor --keys".
//
// Whether a terminal can tell shift+enter from enter cannot be worked out from
// the environment, and cannot be tested with cat: a terminal only sends the
// distinguishable form to a program that has asked for it, so anything that has
// not asked sees a plain return either way and learns nothing. This asks, and
// then shows what actually arrives.
type keyProbe struct {
	seen     []string
	enhanced bool
	asked    bool
}

func (m keyProbe) Init() tea.Cmd { return nil }

func (m keyProbe) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyboardEnhancementsMsg:
		m.enhanced = true
		m.asked = true
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}
		k := msg.Key()
		m.seen = append(m.seen, fmt.Sprintf("  %-18s code %-8q mod %d", msg.String(), k.Code, k.Mod))
		if len(m.seen) > 12 {
			m.seen = m.seen[len(m.seen)-12:]
		}
	}
	return m, nil
}

func (m keyProbe) View() tea.View {
	var b strings.Builder
	b.WriteString("berth doctor --keys\n\n")
	b.WriteString("Press shift+enter, then enter, and compare the two lines.\n")
	b.WriteString("If they read the same, this terminal cannot tell them apart,\n")
	b.WriteString("and alt+enter is the one to use for a new line.\n\n")

	// What arrives when a key is pressed is the answer. Whether the terminal
	// also announced itself is worth showing, but some pass the keys on without
	// ever answering, so its absence is not a verdict.
	if m.enhanced {
		b.WriteString("  the terminal answered that it can report modified keys\n\n")
	}

	if len(m.seen) == 0 {
		b.WriteString("  (nothing pressed yet)\n")
	}
	for _, l := range m.seen {
		b.WriteString(l + "\n")
	}
	b.WriteString("\nq or ctrl+c to finish\n")

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// ProbeKeys runs the key probe until it is quit.
func ProbeKeys() error {
	_, err := tea.NewProgram(keyProbe{}).Run()
	return err
}
