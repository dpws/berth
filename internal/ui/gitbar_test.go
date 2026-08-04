package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/dpws/berth/internal/git"
	"github.com/dpws/berth/internal/tmux"
)

// sessionsIn builds a session list where each session sits in its own
// directory, which is what the bar keys off.
func sessionsIn(dirs map[string]string, names ...string) sessionsMsg {
	out := make([]tmux.Session, 0, len(names))
	for _, n := range names {
		out = append(out, tmux.Session{
			Name: n, Kind: tmux.KindShell, Managed: true, Dir: dirs[n],
		})
	}
	return sessionsMsg(out)
}

func TestGitBarShowsTheBranchOfTheSelectedSession(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsIn(map[string]string{"alpha": "/w/alpha"}, "alpha"))
	m.Update(gitMsg{dir: "/w/alpha", ok: true, status: git.Status{Branch: "main"}})

	bar := m.gitBarView(m.width)
	if !strings.Contains(bar, "main") {
		t.Errorf("bar does not name the branch: %q", bar)
	}
}

// A clean branch level with its upstream should say nothing beyond its name;
// the counts are there to be noticed, and noticing depends on them being rare.
func TestGitBarIsQuietWhenThereIsNothingToSay(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsIn(map[string]string{"alpha": "/w/alpha"}, "alpha"))
	m.Update(gitMsg{dir: "/w/alpha", ok: true, status: git.Status{Branch: "main"}})

	bar := m.gitBarView(m.width)
	for _, mark := range []string{"↑", "↓", "~", "+", "-"} {
		if strings.Contains(bar, mark) {
			t.Errorf("a clean branch in sync still drew %q: %q", mark, bar)
		}
	}
}

func TestGitBarShowsDriftAndDirt(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsIn(map[string]string{"alpha": "/w/alpha"}, "alpha"))
	m.Update(gitMsg{dir: "/w/alpha", ok: true, status: git.Status{
		Branch: "main", Ahead: 2, Behind: 1, Modified: 3, Added: 1,
	}})

	bar := m.gitBarView(m.width)
	for _, want := range []string{"main", "↑2", "↓1", "~3", "+1"} {
		if !strings.Contains(bar, want) {
			t.Errorf("bar is missing %q: %q", want, bar)
		}
	}
	// Deleted is zero, so it should not be drawn at all.
	if strings.Contains(bar, "-0") {
		t.Errorf("a zero count was drawn: %q", bar)
	}
}

// The row is kept whether or not the directory is a repository. A bar that
// came and went with the cursor would change the body height under it, and the
// session would be resized and redrawn on every move.
func TestGitBarKeepsItsRowOutsideARepository(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsIn(map[string]string{"alpha": "/w/plain"}, "alpha"))
	m.Update(gitMsg{dir: "/w/plain", ok: false})

	before := m.bodyHeight()

	m.Update(sessionsIn(map[string]string{"beta": "/w/repo"}, "beta"))
	m.Update(gitMsg{dir: "/w/repo", ok: true, status: git.Status{Branch: "main"}})

	if after := m.bodyHeight(); after != before {
		t.Errorf("body height moved from %d to %d as the cursor changed repository",
			before, after)
	}
	if m.topBarHeight() != 2 {
		t.Errorf("strip height = %d, want 2: the bar and the rule under it", m.topBarHeight())
	}
}

// Hiding the git bar silences the session's half of the strip. It does not give
// a row back, because the row is not the git bar's to give: the other half
// carries berth's own name, which the session list used to spend a row on.
func TestHidingTheGitBarSilencesItWithoutMovingAnything(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsIn(map[string]string{"alpha": "/w/alpha"}, "alpha"))
	m.Update(gitMsg{dir: "/w/alpha", ok: true, status: git.Status{Branch: "main", Ahead: 2}})

	before := m.bodyHeight()
	if !strings.Contains(m.gitBarView(m.width), "main") {
		t.Fatal("the branch was not being shown to begin with")
	}

	m.cfg.HideGitBar = true
	if got := m.bodyHeight(); got != before {
		t.Errorf("body height moved from %d to %d when the bar was hidden", before, got)
	}
	if m.topBarHeight() != 2 {
		t.Errorf("the strip is %d rows with the git half hidden, want 2", m.topBarHeight())
	}

	bar := m.gitBarView(m.width)
	for _, gone := range []string{"main", "↑2"} {
		if strings.Contains(bar, gone) {
			t.Errorf("a hidden bar still drew %q: %q", gone, bar)
		}
	}
	// The directory is the strip's own, not the git bar's, so it stays.
	if !strings.Contains(bar, "alpha") {
		t.Errorf("the directory went with the branch: %q", bar)
	}
}

