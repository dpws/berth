// Package term runs a tmux client inside a pty and renders its output through
// an in-process terminal emulator, so the session can be drawn into a region
// of a larger TUI instead of owning the whole screen.
package term

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

const readBufferSize = 64 * 1024

// focusEventMode is DECSET 1004, the private mode a terminal application sets
// to ask for focus in/out reports.
const focusEventMode = 1004

// Pane is a live view of one tmux session.
//
// Output flows: tmux client -> pty -> emulator -> Render().
// Input flows:  SendKey/SendText/SendMouse/SetFocus -> emulator (which applies
// terminal modes such as application cursor keys, mouse reporting and focus
// reporting) -> pty -> tmux client.
type Pane struct {
	Session string

	// emu is a plain Emulator rather than a SafeEmulator: several of the input
	// methods we need (Focus, Blur, SendMouse) are not covered by the safe
	// wrapper's lock, so the pane owns one mutex over the whole emulator
	// instead of mixing two locking schemes.
	emu   *vt.Emulator
	emuMu sync.Mutex

	ptmx *os.File
	cmd  *exec.Cmd

	// dirty carries a coalesced "something changed" signal. It is buffered
	// with capacity 1 and never closed, so senders never block or panic.
	dirty chan struct{}

	mu     sync.Mutex
	w, h   int
	closed bool

	exited     atomic.Bool
	cursorHide atomic.Bool
	title      atomic.Value // string

	// wantFocus is the focus state the UI last asked for. A SetFocus issued
	// right after Attach is dropped, because the tmux client has not turned
	// focus reporting on yet - so the state is re-sent when it does, which
	// refocus records.
	wantFocus atomic.Bool
	refocus   atomic.Bool
}

// Attach starts a tmux client for session and renders it at w x h cells.
func Attach(session string, w, h int) (*Pane, error) {
	if session == "" {
		return nil, errors.New("no session name")
	}
	w, h = clampSize(w, h)

	p := &Pane{
		Session: session,
		emu:     vt.NewEmulator(w, h),
		dirty:   make(chan struct{}, 1),
		w:       w,
		h:       h,
	}
	p.title.Store(session)
	p.emu.SetCallbacks(vt.Callbacks{
		CursorVisibility: func(visible bool) { p.cursorHide.Store(!visible) },
		Title:            func(t string) { p.title.Store(t) },
	})
	p.watchFocusMode()

	// -u forces UTF-8; the tmux client is what actually renders the session,
	// so nested prefix keys and copy mode keep working as usual.
	cmd := exec.Command("tmux", "-u", "attach-session", "-t", "="+session)
	cmd.Env = append(envWithoutTmux(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
	if err != nil {
		return nil, err
	}
	p.ptmx = ptmx
	p.cmd = cmd

	go p.readLoop()
	go p.writeLoop()
	go p.waitLoop()

	return p, nil
}

// Dirty returns the channel signalled whenever the pane's contents change.
func (p *Pane) Dirty() <-chan struct{} { return p.dirty }

// Exited reports whether the tmux client has gone away (detached or killed).
func (p *Pane) Exited() bool { return p.exited.Load() }

// Closed reports whether Close has been called on this pane.
func (p *Pane) Closed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// Title returns the terminal title reported by the session, if any.
func (p *Pane) Title() string {
	t, _ := p.title.Load().(string)
	return t
}

// Size returns the pane's current cell dimensions.
func (p *Pane) Size() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.w, p.h
}

// Resize changes both the pty window size and the emulator's grid. tmux
// redraws itself in response to SIGWINCH on the pty.
func (p *Pane) Resize(w, h int) {
	w, h = clampSize(w, h)

	p.mu.Lock()
	if p.closed || (w == p.w && h == p.h) {
		p.mu.Unlock()
		return
	}
	p.w, p.h = w, h
	ptmx := p.ptmx
	p.mu.Unlock()

	p.emuMu.Lock()
	p.emu.Resize(w, h)
	p.emuMu.Unlock()

	if ptmx != nil {
		_ = pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
	}
	p.notify()
}

// SendKey forwards a key press to the session.
func (p *Pane) SendKey(key uv.KeyPressEvent) {
	if p.Closed() {
		return
	}
	p.emuMu.Lock()
	defer p.emuMu.Unlock()
	p.emu.SendKey(key)
}

// SendText forwards literal text to the session. Typing arrives from Bubble
// Tea in runs of runes, so this is the normal path for printable input.
func (p *Pane) SendText(s string) {
	if s == "" || p.Closed() {
		return
	}
	p.emuMu.Lock()
	defer p.emuMu.Unlock()
	p.emu.SendText(s)
}

// Paste forwards text as a paste, bracketed when the session asked for it.
func (p *Pane) Paste(s string) {
	if s == "" || p.Closed() {
		return
	}
	p.emuMu.Lock()
	defer p.emuMu.Unlock()
	p.emu.Paste(s)
}

// SendMouse forwards a mouse event. Coordinates must already be relative to
// the pane's own top-left cell. The emulator drops the event unless the
// session turned mouse reporting on, so this is safe to call unconditionally.
func (p *Pane) SendMouse(m uv.MouseEvent) {
	if p.Closed() {
		return
	}
	p.emuMu.Lock()
	defer p.emuMu.Unlock()
	p.emu.SendMouse(m)
}

// watchFocusMode notices when the tmux client turns focus reporting on, which
// it only does once its focus-events option is enabled. Whatever focus state
// the UI asked for before that point was dropped, so it has to be re-sent.
// The handlers report "not handled" so the emulator still applies the mode
// itself; they only observe.
func (p *Pane) watchFocusMode() {
	observe := func(enabled bool) func(ansi.Params) bool {
		return func(params ansi.Params) bool {
			params.ForEach(0, func(_, param int, _ bool) {
				if param == focusEventMode && enabled {
					p.refocus.Store(true)
				}
			})
			return false
		}
	}
	p.emu.RegisterCsiHandler(ansi.Command('?', 0, 'h'), observe(true))
	p.emu.RegisterCsiHandler(ansi.Command('?', 0, 'l'), observe(false))
}

// SetFocus tells the session whether it has the keyboard. Like mouse events,
// this is a no-op unless the session enabled focus reporting - which tmux only
// does when its focus-events option is on.
func (p *Pane) SetFocus(focused bool) {
	p.wantFocus.Store(focused)
	p.applyFocus()
}

func (p *Pane) applyFocus() {
	if p.Closed() {
		return
	}
	p.emuMu.Lock()
	defer p.emuMu.Unlock()
	if p.wantFocus.Load() {
		p.emu.Focus()
	} else {
		p.emu.Blur()
	}
}

// Render returns one styled string per row of the pane. When showCursor is
// true the cell under the cursor is drawn reversed, standing in for a real
// hardware cursor that a sub-region cannot own. A non-nil sel is drawn
// reversed too, which is how a selection looks in every other terminal.
func (p *Pane) Render(showCursor bool, sel *Selection) []string {
	w, h := p.Size()
	buf := uv.NewScreenBuffer(w, h)

	p.emuMu.Lock()
	p.emu.Draw(buf, buf.Bounds())
	pos := p.emu.CursorPosition()
	p.emuMu.Unlock()

	if sel != nil && !sel.Empty() {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if !sel.covers(x, y) {
					continue
				}
				cell := buf.CellAt(x, y)
				if cell == nil {
					cell = uv.NewCell(buf.WidthMethod(), " ")
				} else {
					cell = cell.Clone()
				}
				cell.Style.Attrs ^= uv.AttrReverse
				buf.SetCell(x, y, cell)
			}
		}
	}

	if showCursor && !p.cursorHide.Load() {
		if pos.X >= 0 && pos.X < w && pos.Y >= 0 && pos.Y < h {
			cell := buf.CellAt(pos.X, pos.Y)
			if cell == nil {
				cell = uv.NewCell(buf.WidthMethod(), " ")
			} else {
				cell = cell.Clone()
			}
			cell.Style.Attrs ^= uv.AttrReverse
			buf.SetCell(pos.X, pos.Y, cell)
		}
	}

	lines := strings.Split(buf.Render(), "\n")
	// Guard against a renderer that trims trailing blank rows.
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
}

