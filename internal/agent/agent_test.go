package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dpws/berth/internal/tmux"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// testClaudeWatcher is the watcher with every process it hears about taken to
// be running, since the tests describe processes they have not started. The
// liveness check itself is exercised on its own below.
func testClaudeWatcher(root string) *claudeWatcher {
	w := newClaudeWatcher(root)
	w.alive = func(int) bool { return true }
	return w
}

// statusFile writes the file Claude Code keeps for a running process.
func statusFile(t *testing.T, root string, pid int, sessionID, cwd, status string, age time.Duration) {
	t.Helper()
	ms := time.Now().Add(-age).UnixMilli()
	writeFile(t, filepath.Join(root, "sessions", fmt.Sprintf("%d.json", pid)), fmt.Sprintf(
		`{"pid":%d,"sessionId":%q,"cwd":%q,"status":%q,"updatedAt":%d,"statusUpdatedAt":%d}`,
		pid, sessionID, cwd, status, ms, ms))
}

// touch sets when a transcript was last appended to, which is how berth tells
// a turn that is still going from a status file nobody is writing any more.
func touch(t *testing.T, root, sessionID string, ago time.Duration) {
	t.Helper()
	path := filepath.Join(root, "projects", "-proj", sessionID+".jsonl")
	at := time.Now().Add(-ago)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func transcriptFile(t *testing.T, root, sessionID string, lines ...string) {
	t.Helper()
	body := ""
	for _, l := range lines {
		body += l + "\n"
	}
	writeFile(t, filepath.Join(root, "projects", "-proj", sessionID+".jsonl"), body)
}

func TestClaudeStatusAndTask(t *testing.T) {
	root := t.TempDir()
	statusFile(t, root, 4242, "sess-a", "/work/api", "busy", 0)
	transcriptFile(t, root, "sess-a",
		`{"type":"ai-title","aiTitle":"Add token limits"}`,
		`{"type":"last-prompt","lastPrompt":"add a quit hotkey"}`,
		`{"type":"last-prompt","lastPrompt":"now update the tab title"}`)

	w := testClaudeWatcher(root)
	out := map[string]Info{}
	w.refresh([]tmux.Session{{Name: "api", PanePID: 4242, Dir: "/work/api"}}, out)

	got := out["api"]
	if got.Status != Busy {
		t.Errorf("Status = %q, want busy", got.Status)
	}
	// The latest prompt wins: a session moves on to new work, and the title is
	// only ever written from the opening request.
	if got.Task != "now update the tab title" {
		t.Errorf("Task = %q, want the most recent prompt", got.Task)
	}
}

func TestClaudeFallsBackToAITitle(t *testing.T) {
	root := t.TempDir()
	statusFile(t, root, 7, "sess-b", "/work/x", "idle", 0)
	transcriptFile(t, root, "sess-b", `{"type":"ai-title","aiTitle":"Rewrite the installer"}`)

	out := map[string]Info{}
	testClaudeWatcher(root).refresh([]tmux.Session{{Name: "x", PanePID: 7}}, out)

	if got := out["x"].Task; got != "Rewrite the installer" {
		t.Errorf("Task = %q, want the title when no prompt was recorded", got)
	}
}

// Claude started from a shell inside the pane is not the pane's own process,
// so the working directory is the fallback link.
func TestClaudeMatchesByDirectoryWhenPIDDiffers(t *testing.T) {
	root := t.TempDir()
	statusFile(t, root, 999, "sess-c", "/work/api", "waiting", 0)
	transcriptFile(t, root, "sess-c", `{"type":"last-prompt","lastPrompt":"ship it"}`)

	out := map[string]Info{}
	testClaudeWatcher(root).refresh(
		[]tmux.Session{{Name: "api", PanePID: 12345, Dir: "/work/api"}}, out)

	if got := out["api"].Status; got != Waiting {
		t.Errorf("Status = %q, want waiting", got)
	}
}

// A killed agent leaves its file behind saying "busy". Believing that forever
// would show a dead session as working - so the process is what decides,
// because the clock cannot: see below.
func TestClaudeIgnoresStatusFilesFromDeadProcesses(t *testing.T) {
	root := t.TempDir()
	statusFile(t, root, 4242, "sess-d", "/work/api", "busy", time.Hour)

	w := newClaudeWatcher(root)
	w.alive = func(int) bool { return false }
	out := map[string]Info{}
	w.refresh([]tmux.Session{{Name: "api", PanePID: 4242}}, out)

	if _, ok := out["api"]; ok {
		t.Errorf("a file left by a dead process was reported: %+v", out["api"])
	}
}

// Claude Code writes this file when its status changes and at no other time.
// There is no heartbeat, so a "busy" written an hour ago can be an agent an
// hour into a turn - which is exactly the session you most want the list to be
// right about. What says it is still going is the transcript, which the turn
// keeps appending to.
func TestClaudeBelievesALongTurnThatIsStillGoing(t *testing.T) {
	root := t.TempDir()
	statusFile(t, root, 4242, "sess-l", "/work/api", "busy", 72*time.Minute)
	transcriptFile(t, root, "sess-l", `{"type":"last-prompt","lastPrompt":"port the tests"}`)

	out := map[string]Info{}
	testClaudeWatcher(root).refresh([]tmux.Session{{Name: "api", PanePID: 4242}}, out)

	got, ok := out["api"]
	if !ok {
		t.Fatal("a session an hour into a turn was dropped")
	}
	if got.Status != Busy {
		t.Errorf("Status = %q, want busy", got.Status)
	}
	age, known := got.Age(time.Now())
	if !known || age < 71*time.Minute {
		t.Errorf("Age = %v (known %v), want the length of the turn so far", age, known)
	}
}

// The same file can also simply be abandoned: the process stays up and the
// session goes on being used, while that one write from hours ago sits there
// saying "busy". Believing it showed a session that had been stopped and
// cleared as working all afternoon.
func TestClaudeStopsBelievingAnAbandonedClaim(t *testing.T) {
	root := t.TempDir()
	statusFile(t, root, 4242, "sess-a", "/work/api", "busy", 13*time.Hour)
	transcriptFile(t, root, "sess-a", `{"type":"last-prompt","lastPrompt":"port the tests"}`)
	// Used since - which is what an abandoned file looks like from outside,
	// and must not be enough to resurrect the claim.
	touch(t, root, "sess-a", 2*time.Minute)

	out := map[string]Info{}
	testClaudeWatcher(root).refresh([]tmux.Session{{Name: "api", PanePID: 4242}}, out)

	got := out["api"]
	if got.Status == Busy {
		t.Error("a claim of work from thirteen hours ago was believed")
	}
	// What it was asked is still true, and is kept.
	if got.Task != "port the tests" {
		t.Errorf("Task = %q, want it kept", got.Task)
	}
}

// And in between, a turn is believed only while it is visibly still going.
func TestClaudeNeedsSignsOfLifeToStayBusy(t *testing.T) {
	root := t.TempDir()
	statusFile(t, root, 4242, "sess-q", "/work/api", "busy", 90*time.Minute)
	transcriptFile(t, root, "sess-q", `{"type":"last-prompt","lastPrompt":"port the tests"}`)
	touch(t, root, "sess-q", 80*time.Minute)

	out := map[string]Info{}
	testClaudeWatcher(root).refresh([]tmux.Session{{Name: "api", PanePID: 4242}}, out)
	if got := out["api"].Status; got == Busy {
		t.Error("a turn with nothing moving in it for over an hour was believed")
	}

	// The same turn, with the transcript still being written.
	touch(t, root, "sess-q", time.Minute)
	out = map[string]Info{}
	testClaudeWatcher(root).refresh([]tmux.Session{{Name: "api", PanePID: 4242}}, out)
	if got := out["api"].Status; got != Busy {
		t.Errorf("Status = %q, want busy while the transcript is still moving", got)
	}
}

func TestBelieveWork(t *testing.T) {
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	var never time.Time

	cases := []struct {
		name          string
		status, wrote time.Duration
		want          bool
	}{
		{"just said so", time.Minute, time.Minute, true},
		{"said so recently, no transcript found", 20 * time.Minute, 0, true},
		{"an hour in, still writing", time.Hour, time.Minute, true},
		{"an hour in, gone quiet", time.Hour, time.Hour, false},
		{"half a day, used since", 13 * time.Hour, time.Minute, false},
		{"half a day, quiet too", 13 * time.Hour, 13 * time.Hour, false},
	}
	for _, c := range cases {
		wrote := never
		if c.wrote > 0 {
			wrote = ago(c.wrote)
		}
		if got := believeWork(ago(c.status), wrote, now); got != c.want {
			t.Errorf("%s: believeWork = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestClaudeTaskFollowsNewPrompts(t *testing.T) {
	root := t.TempDir()
	statusFile(t, root, 5, "sess-e", "/w", "busy", 0)
	path := filepath.Join(root, "projects", "-proj", "sess-e.jsonl")
	transcriptFile(t, root, "sess-e", `{"type":"last-prompt","lastPrompt":"first task"}`)

	w := testClaudeWatcher(root)
	sessions := []tmux.Session{{Name: "s", PanePID: 5}}
	out := map[string]Info{}
	w.refresh(sessions, out)
	if got := out["s"].Task; got != "first task" {
		t.Fatalf("Task = %q, want %q", got, "first task")
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"last-prompt","lastPrompt":"second task"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	out = map[string]Info{}
	w.refresh(sessions, out)
	if got := out["s"].Task; got != "second task" {
		t.Errorf("Task = %q, want it to follow the new prompt", got)
	}
}

// codexEvent builds one rollout line.
func codexEvent(ts, payload string) string {
	return fmt.Sprintf(`{"timestamp":%q,"type":"event_msg","payload":%s}`, ts, payload)
}

// ago renders a rollout timestamp d in the past. Codex stamps its own lines,
// so a turn's age is read off the file rather than the clock berth is on.
func ago(d time.Duration) string {
	return time.Now().Add(-d).UTC().Format(time.RFC3339)
}

func TestCodexTracksTurnsAndPrompts(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "r.jsonl"),
		`{"type":"session_meta","payload":{"cwd":"/work/web"}}`+"\n"+
			codexEvent(ago(time.Minute), `{"type":"user_message","message":"fix the retry backoff"}`)+"\n"+
			codexEvent(ago(59*time.Second), `{"type":"task_started"}`)+"\n")

	w := newCodexWatcher(root)
	sessions := []tmux.Session{{Name: "web", Dir: "/work/web"}}
	out := map[string]Info{}
	w.refresh(sessions, out)

	if got := out["web"]; got.Status != Busy || got.Task != "fix the retry backoff" {
		t.Errorf("got %+v, want busy with the prompt as the task", got)
	}

	// The turn finishing flips it back to idle.
	writeFile(t, filepath.Join(root, "r.jsonl"),
		`{"type":"session_meta","payload":{"cwd":"/work/web"}}`+"\n"+
			codexEvent(ago(time.Minute), `{"type":"user_message","message":"fix the retry backoff"}`)+"\n"+
			codexEvent(ago(59*time.Second), `{"type":"task_started"}`)+"\n"+
			codexEvent(ago(51*time.Second), `{"type":"task_complete"}`)+"\n")

	out = map[string]Info{}
	w.refresh(sessions, out)
	if got := out["web"].Status; got != Idle {
		t.Errorf("Status = %q, want idle after task_complete", got)
	}
}

// A rollout is written to all through a turn, so the last line in it is not the
// age of anything. The age runs from where the turn started, and once the turn
// is over, from where it ended.
func TestCodexAgeRunsFromTheTurnBoundary(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "r.jsonl"),
		`{"type":"session_meta","payload":{"cwd":"/work/web"}}`+"\n"+
			codexEvent(ago(30*time.Minute), `{"type":"task_started"}`)+"\n"+
			codexEvent(ago(8*time.Minute), `{"type":"agent_message","message":"still going"}`)+"\n")

	w := newCodexWatcher(root)
	sessions := []tmux.Session{{Name: "web", Dir: "/work/web"}}
	out := map[string]Info{}
	w.refresh(sessions, out)

	age, ok := out["web"].Age(time.Now())
	if !ok || age < 29*time.Minute || age > 31*time.Minute {
		t.Errorf("busy age = %v (known %v), want the half hour since the turn began", age, ok)
	}

	writeFile(t, filepath.Join(root, "r.jsonl"),
		`{"type":"session_meta","payload":{"cwd":"/work/web"}}`+"\n"+
			codexEvent(ago(30*time.Minute), `{"type":"task_started"}`)+"\n"+
			codexEvent(ago(8*time.Minute), `{"type":"agent_message","message":"still going"}`)+"\n"+
			codexEvent(ago(5*time.Minute), `{"type":"task_complete"}`)+"\n")

	out = map[string]Info{}
	w.refresh(sessions, out)
	age, ok = out["web"].Age(time.Now())
	if !ok || age < 4*time.Minute || age > 6*time.Minute {
		t.Errorf("idle age = %v (known %v), want the time since the turn ended", age, ok)
	}
}

// A Codex killed mid-turn never writes task_complete, so its rollout says it is
// working for good. Believing that showed a new session in the same directory
// as busy with the dead one's task.
func TestCodexIgnoresATurnThatWentQuiet(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "r.jsonl"),
		`{"type":"session_meta","payload":{"cwd":"/work/web"}}`+"\n"+
			codexEvent(ago(staleAfter+time.Hour), `{"type":"user_message","message":"fix the retry backoff"}`)+"\n"+
			codexEvent(ago(staleAfter+time.Hour), `{"type":"task_started"}`)+"\n")

	out := map[string]Info{}
	newCodexWatcher(root).refresh([]tmux.Session{{Name: "web", Dir: "/work/web"}}, out)
	if got := out["web"].Status; got != Idle {
		t.Errorf("Status = %q, want idle: nothing that quiet is still running", got)
	}
}

