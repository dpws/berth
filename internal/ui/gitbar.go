package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/dpws/berth/internal/git"
)

// gitEntry is one directory's answer, kept with whether it was a repository at
// all so a directory that is not one is remembered rather than asked about on
// every refresh.
type gitEntry struct {
	status git.Status
	ok     bool
}

// topBarRows is how tall the strip above the body is: the bar itself and the
// rule closing it off from the body. It is a constant rather than something
// measured because the rows are kept whatever the selected directory turns out
// to be - a bar that came and went as the cursor moved would resize the
// terminal underneath it every time, and tmux would redraw the whole session
// for a line of chrome. Both are rows the session list used to spend on its own
// title and the line under it.
const topBarRows = 2

// topBarHeight is the rows the strip takes out of the window.
func (m *Model) topBarHeight() int { return topBarRows }

var (
	gitBranchStyle   = lipgloss.NewStyle().Foreground(colBranch)
	gitAheadStyle    = lipgloss.NewStyle().Foreground(colSuccess)
	gitBehindStyle   = lipgloss.NewStyle().Foreground(colDanger)
	gitModifiedStyle = lipgloss.NewStyle().Foreground(colBranch)
	gitAddedStyle    = lipgloss.NewStyle().Foreground(colSuccess)
	gitDeletedStyle  = lipgloss.NewStyle().Foreground(colDanger)
)

// gitPart is one run of the bar with the style it is drawn in. The bar is
// built as parts rather than as a string because its width has to be known
// before any styling is applied, and lipgloss.Width on an already-styled
// string is both slower and easier to get wrong.
type gitPart struct {
	text  string
	style lipgloss.Style
}

// gitBarView draws the branch and what has changed on it, with the directory
// itself pushed to the right. It spans the session's half of the window only:
// the sidebar's half of the same row carries berth's own name.
func (m *Model) gitBarView(w int) string {
	if w <= 0 {
		return ""
	}

	dir := m.selectedDir()
	right := prettyDir(dir)

	if m.cfg.HideGitBar {
		return gitBarLine(nil, right, w)
	}

	var parts []gitPart
	if st, ok := m.gitStatus[dir]; ok && st.ok {
		parts = append(parts, gitPart{st.status.Branch, gitBranchStyle})
		if detail := gitDetail(st.status); len(detail) > 0 {
			parts = append(parts, gitPart{"  ", lipgloss.NewStyle()})
			parts = append(parts, detail...)
		}
	}
	// With no parts the row still stands, saying only where the session is.
	// That is worth a line on its own, and keeps the body from moving.
	return gitBarLine(parts, right, w)
}

// gitDetail is the drift, then the files, then the lines, each group held
// apart by a wider gap because they answer different questions: what is not
// pushed, what is not committed, and how much of the last of those is still
// unstaged. Anything at zero is left out, so a clean branch level with its
// upstream says nothing at all - which is the point. The bar should only draw
// the eye when there is something on it.
func gitDetail(s git.Status) []gitPart {
	drift := parts(
		part(s.Ahead, "↑%d", gitAheadStyle),
		part(s.Behind, "↓%d", gitBehindStyle),
	)
	files := parts(
		part(s.Modified, "~%d", gitModifiedStyle),
		part(s.Added, "+%d", gitAddedStyle),
		part(s.Deleted, "-%d", gitDeletedStyle),
	)
	// The line counts are joined by a slash rather than a space, so that a
	// group reading "+120/-45" cannot be taken for the file counts beside it,
	// which use the same two signs for a different unit.
	lines := parts(
		part(s.LinesAdded, "+%d", gitAddedStyle),
		part(s.LinesDeleted, "-%d", gitDeletedStyle),
	)

	var out []gitPart
	for _, group := range [][]gitPart{
		joinParts(drift, " "),
		joinParts(files, " "),
		joinParts(lines, "/"),
	} {
		if len(group) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, gitPart{"  ", lipgloss.NewStyle()})
		}
		out = append(out, group...)
	}
	return out
}

// part renders one count, or nothing at all when it is zero.
func part(n int, format string, style lipgloss.Style) []gitPart {
	if n <= 0 {
		return nil
	}
	return []gitPart{{fmt.Sprintf(format, n), style}}
}

// parts flattens the ones that were not left out.
func parts(groups ...[]gitPart) []gitPart {
	var out []gitPart
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// joinParts puts sep between the parts, as its own unstyled part.
func joinParts(in []gitPart, sep string) []gitPart {
	if len(in) < 2 {
		return in
	}
	out := make([]gitPart, 0, len(in)*2-1)
	for i, p := range in {
		if i > 0 {
			out = append(out, gitPart{sep, lipgloss.NewStyle()})
		}
		out = append(out, p)
	}
	return out
}

// partsText is the bar with no styling, which is what its width is measured
// from and what the tests read.
func partsText(in []gitPart) string {
	var b strings.Builder
	for _, p := range in {
		b.WriteString(p.text)
	}
	return b.String()
}

// partsRender draws the parts in their styles.
func partsRender(in []gitPart) string {
	var b strings.Builder
	for _, p := range in {
		b.WriteString(p.style.Render(p.text))
	}
	return b.String()
}

// gitBarLine lays the two halves out, one cell in from each edge. The
// directory is what goes when there is not room for both, since the branch is
// the thing being asked about; below that the branch is truncated in turn.
func gitBarLine(left []gitPart, right string, w int) string {
	// One blank cell at the right so the path does not sit against the edge of
	// the screen. Nothing is added on the left: the gutter is already there,
	// and another space would set the branch one column in from the session's
	// own output directly below it.
	const edge = 1

	leftW := lipgloss.Width(partsText(left))
	rightW := lipgloss.Width(right)

	// Every row of the frame has to be exactly the screen's width, or the
	// terminal wraps one and everything below it slides a line.
	gap := w - edge - leftW - rightW
	switch {
	case rightW > 0 && gap >= 2:
		return partsRender(left) + strings.Repeat(" ", gap) +
			faintStyle.Render(right) + " "
	case leftW > 0:
		// No room for the directory. Give the whole line to the branch.
		return padTo(truncate(partsRender(left), w), w)
	default:
		return padTo(truncate(faintStyle.Render(right), w), w)
	}
}

// prettyDir shortens a path the way a shell prompt does, since the bar has a
// line rather than a paragraph to say where the session is.
func prettyDir(dir string) string {
	if dir == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return dir
	}
	if dir == home {
		return "~"
	}
	// filepath.Rel would happily walk up out of home with "..", which is longer
	// than the path it replaced and reads as nonsense in a prompt.
	if rel := strings.TrimPrefix(dir, home+string(filepath.Separator)); rel != dir {
		return "~" + string(filepath.Separator) + rel
	}
	return dir
}

// selectedDir is the directory the bar is about: the one the selected session
// is sitting in.
func (m *Model) selectedDir() string {
	s, ok := m.selected()
	if !ok {
		return ""
	}
	return s.Dir
}
