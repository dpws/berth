package term

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// ToKeyPress converts a Bubble Tea key press into an ultraviolet key event.
//
// Both sides describe a key with ultraviolet's own type, so this is a
// conversion rather than a translation. The table of named keys this used to
// carry, and the arithmetic that turned control bytes back into the letters
// they stood for, were both making up for a key type that said less than the
// terminal knew; Bubble Tea now asks the terminal to disambiguate and hands
// the answer over whole.
//
// Handing the event to the emulator, rather than encoding bytes ourselves,
// means terminal modes such as application cursor keys are honoured.
func ToKeyPress(msg tea.KeyPressMsg) (uv.KeyPressEvent, bool) {
	k := uv.Key(msg.Key())
	if k.Code == 0 && k.Text == "" {
		return uv.KeyPressEvent(k), false
	}
	return uv.KeyPressEvent(k), true
}

// csiFinal holds the special keys whose plain form ends in a final byte: CSI A
// for up, SS3 P for F1. Held with a modifier they all take one shape,
// CSI 1 ; <mod> <final> - the function keys included, since their SS3 form has
// nowhere to put a parameter and xterm moves them to CSI to make room.
var csiFinal = map[rune]byte{
	uv.KeyUp:    'A',
	uv.KeyDown:  'B',
	uv.KeyRight: 'C',
	uv.KeyLeft:  'D',
	uv.KeyEnd:   'F',
	uv.KeyHome:  'H',
	uv.KeyF1:    'P',
	uv.KeyF2:    'Q',
	uv.KeyF3:    'R',
	uv.KeyF4:    'S',
}

// csiTilde holds the special keys written CSI <n> ~ on their own, and
// CSI <n> ; <mod> ~ with a modifier held.
var csiTilde = map[rune]int{
	uv.KeyInsert: 2,
	uv.KeyDelete: 3,
	uv.KeyPgUp:   5,
	uv.KeyPgDown: 6,
	uv.KeyF5:     15,
	uv.KeyF6:     17,
	uv.KeyF7:     18,
	uv.KeyF8:     19,
	uv.KeyF9:     20,
	uv.KeyF10:    21,
	uv.KeyF11:    23,
	uv.KeyF12:    24,
}

// newlineKeys are the keys that mean "another line, do not send this yet".
//
// Every agent worth running takes more than one line of instruction, and every
// one of them reads an escape followed by a carriage return as the break. What
// the terminal calls that key varies - shift+enter where the terminal can say
// so, alt+enter where it cannot, ctrl+j from habit - so all three are answered
// the same way rather than made to differ for no reason the typist can see.
//
// Enter on its own is deliberately absent: that one still sends the prompt.
var newlineKeys = map[uv.KeyMod]bool{
	uv.ModShift: true,
	uv.ModAlt:   true,
}

// newline is what a modified enter is sent as: escape, then carriage return.
const newline = "\x1b\r"

// modifiedKey encodes a special key held with a modifier, and reports whether
// it is one this needs to handle.
//
// The emulator encodes plain special keys and ctrl+letter itself, but its table
// has no entry for ctrl+left or shift+home. Those reach its default arm, which
// writes nothing unless the key came with no modifier at all - so the key was
// dropped where it stood, and shift+up in a focused pane did nothing whatever.
// Encoding them here is what gets them to the session.
//
// The modifier is xterm's: one, plus a bit for each key held. It deliberately
// does not consult application cursor keys mode, because a modified cursor key
// is sent in CSI form whether that mode is set or not; only the plain form
// changes with it, and the emulator still handles the plain form.
func modifiedKey(k uv.KeyPressEvent) (string, bool) {
	if k.Mod == 0 {
		return "", false
	}

	// A modified enter is a newline rather than a key the session has to know
	// the name of, so it is answered before the CSI table is consulted.
	if k.Code == uv.KeyEnter && newlineKeys[k.Mod] {
		return newline, true
	}
	// ctrl+j is the same request typed a different way. It carries a letter
	// rather than a named key, so it is matched on its own.
	if k.Code == 'j' && k.Mod == uv.ModCtrl {
		return newline, true
	}

	mod := 1
	for _, m := range []struct {
		bit uv.KeyMod
		val int
	}{
		{uv.ModShift, 1},
		{uv.ModAlt, 2},
		{uv.ModCtrl, 4},
		{uv.ModMeta, 8},
	} {
		if k.Mod&m.bit != 0 {
			mod += m.val
		}
	}

	if final, ok := csiFinal[k.Code]; ok {
		return fmt.Sprintf("\x1b[1;%d%c", mod, final), true
	}
	if n, ok := csiTilde[k.Code]; ok {
		return fmt.Sprintf("\x1b[%d;%d~", n, mod), true
	}
	return "", false
}

// ToMouse converts a Bubble Tea mouse message into an ultraviolet mouse event
// positioned at (x, y) in the pane's own coordinate space. The caller is
// responsible for translating screen coordinates into pane coordinates.
//
// As with keys, both sides use ultraviolet's own type, so the button and
// modifier tables this used to carry are gone; only which of the four kinds of
// event it is still has to be said.
func ToMouse(msg tea.MouseMsg, x, y int) (uv.MouseEvent, bool) {
	m := uv.Mouse(msg.Mouse())
	m.X, m.Y = x, y

	switch msg.(type) {
	case tea.MouseClickMsg:
		return uv.MouseClickEvent(m), true
	case tea.MouseReleaseMsg:
		return uv.MouseReleaseEvent(m), true
	case tea.MouseWheelMsg:
		return uv.MouseWheelEvent(m), true
	case tea.MouseMotionMsg:
		return uv.MouseMotionEvent(m), true
	}
	return nil, false
}