func TestCodexIgnoresOtherDirectories(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "r.jsonl"),
		`{"type":"session_meta","payload":{"cwd":"/somewhere/else"}}`+"\n")

	out := map[string]Info{}
	newCodexWatcher(root).refresh([]tmux.Session{{Name: "web", Dir: "/work/web"}}, out)
	if _, ok := out["web"]; ok {
		t.Error("a rollout from another directory was matched")
	}
}

func TestCleanTask(t *testing.T) {
	cases := map[string]string{
		"  hello   world  ":     "hello world",
		"line one\nline two":    "line one line two",
		"tabs\there":            "tabs here",
		"bell\x07and\x1bescape": "bellandescape",
		"":                      "",
		"\n\n":                  "",
	}
	for in, want := range cases {
		if got := cleanTask(in); got != want {
			t.Errorf("cleanTask(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCount(t *testing.T) {
	infos := map[string]Info{
		"a": {Status: Busy},
		"b": {Status: Waiting},
		"c": {Status: Idle},
		"d": {Status: Waiting},
		// Running a command is work of the agent's own, so it counts as
		// working rather than as a state of its own.
		"e": {Status: Shell},
		// Nothing known: in none of the totals, so the tally never claims more
		// sessions than berth can actually speak for.
		"f": {Status: Unknown, Task: "something"},
	}
	got := Count(infos)
	want := Counts{Waiting: 2, Working: 2, Idle: 1}
	if got != want {
		t.Errorf("Count = %+v, want %+v", got, want)
	}
	if got.Total() != 5 {
		t.Errorf("Total = %d, want 5", got.Total())
	}
	if got := Count(nil); got.Total() != 0 {
		t.Errorf("Count(nil) = %+v, want nothing counted", got)
	}
}

func TestStatusPredicates(t *testing.T) {
	if !Waiting.NeedsInput() || Busy.NeedsInput() || Idle.NeedsInput() {
		t.Error("NeedsInput should be true only for waiting")
	}
	if !Busy.Active() || !Shell.Active() || Idle.Active() || Waiting.Active() {
		t.Error("Active should be true only while the agent is doing work")
	}
}

// Codex leaves rollouts that never receive a prompt, and writes to them. A
// directory can hold a dozen; picking the most recent made the task under a
// session flicker away and back as an empty rollout took its turn at being
// newest.
func TestCodexPrefersTheRolloutWithAPrompt(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "real.jsonl"),
		`{"type":"session_meta","payload":{"cwd":"/work"}}`+"\n"+
			codexEvent("2026-08-03T10:00:00Z", `{"type":"user_message","message":"the actual task"}`)+"\n")
	// Written later, but with nothing to say.
	empty := filepath.Join(root, "empty.jsonl")
	writeFile(t, empty, `{"type":"session_meta","payload":{"cwd":"/work"}}`+"\n"+
		codexEvent("2026-08-03T11:00:00Z", `{"type":"token_count"}`)+"\n")
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(empty, later, later); err != nil {
		t.Fatal(err)
	}

	out := map[string]Info{}
	newCodexWatcher(root).refresh([]tmux.Session{{Name: "web", Dir: "/work"}}, out)

	if got := out["web"].Task; got != "the actual task" {
		t.Errorf("Task = %q, want the rollout that has one", got)
	}
}

// Between two rollouts that both have a prompt, the newer one wins - that is
// the session you are actually looking at.
func TestCodexPrefersTheNewerOfTwoRealRollouts(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "old.jsonl"),
		`{"type":"session_meta","payload":{"cwd":"/work"}}`+"\n"+
			codexEvent("2026-08-03T09:00:00Z", `{"type":"user_message","message":"yesterday"}`)+"\n")
	writeFile(t, filepath.Join(root, "new.jsonl"),
		`{"type":"session_meta","payload":{"cwd":"/work"}}`+"\n"+
			codexEvent("2026-08-03T12:00:00Z", `{"type":"user_message","message":"today"}`)+"\n")

	out := map[string]Info{}
	newCodexWatcher(root).refresh([]tmux.Session{{Name: "web", Dir: "/work"}}, out)

	if got := out["web"].Task; got != "today" {
		t.Errorf("Task = %q, want the newer prompt", got)
	}
}

