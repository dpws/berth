package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/dpws/berth/internal/agent"
	"github.com/dpws/berth/internal/config"
	"github.com/dpws/berth/internal/host"
	"github.com/dpws/berth/internal/tmux"
	"github.com/dpws/berth/internal/usage"
)

func TestUsageBarClamps(t *testing.T) {
	cases := []struct {
		percent float64
		filled  int
	}{
		{-10, 0}, {0, 0}, {8, 0}, {50, 3}, {100, 6}, {250, 6},
	}
	const width = 6
	for _, c := range cases {
		bar := usageBar(c.percent, tmux.KindCodex, width)
		if got := strings.Count(ansi.Strip(bar), "▓"); got != c.filled {
			t.Errorf("usageBar(%v) filled %d cells, want %d", c.percent, got, c.filled)
		}
		if got := ansi.StringWidth(bar); got != width {
			t.Errorf("usageBar(%v) is %d cells wide, want %d", c.percent, got, width)
		}
	}
}

// The meter grows with the sidebar instead of sitting at a fixed six cells,
// and gives up rather than crowding out the number.
func TestBarWidthFollowsTheSidebar(t *testing.T) {
	lay := meterLayout{label: 4, pct: 4}
	cases := map[int]int{
		12:  0, // no room for a meter at all
		16:  0, // still below the minimum worth drawing
		17:  barMin,
		28:  17,
		40:  barMax, // capped, not a ruler
		200: barMax,
	}
	for w, want := range cases {
		if got := barWidthFor(w, lay); got != want {
			t.Errorf("barWidthFor(%d) = %d, want %d", w, got, want)
		}
	}
	// A wider label, or a column of figures on the right, leaves the meter less
	// room - not the row more width.
	if barWidthFor(28, meterLayout{label: 9, pct: 4}) >= barWidthFor(28, lay) {
		t.Error("a longer label did not take room from the meter")
	}
	if barWidthFor(28, meterLayout{label: 4, pct: 4, tail: 6}) >= barWidthFor(28, lay) {
		t.Error("a right-hand column did not take room from the meter")
	}
}

// The percentage belongs to the meter, so its column sits against the meter
// rather than out at the right edge beside a figure that is a different fact.
// The number is right-aligned inside that column, so the digits still line up
// down the block - 8% and 88% are read against each other as often as against
// their own bars.
func TestPercentageSitsAgainstTheBar(t *testing.T) {
	lay := meterLayout{label: 4, pct: 4, tail: 6, bar: 10}
	for _, percent := range []float64{8, 88, 100} {
		got := ansi.Strip(meterRow("5h", percent, tmux.KindClaude, 28, lay, "2h 57m"))
		// " " + label + " " + bar + " " + pct, so the sign lands at the end of
		// the percentage column wherever the number itself starts.
		want := 1 + lay.label + 1 + lay.bar + 1 + lay.pct - 1
		// In cells, not bytes: the meter is drawn out of multi-byte glyphs.
		at := ansi.StringWidth(got[:strings.Index(got, "%")])
		if at != want {
			t.Errorf("%.0f%%: the percentage ends at %d, want %d - it has come away from the bar",
				percent, at, want)
		}
		if !strings.HasSuffix(got, "2h 57m") {
			t.Errorf("row = %q, want what is left still held at the right edge", got)
		}
	}
}

// Both blocks are drawn to one layout, so the sidebar reads as a single column
// of figures rather than two that nearly line up.
func TestBothBlocksShareTheirColumns(t *testing.T) {
	m := newTestModel()
	m.cfg.ShowHost = true
	m.Update(sessionsMsg([]tmux.Session{{Name: "api", Kind: tmux.KindClaude, Managed: true}}))
	m.usage = map[string]usage.Limits{
		tmux.KindClaude: {Kind: tmux.KindClaude, Windows: []usage.Window{
			{Label: "5h", Percent: 8, ResetsAt: time.Now().Add(2*time.Hour + 57*time.Minute)},
			{Label: "week", Percent: 88, ResetsAt: time.Now().Add(2*24*time.Hour + 12*time.Hour)},
		}},
	}
	m.host = host.Stats{
		CPU:  host.Meter{Percent: 67, Left: "2.69", Known: true},
		Mem:  host.Meter{Percent: 55, Left: "7.1G", Known: true},
		Disk: host.Meter{Percent: 8, Left: "406G", Known: true},
	}

	rows := append(m.usageBlock(28, 10)[1:], m.hostBlock(28, 10)[1:]...)
	if len(rows) != 5 {
		t.Fatalf("got %d meter rows, want 5", len(rows))
	}
	want := strings.Index(ansi.Strip(rows[0]), "%")
	for i, r := range rows {
		if got := strings.Index(ansi.Strip(r), "%"); got != want {
			t.Errorf("row %d has its percentage at %d, want %d - the two blocks disagree:\n%s",
				i, got, want, ansi.Strip(strings.Join(rows, "\n")))
		}
	}
}

