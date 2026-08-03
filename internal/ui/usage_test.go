package ui

import (
	"fmt"

	"github.com/dpws/berth/internal/agent"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
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
	cases := map[int]int{
		12:  0, // no room for a meter at all
		16:  0, // still below the minimum worth drawing
		18:  barMin,
		28:  16,
		40:  barMax, // capped, not a ruler
		200: barMax,
	}
	for w, want := range cases {
		if got := barWidthFor(w); got != want {
			t.Errorf("barWidthFor(%d) = %d, want %d", w, got, want)
		}
	}
}

func TestUsageRowFitsTheColumn(t *testing.T) {
	windows := []usage.Window{
		{Label: "week", Percent: 100},
		{Label: "5h", Percent: 7.5},
	}
	for _, w := range windows {
		for _, width := range []int{16, 20, 28, 40} {
			got := usageRow(w, tmux.KindClaude, width)
			if ansi.StringWidth(got) > width {
				t.Errorf("usageRow(%q, w=%d) is %d cells, want at most %d: %q",
					w.Label, width, ansi.StringWidth(got), width, ansi.Strip(got))
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
	// With room to spare the block is a divider, both windows, and the reset.
	if got := len(m.usageBlock(28, 8)); got != 4 {
		t.Errorf("full block has %d rows, want 4", got)
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
// its rows would push the legend off or leave a gap.
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

func withAgents(m *Model, infos map[string]agent.Info) { m.agents = infos }

func TestStatusDotReflectsTheAgent(t *testing.T) {
	m := newTestModel()
	s := tmux.Session{Name: "api", Kind: tmux.KindClaude, Attached: 1}

	cases := map[agent.Status]string{
		agent.Waiting: "▲",
		agent.Busy:    "◐",
		agent.Shell:   "◐",
		agent.Idle:    "○",
	}
	for status, want := range cases {
		withAgents(m, map[string]agent.Info{"api": {Status: status}})
		if got, _ := m.statusDot(s); got != want {
			t.Errorf("status %q drew %q, want %q", status, got, want)
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

func TestTaskLineHiddenWhenThereIsNothingToSay(t *testing.T) {
	m := newTestModel()
	s := tmux.Session{Name: "dots", Kind: tmux.KindShell}
	if got := m.taskLine(s, false, 40); got != "" {
		t.Errorf("a session with no agent drew %q", got)
	}

	withAgents(m, map[string]agent.Info{"dots": {Status: agent.Idle}})
	if got := m.taskLine(s, false, 40); got != "" {
		t.Errorf("an agent with no task drew %q", got)
	}

	m.cfg.HideTask = true
	withAgents(m, map[string]agent.Info{"dots": {Status: agent.Busy, Task: "something"}})
	if got := m.taskLine(s, false, 40); got != "" {
		t.Errorf("hide_task still drew %q", got)
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

func TestWindowTitleMarksSessionsWaitingForInput(t *testing.T) {
	m := newTestModel()
	m.Update(sessionsMsg([]tmux.Session{
		{Name: "api", Kind: tmux.KindClaude, Managed: true},
		{Name: "web", Kind: tmux.KindCodex, Managed: true},
		{Name: "docs", Kind: tmux.KindClaude, Managed: true},
	}))

	withAgents(m, map[string]agent.Info{"api": {Status: agent.Busy}})
	if got := m.windowTitle(); got != "api (claude) — berth" {
		t.Errorf("title = %q, want no marker when nothing waits", got)
	}

	// The waiting session is not the selected one, and still marks the tab.
	withAgents(m, map[string]agent.Info{"web": {Status: agent.Waiting}})
	if got := m.windowTitle(); got != "● api (claude) — berth" {
		t.Errorf("title = %q, want a marker", got)
	}

	withAgents(m, map[string]agent.Info{
		"web":  {Status: agent.Waiting},
		"docs": {Status: agent.Waiting},
	})
	if got := m.windowTitle(); got != "●2 api (claude) — berth" {
		t.Errorf("title = %q, want a count", got)
	}
}