// A session can move to Claude Code's background host, and then the process in
// the pane is a client: it stops writing its own status while the session it is
// showing carries on being written by the host, in the same directory. Reading
// the pane's own record then means reading a file nobody is keeping up.
func TestClaudePrefersTheRecordBeingKeptUp(t *testing.T) {
	root := t.TempDir()
	// The pane's own process, quiet since yesterday.
	statusFile(t, root, 4242, "sess-old", "/work/api", "busy", 13*time.Hour)
	transcriptFile(t, root, "sess-old", `{"type":"last-prompt","lastPrompt":"the old one"}`)
	touch(t, root, "sess-old", 13*time.Hour)
	// The host, writing now.
	statusFile(t, root, 5353, "sess-live", "/work/api", "waiting", time.Minute)
	transcriptFile(t, root, "sess-live", `{"type":"last-prompt","lastPrompt":"the live one"}`)

	out := map[string]Info{}
	testClaudeWatcher(root).refresh(
		[]tmux.Session{{Name: "api", PanePID: 4242, Dir: "/work/api"}}, out)

	got := out["api"]
	if got.Status != Waiting {
		t.Errorf("Status = %q, want the live record's - the pane's own is a day old", got.Status)
	}
	if got.Task != "the live one" {
		t.Errorf("Task = %q, want the live record's", got.Task)
	}
}

// But only when the pane's own record has actually gone quiet. Two agents in
// one directory are ordinary, and the one in the pane is the one being asked
// about - it must not lose its own status to a neighbour that wrote last.
func TestClaudeKeepsThePanesOwnRecordWhileItIsFresh(t *testing.T) {
	root := t.TempDir()
	statusFile(t, root, 4242, "sess-mine", "/work/api", "busy", 2*time.Minute)
	transcriptFile(t, root, "sess-mine", `{"type":"last-prompt","lastPrompt":"mine"}`)
	statusFile(t, root, 5353, "sess-other", "/work/api", "idle", 0)
	transcriptFile(t, root, "sess-other", `{"type":"last-prompt","lastPrompt":"someone else"}`)

	out := map[string]Info{}
	testClaudeWatcher(root).refresh(
		[]tmux.Session{{Name: "api", PanePID: 4242, Dir: "/work/api"}}, out)

	if got := out["api"].Task; got != "mine" {
		t.Errorf("Task = %q, want the pane's own while it is being written", got)
	}
	if got := out["api"].Status; got != Busy {
		t.Errorf("Status = %q, want busy", got)
	}
}
