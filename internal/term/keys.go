package term

import (
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