// The columns belong to the sidebar, not to whatever the cursor is on. A plain
// shell has no limits of its own, so measuring only the selection laid its rows
// out as though the times column did not exist and the machine's meters came
// out wider than the same meters a moment earlier.
func TestTheLayoutDoesNotMoveWithTheCursor(t *testing.T) {
	m := newTestModel()
	m.cfg.ShowHost = true
	m.Update(sessionsMsg([]tmux.Session{
		{Name: "api", Kind: tmux.KindClaude, Managed: true},
		{Name: "dots", Kind: tmux.KindShell, Managed: true},
	}))
	m.usage = map[string]usage.Limits{
		tmux.KindClaude: {Kind: tmux.KindClaude, Windows: []usage.Window{
			{Label: "week", Percent: 88, ResetsAt: time.Now().Add(2*24*time.Hour + 12*time.Hour)},
		}},
	}
	m.host = host.Stats{
		CPU:  host.Meter{Percent: 67, Left: "2.69", Known: true},
		Mem:  host.Meter{Percent: 55, Left: "7.1G", Known: true},
		Disk: host.Meter{Percent: 8, Left: "406G", Known: true},
	}

	m.cursor = 0 // the agent
	onAgent := ansi.Strip(strings.Join(m.hostBlock(28, 10), "\n"))
	m.cursor = 1 // the shell
	onShell := ansi.Strip(strings.Join(m.hostBlock(28, 10), "\n"))

	if onAgent != onShell {
		t.Errorf("the machine is drawn differently depending on the selection:\n%s\n---\n%s",
			onAgent, onShell)
	}
}

func TestUsageRowFitsTheColumn(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	windows := []usage.Window{
		{Label: "week", Percent: 100},
		{Label: "5h", Percent: 7.5},
		// A load average past every core on the machine, which is the widest
		// percentage anything asks for.
		{Label: "cpu", Percent: 194},
		{Label: "week", Percent: 100, ResetsAt: now.Add(6*24*time.Hour + 23*time.Hour)},
	}
	for _, win := range windows {
		for _, tailW := range []int{0, 6} {
			for _, width := range []int{16, 20, 28, 40} {
				lay := meterLayout{label: 4, pct: 4, tail: tailW}
				lay.bar = barWidthFor(width, lay)
				got := meterRow(win.Label, win.Percent, tmux.KindClaude, width, lay,
					untilReset(win, now))
				if ansi.StringWidth(got) > width {
					t.Errorf("row(%q, w=%d, tail=%d) is %d cells, want at most %d: %q",
						win.Label, width, tailW, ansi.StringWidth(got), width, ansi.Strip(got))
				}
			}
		}
	}
}

func TestUsageBlockNeedsAnAgent(t *testing.T) {
	m := newTestModel()
	m.usage = map[string]usage.Limits{
		tmux.KindCodex: {Kind: tmux.KindCodex, Windows: []usage.Window{
			{Label: "5h", Percent: 28},
		}},
	}

	m.Update(sessions("plain"))
	if got := m.usageBlock(28, 10); got != nil {
		t.Errorf("a shell session drew a usage block: %q", got)
	}

	m.Update(sessionsMsg([]tmux.Session{
		{Name: "work", Kind: tmux.KindCodex, Managed: true},
	}))
	block := m.usageBlock(28, 10)
	if len(block) == 0 {
		t.Fatal("a codex session drew no usage block")
	}
	if !strings.Contains(ansi.Strip(strings.Join(block, "\n")), "28%") {
		t.Errorf("block does not show the percentage: %q", block)
	}
}

