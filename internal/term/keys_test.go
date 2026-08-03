package term

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
)

// The emulator has no table entry for a special key held with a modifier, so
// every one of these used to be written as nothing at all. The expected
// sequences are xterm's.
func TestModifiedSpecialKeysEncodeAsCSI(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  tea.KeyType
		want string
	}{
		{"shift+up", tea.KeyShiftUp, "\x1b[1;2A"},
		{"shift+down", tea.KeyShiftDown, "\x1b[1;2B"},
		{"shift+right", tea.KeyShiftRight, "\x1b[1;2C"},
		{"shift+left", tea.KeyShiftLeft, "\x1b[1;2D"},
		{"shift+end", tea.KeyShiftEnd, "\x1b[1;2F"},
		{"shift+home", tea.KeyShiftHome, "\x1b[1;2H"},

		{"ctrl+up", tea.KeyCtrlUp, "\x1b[1;5A"},
		{"ctrl+down", tea.KeyCtrlDown, "\x1b[1;5B"},
		{"ctrl+right", tea.KeyCtrlRight, "\x1b[1;5C"},
		{"ctrl+left", tea.KeyCtrlLeft, "\x1b[1;5D"},
		{"ctrl+end", tea.KeyCtrlEnd, "\x1b[1;5F"},
		{"ctrl+home", tea.KeyCtrlHome, "\x1b[1;5H"},

		// The tilde keys carry their number in the first parameter instead.
		{"ctrl+pgup", tea.KeyCtrlPgUp, "\x1b[5;5~"},
		{"ctrl+pgdown", tea.KeyCtrlPgDown, "\x1b[6;5~"},

		// ctrl+shift is 1 + 1 + 4.
		{"ctrl+shift+up", tea.KeyCtrlShiftUp, "\x1b[1;6A"},
		{"ctrl+shift+down", tea.KeyCtrlShiftDown, "\x1b[1;6B"},
		{"ctrl+shift+right", tea.KeyCtrlShiftRight, "\x1b[1;6C"},
		{"ctrl+shift+left", tea.KeyCtrlShiftLeft, "\x1b[1;6D"},
		{"ctrl+shift+end", tea.KeyCtrlShiftEnd, "\x1b[1;6F"},
		{"ctrl+shift+home", tea.KeyCtrlShiftHome, "\x1b[1;6H"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key, ok := ToKeyPress(tea.KeyMsg{Type: tc.key})
			if !ok {
				t.Fatalf("ToKeyPress rejected %s", tc.name)
			}
			got, ok := modifiedKey(key)
			if !ok {
				t.Fatalf("%s was not encoded, so nothing reaches the session", tc.name)
			}
			if got != tc.want {
				t.Errorf("%s encoded as %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// Alt arrives on its own flag rather than as a key type of its own, and belongs
// in the modifier parameter like any other. The emulator's own alt handling
// prefixes an escape instead, which is not what a modified cursor key means.
func TestAltGoesIntoTheModifierParameter(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  tea.KeyMsg
		want string
	}{
		{"alt+up", tea.KeyMsg{Type: tea.KeyUp, Alt: true}, "\x1b[1;3A"},
		{"alt+shift+up", tea.KeyMsg{Type: tea.KeyShiftUp, Alt: true}, "\x1b[1;4A"},
		{"alt+ctrl+left", tea.KeyMsg{Type: tea.KeyCtrlLeft, Alt: true}, "\x1b[1;7D"},
		{"alt+delete", tea.KeyMsg{Type: tea.KeyDelete, Alt: true}, "\x1b[3;3~"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key, ok := ToKeyPress(tc.msg)
			if !ok {
				t.Fatalf("ToKeyPress rejected %s", tc.name)
			}
			got, ok := modifiedKey(key)
			if !ok {
				t.Fatalf("%s was not encoded", tc.name)
			}
			if got != tc.want {
				t.Errorf("%s encoded as %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// Everything the emulator already encodes has to keep going through it, or the
// modes it tracks - application cursor keys above all - stop being applied.
func TestKeysTheEmulatorHandlesAreLeftToIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  tea.KeyMsg
	}{
		{"plain up", tea.KeyMsg{Type: tea.KeyUp}},
		{"plain home", tea.KeyMsg{Type: tea.KeyHome}},
		{"plain delete", tea.KeyMsg{Type: tea.KeyDelete}},
		{"plain f1", tea.KeyMsg{Type: tea.KeyF1}},
		{"plain f5", tea.KeyMsg{Type: tea.KeyF5}},
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}},
		{"tab", tea.KeyMsg{Type: tea.KeyTab}},
		{"escape", tea.KeyMsg{Type: tea.KeyEsc}},
		{"backspace", tea.KeyMsg{Type: tea.KeyBackspace}},
		// shift+tab has its own entry in the emulator: CSI Z, not CSI 1;2I.
		{"shift+tab", tea.KeyMsg{Type: tea.KeyShiftTab}},
		{"ctrl+a", tea.KeyMsg{Type: tea.KeyCtrlA}},
		{"ctrl+space", tea.KeyMsg{Type: tea.KeyNull}},
		{"alt+a", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}, Alt: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key, ok := ToKeyPress(tc.msg)
			if !ok {
				t.Fatalf("ToKeyPress rejected %s", tc.name)
			}
			if seq, ok := modifiedKey(key); ok {
				t.Errorf("%s was encoded here as %q; it belongs to the emulator", tc.name, seq)
			}
		})
	}
}

// A key the emulator cannot encode and this cannot either must not be turned
// into a sequence by accident.
func TestUnknownModifiedKeysAreNotInvented(t *testing.T) {
	for _, code := range []rune{uv.KeyTab, uv.KeyEnter, uv.KeyEscape, uv.KeyBackspace, uv.KeySpace} {
		key := uv.KeyPressEvent{Code: code, Mod: uv.ModCtrl}
		if seq, ok := modifiedKey(key); ok {
			t.Errorf("code %U with ctrl encoded as %q, want no encoding", code, seq)
		}
	}
}