// berth's name and build move out of the session list and into the strip, over
// the column the list occupies.
func TestBrandBarCarriesTheNameAndBuild(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha", "bravo"))

	bar := m.brandBar(28)
	if !strings.Contains(bar, "BERTH") {
		t.Errorf("the strip does not name berth: %q", bar)
	}
	if !strings.Contains(bar, "2") {
		t.Errorf("the strip does not count the sessions: %q", bar)
	}

	// And the list no longer spends a row saying it itself.
	rows := m.sidebarLines(28, m.bodyHeight())
	if strings.Contains(rows[0], "BERTH") {
		t.Errorf("the session list still draws its own title: %q", rows[0])
	}
}

// The reply is filed under the directory it was asked about. The cursor can
// move while a read is in flight, and the answer for the old directory must
// not be shown against the new one.
func TestAReplyIsFiledUnderTheDirectoryItAsked(t *testing.T) {
	m := newTestModel()
	dirs := map[string]string{"alpha": "/w/alpha", "beta": "/w/beta"}
	m.Update(sessionsIn(dirs, "alpha", "beta"))

	// alpha is selected; beta's answer arrives first.
	m.Update(gitMsg{dir: "/w/beta", ok: true, status: git.Status{Branch: "other"}})

	if bar := m.gitBarView(m.width); strings.Contains(bar, "other") {
		t.Errorf("beta's branch was shown while alpha was selected: %q", bar)
	}

	m.Update(gitMsg{dir: "/w/alpha", ok: true, status: git.Status{Branch: "mine"}})
	if bar := m.gitBarView(m.width); !strings.Contains(bar, "mine") {
		t.Errorf("alpha's own branch never appeared: %q", bar)
	}
}

// Moving onto a session in a different directory has to ask about it, however
// the cursor got there.
func TestMovingTheCursorAsksAboutTheNewDirectory(t *testing.T) {
	m := newTestModel()
	dirs := map[string]string{"alpha": "/w/alpha", "beta": "/w/beta"}
	m.Update(sessionsIn(dirs, "alpha", "beta"))

	if m.gitDir != "/w/alpha" {
		t.Fatalf("the first selection was not asked about, gitDir = %q", m.gitDir)
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown}); cmd == nil {
		t.Fatal("moving the cursor produced no command at all")
	}
	if m.gitDir != "/w/beta" {
		t.Errorf("gitDir = %q after moving down, want /w/beta", m.gitDir)
	}
}

// One poll chain, no matter how many times the settings are saved. A tick per
// save is exactly the leak the rate limit block had.
func TestSavingSettingsDoesNotStartASecondPollChain(t *testing.T) {
	m := newTestModel()
	m.Init() // starts the one chain, as it does on a real run
	m.Update(sessions("alpha"))

	gen := m.gitGen
	for i := 0; i < 3; i++ {
		m.applyConfig()
	}
	if m.gitGen != gen {
		t.Errorf("generation moved from %d to %d without the bar being toggled",
			gen, m.gitGen)
	}

	// Toggling it off and on again is the case that does need a new chain.
	m.cfg.HideGitBar = true
	m.applyConfig()
	m.cfg.HideGitBar = false
	m.applyConfig()
	if m.gitGen == gen {
		t.Error("turning the bar off and on again did not start a fresh chain")
	}
}

// A tick from a chain that has been replaced must die rather than run on
// beside its replacement.
func TestARetiredTickDoesNotKeepTicking(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))

	stale := m.gitGen
	m.gitGen++

	if cmd := m.update(gitTickMsg{gen: stale}); cmd != nil {
		t.Error("a tick from a retired chain scheduled more work")
	}
	if cmd := m.update(gitTickMsg{gen: m.gitGen}); cmd == nil {
		t.Error("the current chain stopped ticking")
	}
}

func TestPrettyDirShortensHome(t *testing.T) {
	t.Setenv("HOME", "/home/someone")
	for _, tc := range [2][2]string{
		{"/home/someone", "~"},
		{"/home/someone/code/api", "~/code/api"},
	} {
		if got := prettyDir(tc[0]); got != tc[1] {
			t.Errorf("prettyDir(%q) = %q, want %q", tc[0], got, tc[1])
		}
	}
	// A path that merely starts with the same letters is not inside home.
	if got := prettyDir("/home/someoneelse/x"); got != "/home/someoneelse/x" {
		t.Errorf("prettyDir trimmed a path that was not under home: %q", got)
	}
}

// The bar has one line for a branch, some counts and a path, and a narrow
// window has to lose the least useful of them rather than wrap.
func TestGitBarFitsANarrowWindow(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsIn(map[string]string{"alpha": "/very/long/path/to/a/project"}, "alpha"))
	m.Update(gitMsg{dir: "/very/long/path/to/a/project", ok: true, status: git.Status{
		Branch: "a-rather-long-branch-name", Modified: 2,
	}})

	for _, w := range []int{80, 40, 20, 10, 4, 1} {
		bar := m.gitBarView(w)
		if got := lipgloss.Width(bar); got > w {
			t.Errorf("at width %d the bar came out %d wide: %q", w, got, bar)
		}
		if strings.Contains(bar, "\n") {
			t.Errorf("at width %d the bar wrapped: %q", w, bar)
		}
	}
}