func TestUsageBlockRespectsItsBudget(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsMsg([]tmux.Session{
		{Name: "work", Kind: tmux.KindCodex, Managed: true},
	}))
	m.usage = map[string]usage.Limits{
		tmux.KindCodex: {Kind: tmux.KindCodex, Windows: []usage.Window{
			{Label: "5h", Percent: 28, ResetsAt: time.Now().Add(time.Hour)},
			{Label: "week", Percent: 61, ResetsAt: time.Now().Add(48 * time.Hour)},
		}},
	}

	for _, budget := range []int{0, 1, 2, 3, 4, 8} {
		got := m.usageBlock(28, budget)
		if len(got) > budget {
			t.Errorf("budget %d produced %d rows", budget, len(got))
		}
	}
	// With room to spare the block is a divider and both windows. What each
	// one has left rides on its own row rather than costing another.
	if got := len(m.usageBlock(28, 8)); got != 3 {
		t.Errorf("full block has %d rows, want 3", got)
	}
}

// A reading goes stale as soon as the agent stops running, which for Codex is
// most of the time. The time left is worked out from a fixed moment the agent
// was told about, so it is still right then - and that is exactly when you want
// it. Nothing under the meters says how old the numbers are any more: for
// Codex that line was on screen permanently and for Claude almost never.
func TestTimeLeftShowsAlongsideAStaleReading(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsMsg([]tmux.Session{
		{Name: "work", Kind: tmux.KindCodex, Managed: true},
	}))
	m.usage = map[string]usage.Limits{
		tmux.KindCodex: {Kind: tmux.KindCodex, Sampled: time.Now().Add(-6 * time.Hour),
			Windows: []usage.Window{
				{Label: "5h", Percent: 15, ResetsAt: time.Now().Add(2*time.Hour + 52*time.Minute + time.Second)},
				{Label: "week", Percent: 85, ResetsAt: time.Now().Add(3*24*time.Hour + time.Second)},
			}},
	}

	rows := m.usageBlock(28, 10)
	block := ansi.Strip(strings.Join(rows, "\n"))
	// Each window carries its own, on its own row, rather than the block
	// picking one of them to report underneath.
	if !strings.Contains(ansi.Strip(rows[1]), "2h 52m") {
		t.Errorf("5h row = %q, want the time left on it", ansi.Strip(rows[1]))
	}
	if !strings.Contains(ansi.Strip(rows[2]), "3d") {
		t.Errorf("week row = %q, want its own time left", ansi.Strip(rows[2]))
	}
	if strings.Contains(block, "resets ") {
		t.Errorf("block = %q, want no separate resets line", block)
	}
	if strings.Contains(block, "as of ") {
		t.Errorf("block = %q, want no age under the meters", block)
	}
}

// The percentages have to stay in a column of their own. A "52m" beside a
// "6d 23h" would otherwise walk them out of line with each other.
func TestTimesLeftShareAColumn(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsMsg([]tmux.Session{
		{Name: "work", Kind: tmux.KindCodex, Managed: true},
	}))
	m.usage = map[string]usage.Limits{
		tmux.KindCodex: {Kind: tmux.KindCodex, Windows: []usage.Window{
			{Label: "5h", Percent: 15, ResetsAt: time.Now().Add(52 * time.Minute)},
			{Label: "week", Percent: 85, ResetsAt: time.Now().Add(6*24*time.Hour + 23*time.Hour)},
		}},
	}

	rows := m.usageBlock(40, 10)
	first := strings.Index(ansi.Strip(rows[1]), "%")
	second := strings.Index(ansi.Strip(rows[2]), "%")
	if first != second {
		t.Errorf("percentages sit at %d and %d, want one column:\n%q\n%q",
			first, second, ansi.Strip(rows[1]), ansi.Strip(rows[2]))
	}
}

// A window whose reset has already passed has rolled over; the agent simply has
// not run since to say so. Reporting it would count down from a time gone by.
func TestPastResetsAreNotShown(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	past := usage.Window{Label: "5h", Percent: 90, ResetsAt: now.Add(-time.Hour)}
	ahead := usage.Window{Label: "week", Percent: 40, ResetsAt: now.Add(48 * time.Hour)}

	if got := untilReset(past, now); got != "" {
		t.Errorf("untilReset(passed) = %q, want nothing", got)
	}
	if got := untilReset(usage.Window{Label: "5h"}, now); got != "" {
		t.Errorf("untilReset(no reset at all) = %q, want nothing", got)
	}
	if got := untilReset(ahead, now); got != "2d" {
		t.Errorf("untilReset(two days out) = %q, want %q", got, "2d")
	}

	// The column is only as wide as what is actually shown, so a block whose
	// windows have all rolled over is laid out exactly as one with no resets.
	l := usage.Limits{Windows: []usage.Window{past}}
	if got := resetWidth(l, now); got != 0 {
		t.Errorf("resetWidth = %d, want no column at all", got)
	}
}

