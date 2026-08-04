package term

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// press builds a key press the way a terminal that can disambiguate reports
// one: the key, plus whatever was held with it.
func press(code rune, mods ...uv.KeyMod) uv.KeyPressEvent {
	var mod uv.KeyMod
	for _, m := range mods {
		mod |= m
	}
	return uv.KeyPressEvent{Code: code, Mod: mod}
}

// The emulator has no table entry for a special key held with a modifier, so
// every one of these used to be written as nothing at all. The expected
// sequences are xterm's.
func TestModifiedSpecialKeysEncodeAsCSI(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  uv.KeyPressEvent
		want string
	}{
		{"shift+up", press(uv.KeyUp, uv.ModShift), "\x1b[1;2A"},
		{"shift+down", press(uv.KeyDown, uv.ModShift), "\x1b[1;2B"},
		{"shift+right", press(uv.KeyRight, uv.ModShift), "\x1b[1;2C"},
		{"shift+left", press(uv.KeyLeft, uv.ModShift), "\x1b[1;2D"},
		{"shift+end", press(uv.KeyEnd, uv.ModShift), "\x1b[1;2F"},
		{"shift+home", press(uv.KeyHome, uv.ModShift), "\x1b[1;2H"},

		{"ctrl+up", press(uv.KeyUp, uv.ModCtrl), "\x1b[1;5A"},
		{"ctrl+down", press(uv.KeyDown, uv.ModCtrl), "\x1b[1;5B"},
		{"ctrl+right", press(uv.KeyRight, uv.ModCtrl), "\x1b[1;5C"},
		{"ctrl+left", press(uv.KeyLeft, uv.ModCtrl), "\x1b[1;5D"},
		{"ctrl+end", press(uv.KeyEnd, uv.ModCtrl), "\x1b[1;5F"},
		{"ctrl+home", press(uv.KeyHome, uv.ModCtrl), "\x1b[1;5H"},

		// The tilde keys carry their number in the first parameter instead.
		{"ctrl+pgup", press(uv.KeyPgUp, uv.ModCtrl), "\x1b[5;5~"},
		{"ctrl+pgdown", press(uv.KeyPgDown, uv.ModCtrl), "\x1b[6;5~"},
		{"shift+delete", press(uv.KeyDelete, uv.ModShift), "\x1b[3;2~"},
		{"ctrl+f5", press(uv.KeyF5, uv.ModCtrl), "\x1b[15;5~"},

		// A function key with a final byte moves to CSI to make room for the
		// parameter its SS3 form cannot carry.
		{"shift+f1", press(uv.KeyF1, uv.ModShift), "\x1b[1;2P"},

		// ctrl+shift is 1 + 1 + 4.
		{"ctrl+shift+up", press(uv.KeyUp, uv.ModCtrl, uv.ModShift), "\x1b[1;6A"},
		{"ctrl+shift+left", press(uv.KeyLeft, uv.ModCtrl, uv.ModShift), "\x1b[1;6D"},
		{"ctrl+shift+home", press(uv.KeyHome, uv.ModCtrl, uv.ModShift), "\x1b[1;6H"},

		// Alt belongs in the parameter like any other modifier. Sent as an
		// escape prefix it cannot say alt and shift at once.
		{"alt+up", press(uv.KeyUp, uv.ModAlt), "\x1b[1;3A"},
		{"alt+shift+up", press(uv.KeyUp, uv.ModAlt, uv.ModShift), "\x1b[1;4A"},
		{"alt+ctrl+left", press(uv.KeyLeft, uv.ModAlt, uv.ModCtrl), "\x1b[1;7D"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := modifiedKey(tc.key)
			if !ok {
				t.Fatalf("%s was not encoded, so nothing reaches the session", tc.name)
			}
			if got != tc.want {
				t.Errorf("%s encoded as %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// Enter with a modifier means another line, not another prompt. Which modifier
// the terminal can report varies, and so does how a typist is used to asking,
// so the three spellings answer alike.
func TestModifiedEnterIsANewline(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  uv.KeyPressEvent
	}{
		{"shift+enter", press(uv.KeyEnter, uv.ModShift)},
		{"alt+enter", press(uv.KeyEnter, uv.ModAlt)},
		{"ctrl+j", press('j', uv.ModCtrl)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := modifiedKey(tc.key)
			if !ok {
				t.Fatalf("%s was not encoded", tc.name)
			}
			if got != "\x1b\r" {
				t.Errorf("%s encoded as %q, want an escape and a return", tc.name, got)
			}
		})
	}
}

// Enter on its own still sends the prompt. Were this to answer like the
// modified forms, nothing typed could be submitted at all.
func TestPlainEnterIsNotANewline(t *testing.T) {
	if seq, ok := modifiedKey(press(uv.KeyEnter)); ok {
		t.Errorf("plain enter was encoded as %q; it belongs to the emulator", seq)
	}
	// ctrl+enter is not one of the three, and has no newline meaning to borrow.
	if seq, ok := modifiedKey(press(uv.KeyEnter, uv.ModCtrl)); ok {
		t.Errorf("ctrl+enter was encoded as %q, want it left alone", seq)
	}
}

// Everything the emulator already encodes has to keep going through it, or the
// modes it tracks - application cursor keys above all - stop being applied.
func TestKeysTheEmulatorHandlesAreLeftToIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  uv.KeyPressEvent
	}{
		{"plain up", press(uv.KeyUp)},
		{"plain home", press(uv.KeyHome)},
		{"plain delete", press(uv.KeyDelete)},
		{"plain f1", press(uv.KeyF1)},
		{"plain f5", press(uv.KeyF5)},
		{"enter", press(uv.KeyEnter)},
		{"tab", press(uv.KeyTab)},
		{"escape", press(uv.KeyEscape)},
		{"backspace", press(uv.KeyBackspace)},
		// shift+tab has its own entry in the emulator: CSI Z, not CSI 1;2I.
		{"shift+tab", press(uv.KeyTab, uv.ModShift)},
		{"ctrl+a", press('a', uv.ModCtrl)},
		{"ctrl+space", press(uv.KeySpace, uv.ModCtrl)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if seq, ok := modifiedKey(tc.key); ok {
				t.Errorf("%s was encoded here as %q; it belongs to the emulator", tc.name, seq)
			}
		})
	}
}