// The two groups answer different questions, so they are held apart. With only
// one of them in play there is nothing to hold apart and no double gap.
func TestGitDetailGroupsDriftApartFromDirt(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   git.Status
		want string
	}{
		{"drift and files", git.Status{Ahead: 2, Behind: 1, Modified: 3, Added: 1, Deleted: 4}, "↑2 ↓1  ~3 +1 -4"},
		{"drift only", git.Status{Ahead: 2}, "↑2"},
		{"files only", git.Status{Modified: 3, Deleted: 1}, "~3 -1"},
		{"neither", git.Status{}, ""},

		// The line counts are their own group, joined by a slash so they cannot
		// be read as more file counts.
		{"lines only", git.Status{LinesAdded: 120, LinesDeleted: 45}, "+120/-45"},
		{"added lines alone", git.Status{LinesAdded: 7}, "+7"},
		{"all three groups", git.Status{
			Ahead: 1, Modified: 2, LinesAdded: 9, LinesDeleted: 3,
		}, "↑1  ~2  +9/-3"},
	} {
		if got := partsText(gitDetail(tc.in)); got != tc.want {
			t.Errorf("%s: gitDetail = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The colours are the bar's shorthand, and each has to mean one thing: yellow
// for where you are and what you have touched, green for what you have made,
// red for what you have lost or fallen behind.
func TestGitBarColours(t *testing.T) {
	withColour(t)
	m := newTestModel()
	m.Update(sessionsIn(map[string]string{"alpha": "/w/alpha"}, "alpha"))
	m.Update(gitMsg{dir: "/w/alpha", ok: true, status: git.Status{
		Branch:   "feature",
		Ahead:    2,
		Behind:   1,
		Modified: 3,
		Added:    4,
		Deleted:  5,

		LinesAdded:   120,
		LinesDeleted: 45,
	}})

	bar := m.gitBarView(m.width)
	for _, want := range []struct {
		what string
		text string
	}{
		{"branch", gitBranchStyle.Render("feature")},
		{"commits ahead", gitAheadStyle.Render("↑2")},
		{"commits behind", gitBehindStyle.Render("↓1")},
		{"modified files", gitModifiedStyle.Render("~3")},
		{"added files", gitAddedStyle.Render("+4")},
		{"deleted files", gitDeletedStyle.Render("-5")},
		{"lines added", gitAddedStyle.Render("+120")},
		{"lines deleted", gitDeletedStyle.Render("-45")},
	} {
		if !strings.Contains(bar, want.text) {
			t.Errorf("%s is not drawn in its own colour", want.what)
		}
	}
}

// Green is for what you have and red for what you have not, on both counts, so
// the two must not be drawn the same.
func TestAheadAndBehindDoNotShareAColour(t *testing.T) {
	if gitAheadStyle.GetForeground() == gitBehindStyle.GetForeground() {
		t.Error("ahead and behind are the same colour")
	}
	if gitAddedStyle.GetForeground() == gitDeletedStyle.GetForeground() {
		t.Error("additions and deletions are the same colour")
	}
	if gitBranchStyle.GetForeground() != gitModifiedStyle.GetForeground() {
		t.Error("the branch and modified files were meant to share the one yellow")
	}
}

// The strip is closed off by a rule the width of the window, joined to the
// divider between the columns - the one at the foot of the screen turned the
// other way up.
func TestTopRuleMirrorsTheFooterRule(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("alpha"))
	sideW := m.sidebarWidth()

	top := ansi.Strip(m.topRule(sideW))
	foot := ansi.Strip(m.footerRule(sideW))

	if ansi.StringWidth(top) != m.width {
		t.Errorf("the rule is %d cells, want the window's %d",
			ansi.StringWidth(top), m.width)
	}
	if got := []rune(top)[sideW]; got != '┬' {
		t.Errorf("the rule joins the divider with %q, want ┬", got)
	}
	if got := []rune(foot)[sideW]; got != '┴' {
		t.Errorf("the footer rule joins with %q, want ┴", got)
	}
	// Same drawing, only the join differs.
	if strings.ReplaceAll(top, "┬", "") != strings.ReplaceAll(foot, "┴", "") {
		t.Errorf("the two rules are not mirrors:\n  %q\n  %q", top, foot)
	}
}

// Both rules light the half beside whichever column has the keyboard, so the
// frame says where focus is from the top as well as the bottom.
func TestTopRuleFollowsFocus(t *testing.T) {
	withColour(t)
	m := newTestModel()
	m.Update(sessions("alpha"))
	sideW := m.sidebarWidth()

	lit := focusedDivStyle.Render(strings.Repeat("─", sideW))
	if !strings.Contains(m.topRule(sideW), lit) {
		t.Error("with the list focused, the rule over it is not lit")
	}

	m.focus = focusTerminal
	if strings.Contains(m.topRule(sideW), lit) {
		t.Error("the rule over the list stayed lit after focus moved to the session")
	}
	if !strings.Contains(m.topRule(sideW), focusedDivStyle.Render("┬")) {
		t.Error("the join went dim while the divider below it was lit")
	}
}