func TestShortUntil(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "<1m"},
		{52 * time.Minute, "52m"},
		{2*time.Hour + 52*time.Minute, "2h 52m"},
		{3 * time.Hour, "3h"},
		{23*time.Hour + 59*time.Minute, "23h 59m"},
		{3 * 24 * time.Hour, "3d"},
		{6*24*time.Hour + 23*time.Hour, "6d 23h"},
	}
	for _, c := range cases {
		if got := shortUntil(c.d); got != c.want {
			t.Errorf("shortUntil(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestUsageBlockHiddenByConfig(t *testing.T) {
	m := newTestModel()
	m.cfg.HideUsage = true
	m.Update(sessionsMsg([]tmux.Session{
		{Name: "work", Kind: tmux.KindCodex, Managed: true},
	}))
	m.usage = map[string]usage.Limits{
		tmux.KindCodex: {Kind: tmux.KindCodex, Windows: []usage.Window{{Label: "5h", Percent: 1}}},
	}
	if got := m.usageBlock(28, 10); got != nil {
		t.Errorf("hide_usage still drew a block: %q", got)
	}
}

// The sidebar is drawn as fixed-size lines, so a usage block that miscounts
// its rows would push a session off the list or leave a gap.
func TestSidebarStaysExactlyHighWithUsage(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsMsg([]tmux.Session{
		{Name: "work", Kind: tmux.KindCodex, Managed: true},
		{Name: "other", Kind: tmux.KindClaude, Managed: true},
	}))
	m.usage = map[string]usage.Limits{
		tmux.KindCodex: {Kind: tmux.KindCodex, Windows: []usage.Window{
			{Label: "5h", Percent: 28, ResetsAt: time.Now().Add(time.Hour)},
			{Label: "week", Percent: 61, ResetsAt: time.Now().Add(48 * time.Hour)},
		}},
	}

	for _, h := range []int{1, 2, 3, 5, 8, 12, 30} {
		lines := m.sidebarLines(28, h)
		if len(lines) != h {
			t.Fatalf("sidebarLines(28, %d) returned %d lines", h, len(lines))
		}
		for i, line := range lines {
			if got := ansi.StringWidth(line); got != 28 {
				t.Errorf("h=%d line %d is %d cells wide, want 28: %q",
					h, i, got, ansi.Strip(line))
			}
		}
		if len(m.rowSessions) != len(lines) {
			t.Errorf("h=%d: %d rows mapped for %d lines",
				h, len(m.rowSessions), len(lines))
		}
	}
}

// The usage block used to take every row a short window had left over, so a
// terminal eight or ten rows tall drew meters and not one session.
func TestUsageBlockNeverStarvesTheSessionList(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsMsg([]tmux.Session{
		{Name: "alpha", Kind: tmux.KindCodex, Managed: true},
		{Name: "beta", Kind: tmux.KindCodex, Managed: true},
	}))
	m.usage = map[string]usage.Limits{
		tmux.KindCodex: {Kind: tmux.KindCodex, Windows: []usage.Window{
			{Label: "5h", Percent: 28, ResetsAt: time.Now().Add(time.Hour)},
			{Label: "week", Percent: 61, ResetsAt: time.Now().Add(48 * time.Hour)},
		}},
	}

	for h := 5; h <= 16; h++ {
		m.sidebarLines(28, h)
		shown := 0
		for _, s := range m.rowSessions {
			if s >= 0 {
				shown++
			}
		}
		if shown == 0 {
			t.Errorf("h=%d drew no session rows", h)
		}
	}
}

func withAgents(m *Model, infos map[string]agent.Info) { m.agents = infos }

