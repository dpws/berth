package term

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
)

type keySpec struct {
	code rune
	mod  uv.KeyMod
}

// specialKeys maps Bubble Tea's named keys onto ultraviolet key codes. Values
// that collide with the ctrl+letter range (tab, enter, esc, backspace) are
// listed here so they win over the arithmetic fallback below.
var specialKeys = map[tea.KeyType]keySpec{
	tea.KeyEnter:     {code: uv.KeyEnter},
	tea.KeyTab:       {code: uv.KeyTab},
	tea.KeyEsc:       {code: uv.KeyEscape},
	tea.KeyBackspace: {code: uv.KeyBackspace},
	tea.KeyShiftTab:  {code: uv.KeyTab, mod: uv.ModShift},

	tea.KeyUp:     {code: uv.KeyUp},
	tea.KeyDown:   {code: uv.KeyDown},
	tea.KeyRight:  {code: uv.KeyRight},
	tea.KeyLeft:   {code: uv.KeyLeft},
	tea.KeyHome:   {code: uv.KeyHome},
	tea.KeyEnd:    {code: uv.KeyEnd},
	tea.KeyPgUp:   {code: uv.KeyPgUp},
	tea.KeyPgDown: {code: uv.KeyPgDown},
	tea.KeyDelete: {code: uv.KeyDelete},
	tea.KeyInsert: {code: uv.KeyInsert},

	tea.KeyCtrlUp:     {code: uv.KeyUp, mod: uv.ModCtrl},
	tea.KeyCtrlDown:   {code: uv.KeyDown, mod: uv.ModCtrl},
	tea.KeyCtrlRight:  {code: uv.KeyRight, mod: uv.ModCtrl},
	tea.KeyCtrlLeft:   {code: uv.KeyLeft, mod: uv.ModCtrl},
	tea.KeyCtrlHome:   {code: uv.KeyHome, mod: uv.ModCtrl},
	tea.KeyCtrlEnd:    {code: uv.KeyEnd, mod: uv.ModCtrl},
	tea.KeyCtrlPgUp:   {code: uv.KeyPgUp, mod: uv.ModCtrl},
	tea.KeyCtrlPgDown: {code: uv.KeyPgDown, mod: uv.ModCtrl},

	tea.KeyShiftUp:    {code: uv.KeyUp, mod: uv.ModShift},
	tea.KeyShiftDown:  {code: uv.KeyDown, mod: uv.ModShift},
	tea.KeyShiftRight: {code: uv.KeyRight, mod: uv.ModShift},
	tea.KeyShiftLeft:  {code: uv.KeyLeft, mod: uv.ModShift},
	tea.KeyShiftHome:  {code: uv.KeyHome, mod: uv.ModShift},
	tea.KeyShiftEnd:   {code: uv.KeyEnd, mod: uv.ModShift},

	tea.KeyCtrlShiftUp:    {code: uv.KeyUp, mod: uv.ModCtrl | uv.ModShift},
	tea.KeyCtrlShiftDown:  {code: uv.KeyDown, mod: uv.ModCtrl | uv.ModShift},
	tea.KeyCtrlShiftLeft:  {code: uv.KeyLeft, mod: uv.ModCtrl | uv.ModShift},
	tea.KeyCtrlShiftRight: {code: uv.KeyRight, mod: uv.ModCtrl | uv.ModShift},
	tea.KeyCtrlShiftHome:  {code: uv.KeyHome, mod: uv.ModCtrl | uv.ModShift},
	tea.KeyCtrlShiftEnd:   {code: uv.KeyEnd, mod: uv.ModCtrl | uv.ModShift},

	tea.KeyF1:  {code: uv.KeyF1},
	tea.KeyF2:  {code: uv.KeyF2},
	tea.KeyF3:  {code: uv.KeyF3},
	tea.KeyF4:  {code: uv.KeyF4},
	tea.KeyF5:  {code: uv.KeyF5},
	tea.KeyF6:  {code: uv.KeyF6},
	tea.KeyF7:  {code: uv.KeyF7},
	tea.KeyF8:  {code: uv.KeyF8},
	tea.KeyF9:  {code: uv.KeyF9},
	tea.KeyF10: {code: uv.KeyF10},
	tea.KeyF11: {code: uv.KeyF11},
	tea.KeyF12: {code: uv.KeyF12},
	tea.KeyF13: {code: uv.KeyF13},
	tea.KeyF14: {code: uv.KeyF14},
	tea.KeyF15: {code: uv.KeyF15},
	tea.KeyF16: {code: uv.KeyF16},
	tea.KeyF17: {code: uv.KeyF17},
	tea.KeyF18: {code: uv.KeyF18},
	tea.KeyF19: {code: uv.KeyF19},
	tea.KeyF20: {code: uv.KeyF20},
}

