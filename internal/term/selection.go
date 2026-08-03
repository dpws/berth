package term

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// Selection is a run of cells in a pane, held in the order it was dragged so
// the anchor stays put while the far end moves.
//
// It covers text the way a terminal does rather than as a rectangle: the tail
// of the first row, all of the rows between, and the head of the last. A
// rectangle would be wrong for reading wrapped output back out.
type Selection struct {
	AnchorX, AnchorY int
	CursorX, CursorY int
}

// Empty reports whether the selection covers no cells at all.
func (s Selection) Empty() bool {
	return s.AnchorX == s.CursorX && s.AnchorY == s.CursorY
}

// ordered returns the two ends with the earlier one first.
func (s Selection) ordered() (ax, ay, bx, by int) {
	if s.AnchorY < s.CursorY || (s.AnchorY == s.CursorY && s.AnchorX <= s.CursorX) {
		return s.AnchorX, s.AnchorY, s.CursorX, s.CursorY
	}
	return s.CursorX, s.CursorY, s.AnchorX, s.AnchorY
}

// covers reports whether the cell at x, y falls inside the selection.
func (s Selection) covers(x, y int) bool {
	ax, ay, bx, by := s.ordered()
	switch {
	case y < ay || y > by:
		return false
	case ay == by:
		return x >= ax && x <= bx
	case y == ay:
		return x >= ax
	case y == by:
		return x <= bx
	default:
		return true
	}
}

// SelectedText returns the text the selection covers, one row per line with
// trailing blanks dropped. Rows are what is on screen now, so a selection made
// before the pane redrew returns whatever replaced it.
func (p *Pane) SelectedText(s Selection) string {
	if s.Empty() {
		return ""
	}
	w, h := p.Size()
	buf := p.snapshot(w, h)

	ax, ay, bx, by := s.ordered()
	if ay < 0 {
		ay = 0
	}
	if by >= h {
		by = h - 1
	}

	var rows []string
	for y := ay; y <= by; y++ {
		from, to := 0, w-1
		if y == ay {
			from = ax
		}
		if y == by {
			to = bx
		}
		var row strings.Builder
		for x := from; x <= to && x < w; x++ {
			if x < 0 {
				continue
			}
			if cell := buf.CellAt(x, y); cell != nil {
				row.WriteString(cell.String())
			} else {
				row.WriteByte(' ')
			}
		}
		// Terminal rows are blank-padded to the full width; keeping that
		// padding would paste a block of spaces after every line.
		rows = append(rows, strings.TrimRight(row.String(), " "))
	}
	return strings.Join(rows, "\n")
}

// snapshot draws the emulator into a fresh buffer under the pane's lock.
func (p *Pane) snapshot(w, h int) uv.ScreenBuffer {
	buf := uv.NewScreenBuffer(w, h)
	p.emuMu.Lock()
	p.emu.Draw(buf, buf.Bounds())
	p.emuMu.Unlock()
	return buf
}