// Close detaches the tmux client and releases the pty. The tmux session and
// everything running in it survive.
func (p *Pane) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	ptmx, cmd := p.ptmx, p.cmd
	p.mu.Unlock()

	p.emuMu.Lock()
	_ = p.emu.Close()
	p.emuMu.Unlock()

	if ptmx != nil {
		_ = ptmx.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	p.notify()
	return nil
}

func (p *Pane) readLoop() {
	buf := make([]byte, readBufferSize)
	for {
		n, err := p.ptmx.Read(buf)
		if n > 0 {
			p.emuMu.Lock()
			_, _ = p.emu.Write(buf[:n])
			p.emuMu.Unlock()
			if p.refocus.Swap(false) {
				// Applied outside the Write above: emuMu is already held there
				// and is not reentrant.
				p.applyFocus()
			}
			p.notify()
		}
		if err != nil {
			return
		}
	}
}

func (p *Pane) writeLoop() {
	// emu.Read blocks until the emulator has encoded input to send onward, and
	// returns io.EOF once the emulator is closed. It reads the emulator's
	// internal pipe rather than its screen state, so it deliberately runs
	// without emuMu - holding the lock here would deadlock every input call.
	_, err := io.Copy(p.ptmx, p.emu)
	if err != nil && !errors.Is(err, io.EOF) {
		return
	}
}

func (p *Pane) waitLoop() {
	_ = p.cmd.Wait()
	p.exited.Store(true)
	p.notify()
}

// notify signals a change without ever blocking; a pending signal already
// covers any newer change.
func (p *Pane) notify() {
	select {
	case p.dirty <- struct{}{}:
	default:
	}
}

// envWithoutTmux is the environment with $TMUX and $TMUX_PANE taken out.
//
// tmux refuses to attach a session from inside one - "sessions should be nested
// with care, unset $TMUX to force" - and exits. Running berth from a tmux pane
// therefore started a client that died at once, which cleared the pane, which
// asked for the session again a tenth of a second later: several dead clients a
// second, each leaving a pty behind, until berth ran out of descriptors.
func envWithoutTmux() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "TMUX=") || strings.HasPrefix(kv, "TMUX_PANE=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func clampSize(w, h int) (int, int) {
	if w < 2 {
		w = 2
	}
	if h < 1 {
		h = 1
	}
	return w, h
}