func TestStatusDotReflectsTheAgent(t *testing.T) {
	m := newTestModel()
	s := tmux.Session{Name: "api", Kind: tmux.KindClaude, Attached: 1}

	// The three states must be distinguishable without colour: a turning
	// spinner, a question mark that is asking you something, and a hollow
	// circle for neither.
	cases := map[agent.Status]string{
		agent.Waiting: "?",
		agent.Idle:    "○",
	}
	for status, want := range cases {
		withAgents(m, map[string]agent.Info{"api": {Status: status}})
		if got, _ := m.statusDot(s); got != want {
			t.Errorf("status %q drew %q, want %q", status, got, want)
		}
	}
	for _, status := range []agent.Status{agent.Busy, agent.Shell} {
		withAgents(m, map[string]agent.Info{"api": {Status: status}})
		got, _ := m.statusDot(s)
		if !slices.Contains(spinnerFrames, got) {
			t.Errorf("status %q drew %q, want a spinner frame", status, got)
		}
	}

	// With nothing known, the old attached/detached dot is what is left.
	withAgents(m, nil)
	if got, _ := m.statusDot(s); got != "●" {
		t.Errorf("unknown status drew %q, want the attached dot", got)
	}
}

func TestTaskLinePrefersWhatItIsWaitingFor(t *testing.T) {
	m := newTestModel()
	s := tmux.Session{Name: "api", Kind: tmux.KindClaude}
	withAgents(m, map[string]agent.Info{"api": {
		Status: agent.Waiting,
		Task:   "fix the retry backoff",
		Detail: "approve running: rm -rf build/",
	}})

	got := ansi.Strip(m.taskLine(s, false, 40))
	if !strings.Contains(got, "approve running") {
		t.Errorf("task line = %q, want what it is blocked on", got)
	}

	// While it is working, the task is the useful thing to show.
	withAgents(m, map[string]agent.Info{"api": {Status: agent.Busy, Task: "fix the retry backoff"}})
	got = ansi.Strip(m.taskLine(s, false, 40))
	if !strings.Contains(got, "fix the retry backoff") {
		t.Errorf("task line = %q, want the task", got)
	}
}

// A shell will never have a task, so it gets no row. An agent keeps its row
// even when empty: a row that comes and goes moves every session below it,
// which is a lot of movement to say berth does not know something yet.
func TestTaskRowIsHeldForAgentsAndNotForShells(t *testing.T) {
	m := newTestModel()

	shell := tmux.Session{Name: "dots", Kind: tmux.KindShell}
	if got := m.taskLine(shell, false, 40); got != "" {
		t.Errorf("a shell drew a task row: %q", got)
	}

	agentSession := tmux.Session{Name: "api", Kind: tmux.KindClaude}
	for _, info := range []map[string]agent.Info{
		nil, // nothing known yet
		{"api": {Status: agent.Idle}},
		{"api": {Status: agent.Busy}},
	} {
		withAgents(m, info)
		got := m.taskLine(agentSession, false, 40)
		if got == "" {
			t.Errorf("an agent with no task drew no row at all (%v)", info)
		}
		if strings.TrimSpace(ansi.Strip(got)) != "" {
			t.Errorf("an agent with no task drew %q, want it blank", got)
		}
	}

	m.cfg.HideTask = true
	withAgents(m, map[string]agent.Info{"api": {Status: agent.Busy, Task: "something"}})
	if got := m.taskLine(agentSession, false, 40); got != "" {
		t.Errorf("hide_task still drew %q", got)
	}
}

// How long an agent has been at something is the other half of what the task
// row says, so it sits on the right of it rather than anywhere else.
func TestTaskRowSaysHowLongTheAgentHasBeenAtIt(t *testing.T) {
	m := newTestModel()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	m.clock = func() time.Time { return now }

	agentSession := tmux.Session{Name: "api", Kind: tmux.KindClaude}
	withAgents(m, map[string]agent.Info{"api": {
		Status: agent.Waiting,
		Detail: "may I run git push",
		Since:  now.Add(-12 * time.Minute),
	}})

	line := ansi.Strip(m.taskLine(agentSession, false, 40))
	if !strings.HasSuffix(strings.TrimRight(line, " "), "12m") {
		t.Errorf("task row = %q, want it ending in the age", line)
	}
	if !strings.Contains(line, "may I run git push") {
		t.Errorf("task row = %q, want what it is waiting for still on it", line)
	}

	// A session berth has only just noticed has no age to give, and must not
	// invent one that reads as "just now".
	withAgents(m, map[string]agent.Info{"api": {Status: agent.Busy, Task: "a task"}})
	if got := ansi.Strip(m.taskLine(agentSession, false, 40)); strings.Contains(got, "0s") {
		t.Errorf("task row = %q, want no age when none is known", got)
	}

	m.cfg.HideAgentAge = true
	withAgents(m, map[string]agent.Info{"api": {
		Status: agent.Busy, Task: "a task", Since: now.Add(-time.Hour),
	}})
	if got := ansi.Strip(m.taskLine(agentSession, false, 40)); strings.Contains(got, "1h") {
		t.Errorf("hide_agent_age still drew the age: %q", got)
	}
}

