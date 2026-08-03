package term

import "testing"

func TestSelectionCoversLikeATerminal(t *testing.T) {
	// A three-row selection: the tail of row 1, all of row 2, head of row 3.
	s := Selection{AnchorX: 5, AnchorY: 1, CursorX: 3, CursorY: 3}

	cases := []struct {
		x, y int
		want bool
	}{
		{4, 1, false}, // before the start on the first row
		{5, 1, true},
		{99, 1, true}, // to the end of the first row
		{0, 2, true},  // the whole middle row
		{99, 2, true},
		{0, 3, true},
		{3, 3, true},
		{4, 3, false}, // past the end on the last row
		{0, 0, false}, // above
		{0, 9, false}, // below
	}
	for _, c := range cases {
		if got := s.covers(c.x, c.y); got != c.want {
			t.Errorf("covers(%d,%d) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

// Dragging up or to the left must select the same run as dragging back down.
func TestSelectionIsDirectionAgnostic(t *testing.T) {
	forward := Selection{AnchorX: 2, AnchorY: 1, CursorX: 6, CursorY: 3}
	backward := Selection{AnchorX: 6, AnchorY: 3, CursorX: 2, CursorY: 1}

	for x := 0; x < 10; x++ {
		for y := 0; y < 5; y++ {
			if forward.covers(x, y) != backward.covers(x, y) {
				t.Fatalf("cell %d,%d differs between drag directions", x, y)
			}
		}
	}
}

func TestSelectionOnOneRow(t *testing.T) {
	s := Selection{AnchorX: 2, AnchorY: 0, CursorX: 5, CursorY: 0}
	for x, want := range map[int]bool{1: false, 2: true, 5: true, 6: false} {
		if got := s.covers(x, 0); got != want {
			t.Errorf("covers(%d,0) = %v, want %v", x, got, want)
		}
	}
}

func TestSelectionEmpty(t *testing.T) {
	if !(Selection{AnchorX: 3, AnchorY: 2, CursorX: 3, CursorY: 2}).Empty() {
		t.Error("a selection that never moved should be empty")
	}
	if (Selection{AnchorX: 3, AnchorY: 2, CursorX: 4, CursorY: 2}).Empty() {
		t.Error("a selection of one cell should not be empty")
	}
}