// ctrlSymbols covers ctrl+\, ctrl+], ctrl+^ and ctrl+_, whose key types are
// the control bytes 28..31.
var ctrlSymbols = []rune{'\\', ']', '^', '_'}

// ToKeyPress converts a Bubble Tea key message into an ultraviolet key event.
// Handing the event to the emulator (rather than encoding bytes ourselves)
// means terminal modes such as application cursor keys are honoured.
func ToKeyPress(msg tea.KeyMsg) (uv.KeyPressEvent, bool) {
	var k uv.Key
	if msg.Alt {
		k.Mod |= uv.ModAlt
	}

	switch msg.Type {
	case tea.KeyRunes:
		if len(msg.Runes) == 0 {
			return uv.KeyPressEvent(k), false
		}
		k.Code = msg.Runes[0]
		k.Text = string(msg.Runes)
		return uv.KeyPressEvent(k), true
	case tea.KeySpace:
		k.Code = uv.KeySpace
		if k.Mod == 0 {
			k.Text = " "
		}
		return uv.KeyPressEvent(k), true
	}

	if spec, ok := specialKeys[msg.Type]; ok {
		k.Code = spec.code
		k.Mod |= spec.mod
		return uv.KeyPressEvent(k), true
	}

	switch t := msg.Type; {
	case t == tea.KeyNull: // ctrl+@ / ctrl+space
		k.Code = uv.KeySpace
		k.Mod |= uv.ModCtrl
	case t >= 1 && t <= 26:
		k.Code = rune('a' + int(t) - 1)
		k.Mod |= uv.ModCtrl
	case t >= 28 && t <= 31:
		k.Code = ctrlSymbols[int(t)-28]
		k.Mod |= uv.ModCtrl
	default:
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

// mouseButtons maps Bubble Tea's button enum onto ultraviolet's. Both follow
// the X11 numbering, but they are separate types with separate zero values.
var mouseButtons = map[tea.MouseButton]uv.MouseButton{
	tea.MouseButtonNone:       uv.MouseNone,
	tea.MouseButtonLeft:       uv.MouseLeft,
	tea.MouseButtonMiddle:     uv.MouseMiddle,
	tea.MouseButtonRight:      uv.MouseRight,
	tea.MouseButtonWheelUp:    uv.MouseWheelUp,
	tea.MouseButtonWheelDown:  uv.MouseWheelDown,
	tea.MouseButtonWheelLeft:  uv.MouseWheelLeft,
	tea.MouseButtonWheelRight: uv.MouseWheelRight,
	tea.MouseButtonBackward:   uv.MouseBackward,
	tea.MouseButtonForward:    uv.MouseForward,
}

// ToMouse converts a Bubble Tea mouse message into an ultraviolet mouse event
// positioned at (x, y) in the pane's own coordinate space. The caller is
// responsible for translating screen coordinates into pane coordinates.
func ToMouse(msg tea.MouseMsg, x, y int) (uv.MouseEvent, bool) {
	button, ok := mouseButtons[msg.Button]
	if !ok {
		return nil, false
	}

	m := uv.Mouse{X: x, Y: y, Button: button}
	if msg.Shift {
		m.Mod |= uv.ModShift
	}
	if msg.Alt {
		m.Mod |= uv.ModAlt
	}
	if msg.Ctrl {
		m.Mod |= uv.ModCtrl
	}

	if tea.MouseEvent(msg).IsWheel() {
		return uv.MouseWheelEvent(m), true
	}
	switch msg.Action {
	case tea.MouseActionPress:
		return uv.MouseClickEvent(m), true
	case tea.MouseActionRelease:
		return uv.MouseReleaseEvent(m), true
	case tea.MouseActionMotion:
		return uv.MouseMotionEvent(m), true
	}
	return nil, false
}