// The age has to fit beside the task rather than push the row past the sidebar,
// which would spill the list into the terminal next to it.
func TestTaskRowKeepsItsWidthWithAnAge(t *testing.T) {
	m := newTestModel()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	m.clock = func() time.Time { return now }

	agentSession := tmux.Session{Name: "api", Kind: tmux.KindClaude}
	withAgents(m, map[string]agent.Info{"api": {
		Status: agent.Busy,
		Task:   strings.Repeat("long ", 40),
		Since:  now.Add(-73 * time.Hour),
	}})

	for _, w := range []int{16, 20, 28, 40} {
		for _, selected := range []bool{false, true} {
			got := ansi.Strip(m.taskLine(agentSession, selected, w))
			if ansi.StringWidth(got) > w {
				t.Errorf("width %d selected %v: row is %d cells: %q",
					w, selected, ansi.StringWidth(got), got)
			}
			if !strings.Contains(got, "3d") {
				t.Errorf("width %d selected %v: age missing from %q", w, selected, got)
			}
		}
	}
}

func TestShortAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m"},
		{59 * time.Minute, "59m"},
		{time.Hour + 30*time.Minute, "1h"},
		{23 * time.Hour, "23h"},
		{50 * time.Hour, "2d"},
	}
	for _, c := range cases {
		if got := shortAge(c.d); got != c.want {
			t.Errorf("shortAge(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// Task lines make a session take two rows, so scrolling and click mapping both
// have to work in rows rather than in sessions.
func TestListScrollsByRowAndKeepsTheCursorVisible(t *testing.T) {
	m := newTestModel()
	var sessions []tmux.Session
	infos := map[string]agent.Info{}
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("s%d", i)
		sessions = append(sessions, tmux.Session{Name: name, Kind: tmux.KindClaude, Managed: true})
		infos[name] = agent.Info{Status: agent.Busy, Task: "working on " + name}
	}
	m.Update(sessionsMsg(sessions))
	withAgents(m, infos)

	for cursor := 0; cursor < len(sessions); cursor++ {
		m.cursor = cursor
		lines := m.sidebarLines(28, 14)
		if len(lines) != 14 {
			t.Fatalf("cursor %d: %d lines, want 14", cursor, len(lines))
		}
		found := false
		for _, row := range m.rowSessions {
			if row == cursor {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("cursor %d scrolled out of view", cursor)
		}
	}
}

// Clicking a task line should select the session it belongs to, not miss.
func TestClickingATaskLineSelectsItsSession(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsMsg([]tmux.Session{
		{Name: "api", Kind: tmux.KindClaude, Managed: true},
		{Name: "web", Kind: tmux.KindCodex, Managed: true},
	}))
	withAgents(m, map[string]agent.Info{
		"api": {Status: agent.Busy, Task: "first"},
		"web": {Status: agent.Busy, Task: "second"},
	})
	m.sidebarLines(28, 14)

	// Skip the header rows, which belong to no session.
	first := 0
	for first < len(m.rowSessions) && m.rowSessions[first] < 0 {
		first++
	}

	// The list rows are then: api, api-task, web, web-task.
	want := []int{0, 0, 1, 1}
	for i, w := range want {
		if first+i >= len(m.rowSessions) {
			t.Fatalf("only %d rows mapped", len(m.rowSessions))
		}
		if got := m.rowSessions[first+i]; got != w {
			t.Errorf("list row %d maps to session %d, want %d", i, got, w)
		}
	}
}

func TestWindowTitleTalliesWhatTheAgentsAreDoing(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsMsg([]tmux.Session{
		{Name: "api", Kind: tmux.KindClaude, Managed: true},
		{Name: "web", Kind: tmux.KindCodex, Managed: true},
		{Name: "docs", Kind: tmux.KindClaude, Managed: true},
	}))

	// Nothing known about any of them yet: the title says only what is
	// selected, rather than a row of zeroes.
	if got := m.windowTitle(); got != "api (claude) — berth" {
		t.Errorf("title = %q, want no tally when nothing is known", got)
	}

	withAgents(m, map[string]agent.Info{"api": {Status: agent.Busy}})
	if got := m.windowTitle(); got != "⠋ 1 api (claude) — berth" {
		t.Errorf("title = %q, want the working count", got)
	}

	// The waiting session is not the selected one, and still marks the tab.
	withAgents(m, map[string]agent.Info{"web": {Status: agent.Waiting}})
	if got := m.windowTitle(); got != "? 1 api (claude) — berth" {
		t.Errorf("title = %q, want the waiting count", got)
	}

	// All three states at once, waiting first: a tab bar truncates from the
	// end, so the one that needs you has to lead.
	withAgents(m, map[string]agent.Info{
		"api":  {Status: agent.Idle},
		"web":  {Status: agent.Waiting},
		"docs": {Status: agent.Busy},
	})
	if got := m.windowTitle(); got != "? 1 ⠋ 1 ○ 1 api (claude) — berth" {
		t.Errorf("title = %q, want all three tallied with waiting first", got)
	}

	withAgents(m, map[string]agent.Info{
		"web":  {Status: agent.Waiting},
		"docs": {Status: agent.Waiting},
	})
	if got := m.windowTitle(); got != "? 2 api (claude) — berth" {
		t.Errorf("title = %q, want a count", got)
	}
}

// The tab turns while an agent works. A tab that is moving says the machine is
// still going from across the room, which is most of what a tab is for.
func TestWindowTitleSpinnerTurns(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsMsg([]tmux.Session{{Name: "api", Kind: tmux.KindClaude, Managed: true}}))
	withAgents(m, map[string]agent.Info{"api": {Status: agent.Busy}})

	seen := map[string]bool{}
	for i := range spinnerFrames {
		m.spinner = i
		title := m.windowTitle()
		frame, _, _ := strings.Cut(title, " ")
		if frame != spinnerFrames[i] {
			t.Errorf("spinner %d drew %q, want %q", i, frame, spinnerFrames[i])
		}
		seen[frame] = true
	}
	if len(seen) != len(spinnerFrames) {
		t.Errorf("the tab used %d of the %d frames", len(seen), len(spinnerFrames))
	}

	// Nothing working, nothing turning: an idle berth writes no titles.
	withAgents(m, map[string]agent.Info{"api": {Status: agent.Idle}})
	before := m.windowTitle()
	m.spinner++
	if got := m.windowTitle(); got != before {
		t.Errorf("with nothing working the title still moved: %q then %q", before, got)
	}
}