// A key this cannot encode must not be turned into a sequence by accident.
func TestUnknownModifiedKeysAreNotInvented(t *testing.T) {
	for _, code := range []rune{uv.KeyTab, uv.KeyEscape, uv.KeyBackspace, uv.KeySpace, 'q'} {
		if seq, ok := modifiedKey(press(code, uv.ModCtrl)); ok {
			t.Errorf("code %U with ctrl encoded as %q, want no encoding", code, seq)
		}
	}
}

// Bubble Tea and the emulator describe a key with the same type, so the
// conversion has to carry the whole of it across rather than rebuilding the
// parts it happens to know the names of.
func TestToKeyPressCarriesTheWholeKey(t *testing.T) {
	got, ok := ToKeyPress(tea.KeyPressMsg{Code: uv.KeyUp, Mod: uv.ModCtrl | uv.ModShift})
	if !ok {
		t.Fatal("a modified arrow was rejected")
	}
	if got.Code != uv.KeyUp || got.Mod != uv.ModCtrl|uv.ModShift {
		t.Errorf("converted to %+v, want the code and both modifiers", got)
	}

	// Printable input keeps the text it produced, which is what tells typing
	// apart from a key that has to be encoded.
	text, ok := ToKeyPress(tea.KeyPressMsg{Code: 'é', Text: "é"})
	if !ok || text.Text != "é" {
		t.Errorf("printable input converted to %+v, want its text kept", text)
	}

	// A message carrying neither is nothing to send.
	if _, ok := ToKeyPress(tea.KeyPressMsg{}); ok {
		t.Error("an empty key press was accepted")
	}
}

// The four kinds of mouse event are separate types now, and each has to come
// out as the matching emulator event, at the coordinates the caller worked out
// rather than the ones the screen reported.
func TestToMouseKeepsTheKindAndThePosition(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  tea.MouseMsg
	}{
		{"click", tea.MouseClickMsg{Button: tea.MouseLeft}},
		{"release", tea.MouseReleaseMsg{Button: tea.MouseLeft}},
		{"wheel", tea.MouseWheelMsg{Button: tea.MouseWheelDown}},
		{"motion", tea.MouseMotionMsg{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev, ok := ToMouse(tc.msg, 7, 9)
			if !ok {
				t.Fatalf("%s was not converted", tc.name)
			}

			var x, y int
			var kind string
			switch e := ev.(type) {
			case uv.MouseClickEvent:
				x, y, kind = e.X, e.Y, "click"
			case uv.MouseReleaseEvent:
				x, y, kind = e.X, e.Y, "release"
			case uv.MouseWheelEvent:
				x, y, kind = e.X, e.Y, "wheel"
			case uv.MouseMotionEvent:
				x, y, kind = e.X, e.Y, "motion"
			default:
				t.Fatalf("%s became %T", tc.name, ev)
			}

			if kind != tc.name {
				t.Errorf("%s came out as a %s", tc.name, kind)
			}
			if x != 7 || y != 9 {
				t.Errorf("%s landed at %d,%d, want the pane coordinates 7,9", tc.name, x, y)
			}
		})
	}
}