// The tally is what berth knows about the agents, so the setting that stops it
// watching them has to take the tally with it.
func TestWindowTitleHasNoTallyWithoutAgentStatus(t *testing.T) {
	cfg := config.Default()
	cfg.HideAgentStatus = true
	m := New(cfg)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(sessionsMsg([]tmux.Session{{Name: "api", Kind: tmux.KindClaude, Managed: true}}))

	m.readAgents(m.sessions)
	if got := m.windowTitle(); got != "api (claude) — berth" {
		t.Errorf("title = %q, want no tally when the agents are not watched", got)
	}
}

// The host block is opt-in: berth is a session list first, and the machine is
// usually not the thing in question.
func TestHostBlockIsOffUntilAskedFor(t *testing.T) {
	m := newTestModel()
	m.Update(sessions("plain"))
	m.host = host.Stats{
		CPU:  host.Meter{Percent: 24, Left: "0.96", Known: true},
		Mem:  host.Meter{Percent: 61, Left: "4.8G", Known: true},
		Disk: host.Meter{Percent: 72, Left: "31G", Known: true},
	}

	if got := m.hostBlock(28, 10); got != nil {
		t.Errorf("the block drew without being asked for: %q", got)
	}

	m.cfg.ShowHost = true
	got := m.hostBlock(28, 10)
	// A divider and the three meters.
	if len(got) != 4 {
		t.Fatalf("block has %d rows, want 4: %q", len(got), ansi.Strip(strings.Join(got, "|")))
	}
	body := ansi.Strip(strings.Join(got, "\n"))
	for _, want := range []string{"cpu", "0.96", "mem", "4.8G", "disk", "31G", "61%"} {
		if !strings.Contains(body, want) {
			t.Errorf("block = %q, want %q on it", body, want)
		}
	}
}

// It draws for any session. The rate limits are about the plan an agent spends,
// but the machine is underneath a plain shell just the same.
func TestHostBlockDrawsForAShellToo(t *testing.T) {
	m := newTestModel()
	m.cfg.ShowHost = true
	m.Update(sessions("dots"))
	m.host = host.Stats{Mem: host.Meter{Percent: 61, Left: "4.8G", Known: true}}

	if got := m.hostBlock(28, 10); len(got) == 0 {
		t.Error("a shell session drew no host block")
	}
}

// A figure the machine would not give up is left out. Drawing it as an empty
// bar would say the machine is idle, which is a different claim from silence.
func TestUnknownHostMetersAreLeftOut(t *testing.T) {
	m := newTestModel()
	m.cfg.ShowHost = true
	m.Update(sessions("plain"))
	m.host = host.Stats{
		CPU:  host.Meter{Percent: 24, Left: "0.96", Known: true},
		Disk: host.Meter{Percent: 72, Left: "31G", Known: true},
	}

	got := m.hostBlock(28, 10)
	body := ansi.Strip(strings.Join(got, "\n"))
	if strings.Contains(body, "mem") {
		t.Errorf("block = %q, want no row for the reading that failed", body)
	}
	if len(got) != 3 {
		t.Errorf("block has %d rows, want the divider and the two that read", len(got))
	}

	// Nothing at all read: no block, not an empty one under a divider.
	m.host = host.Stats{}
	if got := m.hostBlock(28, 10); got != nil {
		t.Errorf("a machine berth cannot read drew %q", got)
	}
}

// The block is drawn into fixed-width lines like everything else in the column.
func TestHostBlockFitsTheColumn(t *testing.T) {
	m := newTestModel()
	m.cfg.ShowHost = true
	m.Update(sessions("plain"))
	m.host = host.Stats{
		CPU:  host.Meter{Percent: 194, Left: "7.76", Known: true},
		Mem:  host.Meter{Percent: 61, Left: "4.8G", Known: true},
		Disk: host.Meter{Percent: 72, Left: "406G", Known: true},
	}

	for _, w := range []int{12, 16, 20, 28, 40} {
		for _, line := range m.hostBlock(w, 10) {
			if got := ansi.StringWidth(line); got > w {
				t.Errorf("w=%d: row is %d cells: %q", w, got, ansi.Strip(line))
			}
		}
	}
	for _, budget := range []int{0, 1, 2, 3, 4, 8} {
		if got := m.hostBlock(28, budget); len(got) > budget {
			t.Errorf("budget %d produced %d rows", budget, len(got))
		}
	}
}

// The sidebar is drawn as fixed-size lines, so a second block under the limits
// has to be counted the same way the first one is or the column overruns.
func TestSidebarStaysExactlyHighWithTheHostBlock(t *testing.T) {
	m := newTestModel()
	m.cfg.ShowHost = true
	m.Update(sessionsMsg([]tmux.Session{
		{Name: "work", Kind: tmux.KindCodex, Managed: true},
		{Name: "other", Kind: tmux.KindClaude, Managed: true},
	}))
	m.usage = map[string]usage.Limits{
		tmux.KindCodex: {Kind: tmux.KindCodex, Windows: []usage.Window{
			{Label: "5h", Percent: 28, ResetsAt: time.Now().Add(time.Hour)},
			{Label: "week", Percent: 61, ResetsAt: time.Now().Add(48 * time.Hour)},
		}},
	}
	m.host = host.Stats{
		CPU:  host.Meter{Percent: 24, Left: "0.96", Known: true},
		Mem:  host.Meter{Percent: 61, Left: "4.8G", Known: true},
		Disk: host.Meter{Percent: 72, Left: "31G", Known: true},
	}

	for _, h := range []int{1, 2, 3, 5, 8, 12, 20, 30} {
		lines := m.sidebarLines(28, h)
		if len(lines) != h {
			t.Fatalf("sidebarLines(28, %d) returned %d lines", h, len(lines))
		}
		for i, line := range lines {
			if got := ansi.StringWidth(line); got != 28 {
				t.Errorf("h=%d line %d is %d cells: %q", h, i, got, ansi.Strip(line))
			}
		}
		if len(m.rowSessions) != len(lines) {
			t.Errorf("h=%d: %d rows mapped for %d lines", h, len(m.rowSessions), len(lines))
		}
		// The list must still get a session, however many blocks are stacked
		// under it - a sidebar of meters and no sessions is not a sidebar.
		shown := 0
		for _, s := range m.rowSessions {
			if s >= 0 {
				shown++
			}
		}
		if h >= 5 && shown == 0 {
			t.Errorf("h=%d drew no session rows", h)
		}
	}
}
